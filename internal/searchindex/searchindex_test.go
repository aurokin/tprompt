package searchindex

import "testing"

// rec is a sample caller record, proving the index is generic over an arbitrary
// item type adapted via fields/tieKey rather than coupled to any one domain row.
type rec struct {
	id    string
	title string
	desc  string
	tags  []string
}

func recFields(r rec) Fields {
	return Fields{ID: r.id, Title: r.title, Description: r.desc, Tags: r.tags}
}

func recTieKey(r rec) string { return r.id }

func idsOf[T any](matches []Match[T], id func(T) string) []string {
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = id(m.Item)
	}
	return out
}

func recIDs(matches []Match[rec]) []string {
	return idsOf(matches, func(r rec) string { return r.id })
}

func equalStrings(a, b []string) bool {
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

func TestIndex_EmptyQueryReturnsCatalogInTieKeyOrder(t *testing.T) {
	items := []rec{{id: "mango"}, {id: "alpha"}, {id: "code-review"}}
	ix := New(items, recFields, recTieKey)

	got := recIDs(ix.Query(""))
	want := []string{"alpha", "code-review", "mango"}
	if !equalStrings(got, want) {
		t.Fatalf("catalog order = %v, want %v", got, want)
	}
}

func TestIndex_NoMatchReturnsEmpty(t *testing.T) {
	items := []rec{
		{id: "alpha", title: "Alpha", desc: "first"},
		{id: "beta", title: "Beta", desc: "second"},
	}
	ix := New(items, recFields, recTieKey)

	got := ix.Query("xyzxyz")
	if len(got) != 0 {
		t.Fatalf("expected no matches, got %v", recIDs(got))
	}
}

func TestIndex_IDMatchOutranksTitleMatch(t *testing.T) {
	// "foo" matches one row's id and another's title with identical raw fuzzy
	// scores; the id match wins on weight (1.0 vs 0.75).
	items := []rec{
		{id: "foo", title: "alpha", desc: "first"},
		{id: "bravo", title: "foo", desc: "second"},
	}
	ix := New(items, recFields, recTieKey)

	got := recIDs(ix.Query("foo"))
	if len(got) < 2 || got[0] != "foo" {
		t.Fatalf("expected id-match row first, got %v", got)
	}
}

func TestIndex_TitleOutranksDescription(t *testing.T) {
	items := []rec{
		{id: "alpha", title: "kubernetes", desc: "first"},
		{id: "bravo", title: "Bravo", desc: "kubernetes"},
	}
	ix := New(items, recFields, recTieKey)

	got := recIDs(ix.Query("kubernetes"))
	if len(got) < 2 || got[0] != "alpha" {
		t.Fatalf("expected title-match row first, got %v", got)
	}
}

func TestIndex_DescriptionOutranksTags(t *testing.T) {
	items := []rec{
		{id: "alpha", title: "A", desc: "kubernetes", tags: []string{"other"}},
		{id: "bravo", title: "B", desc: "other", tags: []string{"kubernetes"}},
	}
	ix := New(items, recFields, recTieKey)

	got := recIDs(ix.Query("kubernetes"))
	if len(got) < 2 || got[0] != "alpha" {
		t.Fatalf("expected description-match row first, got %v", got)
	}
}

func TestIndex_MultiFieldMatchOutranksSingleField(t *testing.T) {
	// Both rows match "zed" on id with the same score; the first also matches
	// on title, so its accumulated score is higher at the same best priority.
	items := []rec{
		{id: "zed", title: "zed", desc: "alpha"},
		{id: "zed-only", title: "other", desc: "alpha"},
	}
	ix := New(items, recFields, recTieKey)

	got := recIDs(ix.Query("zed"))
	if len(got) < 2 || got[0] != "zed" {
		t.Fatalf("expected multi-field row first, got %v", got)
	}
}

func TestIndex_EqualScoreTiebreakByTieKey(t *testing.T) {
	// Identical title match on both rows → identical score and priority, so the
	// tieKey (id) decides: "a-id" before "b-id".
	items := []rec{
		{id: "b-id", title: "kubernetes"},
		{id: "a-id", title: "kubernetes"},
	}
	ix := New(items, recFields, recTieKey)

	got := recIDs(ix.Query("kubernetes"))
	want := []string{"a-id", "b-id"}
	if !equalStrings(got, want) {
		t.Fatalf("tiebreak order = %v, want %v", got, want)
	}
}

func TestIndex_TagsCorpusMatches(t *testing.T) {
	items := []rec{{id: "alpha", tags: []string{"debug", "tooling"}}}
	ix := New(items, recFields, recTieKey)

	got := recIDs(ix.Query("debug"))
	if len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("tags should be searchable, got %v", got)
	}
}

// A second, structurally different item type exercises the generic seam beyond
// rec, confirming the index carries the caller's own type back out in Match.
func TestIndex_GenericOverArbitraryType(t *testing.T) {
	type doc struct {
		slug string
		name string
	}
	items := []doc{
		{slug: "readme", name: "Read Me"},
		{slug: "guide", name: "Guide"},
	}
	ix := New(items,
		func(d doc) Fields { return Fields{ID: d.slug, Title: d.name} },
		func(d doc) string { return d.slug },
	)

	got := ix.Query("guide")
	if len(got) != 1 || got[0].Item.slug != "guide" {
		t.Fatalf("expected the guide doc, got %+v", got)
	}
}
