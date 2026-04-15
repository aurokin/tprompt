package daemon

import "testing"

func TestJobZeroValue(t *testing.T) {
	var j Job
	if j.Source != "" || j.Enter {
		t.Fatal("zero Job should be empty")
	}
}
