package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/handoff"
	"github.com/hsadler/tprompt/internal/promptsource"
	"github.com/hsadler/tprompt/internal/store"
)

func runDoctor(deps Deps) error {
	w := deps.Stdout
	env := envOrEmpty(deps.Env)
	lookPath := lookPathOrMissing(deps.LookPath)
	var firstErr error

	cfg, cfgErr := checkConfig(w, deps)
	if cfgErr != nil {
		firstErr = cfgErr
	} else if err := checkPromptsDir(w, cfg); err != nil {
		firstErr = err
	} else if err := checkDiscovery(w, deps, cfg); err != nil {
		firstErr = err
	}

	checkTmux(w, env)
	checkPopupBinding(w, deps, env)
	if cfgErr == nil {
		// Clipboard check needs a loaded cfg; earlier prompt/discovery failures
		// don't affect it.
		checkClipboard(w, env, lookPath, cfg)
		checkPicker(w, lookPath, cfg)
		checkHandoff(w, deps, cfg)
	}
	return firstErr
}

func envOrEmpty(f func(string) string) func(string) string {
	if f == nil {
		return func(string) string { return "" }
	}
	return f
}

func lookPathOrMissing(f func(string) (string, error)) func(string) (string, error) {
	if f == nil {
		return func(string) (string, error) { return "", os.ErrNotExist }
	}
	return f
}

func checkConfig(w io.Writer, deps Deps) (config.Resolved, error) {
	cfg, err := deps.LoadConfig(*deps.ConfigPath)
	if err != nil {
		printFail(w, err.Error())
		return config.Resolved{}, err
	}
	source := cfg.ConfigPath
	if source == "" {
		source = "defaults"
	}
	printOK(w, fmt.Sprintf("config loaded (%s)", source))
	return cfg, nil
}

func checkPromptsDir(w io.Writer, cfg config.Resolved) error {
	sources, err := promptSources(cfg)
	if err != nil {
		printFail(w, err.Error())
		return err
	}
	printOK(w, fmt.Sprintf("prompt priority: %s", activePromptPriority(cfg)))
	hasProject := false
	for _, source := range sources {
		if source.Scope == promptsource.ScopeProject {
			hasProject = true
		}
		if err := checkPromptSource(w, cfg, source); err != nil {
			return err
		}
	}
	if !hasProject {
		printOK(w, "project overlay: no project overlay")
	}
	return nil
}

