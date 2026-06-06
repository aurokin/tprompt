package importtui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// NewRenderer returns the production bubbletea-backed Renderer. Each Run builds
// a fresh Model and drives it through tea.NewProgram, returning the Model's
// captured Result.
//
// The program runs with tea.WithAltScreen so the picker draws on the alternate
// screen and is torn down when Run returns — leaving the inline stdout buffer
// holding only the created-path lines the caller prints afterward (the
// scriptable-equivalence contract). This is the one intentional divergence from
// tui.NewRenderer, which renders inline inside a tmux popup.
func NewRenderer(io ProgramIO) Renderer {
	return &bubbleRenderer{io: io}
}

type bubbleRenderer struct {
	io ProgramIO
}

func (r *bubbleRenderer) Run(state State) (Result, error) {
	m := NewModel(state)
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if r.io.Input != nil {
		opts = append(opts, tea.WithInput(r.io.Input))
	}
	if r.io.Output != nil {
		opts = append(opts, tea.WithOutput(r.io.Output))
	}
	final, err := tea.NewProgram(m, opts...).Run()
	if err != nil {
		return Result{}, fmt.Errorf("importtui: run bubbletea program: %w", err)
	}
	fm, ok := final.(Model)
	if !ok {
		return Result{}, fmt.Errorf("importtui: final model has unexpected type %T", final)
	}
	return fm.Result(), nil
}
