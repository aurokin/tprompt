package keybind

import "testing"

func TestAssignmentZeroValue(t *testing.T) {
	var a Assignment
	if len(a.Bindings) != 0 || len(a.Overflow) != 0 {
		t.Fatal("zero Assignment should be empty")
	}
}
