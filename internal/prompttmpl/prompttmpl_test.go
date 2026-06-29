package prompttmpl

import (
	"errors"
	"strings"
	"testing"
)

func TestRenderSubstitutesDeclaredPlaceholders(t *testing.T) {
	vars := []Variable{
		{Name: "issue-id", Required: true},
		{Name: "focus", Default: "correctness"},
	}

	got, err := Render("Review {{ issue-id }} for {{focus}}.", vars, map[string]string{
		"issue-id": "AUR-123",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "Review AUR-123 for correctness."
	if got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}
}

func TestRenderEscapesLiteralOpeningDelimiter(t *testing.T) {
	vars := []Variable{{Name: "name"}}

	got, err := Render(`Keep \{{name}} literal, replace {{name}}.`, vars, map[string]string{"name": "value"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "Keep {{name}} literal, replace value."
	if got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}
}

func TestRenderNoVariablesLeavesBodyAlone(t *testing.T) {
	const body = "Literal {{not-a-template}} stays put."
	got, err := Render(body, nil, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != body {
		t.Fatalf("Render = %q, want unchanged", got)
	}
}

func TestValidateRejectsBadVariableNames(t *testing.T) {
	for _, name := range []string{"", "Issue", "issue_id", "issue--id", "1issue"} {
		t.Run(name, func(t *testing.T) {
			err := ValidateVariables([]Variable{{Name: name}})
			var invalid *InvalidVariableError
			if !errors.As(err, &invalid) {
				t.Fatalf("ValidateVariables(%q) err = %T: %v, want InvalidVariableError", name, err, err)
			}
		})
	}
}

func TestValidateAcceptsKebabCaseWithDigits(t *testing.T) {
	vars := []Variable{{Name: "issue-1"}, {Name: "p0-context"}}
	if err := ValidateVariables(vars); err != nil {
		t.Fatalf("ValidateVariables: %v", err)
	}
}

func TestValidateRejectsDuplicateVariable(t *testing.T) {
	err := ValidateVariables([]Variable{{Name: "issue"}, {Name: "issue"}})
	var invalid *InvalidVariableError
	if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("err = %T: %v, want duplicate InvalidVariableError", err, err)
	}
}

func TestValidateRejectsReservedFlagName(t *testing.T) {
	err := ValidateVariables([]Variable{{Name: "target-pane"}})
	var invalid *InvalidVariableError
	if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "built-in flag") {
		t.Fatalf("err = %T: %v, want reserved-name InvalidVariableError", err, err)
	}
}

func TestValidateRejectsUnusedVariable(t *testing.T) {
	vars := []Variable{
		{Name: "issue"},
		{Name: "focus"},
	}
	err := Validate(vars, "Review {{issue}}.")
	var invalid *InvalidVariableError
	if !errors.As(err, &invalid) || invalid.Name != "focus" || !strings.Contains(err.Error(), "not used") {
		t.Fatalf("err = %T: %v, want unused InvalidVariableError for focus", err, err)
	}
}

func TestValidateRejectsUnknownPlaceholder(t *testing.T) {
	err := Validate([]Variable{{Name: "issue"}}, "Review {{focus}}.")
	var invalid *InvalidPlaceholderError
	if !errors.As(err, &invalid) || invalid.Placeholder != "focus" {
		t.Fatalf("err = %T: %v, want InvalidPlaceholderError for focus", err, err)
	}
}

func TestValidateRejectsUnclosedPlaceholder(t *testing.T) {
	err := Validate([]Variable{{Name: "issue"}}, "Review {{issue.")
	var invalid *InvalidPlaceholderError
	if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "missing closing") {
		t.Fatalf("err = %T: %v, want unclosed InvalidPlaceholderError", err, err)
	}
}

func TestValidateValuesRejectsMissingRequired(t *testing.T) {
	err := ValidateValues([]Variable{{Name: "issue", Required: true}}, map[string]string{"issue": "  "})
	var missing *MissingValueError
	if !errors.As(err, &missing) || missing.Name != "issue" {
		t.Fatalf("err = %T: %v, want MissingValueError for issue", err, err)
	}
}
