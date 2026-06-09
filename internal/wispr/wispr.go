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

// buildFrontmatter renders the prompt header in the same field order and shape
// that `tprompt new` scaffolds (title, description, tags, key, mode, enter), so
// an imported prompt is as immediately editable as a scaffolded one (AUR-526).
// Only title and tags are populated; description/key/mode/enter are empty stubs
// the author fills in later.
//
// title and tags are the only snippet-controlled fields — a phrase may contain
// ':' or quotes, and a --tag value is arbitrary — so they are YAML-marshaled to
// stay well-formed (tags in flow style so the provenance tag renders as the
// compact `tags: [wispr]`). The empty stubs are written as bare keys with no
// value, which both matches new's scaffold byte-for-byte and is load-bearing:
// marshaling them as empty strings would emit `enter: ""`, which then fails to
// unmarshal back into promptmeta.Meta's *bool Enter field and would make every
// imported prompt unloadable. A bare `enter:` decodes as null → nil, the
// intended "unset". Marshal cannot fail for these fixed string / []string
// structs; if that invariant changes, the importer is broken rather than looking
// at an unimportable snippet. The new↔import field-set parity is guarded by a
// test in internal/app.
func buildFrontmatter(phrase string, tags []string) []byte {
	titleFM := mustYAMLMarshal(struct {
		Title string `yaml:"title"`
	}{Title: phrase})
	tagsFM := mustYAMLMarshal(struct {
		Tags []string `yaml:"tags,flow"`
	}{Tags: tags})

	var b strings.Builder
	b.Write(titleFM) // title: <phrase>\n
	b.WriteString("description:\n")
	b.Write(tagsFM) // tags: [wispr]\n
	b.WriteString("key:\n")
	b.WriteString("mode:\n")
	b.WriteString("enter:\n")
	return []byte(b.String())
}

func mustYAMLMarshal(v any) (out []byte) {
	defer func() {
		if recover() != nil {
			panic("wispr frontmatter marshal invariant violated")
		}
	}()
	out, err := yaml.Marshal(v)
	if err != nil {
		panic("wispr frontmatter marshal invariant violated")
	}
	return out
}

// Tags is the provenance tag set stamped on the imported prompt: the supplied
// tag (default "wispr"), plus "starred" when the snippet is starred. It is the
// single source of truth for a snippet's tags so the frontmatter writer
// (ToPrompt) and the import picker's search corpus cannot drift.
func (s Snippet) Tags(tag string) []string {
	tags := []string{tag}
	if s.Starred {
		tags = append(tags, "starred")
	}
	return tags
}

// ToPrompt maps a snippet to a prompt id (filename stem) and the markdown file
// content. tag is the provenance tag stamped on every imported prompt (default
// "wispr"); a starred snippet also gets a "starred" tag.
//
// id is always a valid store id (the phrase slug, or a deterministic
// `wispr-<uuid>` fallback when the phrase has no sluggable characters), so a
// caller can report it even when the snippet is skipped. ok is false when the
// snippet has no usable body (empty/whitespace-only replacement): a bodyless
// prompt delivers nothing, so the importer skips it.
//
// The title always preserves the original phrase verbatim, even when the id
// falls back to a UUID-derived form.
func (s Snippet) ToPrompt(tag string) (id string, markdown []byte, ok bool) {
	id = s.slugID()
	if strings.TrimSpace(s.Replacement) == "" {
		return id, nil, false
	}
	fm := buildFrontmatter(s.Phrase, s.Tags(tag))

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fm)
	b.WriteString("---\n")
	// A blank line after the closing fence is the canonical layout. promptmeta.Parse
	// strips exactly one leading line break after the fence and one trailing line
	// break, so this blank line (not the replacement's own first byte) absorbs the
	// leading trim — preserving a replacement that itself starts with a newline —
	// and the trailing newline absorbs the trailing trim. Net: the delivered body
	// matches Replacement through promptmeta's one-break trim contract; the only
	// known byte-level exception is a replacement ending in bare '\r', which joins
	// the appended '\n' into a trailing CRLF that promptmeta trims as one break.
	b.WriteString("\n")
	b.WriteString(s.Replacement)
	b.WriteString("\n")
	return id, []byte(b.String()), true
}

// Title is the snippet's human label — the original phrase, verbatim. It is what
// the import picker renders as a row label. Provided so the import engine can treat
// a snippet as a source-neutral record (see internal/app ImportRecord) without
// reaching for the Phrase field directly.
func (s Snippet) Title() string { return s.Phrase }

// Disambiguator is the stable, non-empty seed the import engine appends to resolve
// intra-batch id collisions (the first 6 slug-safe chars of the snippet's UUID).
// It is stable across runs because the UUID is, so disambiguated ids are
// deterministic. Part of the source-neutral record contract (ImportRecord).
func (s Snippet) Disambiguator() string { return IDPrefix(s.ID, 6) }

// slugID is the prompt id derived from the phrase: its slug, or a deterministic
// `wispr-<first 8 of the UUID>` fallback when the phrase slugifies to nothing
// (all punctuation / non-ASCII). The fallback keeps such snippets importable
// under a synthetic but stable id; the title still preserves the original phrase.
func (s Snippet) slugID() string {
	if slug := slugify(s.Phrase); slug != "" {
		return slug
	}
	return "wispr-" + IDPrefix(s.ID, 8)
}

// IDPrefix returns the first n slug-safe characters of a Wispr UUID (lowercased,
// non-`[a-z0-9]` runes dropped). It is the disambiguation seed for empty-slug
// fallbacks and intra-batch id collisions, and is stable across runs because the
// UUID is stable. A UUID with fewer than n slug-safe characters yields all of them.
func IDPrefix(id string, n int) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
		if b.Len() == n {
			break
		}
	}
	return b.String()
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
// ok=false when there is no known default, in which case the caller must require
// --db-path. Only macOS has a zero-config default; every other platform —
// including Linux and Windows-via-WSL2, since tprompt ships no native Windows
// binary — returns ok=false. home is the user's home directory (used on macOS).
func DefaultDBPath(goos, home string) (string, bool) {
	if goos == "darwin" && home != "" {
		return filepath.Join(home, "Library", "Application Support", "Wispr Flow", "flow.sqlite"), true
	}
	return "", false
}
