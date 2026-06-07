package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aurokin/tprompt/internal/promptsource"
	"github.com/aurokin/tprompt/internal/wispr"
)

func planFor(t *testing.T, dir string, snippets []wispr.Snippet, flags importFlags) []planItem {
	t.Helper()
	src := promptsource.Source{Path: dir, Scope: promptsource.ScopeGlobal}
	return dryRunPlan(src, []promptsource.Source{src}, snippets, flags)
}

// TestDryRunPlan_ClassifiesTheThreeConflictCases pins the §34 classification the
// import TUI will render: a fresh id is planImportable, an exact-target re-run is
// planExists, and a same-id prompt at another path is planCrossPath. dryRunPlan is
// write-free — none of these create a file.
func TestDryRunPlan_ClassifiesTheThreeConflictCases(t *testing.T) {
	snip := wispr.Snippet{ID: "u1", Phrase: "code review", Replacement: "body"}

	t.Run("importable (fresh id)", func(t *testing.T) {
		dir := t.TempDir()
		plan := planFor(t, dir, []wispr.Snippet{snip}, importFlags{tag: defaultWisprTag})
		if len(plan) != 1 || plan[0].status != planImportable {
			t.Fatalf("status = %d, want planImportable (%d)", plan[0].status, planImportable)
		}
		if plan[0].id != "code-review" {
			t.Errorf("id = %q, want code-review", plan[0].id)
		}
		if pathExists(filepath.Join(dir, "code-review.md")) {
			t.Error("dryRunPlan must not write a file")
		}
	})

	t.Run("exists (exact-target re-run)", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "code-review.md")
		if err := os.WriteFile(target, []byte("---\ntitle: x\n---\n\nbody\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		plan := planFor(t, dir, []wispr.Snippet{snip}, importFlags{tag: defaultWisprTag})
		if plan[0].status != planExists {
			t.Fatalf("status = %d, want planExists (%d)", plan[0].status, planExists)
		}
		if plan[0].blocker != target {
			t.Errorf("blocker = %q, want %q (the exact target)", plan[0].blocker, target)
		}
	})

	t.Run("crossPath (same id at another path)", func(t *testing.T) {
		dir := t.TempDir()
		sub := filepath.Join(dir, "agents")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		other := filepath.Join(sub, "code-review.md")
		if err := os.WriteFile(other, []byte("---\ntitle: x\n---\n\nbody\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		plan := planFor(t, dir, []wispr.Snippet{snip}, importFlags{tag: defaultWisprTag})
		if plan[0].status != planCrossPath {
			t.Fatalf("status = %d, want planCrossPath (%d)", plan[0].status, planCrossPath)
		}
		if plan[0].blocker != other {
			t.Errorf("blocker = %q, want %q (the other-path duplicate)", plan[0].blocker, other)
		}
	})
}

// TestDryRunPlan_ClaimOrdering pins the intra-batch slug-claim ordering the
// executor relies on for byte-identical ids: an empty-body snippet is classified
// planEmptyBody and claims NO slug (so a later same-phrase sibling keeps the bare
// slug), while an importable snippet claims its id (so the next same-phrase sibling
// is suffixed). Threaded in snippet order, identical to the original single-pass
// loop. (planInvalidID is a defensive backstop unreachable through the real
// slug/uuid mapping, so it is exercised by the executor switch, not constructed
// here.)
func TestDryRunPlan_ClaimOrdering(t *testing.T) {
	dir := t.TempDir()
	plan := planFor(t, dir, []wispr.Snippet{
		{ID: "aaaaaaaa-0000-0000-0000-000000000000", Phrase: "code review", Replacement: ""},       // empty body → no claim
		{ID: "bbbbbbbb-1111-0000-0000-000000000000", Phrase: "code review", Replacement: "body B"}, // importable → bare slug
		{ID: "cccccccc-2222-0000-0000-000000000000", Phrase: "code-review", Replacement: "body C"}, // importable → suffixed
	}, importFlags{tag: defaultWisprTag})

	if plan[0].status != planEmptyBody {
		t.Fatalf("item 0 status = %d, want planEmptyBody (%d)", plan[0].status, planEmptyBody)
	}
	if plan[1].status != planImportable || plan[1].id != "code-review" {
		t.Errorf("item 1 status=%d id=%q, want planImportable code-review (empty body must not claim the slug)", plan[1].status, plan[1].id)
	}
	if plan[2].status != planImportable || plan[2].id != "code-review-cccccc" {
		t.Errorf("item 2 status=%d id=%q, want planImportable code-review-cccccc (importable claimed, so this is suffixed)", plan[2].status, plan[2].id)
	}
}

// TestPickerItems_PopulatesTags pins that each picker row carries the snippet's
// provenance tags — the active --tag, plus "starred" for a starred snippet — so
// the import TUI's `/`-search can match on them. The tags are sourced from
// wispr.Snippet.Tags, the same path the writer uses for frontmatter, so a search
// hit reflects exactly what gets written.
func TestPickerItems_PopulatesTags(t *testing.T) {
	dir := t.TempDir()
	snips := []wispr.Snippet{
		{ID: "u1", Phrase: "code review", Replacement: "body"},
		{ID: "u2", Phrase: "deploy steps", Replacement: "body", Starred: true},
	}
	plan := planFor(t, dir, snips, importFlags{tag: "imported"})
	items := pickerItems(plan, "imported")
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}

	byID := map[string][]string{}
	for _, it := range items {
		byID[it.ID] = it.Tags
	}
	if got := byID["code-review"]; strings.Join(got, ",") != "imported" {
		t.Errorf("code-review tags = %v, want [imported]", got)
	}
	if got := byID["deploy-steps"]; strings.Join(got, ",") != "imported,starred" {
		t.Errorf("deploy-steps tags = %v, want [imported starred]", got)
	}
}
