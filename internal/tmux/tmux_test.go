package tmux

import "testing"

func TestAdapterInterfaceShape(t *testing.T) {
	var _ Adapter = (*stubAdapter)(nil)
}

type stubAdapter struct{}

func (stubAdapter) CurrentContext() (TargetContext, error)       { return TargetContext{}, nil }
func (stubAdapter) PaneExists(string) (bool, error)              { return false, nil }
func (stubAdapter) IsTargetSelected(TargetContext) (bool, error) { return false, nil }
func (stubAdapter) CapturePaneTail(string, int) (string, error)  { return "", nil }
func (stubAdapter) Paste(TargetContext, string, bool) error      { return nil }
func (stubAdapter) Type(TargetContext, string, bool) error       { return nil }
func (stubAdapter) DisplayMessage(string, string) error          { return nil }
