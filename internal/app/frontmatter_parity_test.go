package app

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hsadler/tprompt/internal/wispr"
)

// frontmatterKeys returns the ordered top-level keys of a prompt file's YAML
// frontmatter block (the lines between the first two `---` fences). It assumes
// single-line `key: value` entries, which holds for the scaffold and for the
// simple phrase the parity test imports.
func frontmatterKeys(md string) []string {
	var keys []string
	inFrontmatter := false
	for _, line := range strings.Split(md, "\n") {
		if line == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			break // closing fence
		}
		if !inFrontmatter || strings.TrimSpace(line) == "" {
			continue
		}
		if i := strings.IndexByte(line, ':'); i > 0 {
			keys = append(keys, line[:i])
		}
	}
	return keys
}

// TestImportFrontmatterMatchesNewScaffold pins the new↔import field-set parity
// contract (AUR-526): a prompt imported from Wispr carries exactly the same
// frontmatter keys, in the same order, as `tprompt new` scaffolds. The field set
// is defined in two places by necessity — new.go's scaffoldTemplate (a literal)
// and wispr.buildFrontmatter (marshaled) — so this test, not a shared
// abstraction, is the contract that keeps them in sync. Add a 7th field to one
// side without the other and this fails loudly.
func TestImportFrontmatterMatchesNewScaffold(t *testing.T) {
	scaffoldKeys := frontmatterKeys(scaffoldTemplate)
	if len(scaffoldKeys) == 0 {
		t.Fatal("no frontmatter keys parsed from scaffoldTemplate")
	}

	_, md, ok := wispr.Snippet{
		ID:          "11111111-2222-3333-4444-555555555555",
		Phrase:      "organize thoughts prompt",
		Replacement: "body",
	}.ToPrompt("wispr")
	if !ok {
		t.Fatal("ToPrompt ok = false, want true")
	}
	importKeys := frontmatterKeys(string(md))

	if !reflect.DeepEqual(importKeys, scaffoldKeys) {
		t.Errorf("import frontmatter field set diverged from `new` scaffold:\n import   = %v\n scaffold = %v",
			importKeys, scaffoldKeys)
	}
}
