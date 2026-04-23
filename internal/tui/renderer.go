package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// NewRenderer returns the production bubbletea-backed Renderer. The returned
// implementation constructs a fresh Model per Run call and drives it through
// tea.NewProgram; the Model's captured Result is returned to the caller.
func NewRenderer(deps ModelDeps) Renderer {
	return &bubbleRenderer{deps: deps}
}

type bubbleRenderer struct {
	deps ModelDeps
}

func (r *bubbleRenderer) Run(state State) (Result, error) {
	m := NewModel(state, r.deps)
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return Result{}, fmt.Errorf("tui: run bubbletea program: %w", err)
	}
	fm, ok := final.(Model)
	if !ok {
		return Result{}, fmt.Errorf("tui: final model has unexpected type %T", final)
	}
	return fm.Result(), nil
}
