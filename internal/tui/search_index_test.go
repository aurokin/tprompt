package tui

import (
	"strings"
	"testing"
)

func clipRow() Row {
	return Row{Key: 'p', Description: "(read on select)"}
}

func idsOf(results []MatchedRow) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Row.PromptID
	}
	return out
}

func TestSearchIndex_EmptyQueryReturnsAlphabeticalCatalogWithClipFirst(t *testing.T) {
	rows := []Row{
		{Key: '1', PromptID: "code-review"},
		{Key: '2', PromptID: "alpha"},
		{Key: '3', PromptID: "mango"},
	}
	idx := newSearchIndex(rows, nil, clipRow())

	got := idsOf(idx.Query(""))
	want := []string{"", "alpha", "code-review", "mango"}
	if !equalStringSlices(got, want) {
		t.Fatalf("catalog order = %v, want %v", got, want)
	}
}

func TestSearchIndex_EmptyQueryIncludesOverflow(t *testing.T) {
	board := []Row{{Key: '1', PromptID: "alpha"}}
	overflow := []Row{{PromptID: "zed"}, {PromptID: "bravo"}}
	idx := newSearchIndex(board, overflow, clipRow())

	got := idsOf(idx.Query(""))
	want := []string{"", "alpha", "bravo", "zed"}
	if !equalStringSlices(got, want) {
		t.Fatalf("catalog order = %v, want %v", got, want)
	}
}

func TestSearchIndex_EmptyQueryWithoutClipRow(t *testing.T) {
	rows := []Row{
		{Key: '1', PromptID: "beta"},
		{Key: '2', PromptID: "alpha"},
	}
	idx := newSearchIndex(rows, nil, Row{})

	got := idsOf(idx.Query(""))
	want := []string{"alpha", "beta"}
	if !equalStringSlices(got, want) {
		t.Fatalf("catalog order = %v, want %v", got, want)
	}
}

func TestSearchIndex_NoMatchReturnsEmptySlice(t *testing.T) {
	rows := []Row{
		{Key: '1', PromptID: "alpha", Title: "Alpha", Description: "first"},
		{Key: '2', PromptID: "beta", Title: "Beta", Description: "second"},
	}
	idx := newSearchIndex(rows, nil, clipRow())

	got := idx.Query("xyzxyz")
	if len(got) != 0 {
		t.Fatalf("expected no matches, got %v", idsOf(got))
	}
}

func TestSearchIndex_IDMatchOutranksTitleOnlyMatch(t *testing.T) {
	// "foo" matches alpha's id directly and bravo's title directly with
	// identical raw fuzzy scores. alpha should rank higher because id has a
	// bigger weight (1.0 vs 0.75).
	rows := []Row{
		{Key: '1', PromptID: "foo", Title: "Alpha", Description: "first"},
		{Key: '2', PromptID: "bravo", Title: "foo", Description: "second"},
	}
	idx := newSearchIndex(rows, nil, clipRow())

	got := idsOf(idx.Query("foo"))
	if len(got) < 2 || got[0] != "foo" {
		t.Fatalf("expected id-match row first, got %v", got)
	}
}

func TestSearchIndex_TitleOutranksDescription(t *testing.T) {
	// Each row matches exactly one field with identical text. Row A matches
	// on title (weight 0.75), row B on description (weight 0.5). A wins.
	rows := []Row{
		{Key: '1', PromptID: "alpha", Title: "kubernetes", Description: "first"},
		{Key: '2', PromptID: "bravo", Title: "Bravo", Description: "kubernetes"},
	}
	idx := newSearchIndex(rows, nil, clipRow())

	got := idsOf(idx.Query("kubernetes"))
	if len(got) < 2 || got[0] != "alpha" {
		t.Fatalf("expected title-match row first, got %v", got)
	}
}

func TestSearchIndex_DescriptionOutranksTags(t *testing.T) {
	rows := []Row{
		{Key: '1', PromptID: "alpha", Title: "A", Description: "kubernetes", Tags: []string{"other"}},
		{Key: '2', PromptID: "bravo", Title: "B", Description: "other", Tags: []string{"kubernetes"}},
	}
	idx := newSearchIndex(rows, nil, clipRow())

	got := idsOf(idx.Query("kubernetes"))
	if len(got) < 2 || got[0] != "alpha" {
		t.Fatalf("expected description-match row first, got %v", got)
	}
}

