package app

import (
	"fmt"
	"io"
	"os"

	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/store"
)

func runDoctor(deps Deps) error {
	w := deps.Stdout
	var firstErr error

	cfg, err := checkConfig(w, deps)
	if err != nil {
		firstErr = err
	} else if err := checkPromptsDir(w, cfg); err != nil {
		firstErr = err
	} else if err := checkDiscovery(w, deps, cfg); err != nil {
		firstErr = err
	}

	checkTmux(w, deps)
	return firstErr
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

func checkTmux(w io.Writer, deps Deps) {
	if deps.Env("TMUX") != "" {
		printOK(w, "inside tmux")
	} else {
		printWarn(w, "not inside tmux")
	}
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
