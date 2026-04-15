package keybind

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestAssignmentZeroValue(t *testing.T) {
	var a Assignment
	if len(a.Bindings) != 0 || len(a.Overflow) != 0 {
		t.Fatal("zero Assignment should be empty")
	}
}

func TestResolveUsesFrontmatterThenAutoAssignsAlphabetically(t *testing.T) {
	got, err := Resolve(
		[]Input{
			{ID: "bravo"},
			{ID: "alpha", Key: "M", HasKey: true, Path: "/tmp/alpha.md"},
			{ID: "charlie"},
		},
		map[rune]string{'p': "clipboard"},
		[]rune("1mp2"),
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := Assignment{
		Bindings: map[rune]string{
			'm': "alpha",
			'1': "bravo",
			'2': "charlie",
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Resolve() mismatch (-want +got):\n%s", diff)
	}
}

func TestResolveReturnsOverflowWhenPoolIsExhausted(t *testing.T) {
	got, err := Resolve(
		[]Input{
			{ID: "alpha"},
			{ID: "bravo"},
			{ID: "charlie"},
		},
		nil,
		[]rune("1"),
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := Assignment{
		Bindings: map[rune]string{'1': "alpha"},
		Overflow: []string{"bravo", "charlie"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Resolve() mismatch (-want +got):\n%s", diff)
	}
}

func TestResolveRejectsDuplicateKeybindCaseInsensitive(t *testing.T) {
	_, err := Resolve(
		[]Input{
			{ID: "alpha", Key: "C", HasKey: true, Path: "/prompts/alpha.md"},
			{ID: "bravo", Key: "c", HasKey: true, Path: "/prompts/bravo.md"},
		},
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("want error, got nil")
	}

	var dupErr *DuplicateKeybindError
	if !errors.As(err, &dupErr) {
		t.Fatalf("want DuplicateKeybindError, got %T", err)
	}
	if dupErr.Key != 'c' {
		t.Fatalf("Key = %q, want %q", string(dupErr.Key), "c")
	}
	if diff := cmp.Diff([]string{"/prompts/alpha.md", "/prompts/bravo.md"}, dupErr.Paths); diff != "" {
		t.Fatalf("Paths mismatch (-want +got):\n%s", diff)
	}
}

func TestResolveRejectsReservedKeyCollision(t *testing.T) {
	_, err := Resolve(
		[]Input{{ID: "alpha", Key: "P", HasKey: true, Path: "/prompts/alpha.md"}},
		map[rune]string{'p': "clipboard"},
		nil,
	)
	if err == nil {
		t.Fatal("want error, got nil")
	}

	var reservedErr *ReservedKeybindError
	if !errors.As(err, &reservedErr) {
		t.Fatalf("want ReservedKeybindError, got %T", err)
	}
	if reservedErr.Key != 'p' {
		t.Fatalf("Key = %q, want %q", string(reservedErr.Key), "p")
	}
}

func TestResolveRejectsMalformedKeys(t *testing.T) {
	tests := []string{"", "ctrl+x", "\x1b"}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			_, err := Resolve([]Input{{ID: "alpha", Key: raw, HasKey: true, Path: "/prompts/alpha.md"}}, nil, nil)
			if err == nil {
				t.Fatal("want error, got nil")
			}

			var malformed *MalformedKeybindError
			if !errors.As(err, &malformed) {
				t.Fatalf("want MalformedKeybindError, got %T", err)
			}
		})
	}
}
