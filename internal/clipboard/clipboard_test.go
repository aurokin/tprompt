package clipboard

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestStaticReaderReturnsContent(t *testing.T) {
	r := NewStatic([]byte("hello"))
	got, err := r.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if diff := cmp.Diff([]byte("hello"), got); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}

func TestNewAutoDetectStubReturnsErrNotImplemented(t *testing.T) {
	r, err := NewAutoDetect()
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("want ErrNotImplemented, got err=%v", err)
	}
	if r != nil {
		t.Fatalf("want nil Reader, got %v", r)
	}
}

func TestNewCommandStubReaderFailsLoud(t *testing.T) {
	r := NewCommand([]string{"pbpaste"})
	if r == nil {
		t.Fatal("NewCommand stub must return a non-nil Reader so Read can fail loud")
	}
	if _, err := r.Read(); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("want ErrNotImplemented from Read, got %v", err)
	}
}
