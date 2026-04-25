package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
	"github.com/hsadler/tprompt/internal/picker"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/submitter"
	"github.com/hsadler/tprompt/internal/tmux"
	"github.com/hsadler/tprompt/internal/tui"
)

func TestZeroArgCommandsRejectExtraOperands(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "list", args: []string{"list", "extra"}},
		{name: "paste", args: []string{"paste", "extra"}},
		{name: "doctor", args: []string{"doctor", "extra"}},
		{name: "tui", args: []string{"tui", "--target-pane", "%0", "extra"}},
		{name: "pick", args: []string{"pick", "extra"}},
		{name: "daemon start", args: []string{"daemon", "start", "extra"}},
		{name: "daemon status", args: []string{"daemon", "status", "extra"}},
		{name: "daemon stop", args: []string{"daemon", "stop", "extra"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := executeRoot(t, tt.args...)
			if err == nil {
				t.Fatal("want usage error, got nil")
			}
			if errors.Is(err, ErrNotImplemented) {
				t.Fatalf("want args validation error, got handler error %v", err)
			}
			if !strings.Contains(err.Error(), "unknown command") {
				t.Fatalf("want cobra usage error, got %v", err)
			}
		})
	}
}

func TestZeroArgCommandsAcceptBareInvocation(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "pick", args: []string{"pick"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := &fakeStore{summaries: []store.Summary{}}
			deps := workingDeps(t, fs)
			deps.NewPicker = func(config.Resolved) (picker.Picker, error) {
				return &fakePicker{cancelled: true}, nil
			}
			_, _, err := executeRootWith(t, deps, tt.args...)
			if err != nil {
				t.Fatalf("want nil, got %v", err)
			}
		})
	}
}

func executeRoot(t *testing.T, args ...string) (stdout string, stderr string, err error) {
	t.Helper()

	root := NewRootCmd(fakeDeps(t))
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)

	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

func executeRootWith(t *testing.T, deps Deps, args ...string) (stdout string, stderr string, err error) {
	t.Helper()

	var outBuf, errBuf bytes.Buffer
	deps.Stdout = &outBuf
	deps.Stderr = &errBuf

	root := NewRootCmd(deps)
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)

	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

type fakeStore struct {
	summaries   []store.Summary
	prompts     map[string]store.Prompt
	discoverErr error
}

type fakePicker struct {
	selected  string
	cancelled bool
	err       error
	gotIDs    []string
}

func (f *fakePicker) Select(ids []string) (string, bool, error) {
	f.gotIDs = append([]string(nil), ids...)
	return f.selected, f.cancelled, f.err
}

func (f *fakeStore) Discover() error { return f.discoverErr }

func (f *fakeStore) List() ([]store.Summary, error) {
	if f.discoverErr != nil {
		return nil, f.discoverErr
	}
	return f.summaries, nil
}

func (f *fakeStore) Resolve(id string) (store.Prompt, error) {
	if f.discoverErr != nil {
		return store.Prompt{}, f.discoverErr
	}
	p, ok := f.prompts[id]
	if !ok {
		return store.Prompt{}, &store.NotFoundError{ID: id}
	}
	return p, nil
}

func workingDeps(t *testing.T, fs *fakeStore) Deps {
	t.Helper()
	configPath := ""
	return Deps{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		Stdin:      &bytes.Buffer{},
		Env:        func(string) string { return "" },
		ConfigPath: &configPath,
		LoadConfig: func(string) (config.Resolved, error) {
			return config.Resolved{
				PromptsDir: "/test/prompts",
				PickerArgv: []string{"fzf"},
			}, nil
		},
		LoadPasteConfig: func(string) (config.Resolved, error) {
			return config.Resolved{
				PromptsDir:    "/test/prompts",
				DefaultMode:   "paste",
				Sanitize:      "off",
				MaxPasteBytes: 1 << 20,
			}, nil
		},
		LoadDaemonConfig: func(string) (config.Resolved, error) {
			return config.Resolved{
				SocketPath:    "/tmp/tprompt-test.sock",
				LogPath:       "/tmp/tprompt-test.log",
				MaxPasteBytes: 1 << 20,
			}, nil
		},
		NewStore: func(config.Resolved) (store.Store, error) {
			return fs, nil
		},
		NewTmux: func() (tmux.Adapter, error) {
			return nil, ErrNotImplemented
		},
		NewClip: func(config.Resolved) (clipboard.Reader, error) {
			return nil, ErrNotImplemented
		},
		NewPicker: func(config.Resolved) (picker.Picker, error) {
			return nil, ErrNotImplemented
		},
		NewDaemonClient: func(config.Resolved) (daemon.Client, error) {
			return nil, ErrNotImplemented
		},
		StartDaemon: func(config.Resolved, string) error {
			return ErrNotImplemented
		},
		NewRenderer: func(config.Resolved, store.Store, submitter.Submitter) (tui.Renderer, error) {
			return cancelRenderer{}, nil
		},
		NewSubmitter: func(config.Resolved, store.Store, daemon.Client, tmux.TargetContext) submitter.Submitter {
			return noopSubmitter{}
		},
	}
}

