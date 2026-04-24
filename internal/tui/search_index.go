package tui

import (
	"sort"
	"strings"

	"github.com/sahilm/fuzzy"
)

// MatchedRow is one entry in a search result: a Row plus its weighted score.
// Empty-query catalog entries carry Score = 0 — the Score field is only
// meaningful when comparing results within a single non-empty query.
type MatchedRow struct {
	Row   Row
	Score float64
}

const (
	weightID          = 1.0
	weightTitle       = 0.75
	weightDescription = 0.5
	weightTags        = 0.25
)

// SearchIndex fuzzy-matches prompt rows across four per-field corpuses with
// weighted scoring. All sahilm/fuzzy coupling lives in this file; callers see
// only []MatchedRow.
type SearchIndex struct {
	rows    []Row
	hasClip bool

	ids          []string
	titles       []string
	descriptions []string
	tagsText     []string

	// alphabeticalOrder lists indices of non-clip rows sorted by PromptID.
	// Used for the empty-query catalog and as the tiebreak order for equal
	// weighted scores.
	alphabeticalOrder []int
}

// newSearchIndex builds the indexer. rows is the board rows (clip row not
// included); overflow is the hidden overflow rows; clipRow is the pinned
// clipboard row if one exists (identified by Key != 0). The clip row appears
// only in the empty-query catalog — it is omitted from non-empty query
// results because it has no searchable content.
func newSearchIndex(rows []Row, overflow []Row, clipRow Row) *SearchIndex {
	hasClip := clipRow.Key != 0

	capacity := len(rows) + len(overflow)
	if hasClip {
		capacity++
	}
	all := make([]Row, 0, capacity)
	if hasClip {
		all = append(all, clipRow)
	}
	all = append(all, rows...)
	all = append(all, overflow...)

	idx := &SearchIndex{
		rows:         all,
		hasClip:      hasClip,
		ids:          make([]string, len(all)),
		titles:       make([]string, len(all)),
		descriptions: make([]string, len(all)),
		tagsText:     make([]string, len(all)),
	}
	for i, r := range all {
		idx.ids[i] = r.PromptID
		idx.titles[i] = r.Title
		idx.descriptions[i] = r.Description
		idx.tagsText[i] = strings.Join(r.Tags, " ")
	}

	nonClip := make([]int, 0, len(all))
	for i, r := range all {
		if r.PromptID == "" {
			continue
		}
		nonClip = append(nonClip, i)
	}
	sort.Slice(nonClip, func(a, b int) bool {
		return all[nonClip[a]].PromptID < all[nonClip[b]].PromptID
	})
	idx.alphabeticalOrder = nonClip

	return idx
}

// Query returns matched rows sorted by weighted score descending, with an
// alphabetical-by-id tiebreak. An empty query returns the full catalog
// alphabetically with the clipboard row first.
func (s *SearchIndex) Query(q string) []MatchedRow {
	if q == "" {
		return s.catalog()
	}
	return s.ranked(q)
}

func (s *SearchIndex) catalog() []MatchedRow {
	out := make([]MatchedRow, 0, len(s.rows))
	if s.hasClip {
		out = append(out, MatchedRow{Row: s.rows[0]})
	}
	for _, idx := range s.alphabeticalOrder {
		out = append(out, MatchedRow{Row: s.rows[idx]})
	}
	return out
}

func (s *SearchIndex) ranked(q string) []MatchedRow {
	scores := make(map[int]float64)
	accumulate := func(corpus []string, weight float64) {
		for _, m := range fuzzy.Find(q, corpus) {
			if s.rows[m.Index].PromptID == "" {
				// The clip row has no searchable content, but guard in case a
				// future caller passes a clip row with a populated field.
				continue
			}
			scores[m.Index] += float64(m.Score) * weight
		}
	}
	accumulate(s.ids, weightID)
	accumulate(s.titles, weightTitle)
	accumulate(s.descriptions, weightDescription)
	accumulate(s.tagsText, weightTags)

	if len(scores) == 0 {
		return []MatchedRow{}
	}
	out := make([]MatchedRow, 0, len(scores))
	for idx, score := range scores {
		out = append(out, MatchedRow{Row: s.rows[idx], Score: score})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Score != out[b].Score {
			return out[a].Score > out[b].Score
		}
		return out[a].Row.PromptID < out[b].Row.PromptID
	})
	return out
}
