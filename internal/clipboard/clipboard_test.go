package clipboard

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// --- Detect / NewAutoDetect ---

func fakeEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func fakeLookPath(available ...string) func(string) (string, error) {
	set := make(map[string]struct{}, len(available))
	for _, a := range available {
		set[a] = struct{}{}
	}
	return func(name string) (string, error) {
		if _, ok := set[name]; ok {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
}

func TestDetect_WaylandBeatsX11(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Detect on darwin short-circuits to pbpaste")
	}
	argv, reason, ok := Detect(
		fakeEnv(map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}),
		fakeLookPath("wl-paste", "xclip", "xsel"),
	)
	if !ok || reason != "Wayland" {
		t.Fatalf("want Wayland ok, got ok=%v reason=%q", ok, reason)
	}
	want := []string{"wl-paste", "-n"}
	if diff := cmp.Diff(want, argv); diff != "" {
		t.Fatalf("argv mismatch (-want +got):\n%s", diff)
	}
}

func TestDetect_WaylandFallsThroughWhenWlPasteMissing(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Detect on darwin short-circuits to pbpaste")
	}
	// WAYLAND_DISPLAY is set but wl-paste is not installed. Detect should not
	// claim wl-paste; it should fall through to X11 if $DISPLAY is present, or
	// report no reader otherwise.
	argv, reason, ok := Detect(
		fakeEnv(map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}),
		fakeLookPath("xclip"),
	)
	if !ok || reason != "X11" {
		t.Fatalf("want X11 ok, got ok=%v reason=%q argv=%v", ok, reason, argv)
	}
	if argv[0] != "xclip" {
		t.Fatalf("argv[0] = %q, want xclip", argv[0])
	}
}

func TestDetect_WaylandNoToolInstalled(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Detect on darwin short-circuits to pbpaste")
	}
	// WAYLAND_DISPLAY set, wl-paste missing, no X11 fallback → no reader.
	_, _, ok := Detect(
		fakeEnv(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}),
		fakeLookPath(),
	)
	if ok {
		t.Fatal("expected no reader when wl-paste is not on $PATH")
	}
}

func TestDetect_X11PrefersXclip(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Detect on darwin short-circuits to pbpaste")
	}
	argv, reason, ok := Detect(
		fakeEnv(map[string]string{"DISPLAY": ":0"}),
		fakeLookPath("xclip", "xsel"),
	)
	if !ok || reason != "X11" {
		t.Fatalf("want X11 ok, got ok=%v reason=%q", ok, reason)
	}
	want := []string{"xclip", "-selection", "clipboard", "-o"}
	if diff := cmp.Diff(want, argv); diff != "" {
		t.Fatalf("argv mismatch (-want +got):\n%s", diff)
	}
}

func TestDetect_X11FallsBackToXsel(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Detect on darwin short-circuits to pbpaste")
	}
	argv, reason, ok := Detect(
		fakeEnv(map[string]string{"DISPLAY": ":0"}),
		fakeLookPath("xsel"),
	)
	if !ok || reason != "X11" {
		t.Fatalf("want X11 ok, got ok=%v reason=%q", ok, reason)
	}
	want := []string{"xsel", "-b", "-o"}
	if diff := cmp.Diff(want, argv); diff != "" {
		t.Fatalf("argv mismatch (-want +got):\n%s", diff)
	}
}

func TestDetect_NoReader(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Detect on darwin always finds pbpaste")
	}
	_, _, ok := Detect(fakeEnv(nil), fakeLookPath())
	if ok {
		t.Fatal("expected no reader")
	}
}

func TestDetect_X11NoToolInstalled(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Detect on darwin short-circuits to pbpaste")
	}
	_, _, ok := Detect(
		fakeEnv(map[string]string{"DISPLAY": ":0"}),
		fakeLookPath(),
	)
	if ok {
		t.Fatal("expected no reader when $DISPLAY set but no tool installed")
	}
}