// cancelRenderer is the test-side equivalent of the production cancel stub.
type cancelRenderer struct{}

func (cancelRenderer) Run(tui.State) (tui.Result, error) {
	return tui.Result{Action: tui.ActionCancel}, nil
}

// noopSubmitter is the default Submitter for test deps that never exercise a
// submission. runTUI builds a Submitter before rendering (AUR-24), so cancel-
// path tests still dereference NewSubmitter even though Submit is never called.
type noopSubmitter struct{}

func (noopSubmitter) Submit(tui.Result) error { return nil }

func TestListPrintsIDsAlphabetically(t *testing.T) {
	fs := &fakeStore{
		summaries: []store.Summary{
			{ID: "alpha", Key: "1", KeySource: store.KeySourceAuto},
			{ID: "beta", Key: "b", KeySource: store.KeySourceExplicit},
			{ID: "gamma", KeySource: store.KeySourceOverflow},
		},
	}
	stdout, _, err := executeRootWith(t, workingDeps(t, fs), "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := strings.Join([]string{
		"alpha  key 1 (auto)",
		"beta  key b (explicit)",
		"gamma  key none (overflow, not on board)",
		"",
	}, "\n")
	if stdout != want {
		t.Fatalf("stdout mismatch\ngot:  %q\nwant: %q", stdout, want)
	}
}

func TestListEmptyStore(t *testing.T) {
	fs := &fakeStore{summaries: []store.Summary{}}
	stdout, _, err := executeRootWith(t, workingDeps(t, fs), "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "" {
		t.Fatalf("want empty stdout, got %q", stdout)
	}
}

func TestPickPrintsSelectedID(t *testing.T) {
	fs := &fakeStore{
		summaries: []store.Summary{
			{ID: "alpha"},
			{ID: "beta"},
		},
	}
	fp := &fakePicker{selected: "beta"}
	deps := workingDeps(t, fs)
	deps.NewPicker = func(config.Resolved) (picker.Picker, error) { return fp, nil }

	stdout, _, err := executeRootWith(t, deps, "pick")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "beta\n" {
		t.Fatalf("stdout = %q, want beta newline", stdout)
	}
	wantIDs := []string{"alpha", "beta"}
	if !stringSliceEqual(fp.gotIDs, wantIDs) {
		t.Fatalf("picker ids = %v, want %v", fp.gotIDs, wantIDs)
	}
}

func TestPickCancelPrintsNothing(t *testing.T) {
	fs := &fakeStore{summaries: []store.Summary{{ID: "alpha"}}}
	deps := workingDeps(t, fs)
	deps.NewPicker = func(config.Resolved) (picker.Picker, error) {
		return &fakePicker{cancelled: true}, nil
	}

	stdout, _, err := executeRootWith(t, deps, "pick")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
}

func TestPickRejectsEmptyPickerCommand(t *testing.T) {
	fs := &fakeStore{summaries: []store.Summary{{ID: "alpha"}}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: "/test/prompts"}, nil
	}

	_, _, err := executeRootWith(t, deps, "pick")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var ve *config.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *config.ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "picker_command" {
		t.Fatalf("field = %q, want picker_command", ve.Field)
	}
}

