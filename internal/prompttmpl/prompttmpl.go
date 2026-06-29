// Package prompttmpl validates and renders prompt template variables.
//
// Templates are intentionally narrow: ordered string variables declared in
// frontmatter and `{{name}}` placeholders in the markdown body. There are no
// conditionals, functions, includes, or composition.
package prompttmpl

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Variable is one frontmatter-declared template input.
type Variable struct {
	Name        string `yaml:"name"`
	Label       string `yaml:"label"`
	Description string `yaml:"description"`
	Default     string `yaml:"default"`
	Required    bool   `yaml:"required"`
}

// InvalidVariableError reports a malformed variable declaration.
type InvalidVariableError struct {
	Name   string
	Reason string
}

func (e *InvalidVariableError) Error() string {
	if e.Name == "" {
		return fmt.Sprintf("invalid template variable: %s", e.Reason)
	}
	return fmt.Sprintf("invalid template variable %q: %s", e.Name, e.Reason)
}

// InvalidPlaceholderError reports a malformed or undeclared body placeholder.
type InvalidPlaceholderError struct {
	Placeholder string
	Offset      int
	Reason      string
}

func (e *InvalidPlaceholderError) Error() string {
	if e.Placeholder == "" {
		return fmt.Sprintf("invalid template placeholder at byte %d: %s", e.Offset, e.Reason)
	}
	return fmt.Sprintf("invalid template placeholder %q at byte %d: %s", e.Placeholder, e.Offset, e.Reason)
}

// MissingValueError reports a required variable whose final value is empty.
type MissingValueError struct {
	Name string
}

func (e *MissingValueError) Error() string {
	return fmt.Sprintf("--%s is required", e.Name)
}

// ReservedFlagNames are command/root flags a prompt variable may not shadow.
var ReservedFlagNames = map[string]struct{}{
	"config":      {},
	"enter":       {},
	"h":           {},
	"help":        {},
	"mode":        {},
	"sanitize":    {},
	"target-pane": {},
	"version":     {},
}

// Validate checks the variable declarations and, when variables are declared,
// the placeholders in body. Bodies are ignored for non-template prompts so
// existing prompts containing literal `{{...}}` remain valid.
func Validate(vars []Variable, body string) error {
	if err := ValidateVariables(vars); err != nil {
		return err
	}
	if len(vars) == 0 {
		return nil
	}
	declared := declaredNames(vars)
	used := make(map[string]struct{}, len(vars))
	if err := scanPlaceholders(body, func(name string, offset int) error {
		if !validName(name) {
			return &InvalidPlaceholderError{Placeholder: name, Offset: offset, Reason: "must match lowercase kebab-case"}
		}
		if _, ok := declared[name]; !ok {
			return &InvalidPlaceholderError{Placeholder: name, Offset: offset, Reason: "not declared in frontmatter variables"}
		}
		used[name] = struct{}{}
		return nil
	}); err != nil {
		return err
	}
	for _, v := range vars {
		if _, ok := used[v.Name]; !ok {
			return &InvalidVariableError{Name: v.Name, Reason: "declared but not used in body"}
		}
	}
	return nil
}

// ValidateVariables checks only frontmatter declarations.
func ValidateVariables(vars []Variable) error {
	seen := map[string]struct{}{}
	for _, v := range vars {
		if v.Name == "" {
			return &InvalidVariableError{Reason: "name is required"}
		}
		if !validName(v.Name) {
			return &InvalidVariableError{Name: v.Name, Reason: "name must match lowercase kebab-case"}
		}
		if _, reserved := ReservedFlagNames[v.Name]; reserved {
			return &InvalidVariableError{Name: v.Name, Reason: "collides with a built-in flag"}
		}
		if _, ok := seen[v.Name]; ok {
			return &InvalidVariableError{Name: v.Name, Reason: "duplicate variable name"}
		}
		seen[v.Name] = struct{}{}
	}
	return nil
}

// Defaults returns a value map seeded from variable defaults.
func Defaults(vars []Variable) map[string]string {
	values := make(map[string]string, len(vars))
	for _, v := range vars {
		values[v.Name] = v.Default
	}
	return values
}

