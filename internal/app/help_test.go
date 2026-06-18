package app

import (
	"strings"
	"testing"
)

func TestRootLongDescribesWorkflows(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	long := root.Long
	if long == "" {
		t.Fatal("root.Long is empty")
	}
	for _, want := range []string{"new", "send", "paste", "pick", "tui"} {
		if !strings.Contains(long, want) {
			t.Errorf("root.Long missing workflow %q\n--- Long ---\n%s", want, long)
		}
	}
	if !strings.Contains(strings.ToLower(long), "handoff") {
		t.Errorf("root.Long should describe tui as handoff-backed\n%s", long)
	}
}

func TestRootLongAvoidsUnsupportedBehavior(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	assertLongAvoids(t, "tprompt", root.Long, []string{
		"remote",   // local-only delivery
		"modifier", // single-char keybinds only
		"preview",  // no live clipboard preview
	})
}

func TestSubcommandHelpText(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))

	cases := []struct {
		path   []string
		want   []string
		banned []string
	}{
		{
			path:   []string{"send"},
			want:   []string{"synchronous", "does not use handoff"},
			banned: []string{"remote"},
		},
		{
			path:   []string{"paste"},
			want:   []string{"clipboard", "same-host", "synchronous"},
			banned: []string{"preview", "remote"},
		},
		{
			path: []string{"pick"},
			want: []string{"external picker", "prints the selected id", "does not deliver"},
		},
		{
			path:   []string{"new"},
			want:   []string{"scaffold", "global prompts", "--project", "absolute path", "refuses to overwrite"},
			banned: nil,
		},
		{
			path:   []string{"tui"},
			want:   []string{"handoff", "--target-pane"},
			banned: nil,
		},
	}

	for _, tc := range cases {
		t.Run(strings.Join(tc.path, "_"), func(t *testing.T) {
			cmd, _, err := root.Find(tc.path)
			if err != nil {
				t.Fatalf("find %v: %v", tc.path, err)
			}
			if cmd == nil {
				t.Fatalf("nil cmd for %v", tc.path)
			}
			if cmd.Long == "" {
				t.Fatalf("%s.Long is empty", strings.Join(tc.path, " "))
			}
			lowered := strings.ToLower(cmd.Long)
			for _, w := range tc.want {
				if !strings.Contains(lowered, strings.ToLower(w)) {
					t.Errorf("%s.Long missing %q\n--- Long ---\n%s",
						strings.Join(tc.path, " "), w, cmd.Long)
				}
			}
			assertLongAvoids(t, strings.Join(tc.path, " "), cmd.Long, tc.banned)
		})
	}
}

func TestTUITargetPaneFlagDocumentsDirectMode(t *testing.T) {
	// --target-pane is no longer a hard-required flag (AUR-446): omitting it
	// enters direct mode against the current pane. The usage text should say so
	// rather than advertise "required".
	root := NewRootCmd(fakeDeps(t))
	cmd, _, err := root.Find([]string{"tui"})
	if err != nil {
		t.Fatalf("find tui: %v", err)
	}
	flag := cmd.Flag("target-pane")
	if flag == nil {
		t.Fatal("--target-pane flag missing")
	}
	if !strings.Contains(strings.ToLower(flag.Usage), "current pane") {
		t.Errorf("--target-pane usage should describe the direct-mode fallback, got %q", flag.Usage)
	}
}

func assertLongAvoids(t *testing.T, name, long string, banned []string) {
	t.Helper()
	lowered := strings.ToLower(long)
	for _, b := range banned {
		if b == "" {
			continue
		}
		if strings.Contains(lowered, strings.ToLower(b)) {
			t.Errorf("%s.Long should not mention %q\n--- Long ---\n%s", name, b, long)
		}
	}
}
