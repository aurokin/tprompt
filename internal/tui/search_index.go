package tui

import "github.com/hsadler/tprompt/internal/searchindex"

// MatchedRow is one entry in a search result: a Row plus its weighted score.
// Empty-query catalog entries carry Score = 0 — the Score field is only
// meaningful when comparing results within a single non-empty query.
type MatchedRow struct {
	Row   Row
	Score float64
}

// SearchIndex fuzzy-matches prompt rows. The scoring core lives in the shared,
// dependency-free internal/searchindex package; this type is a thin board-aware
// adapter that owns the one concern the generic core does not know about: the
// clipboard row, which is pinned first in the empty-query catalog and excluded
// from non-empty query results (it has no searchable content).
type SearchIndex struct {
	core    *searchindex.Index[Row]
	clipRow Row
	hasClip bool
}

// rowFields adapts a board Row to the four weighted search corpuses.
func rowFields(r Row) searchindex.Fields {
	return searchindex.Fields{
		ID:          r.PromptID,
		Title:       r.Title,
		Description: r.Description,
		Tags:        r.Tags,
	}
}

// rowTieKey is the stable catalog/tiebreak key. PromptID + "\x00" + Scope
// reproduces the exact (PromptID, Scope) ordering the board has always used:
// "\x00" sorts below any rune, so a shorter id prefix sorts first and same-id
// rows order by Scope.
func rowTieKey(r Row) string {
	return r.PromptID + "\x00" + r.Scope
}

// newSearchIndex builds the indexer. rows is the board rows (clip row not
// included); overflow is the hidden overflow rows; clipRow is the clipboard
// row if one exists. The clip row appears only in the empty-query catalog — it
// is omitted from non-empty query results because it has no searchable content.
func newSearchIndex(rows []Row, overflow []Row, clipRow Row) *SearchIndex {
	hasClip := clipRow.PromptID == "" &&
		(clipRow.Key != 0 || clipRow.Title != "" || clipRow.Description != "" || len(clipRow.Tags) > 0)

	indexed := make([]Row, 0, len(rows)+len(overflow))
	for _, r := range rows {
		if r.PromptID != "" {
			indexed = append(indexed, r)
		}
	}
	for _, r := range overflow {
		if r.PromptID != "" {
			indexed = append(indexed, r)
		}
	}

	return &SearchIndex{
		core:    searchindex.New(indexed, rowFields, rowTieKey),
		clipRow: clipRow,
		hasClip: hasClip,
	}
}

// Query returns matched rows sorted by best matched field priority first, then
// weighted score descending, with an alphabetical-by-id tiebreak. An empty
// query returns the full catalog alphabetically with the clipboard row first.
func (s *SearchIndex) Query(q string) []MatchedRow {
	if q == "" {
		return s.catalog()
	}
	matches := s.core.Query(q)
	out := make([]MatchedRow, 0, len(matches))
	for _, m := range matches {
		out = append(out, MatchedRow{Row: m.Item, Score: m.Score})
	}
	return out
}

func (s *SearchIndex) catalog() []MatchedRow {
	matches := s.core.Query("")
	out := make([]MatchedRow, 0, len(matches)+1)
	if s.hasClip {
		out = append(out, MatchedRow{Row: s.clipRow})
	}
	for _, m := range matches {
		out = append(out, MatchedRow{Row: m.Item})
	}
	return out
}