func TestShowFullMetadata(t *testing.T) {
	enter := false
	fs := &fakeStore{
		prompts: map[string]store.Prompt{
			"code-review": {
				Summary: store.Summary{
					ID:          "code-review",
					Title:       "Code Review",
					Description: "Deep review prompt",
					Tags:        []string{"review", "code"},
					Key:         "c",
					KeySource:   store.KeySourceExplicit,
					Path:        "/prompts/code-review.md",
				},
				Body: "Review this code.\n",
				Defaults: store.DeliveryDefaults{
					Mode:  "paste",
					Enter: &enter,
				},
			},
		},
	}
	stdout, _, err := executeRootWith(t, workingDeps(t, fs), "show", "code-review")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := strings.Join([]string{
		"ID: code-review",
		"Source: /prompts/code-review.md",
		"Title: Code Review",
		"Description: Deep review prompt",
		"Tags: review, code",
		"Key: c (explicit)",
		"",
		"Review this code.",
		"",
	}, "\n")
	if stdout != want {
		t.Fatalf("stdout mismatch\ngot:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestShowMinimalMetadata(t *testing.T) {
	fs := &fakeStore{
		prompts: map[string]store.Prompt{
			"bare": {
				Summary: store.Summary{
					ID:        "bare",
					Path:      "/prompts/bare.md",
					KeySource: store.KeySourceOverflow,
				},
				Body: "Just a body.\n",
			},
		},
	}
	stdout, _, err := executeRootWith(t, workingDeps(t, fs), "show", "bare")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "ID: bare\nSource: /prompts/bare.md\nKey: none (overflow, not on board)\n\nJust a body.\n"
	if stdout != want {
		t.Fatalf("stdout mismatch\ngot:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestShowMinimalWithAutoAssignedKey(t *testing.T) {
	fs := &fakeStore{
		prompts: map[string]store.Prompt{
			"bare": {
				Summary: store.Summary{
					ID:        "bare",
					Path:      "/prompts/bare.md",
					Key:       "b",
					KeySource: store.KeySourceAuto,
				},
				Body: "Just a body.\n",
			},
		},
	}
	stdout, _, err := executeRootWith(t, workingDeps(t, fs), "show", "bare")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "ID: bare\nSource: /prompts/bare.md\nKey: b (auto)\n\nJust a body.\n"
	if stdout != want {
		t.Fatalf("stdout mismatch\ngot:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestShowNotFound(t *testing.T) {
	fs := &fakeStore{prompts: map[string]store.Prompt{}}
	_, _, err := executeRootWith(t, workingDeps(t, fs), "show", "nope")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var nf *store.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want NotFoundError, got %T: %v", err, err)
	}
}

func TestShowLoadConfigError(t *testing.T) {
	configErr := &config.ValidationError{Field: "prompts_dir", Message: "must be set"}
	deps := workingDeps(t, &fakeStore{})
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{}, configErr
	}
	_, _, err := executeRootWith(t, deps, "show", "anything")
	var ve *config.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %T: %v", err, err)
	}
}

func TestShowNewStoreError(t *testing.T) {
	storeErr := errors.New("store init failed")
	deps := workingDeps(t, &fakeStore{})
	deps.NewStore = func(config.Resolved) (store.Store, error) {
		return nil, storeErr
	}
	_, _, err := executeRootWith(t, deps, "show", "anything")
	if !errors.Is(err, storeErr) {
		t.Fatalf("want storeErr, got %v", err)
	}
}

func TestListLoadConfigError(t *testing.T) {
	configErr := &config.ValidationError{Field: "prompts_dir", Message: "must be set"}
	deps := workingDeps(t, &fakeStore{})
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{}, configErr
	}
	_, _, err := executeRootWith(t, deps, "list")
	var ve *config.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %T: %v", err, err)
	}
}

func TestListNewStoreError(t *testing.T) {
	storeErr := errors.New("store init failed")
	deps := workingDeps(t, &fakeStore{})
	deps.NewStore = func(config.Resolved) (store.Store, error) {
		return nil, storeErr
	}
	_, _, err := executeRootWith(t, deps, "list")
	if !errors.Is(err, storeErr) {
		t.Fatalf("want storeErr, got %v", err)
	}
}

func TestListDiscoverError(t *testing.T) {
	dupErr := &store.DuplicatePromptIDError{
		ID:    "x",
		Paths: []string{"/a/x.md", "/b/x.md"},
	}
	fs := &fakeStore{discoverErr: dupErr}
	_, _, err := executeRootWith(t, workingDeps(t, fs), "list")
	var de *store.DuplicatePromptIDError
	if !errors.As(err, &de) {
		t.Fatalf("want DuplicatePromptIDError, got %T: %v", err, err)
	}
}
