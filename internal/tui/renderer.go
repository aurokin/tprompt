package tui

import (
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

// ProgramIO carries the streams Bubble Tea should use instead of process-global
// stdin/stdout defaults.
type ProgramIO struct {
	Input  io.Reader
	Output io.Writer
}

// NewRenderer returns the production bubbletea-backed Renderer. The returned
// implementation constructs a fresh Model per Run call and drives it through
// tea.NewProgram; the Model's captured Result is returned to the caller.
func NewRenderer(deps ModelDeps, io ProgramIO) Renderer {
	return &bubbleRenderer{deps: deps, io: io}
}

type bubbleRenderer struct {
	deps ModelDeps
	io   ProgramIO
}

func (r *bubbleRenderer) Run(state State) (Result, error) {
	m := NewModel(state, r.deps)
	var opts []tea.ProgramOption
	if r.io.Input != nil {
		opts = append(opts, tea.WithInput(r.io.Input))
	}
	if r.io.Output != nil {
		opts = append(opts, tea.WithOutput(r.io.Output))
	}
	final, err := tea.NewProgram(m, opts...).Run()
	if err != nil {
		return Result{}, fmt.Errorf("tui: run bubbletea program: %w", err)
	}
	fm, ok := final.(Model)
	if !ok {
		return Result{}, fmt.Errorf("tui: final model has unexpected type %T", final)
	}
	return fm.Result(), nil
}
