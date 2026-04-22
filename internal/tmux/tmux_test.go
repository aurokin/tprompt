package tmux

import (
	"context"
	"testing"
)

func TestAdapterInterfaceShape(t *testing.T) {
	var _ Adapter = (*stubAdapter)(nil)
}

type stubAdapter struct{}

func (stubAdapter) CurrentContext() (TargetContext, error) { return TargetContext{}, nil }
func (stubAdapter) PaneExists(context.Context, string) (bool, error) {
	return false, nil
}

func (stubAdapter) IsTargetSelected(context.Context, TargetContext) (bool, error) {
	return false, nil
}

func (stubAdapter) CapturePaneTail(string, int) (string, error) { return "", nil }

func (stubAdapter) Paste(context.Context, TargetContext, string, bool) error {
	return nil
}

func (stubAdapter) Type(context.Context, TargetContext, string, bool) error {
	return nil
}
func (stubAdapter) DisplayMessage(MessageTarget, string) error { return nil }
