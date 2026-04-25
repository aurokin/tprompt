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
	for _, want := range []string{"send", "paste", "pick", "tui", "daemon"} {
		if !strings.Contains(long, want) {
			t.Errorf("root.Long missing workflow %q\n--- Long ---\n%s", want, long)
		}
	}
	if !strings.Contains(strings.ToLower(long), "daemon-backed") {
		t.Errorf("root.Long should describe tui as daemon-backed\n%s", long)
	}
}

func TestRootLongAvoidsUnsupportedBehavior(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	assertLongAvoids(t, "tprompt", root.Long, []string{
		"templat",  // no prompt templating
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
			want:   []string{"synchronous", "does not use the daemon"},
			banned: []string{"templat", "remote"},
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
			path:   []string{"tui"},
			want:   []string{"daemon", "--target-pane", "--daemon-auto-start"},
			banned: []string{"templat"},
		},
		{
			path: []string{"daemon"},
			want: []string{"start", "status", "stop"},
		},
		{
			path: []string{"daemon", "start"},
			want: []string{"foreground"},
		},
		{
			path: []string{"daemon", "status"},
			want: []string{"read-only"},
		},
		{
			path: []string{"daemon", "stop"},
			want: []string{"graceful", "daemon not running"},
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

func TestTUIRequiredTargetPaneFlagSurfaced(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	cmd, _, err := root.Find([]string{"tui"})
	if err != nil {
		t.Fatalf("find tui: %v", err)
	}
	flag := cmd.Flag("target-pane")
	if flag == nil {
		t.Fatal("--target-pane flag missing")
	}
	if !strings.Contains(strings.ToLower(flag.Usage), "required") {
		t.Errorf("--target-pane usage should advertise required, got %q", flag.Usage)
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
