package tui

import "testing"

func TestActionConstants(t *testing.T) {
	if ActionPrompt == ActionClipboard || ActionPrompt == ActionCancel {
		t.Fatal("action constants must be distinct")
	}
}

func TestRowDisplayDescriptionPrefersDescription(t *testing.T) {
	row := Row{
		Title:       "Code Review",
		Description: "Review for correctness and risk",
	}

	if got := row.DisplayDescription(); got != row.Description {
		t.Fatalf("DisplayDescription() = %q, want %q", got, row.Description)
	}
}

func TestRowDisplayDescriptionFallsBackToTitle(t *testing.T) {
	row := Row{Title: "Code Review"}

	if got := row.DisplayDescription(); got != row.Title {
		t.Fatalf("DisplayDescription() = %q, want %q", got, row.Title)
	}
}

func TestRowDisplayDescriptionFallsBackToBlank(t *testing.T) {
	if got := (Row{}).DisplayDescription(); got != "" {
		t.Fatalf("DisplayDescription() = %q, want blank", got)
	}
}

func TestRowSearchTextIncludesSearchableMetadata(t *testing.T) {
	row := Row{
		PromptID:    "code-review",
		Title:       "Code Review",
		Description: "Review for correctness and risk",
		Tags:        []string{"review", "code"},
	}

	got := row.SearchText()
	want := "code-review Code Review Review for correctness and risk review code"
	if got != want {
		t.Fatalf("SearchText() = %q, want %q", got, want)
	}
}
