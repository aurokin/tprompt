// Package wispr reads prompt-shaped snippets out of a local Wispr Flow
// flow.sqlite and maps them to tprompt markdown prompts. There is no Wispr
// export API; the local SQLite file is the only viable source (see the
// "Wispr Flow snippet import" milestone PRD). The modernc.org/sqlite driver is
// imported only in this package so the rest of the binary stays CGO-free.
package wispr

import (
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Snippet is one live Wispr Flow snippet: a Dictionary row with isSnippet=1 and
// isDeleted=0. Phrase is the snippet's trigger/name; Replacement is its
// expansion (the prompt body).
type Snippet struct {
	ID          string // Wispr UUID; used only to disambiguate id collisions
	Phrase      string // → title (verbatim) and slug source for the filename id
	Replacement string // → markdown body
	Starred     bool   // isStarred=1 → appends the "starred" tag
}

// Reader yields the live snippets from a Wispr Flow database. It is injected via
// app.Deps so tests can supply a fake without a real DB.
type Reader interface {
	Snippets() ([]Snippet, error)
}

// frontmatter is the YAML-marshaled prompt header. Marshaling (not string
// templating) is required because a phrase may contain ':' or quotes that would
// corrupt a hand-built file. Tags use flow style so the provenance tag renders
// as the compact `tags: [wispr]`.
type frontmatter struct {
	Title string   `yaml:"title"`
	Tags  []string `yaml:"tags,flow"`
}

// ToPrompt maps a snippet to a prompt id (filename stem) and the markdown file
// content. tag is the provenance tag stamped on every imported prompt (default
// "wispr"); a starred snippet also gets a "starred" tag.
//
// ok is false when the snippet has no usable body (empty/whitespace-only
// replacement): a bodyless prompt delivers nothing, so the importer skips it.
//
// The title always preserves the original phrase verbatim, even when the slug
// derives a different id.
func (s Snippet) ToPrompt(tag string) (id string, markdown []byte, ok bool) {
	if strings.TrimSpace(s.Replacement) == "" {
		return "", nil, false
	}
	tags := []string{tag}
	if s.Starred {
		tags = append(tags, "starred")
	}
	fm, err := yaml.Marshal(frontmatter{Title: s.Phrase, Tags: tags})
	if err != nil {
		// frontmatter is a fixed struct of strings; Marshal cannot fail in
		// practice. Treat the impossible failure as unimportable rather than panic.
		return "", nil, false
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fm)
	b.WriteString("---\n")
	// A blank line after the closing fence is the canonical layout. promptmeta.Parse
	// strips exactly one leading line break after the fence and one trailing line
	// break, so this blank line (not the replacement's own first byte) absorbs the
	// leading trim — preserving a replacement that itself starts with a newline —
	// and the trailing newline absorbs the trailing trim. Net: the delivered body
	// equals Replacement byte-for-byte.
	b.WriteString("\n")
	b.WriteString(s.Replacement)
	b.WriteString("\n")
	return slugify(s.Phrase), []byte(b.String()), true
}

// slugify derives a filename-stem id from a free-text phrase: lowercase, map
// whitespace and underscores to '-', drop anything outside [a-z0-9-], collapse
// runs of '-', and trim leading/trailing '-'. The result is safe for the store
// (no path separators, no leading dot, printable ASCII). It may be empty for an
// all-punctuation or non-ASCII phrase; callers handle the empty case.
func slugify(phrase string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(phrase) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		case r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '_' || r == '-':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		default:
			// Drop unicode/punctuation that has no slug representation.
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// DefaultDBPath returns the conventional flow.sqlite location for goos, or
// ok=false when there is no known default (e.g. Linux), in which case the caller
// must require --db-path. getenv reads environment variables (for the Windows
// %APPDATA% lookup); home is the user's home directory (used on macOS).
func DefaultDBPath(goos string, getenv func(string) string, home string) (string, bool) {
	switch goos {
	case "darwin":
		if home == "" {
			return "", false
		}
		return filepath.Join(home, "Library", "Application Support", "Wispr Flow", "flow.sqlite"), true
	case "windows":
		appData := getenv("APPDATA")
		if appData == "" {
			return "", false
		}
		return filepath.Join(appData, "Wispr Flow", "flow.sqlite"), true
	default:
		return "", false
	}
}