func checkPromptSource(w io.Writer, cfg config.Resolved, source promptsource.Source) error {
	path := source.Path
	if source.AutoCreateOnAccess {
		if mkErr := os.MkdirAll(path, 0o700); mkErr != nil {
			createErr := &store.PromptsDirCreateError{Path: path, Err: mkErr}
			printFail(w, createErr.Error())
			return createErr
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return reportPromptSourceStatError(w, cfg, source, err)
	}
	if !info.IsDir() {
		missingErr := &store.PromptsDirMissingError{Path: path}
		printFail(w, missingErr.Error())
		return missingErr
	}
	printOK(w, fmt.Sprintf("prompts directory exists (scope %s, %s) %s",
		source.Scope, path, sourceOriginLabel(source, cfg)))
	return nil
}

func reportPromptSourceStatError(w io.Writer, cfg config.Resolved, source promptsource.Source, err error) error {
	path := source.Path
	if errors.Is(err, os.ErrNotExist) {
		if source.Optional {
			printWarn(w, fmt.Sprintf("prompts directory missing (scope %s, %s) %s",
				source.Scope, path, sourceOriginLabel(source, cfg)))
			return nil
		}
		missingErr := &store.PromptsDirMissingError{Path: path}
		printFail(w, missingErr.Error())
		return missingErr
	}
	statErr := fmt.Errorf("stat prompts directory %s: %w", path, err)
	printFail(w, statErr.Error())
	return statErr
}

// sourceOriginLabel describes where source's path came from so doctor output
// distinguishes a resolved default from an explicit user setting.
func sourceOriginLabel(source promptsource.Source, cfg config.Resolved) string {
	if source.Optional {
		return "[additional]"
	}
	if source.Scope == promptsource.ScopeProject {
		return "[project]"
	}
	if cfg.PromptsDir == "" {
		return "[default]"
	}
	return "[explicit]"
}

func checkDiscovery(w io.Writer, deps Deps, cfg config.Resolved) error {
	s, err := deps.NewStore(cfg)
	if err != nil {
		printFail(w, err.Error())
		return err
	}
	summaries, err := s.List()
	if err != nil {
		printFail(w, err.Error())
		return err
	}
	if len(summaries) == 0 {
		// An empty store is not an error, but doctor is where a stuck first-run
		// user looks, so flag it as a warn pointing at `tprompt new`. Warn does
		// not change the exit code (runDoctor returns only the first FAIL).
		printWarn(w, "0 prompts discovered (run 'tprompt new <id>' to create one)")
		return nil
	}
	printOK(w, fmt.Sprintf("%d prompts discovered", len(summaries)))
	return nil
}

func checkTmux(w io.Writer, env func(string) string) {
	if env("TMUX") != "" {
		printOK(w, "inside tmux")
	} else {
		printWarn(w, "not inside tmux")
	}
}

// bindingLister is the narrow slice of the tmux adapter that checkPopupBinding
// needs. Defined consumer-side (not on tmux.Adapter) and recovered by type
// assertion so adding the list-keys query does not force every Adapter fake to
// implement it.
type bindingLister interface {
	ListKeys(ctx context.Context) (string, error)
}

// checkPopupBinding warns when no tmux key binding invokes tprompt — the single
// thing most newcomers miss, on which doctor previously gave false confidence.
// Warn-only (never FAIL), like the other optional checks, so it does not affect
// the exit code. Gated on being inside tmux: `list-keys` needs a running
// server, and checkTmux already reports the not-inside-tmux case, so probing
// outside tmux would only produce a noisy spurious warning.
func checkPopupBinding(w io.Writer, deps Deps, env func(string) string) {
	if env("TMUX") == "" {
		return
	}
	adapter, err := deps.NewTmux()
	if err != nil || adapter == nil {
		printWarn(w, "could not check tmux key bindings (tmux adapter unavailable)")
		return
	}
	lister, ok := adapter.(bindingLister)
	if !ok {
		printWarn(w, "could not check tmux key bindings (list-keys unsupported)")
		return
	}
	out, err := lister.ListKeys(context.Background())
	if err != nil {
		printWarn(w, fmt.Sprintf("could not check tmux key bindings: %v", err))
		return
	}
	// Heuristic per the issue: a binding whose command string contains
	// "tprompt". Deliberately broad (any tprompt-invoking binding counts) and
	// warn-only, so a false positive is harmless and a miss only nudges setup.
	if strings.Contains(out, "tprompt") {
		printOK(w, "tmux key binding runs tprompt")
		return
	}
	printWarn(w, "no tmux key binding runs tprompt (run 'tprompt init' to wire the popup)")
}

func checkClipboard(w io.Writer, env func(string) string, lookPath func(string) (string, error), cfg config.Resolved) {
	if len(cfg.ClipboardArgv) > 0 {
		tool := cfg.ClipboardArgv[0]
		if _, err := lookPath(tool); err != nil {
			printWarn(w, fmt.Sprintf("clipboard reader: %s (override) not found on $PATH", tool))
			return
		}
		printOK(w, fmt.Sprintf("clipboard reader: %s (override)", tool))
		return
	}

	argv, reason, ok := clipboard.Detect(env, lookPath)
	if !ok {
		printWarn(w, "clipboard reader: none available (install pbpaste, wl-paste, xclip, or xsel)")
		return
	}
	printOK(w, fmt.Sprintf("clipboard reader: %s (auto-detected, %s)", argv[0], reason))
}

func checkPicker(w io.Writer, lookPath func(string) (string, error), cfg config.Resolved) {
	if len(cfg.PickerArgv) == 0 {
		printWarn(w, "picker command: none configured (tprompt pick unavailable)")
		return
	}
	tool := cfg.PickerArgv[0]
	if _, err := lookPath(tool); err != nil {
		printWarn(w, fmt.Sprintf("picker command: %s not found on $PATH (tprompt pick unavailable)", tool))
		return
	}
	printOK(w, fmt.Sprintf("picker command: %s", tool))
}

func checkHandoff(w io.Writer, deps Deps, cfg config.Resolved) {
	if deps.NewTUIClient == nil {
		printWarn(w, "TUI handoff unavailable: no client factory configured")
		return
	}
	if _, err := deps.NewTUIClient(cfg); err != nil {
		printWarn(w, fmt.Sprintf("TUI handoff unavailable: %v", err))
		return
	}
	printOK(w, fmt.Sprintf("TUI handoff ready (%s)", handoff.JobsDir(cfg)))
}

func printOK(w io.Writer, msg string) {
	_, _ = fmt.Fprintf(w, "ok   %s\n", msg)
}

func printWarn(w io.Writer, msg string) {
	_, _ = fmt.Fprintf(w, "warn %s\n", msg)
}

func printFail(w io.Writer, msg string) {
	_, _ = fmt.Fprintf(w, "FAIL %s\n", msg)
}