func TestNewAutoDetect_ReturnsInstallHintWhenNothingAvailable(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("NewAutoDetect on darwin always finds pbpaste")
	}
	r, err := NewAutoDetect(fakeEnv(nil), fakeLookPath())
	if r != nil {
		t.Fatalf("want nil reader, got %v", r)
	}
	if !errors.Is(err, ErrNoReaderAvailable) {
		t.Fatalf("want ErrNoReaderAvailable, got %v", err)
	}
	for _, hint := range []string{"pbpaste", "wl-paste", "xclip", "xsel", "clipboard_read_command"} {
		if !strings.Contains(err.Error(), hint) {
			t.Errorf("install hint missing %q", hint)
		}
	}
}

// --- NewCommand ---

func TestNewCommand_CapturesStdout(t *testing.T) {
	r := NewCommand([]string{"sh", "-c", "printf hello"})
	got, err := r.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}

func TestNewCommand_NonZeroSurfacesStderr(t *testing.T) {
	r := NewCommand([]string{"sh", "-c", "printf boom >&2; exit 7"})
	_, err := r.Read()
	if err == nil {
		t.Fatal("expected error")
	}
	var readErr *ReadError
	if !errors.As(err, &readErr) {
		t.Fatalf("want ReadError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error missing stderr text: %v", err)
	}
	if !strings.Contains(err.Error(), "sh") {
		t.Errorf("error missing argv[0]: %v", err)
	}
}

func TestNewCommand_EmptyArgv(t *testing.T) {
	r := NewCommand(nil)
	if _, err := r.Read(); err == nil {
		t.Fatal("expected error for empty argv")
	}
}

// --- Validate ---

func TestValidate(t *testing.T) {
	type wantKind int
	const (
		wantOK wantKind = iota
		wantEmpty
		wantInvalidUTF8
		wantOversize
	)
	cases := []struct {
		name    string
		content []byte
		cap     int64
		kind    wantKind
		sub     string
	}{
		{"happy", []byte("hello"), 1024, wantOK, ""},
		{"empty", []byte{}, 1024, wantEmpty, "clipboard is empty"},
		{"nil", nil, 1024, wantEmpty, "clipboard is empty"},
		{"invalid utf-8", []byte{0xff, 0xfe}, 1024, wantInvalidUTF8, "not valid UTF-8"},
		{"at cap", []byte("abcde"), 5, wantOK, ""},
		{"over cap", []byte("abcdef"), 5, wantOversize, "exceeds max_paste_bytes (6 > 5)"},
		{"unlimited cap", []byte("huge"), 0, wantOK, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.content, tc.cap)
			if tc.kind == wantOK {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.sub)
			}
			if !strings.Contains(err.Error(), tc.sub) {
				t.Fatalf("error = %q, want substring %q", err, tc.sub)
			}
			switch tc.kind {
			case wantEmpty:
				var target *EmptyClipboardError
				if !errors.As(err, &target) {
					t.Fatalf("want *EmptyClipboardError, got %T", err)
				}
			case wantInvalidUTF8:
				var target *InvalidUTF8Error
				if !errors.As(err, &target) {
					t.Fatalf("want *InvalidUTF8Error, got %T", err)
				}
			case wantOversize:
				var target *OversizeError
				if !errors.As(err, &target) {
					t.Fatalf("want *OversizeError, got %T", err)
				}
				if target.Bytes != len(tc.content) || target.Limit != tc.cap {
					t.Fatalf("OversizeError = {Bytes:%d Limit:%d}, want {Bytes:%d Limit:%d}",
						target.Bytes, target.Limit, len(tc.content), tc.cap)
				}
			}
		})
	}
}

func TestStaticReaderReturnsContent(t *testing.T) {
	r := NewStatic([]byte("hello"))
	got, err := r.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if diff := cmp.Diff([]byte("hello"), got); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}
