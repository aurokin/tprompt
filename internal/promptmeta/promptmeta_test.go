package promptmeta

import (
	"errors"
	"testing"
)

func TestParseStubReturnsNotImplemented(t *testing.T) {
	if _, err := Parse(nil); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Parse stub err = %v, want ErrNotImplemented", err)
	}
}
