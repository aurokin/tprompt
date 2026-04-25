package daemon

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFormatLineFullEntry(t *testing.T) {
	ts := time.Date(2026, 4, 16, 12, 30, 45, 0, time.UTC)
	got := formatLine(ts, Entry{
		JobID:    "j-1",
		Pane:     "%5",
		Source:   SourcePrompt,
		PromptID: "code-review",
		Outcome:  OutcomeDelivered,
		Msg:      "delivery succeeded",
	})
	want := `time=2026-04-16T12:30:45Z job_id=j-1 pane=%5 source=prompt prompt_id=code-review outcome=delivered msg="delivery succeeded"` + "\n"
	if got != want {
		t.Fatalf("formatLine mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestFormatLineOmitsEmptyFields(t *testing.T) {
	ts := time.Date(2026, 4, 16, 12, 30, 45, 0, time.UTC)
	got := formatLine(ts, Entry{
		Outcome: OutcomeStarted,
		Msg:     "pid=12345 socket=/tmp/x",
	})
	want := `time=2026-04-16T12:30:45Z outcome=started msg="pid=12345 socket=/tmp/x"` + "\n"
	if got != want {
		t.Fatalf("formatLine mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestFormatLineQuotesAndEscapes(t *testing.T) {
	ts := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	got := formatLine(ts, Entry{
		JobID:   "j-1",
		Outcome: OutcomeSanitizeReject,
		Msg:     `escape "OSC" at byte 12` + "\n" + "trailing",
	})
	if !strings.Contains(got, `msg="escape \"OSC\" at byte 12\ntrailing"`) {
		t.Fatalf("expected escaped msg, got: %q", got)
	}
}

func TestFormatLineUTCsLocalTime(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	ts := time.Date(2026, 4, 16, 20, 0, 0, 0, loc) // == 12:00 UTC
	got := formatLine(ts, Entry{Outcome: OutcomeStarted})
	if !strings.HasPrefix(got, "time=2026-04-16T12:00:00Z ") {
		t.Fatalf("expected UTC normalization, got: %q", got)
	}
}

func TestNewLoggerDefersFileCreationUntilFirstLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "daemon.log")

	logger, err := NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	// Parent dir is created eagerly — the lazy part is only the file.
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("parent dir should exist after NewLogger: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("log file should not exist before first Log, stat err = %v", err)
	}

	if err := logger.Log(Entry{Outcome: OutcomeStarted, Msg: "hi"}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("log file should exist after first Log: %v", err)
	}
}

func TestLoggerCloseIsNoopWhenFileNeverOpened(t *testing.T) {
	logger, err := NewLogger(filepath.Join(t.TempDir(), "daemon.log"))
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("Close on unused logger should be a no-op, got %v", err)
	}
}

func TestNewLoggerCreatesParentDirAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "subdir", "daemon.log")

	logger, err := NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer func() { _ = logger.Close() }()

	if err := logger.Log(Entry{Outcome: OutcomeStarted, Msg: "hi"}); err != nil {
		t.Fatalf("Log: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file perms = %o, want 0600", info.Mode().Perm())
	}

	parentInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat parent: %v", err)
	}
	if parentInfo.Mode().Perm() != 0o700 {
		t.Fatalf("parent perms = %o, want 0700", parentInfo.Mode().Perm())
	}
}

func TestLoggerAppendsAcrossOpens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.log")

	first, err := NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	if err := first.Log(Entry{Outcome: OutcomeStarted, Msg: "first"}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger reopen: %v", err)
	}
	if err := second.Log(Entry{Outcome: OutcomeStopped, Msg: "second"}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	got := string(contents)
	if !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Fatalf("expected both entries, got: %q", got)
	}
	if strings.Count(got, "\n") != 2 {
		t.Fatalf("expected 2 lines, got: %q", got)
	}
}

func TestLoggerConcurrentWritesNeverInterleave(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWriter(&buf)

	const writers = 8
	const perWriter = 50
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				_ = logger.Log(Entry{
					JobID:   "j-x",
					Outcome: OutcomeDelivered,
					Msg:     "writer says hello world",
				})
			}
		}(w)
	}
	wg.Wait()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != writers*perWriter {
		t.Fatalf("expected %d lines, got %d", writers*perWriter, len(lines))
	}
	for i, line := range lines {
		if !strings.HasPrefix(line, "time=") {
			t.Fatalf("line %d does not start with time=: %q", i, line)
		}
		if !strings.Contains(line, `msg="writer says hello world"`) {
			t.Fatalf("line %d missing full msg (interleaved write?): %q", i, line)
		}
	}
}

func TestNewLoggerRejectsEmptyPath(t *testing.T) {
	if _, err := NewLogger(""); err == nil {
		t.Fatal("want error, got nil")
	}
}

type failingWriter struct {
	err   error
	calls int
}

func (f *failingWriter) Write(p []byte) (int, error) {
	f.calls++
	return 0, f.err
}

func TestLoggerStderrFallbackOnWriteFailure(t *testing.T) {
	writeErr := errors.New("disk full")
	fw := &failingWriter{err: writeErr}
	var stderr bytes.Buffer
	logger := &Logger{w: fw, now: time.Now, stderr: &stderr}

	for i := 0; i < 3; i++ {
		if err := logger.Log(Entry{Outcome: OutcomeStarted, Msg: "hi"}); !errors.Is(err, writeErr) {
			t.Fatalf("Log returned %v, want %v", err, writeErr)
		}
	}

	if fw.calls != 3 {
		t.Fatalf("expected 3 write attempts (daemon keeps trying), got %d", fw.calls)
	}

	out := stderr.String()
	if !strings.Contains(out, "tprompt: daemon log write failed: disk full") {
		t.Fatalf("expected stderr notice, got: %q", out)
	}
	if !strings.Contains(out, "further errors suppressed") {
		t.Fatalf("expected suppression hint, got: %q", out)
	}
	if got := strings.Count(out, "\n"); got != 1 {
		t.Fatalf("expected exactly one notice line, got %d: %q", got, out)
	}
}

func TestLoggerStderrFallbackOnOpenFailure(t *testing.T) {
	// Path under a directory that doesn't exist, so lazy OpenFile fails
	// every call.
	path := filepath.Join(t.TempDir(), "missing-dir", "daemon.log")
	var stderr bytes.Buffer
	logger := &Logger{path: path, now: time.Now, stderr: &stderr}

	for i := 0; i < 3; i++ {
		if err := logger.Log(Entry{Outcome: OutcomeStarted, Msg: "hi"}); err == nil {
			t.Fatalf("Log: want error, got nil")
		}
	}

	out := stderr.String()
	if !strings.Contains(out, "tprompt: daemon log open failed") {
		t.Fatalf("expected open-failure stderr notice, got: %q", out)
	}
	if !strings.Contains(out, "further errors suppressed") {
		t.Fatalf("expected suppression hint, got: %q", out)
	}
	if got := strings.Count(out, "\n"); got != 1 {
		t.Fatalf("expected exactly one notice line, got %d: %q", got, out)
	}
}

func TestNewLoggerWriterHasNoStderrFallback(t *testing.T) {
	// NewLoggerWriter is the test helper; it must not grab os.Stderr.
	logger := NewLoggerWriter(&failingWriter{err: errors.New("nope")})
	if logger.stderr != nil {
		t.Fatalf("NewLoggerWriter should leave stderr nil, got %T", logger.stderr)
	}
	_ = logger.Log(Entry{Outcome: OutcomeStarted}) // must not panic or print
}