// ValidateValues checks required variable values after defaults and user input
// have been applied.
func ValidateValues(vars []Variable, values map[string]string) error {
	for _, v := range vars {
		if !v.Required {
			continue
		}
		if strings.TrimSpace(values[v.Name]) == "" {
			return &MissingValueError{Name: v.Name}
		}
	}
	return nil
}

// Render substitutes declared placeholders with values. `\{{` escapes the
// opening delimiter and renders as a literal `{{`.
func Render(body string, vars []Variable, values map[string]string) (string, error) {
	if len(vars) == 0 {
		return body, nil
	}
	if err := Validate(vars, body); err != nil {
		return "", err
	}
	merged := Defaults(vars)
	for name, value := range values {
		merged[name] = value
	}
	if err := ValidateValues(vars, merged); err != nil {
		return "", err
	}

	var out strings.Builder
	out.Grow(len(body))
	err := scanTemplate(body, func(segment string, _ int) error {
		out.WriteString(segment)
		return nil
	}, func(name string, offset int) error {
		value, ok := merged[name]
		if !ok {
			return &InvalidPlaceholderError{Placeholder: name, Offset: offset, Reason: "not declared in frontmatter variables"}
		}
		out.WriteString(value)
		return nil
	})
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

// Names returns variable names in declaration order.
func Names(vars []Variable) []string {
	out := make([]string, 0, len(vars))
	for _, v := range vars {
		out = append(out, v.Name)
	}
	return out
}

func declaredNames(vars []Variable) map[string]struct{} {
	declared := make(map[string]struct{}, len(vars))
	for _, v := range vars {
		declared[v.Name] = struct{}{}
	}
	return declared
}

func validName(name string) bool {
	if name == "" {
		return false
	}
	first, _ := utf8.DecodeRuneInString(name)
	if first < 'a' || first > 'z' {
		return false
	}
	parts := strings.Split(name, "-")
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			switch {
			case r >= 'a' && r <= 'z':
			case r >= '0' && r <= '9':
			default:
				return false
			}
		}
	}
	return true
}

func scanPlaceholders(body string, visit func(name string, offset int) error) error {
	return scanTemplate(body, func(string, int) error { return nil }, visit)
}

func scanTemplate(body string, text func(segment string, offset int) error, placeholder func(name string, offset int) error) error {
	for i := 0; i < len(body); {
		switch {
		case strings.HasPrefix(body[i:], `\{{`):
			if err := text("{{", i); err != nil {
				return err
			}
			i += len(`\{{`)
		case strings.HasPrefix(body[i:], "{{"):
			next, err := emitPlaceholder(body, i, placeholder)
			if err != nil {
				return err
			}
			i = next
		default:
			next := nextSpecial(body, i)
			if err := text(body[i:next], i); err != nil {
				return err
			}
			i = next
		}
	}
	return nil
}

// emitPlaceholder parses the `{{name}}` token at offset i, invokes placeholder,
// and returns the offset just past the closing `}}`.
func emitPlaceholder(body string, i int, placeholder func(name string, offset int) error) (int, error) {
	closeRel := strings.Index(body[i+2:], "}}")
	if closeRel < 0 {
		return 0, &InvalidPlaceholderError{Offset: i, Reason: "missing closing }}"}
	}
	closeIdx := i + 2 + closeRel
	name := strings.TrimSpace(body[i+2 : closeIdx])
	if name == "" {
		return 0, &InvalidPlaceholderError{Offset: i, Reason: "empty placeholder name"}
	}
	if err := placeholder(name, i); err != nil {
		return 0, err
	}
	return closeIdx + 2, nil
}

func nextSpecial(body string, start int) int {
	next := len(body)
	for _, marker := range []string{`{{`, `\{{`} {
		if idx := strings.Index(body[start:], marker); idx >= 0 && start+idx < next {
			next = start + idx
		}
	}
	if next == start {
		_, size := utf8.DecodeRuneInString(body[start:])
		return start + size
	}
	return next
}
