package picker

import "testing"

func TestPickerInterfaceShape(t *testing.T) {
	var _ Picker = stubPicker{}
}

type stubPicker struct{}

func (stubPicker) Select([]string) (string, bool, error) { return "", true, nil }
