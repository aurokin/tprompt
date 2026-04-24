package picker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPickerInterfaceShape(t *testing.T) {
	var _ Picker = stubPicker{}
}

type stubPicker struct{}

func (stubPicker) Select([]string) (string, bool, error) { return "", true, nil }

func TestCommandSelectHappyPath(t *testing.T) {
	path := writeScript(t, "pick.sh", `#!/bin/sh
cat >/tmp/tprompt-picker-stdin
printf 'beta\n'
`)
	p := NewCommand([]string{path})

	got, cancelled, err := p.Select([]string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if cancelled {
		t.Fatal("cancelled = true, want false")
	}
	if got != "beta" {
		t.Fatalf("selected = %q, want beta", got)
	}
}

func TestCommandSelectCancelExitCodes(t *testing.T) {
	for _, code := range []string{"1", "130"} {
		t.Run(code, func(t *testing.T) {
			path := writeScript(t, "cancel.sh", "#!/bin/sh\nexit "+code+"\n")
			_, cancelled, err := NewCommand([]string{path}).Select([]string{"alpha"})
			if err != nil {
				t.Fatalf("Select: %v", err)
			}
			if !cancelled {
				t.Fatal("cancelled = false, want true")
			}
		})
	}
}

func TestCommandSelectFailureSurfacesStderr(t *testing.T) {
	path := writeScript(t, "fail.sh", `#!/bin/sh
printf 'no picker here\n' >&2
exit 2
`)
	_, cancelled, err := NewCommand([]string{path}).Select([]string{"alpha"})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if cancelled {
		t.Fatal("cancelled = true, want false")
	}
	if !strings.Contains(err.Error(), "no picker here") {
		t.Fatalf("error = %q, want stderr", err)
	}
}

func TestCommandSelectRejectsUnknownID(t *testing.T) {
	path := writeScript(t, "unknown.sh", "#!/bin/sh\nprintf 'gamma\\n'\n")
	_, _, err := NewCommand([]string{path}).Select([]string{"alpha", "beta"})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown prompt ID") {
		t.Fatalf("error = %q", err)
	}
}

func TestCommandSelectRejectsMultipleLines(t *testing.T) {
	path := writeScript(t, "multi.sh", "#!/bin/sh\nprintf 'alpha\\nbeta\\n'\n")
	_, _, err := NewCommand([]string{path}).Select([]string{"alpha", "beta"})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "multiple lines") {
		t.Fatalf("error = %q", err)
	}
}

func TestCommandSelectEmptyOutputIsCancel(t *testing.T) {
	path := writeScript(t, "empty.sh", "#!/bin/sh\nexit 0\n")
	_, cancelled, err := NewCommand([]string{path}).Select([]string{"alpha"})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if !cancelled {
		t.Fatal("cancelled = false, want true")
	}
}

func TestParseSelectionTrimsWhitespace(t *testing.T) {
	got, err := parseSelection("  alpha\n", []string{"alpha"})
	if err != nil {
		t.Fatalf("parseSelection: %v", err)
	}
	if got != "alpha" {
		t.Fatalf("selection = %q, want alpha", got)
	}
}

func writeScript(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