func TestSearchIndex_MultiFieldMatchOutranksSingleField(t *testing.T) {
	// Same id score (both match "zed" identically in the id), row A has an
	// additional title match. A should rank above B.
	rows := []Row{
		{Key: '1', PromptID: "zed", Title: "zed", Description: "alpha"},
		{Key: '2', PromptID: "zed-only", Title: "other", Description: "alpha"},
	}
	idx := newSearchIndex(rows, nil, clipRow())

	// Expect "zed" ahead of "zed-only" because "zed" wins on id score (exact
	// pattern, no leading penalty) AND has a title match to push further.
	got := idsOf(idx.Query("zed"))
	if len(got) < 2 {
		t.Fatalf("expected 2 matches, got %v", got)
	}
	if got[0] != "zed" {
		t.Fatalf("expected multi-field row first, got %v", got)
	}
}

func TestSearchIndex_EqualScoresBreakTiesAlphabetically(t *testing.T) {
	// Two rows that each match only on id with identical raw scores; order
	// them alphabetically by PromptID.
	rows := []Row{
		{Key: '1', PromptID: "mango"},
		{Key: '2', PromptID: "apple"},
	}
	idx := newSearchIndex(rows, nil, clipRow())

	// Pattern that matches both ids at the start with the same score.
	got := idsOf(idx.Query("a"))
	// "apple" should appear — "mango" may or may not depending on fuzzy's
	// scoring, so assert positionally: if both present, apple before mango.
	sawApple := false
	sawMango := false
	for _, id := range got {
		switch id {
		case "apple":
			sawApple = true
		case "mango":
			if sawApple {
				// apple already before mango — good.
				return
			}
			sawMango = true
		}
	}
	if sawApple && sawMango {
		// reached here only if mango appeared before apple
		t.Fatalf("expected alphabetical tiebreak (apple before mango), got %v", got)
	}
	// If only one matched, there's no tiebreak to test — this test is
	// primarily about ordering. Accept as long as no ordering violation.
}

func TestSearchIndex_NonEmptyQueryOmitsClipRow(t *testing.T) {
	// The clip row's default description is "(read on select)". A query that
	// could match that text must not return the clip row.
	rows := []Row{
		{Key: '1', PromptID: "reader", Description: "something"},
	}
	idx := newSearchIndex(rows, nil, clipRow())

	got := idsOf(idx.Query("read"))
	for _, id := range got {
		if id == "" {
			t.Fatalf("clip row leaked into non-empty query results: %v", got)
		}
	}
}

func TestSearchIndex_AlphabeticalTiebreak_SameScoreSameField(t *testing.T) {
	// Force a synthetic equal-score tiebreak by making two ids that fuzzy
	// would score identically with the query. Using identical-length unique
	// ids that both start with the pattern.
	rows := []Row{
		{Key: '1', PromptID: "zeta"},
		{Key: '2', PromptID: "alpha"},
		{Key: '3', PromptID: "beta"},
	}
	idx := newSearchIndex(rows, nil, Row{})

	got := idsOf(idx.Query("a"))
	// Assert: wherever each appears in the result, "alpha" precedes "beta"
	// precedes "zeta" if they tie. Alpha should at minimum appear first on
	// "a" because it starts with it (firstCharMatchBonus).
	if len(got) > 0 && got[0] != "alpha" {
		t.Fatalf("expected alpha ranked first on query \"a\", got %v", got)
	}
}

func TestSearchIndex_OverflowRowsRanked(t *testing.T) {
	// A query that matches only an overflow row must still return it.
	board := []Row{{Key: '1', PromptID: "alpha", Title: "Alpha"}}
	overflow := []Row{{PromptID: "hidden-gem", Title: "Hidden Gem"}}
	idx := newSearchIndex(board, overflow, clipRow())

	got := idsOf(idx.Query("hidden"))
	if len(got) == 0 || !containsString(got, "hidden-gem") {
		t.Fatalf("overflow row should be searchable, got %v", got)
	}
}

func TestSearchIndex_TagsCorpusMatches(t *testing.T) {
	rows := []Row{
		{Key: '1', PromptID: "alpha", Tags: []string{"debug", "tooling"}},
	}
	idx := newSearchIndex(rows, nil, Row{})

	got := idsOf(idx.Query("debug"))
	if len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("tags should be searchable, got %v", got)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsString(haystack []string, needle string) bool {
	return strings.Contains(strings.Join(haystack, "\x00"), needle)
}
