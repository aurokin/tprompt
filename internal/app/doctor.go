package app

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
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
	if cfgErr == nil {
		// Clipboard check needs a loaded cfg; earlier prompt/discovery failures
		// don't affect it.
		checkClipboard(w, env, lookPath, cfg)
		checkPicker(w, lookPath, cfg)
		if err := checkDaemon(w, deps, cfg); err != nil && firstErr == nil {
			firstErr = err
		}
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
	info, err := os.Stat(cfg.PromptsDir)
	if err != nil || !info.IsDir() {
		err := &store.PromptsDirMissingError{Path: cfg.PromptsDir}
		printFail(w, err.Error())
		return err
	}
	printOK(w, fmt.Sprintf("prompts directory exists (%s)", cfg.PromptsDir))
	return nil
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

func checkDaemon(w io.Writer, deps Deps, cfg config.Resolved) error {
	if err := validateDaemonStatusConfig(cfg); err != nil {
		printFail(w, err.Error())
		return err
	}
	client, err := deps.NewDaemonClient(cfg)
	if err != nil {
		printWarn(w, fmt.Sprintf("daemon unreachable (%s): %v", cfg.SocketPath, err))
		return nil
	}
	status, err := client.Status()
	if err != nil {
		var socketErr *daemon.SocketUnavailableError
		if errors.As(err, &socketErr) {
			printWarn(w, fmt.Sprintf("daemon not running (%s): %s", socketErr.Path, socketErr.Reason))
			return nil
		}
		printWarn(w, fmt.Sprintf("daemon unreachable (%s): %v", cfg.SocketPath, err))
		return nil
	}
	socket := status.Socket
	if socket == "" {
		socket = cfg.SocketPath
	}
	printOK(w, fmt.Sprintf("daemon reachable (%s)", socket))
	return nil
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
