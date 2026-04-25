package daemon

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Outcome values for a log entry. Kept as a closed set so log greps stay
// stable.
const (
	OutcomeStarted        = "started"
	OutcomeStopped        = "stopped"
	OutcomeDelivered      = "delivered"
	OutcomeReplaced       = "replaced"
	OutcomeTimeout        = "timeout"
	OutcomePaneMissing    = "pane_missing"
	OutcomeSanitizeReject = "sanitize_reject"
	OutcomeOversize       = "oversize"
	OutcomeDeliveryError  = "delivery_error"
	OutcomeIPCError       = "ipc_error"
	OutcomeWarning        = "warning"
)

// Entry is the metadata-only payload accepted by Logger. The struct
// deliberately has no Body field — sanitizer rejections record class and
// offset in Msg, never raw content (docs/commands/daemon.md "Append-only
// log").
type Entry struct {
	JobID    string
	Pane     string
	Source   string
	PromptID string
	Outcome  string
	Msg      string
}

// Logger writes append-only one-line logfmt entries to a file shared across
// goroutines. Concurrency is mutex-guarded so entries never interleave
// within the daemon process.
//
// The log file is opened lazily on the first Log call so that a failed
// Listen (contended socket, permissions error) doesn't leave an empty log
// file behind. NewLogger only validates the path and mkdir-p's the parent
// directory.
//
// If open or write fails (disk full, broken file handle, etc.) the logger
// emits a single notice to stderr the first time and then swallows further
// errors so the daemon keeps running. The notice makes silent log loss
// visible to whoever is watching the daemon's terminal.
type Logger struct {
	mu       sync.Mutex
	path     string
	w        io.Writer
	closer   io.Closer
	now      func() time.Time
	stderr   io.Writer
	notified bool
}

// NewLogger validates the path and creates the parent directory at 0700.
// The log file itself is created on the first Log call (0600, per-user
// only). Caller is responsible for Close.
func NewLogger(path string) (*Logger, error) {
	if path == "" {
		return nil, errors.New("daemon: log path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("daemon: create log dir: %w", err)
	}
	return &Logger{path: path, now: time.Now, stderr: os.Stderr}, nil
}

// NewLoggerWriter wraps an arbitrary writer, used by tests. The caller owns
// the writer; Logger.Close is a no-op in this mode. No stderr fallback is
// wired — tests that want to observe the "log write failed" notice
// construct a Logger literal directly with a stderr writer.
func NewLoggerWriter(w io.Writer) *Logger {
	return &Logger{w: w, now: time.Now}
}

// Close releases the underlying file if one was opened. Safe to call on a
// NewLoggerWriter instance or on a file-backed logger that never wrote.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closer == nil {
		return nil
	}
	return l.closer.Close()
}

// ensureOpen opens the log file on first use. Must be called with mu held.
func (l *Logger) ensureOpen() error {
	if l.w != nil {
		return nil
	}
	// #nosec G304 -- path is from per-user config; daemon and log are scoped to one user.
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("daemon: open log: %w", err)
	}
	l.w = f
	l.closer = f
	return nil
}

// Log writes one entry. Empty fields (other than time) are omitted so
// lifecycle events like "daemon started" don't carry empty job_id/pane keys.
// Returns the underlying open or write error, if any — callers typically
// ignore it since the daemon must keep running on a wedged log file.
func (l *Logger) Log(e Entry) error {
	line := formatLine(l.now(), e)
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.ensureOpen(); err != nil {
		l.notifyOnce("open", err)
		return err
	}
	_, err := l.w.Write([]byte(line))
	if err != nil {
		l.notifyOnce("write", err)
	}
	return err
}

// notifyOnce emits a single stderr notice for the first failure and
// suppresses subsequent ones. Must be called with mu held.
func (l *Logger) notifyOnce(op string, err error) {
	if l.notified || l.stderr == nil {
		return
	}
	_, _ = fmt.Fprintf(l.stderr,
		"tprompt: daemon log %s failed: %v (further errors suppressed)\n", op, err)
	l.notified = true
}

func formatLine(t time.Time, e Entry) string {
	var b strings.Builder
	b.WriteString("time=")
	b.WriteString(t.UTC().Format(time.RFC3339Nano))
	appendField(&b, "job_id", e.JobID)
	appendField(&b, "pane", e.Pane)
	appendField(&b, "source", e.Source)
	appendField(&b, "prompt_id", e.PromptID)
	appendField(&b, "outcome", e.Outcome)
	appendField(&b, "msg", e.Msg)
	b.WriteByte('\n')
	return b.String()
}

func appendField(b *strings.Builder, key, value string) {
	if value == "" {
		return
	}
	b.WriteByte(' ')
	b.WriteString(key)
	b.WriteByte('=')
	b.WriteString(quoteIfNeeded(value))
}

func quoteIfNeeded(v string) string {
	needs := false
	for _, r := range v {
		if r == ' ' || r == '"' || r == '=' || r == '\\' || r < 0x20 {
			needs = true
			break
		}
	}
	if !needs {
		return v
	}
	var b strings.Builder
	b.Grow(len(v) + 2)
	b.WriteByte('"')
	for _, r := range v {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
