// Package searchindex is a dependency-free fuzzy search core shared by the board
// TUI (internal/tui) and the import picker (internal/importtui). It fuzzy-matches
// items across four weighted per-field corpuses (id, title, description, tags) and
// ranks them by best-matched-field priority, then weighted score, then a caller
// supplied stable tiebreak.
//
// It is generic over the caller's item type: a caller adapts its row to the four
// search Fields and a tieKey via the functions passed to New, and gets its own typed
// items back in Match. The package knows nothing about the board's clip row, Scope,
// or the import picker's conflicts — those domain concerns live in the callers'
// adapters. All sahilm/fuzzy coupling is confined here.
package searchindex

import (
	"sort"
	"strings"

	"github.com/sahilm/fuzzy"
)

// Fields are the four weighted search corpuses for one indexed item.
type Fields struct {
	ID          string
	Title       string
	Description string
	Tags        []string
}

// Match is one query result: the caller's item and its weighted score. Score is
// only meaningful when comparing results within a single non-empty query;
// empty-query catalog entries carry Score 0.
type Match[T any] struct {
	Item  T
	Score float64
}

const (
	weightID          = 1.0
	weightTitle       = 0.75
	weightDescription = 0.5
	weightTags        = 0.25
)

const (
	priorityID = iota
	priorityTitle
	priorityDescription
	priorityTags
)

type matchRank struct {
	score        float64
	bestPriority int
}

// Index fuzzy-matches items across the four weighted corpuses. Build it with New;
// query it with Query. It is immutable after construction.
type Index[T any] struct {
	items        []T
	ids          []string
	titles       []string
	descriptions []string
	tagsText     []string
	tieKeys      []string
	order        []int // item indices sorted by tieKey: the empty-query catalog order
}

// New builds an index over items. fields extracts the four search corpuses for an
// item; tieKey returns a stable per-item ordering key used both for the empty-query
// catalog and as the deterministic tiebreak for equal-priority, equal-score matches
// (so results never depend on map iteration order). The tieKey should be unique per
// item where a stable total order matters.
func New[T any](items []T, fields func(T) Fields, tieKey func(T) string) *Index[T] {
	idx := &Index[T]{
		items:        items,
		ids:          make([]string, len(items)),
		titles:       make([]string, len(items)),
		descriptions: make([]string, len(items)),
		tagsText:     make([]string, len(items)),
		tieKeys:      make([]string, len(items)),
	}
	for i, it := range items {
		f := fields(it)
		idx.ids[i] = f.ID
		idx.titles[i] = f.Title
		idx.descriptions[i] = f.Description
		idx.tagsText[i] = strings.Join(f.Tags, " ")
		idx.tieKeys[i] = tieKey(it)
	}
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return idx.tieKeys[order[a]] < idx.tieKeys[order[b]]
	})
	idx.order = order
	return idx
}

// Query returns matched items sorted by best-matched-field priority first, then
// weighted score descending, with a tieKey tiebreak. An empty query returns the
// full catalog in tieKey order (Score 0).
func (ix *Index[T]) Query(q string) []Match[T] {
	if q == "" {
		return ix.catalog()
	}
	return ix.ranked(q)
}

func (ix *Index[T]) catalog() []Match[T] {
	out := make([]Match[T], 0, len(ix.items))
	for _, i := range ix.order {
		out = append(out, Match[T]{Item: ix.items[i]})
	}
	return out
}

func (ix *Index[T]) ranked(q string) []Match[T] {
	ranks := make(map[int]matchRank)
	ix.accumulate(q, ix.ids, weightID, priorityID, ranks)
	ix.accumulate(q, ix.titles, weightTitle, priorityTitle, ranks)
	ix.accumulate(q, ix.descriptions, weightDescription, priorityDescription, ranks)
	ix.accumulate(q, ix.tagsText, weightTags, priorityTags, ranks)
	if len(ranks) == 0 {
		return []Match[T]{}
	}

	idxs := make([]int, 0, len(ranks))
	for i := range ranks {
		idxs = append(idxs, i)
	}
	sort.SliceStable(idxs, func(a, b int) bool {
		ra, rb := ranks[idxs[a]], ranks[idxs[b]]
		if ra.bestPriority != rb.bestPriority {
			return ra.bestPriority < rb.bestPriority
		}
		if ra.score != rb.score {
			return ra.score > rb.score
		}
		return ix.tieKeys[idxs[a]] < ix.tieKeys[idxs[b]]
	})
	out := make([]Match[T], 0, len(idxs))
	for _, i := range idxs {
		out = append(out, Match[T]{Item: ix.items[i], Score: ranks[i].score})
	}
	return out
}

func (ix *Index[T]) accumulate(q string, corpus []string, weight float64, priority int, ranks map[int]matchRank) {
	for _, m := range fuzzy.Find(q, corpus) {
		rank, ok := ranks[m.Index]
		if !ok || priority < rank.bestPriority {
			rank.bestPriority = priority
		}
		rank.score += float64(m.Score) * weight
		ranks[m.Index] = rank
	}
}
