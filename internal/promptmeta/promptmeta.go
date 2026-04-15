// Package promptmeta parses YAML frontmatter out of prompt markdown files and
// returns the body. Only the body is ever injected (DECISIONS.md §9).
package promptmeta

import "errors"

// ErrNotImplemented is returned by the Phase 0 Parse stub so tests that hit it
// fail loudly instead of silently accepting an empty Parsed value.
var ErrNotImplemented = errors.New("promptmeta: Parse not implemented")

// Meta holds the supported frontmatter keys (docs/storage/prompt-store.md).
type Meta struct {
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags"`
	Mode        string   `yaml:"mode"`
	Enter       *bool    `yaml:"enter"`
	Key         string   `yaml:"key"`
}

// Parsed is the result of splitting a prompt file into metadata and body.
type Parsed struct {
	Meta Meta
	Body string
}

// Parse reads prompt-file bytes and returns frontmatter metadata plus body.
// Phase 1 will implement the actual YAML split.
func Parse([]byte) (Parsed, error) {
	return Parsed{}, ErrNotImplemented
}
