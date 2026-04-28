package store

import (
	"sort"

	"github.com/hsadler/tprompt/internal/promptsource"
)

// ConflictPolicy is intentionally small in this slice: AUR-147 has only the
// global tier, where duplicate ids across sources are configuration errors.
// AUR-148 extends this policy for project/global winner selection.
type ConflictPolicy string

const (
	ConflictPolicyGlobalOnly ConflictPolicy = "global-only"
)

type sourcePrompts struct {
	Source  promptsource.Source
	Prompts []discoveredPrompt
}

type conflictResolution struct {
	Winners map[string]discoveredPrompt
	Shadows []discoveredPrompt
}

func resolveConflicts(sources []sourcePrompts, _ ConflictPolicy) (conflictResolution, error) {
	byID := make(map[string][]discoveredPrompt)
	for _, source := range sources {
		for _, prompt := range source.Prompts {
			byID[prompt.prompt.ID] = append(byID[prompt.prompt.ID], prompt)
		}
	}

	ids := make([]string, 0, len(byID))
	for id, prompts := range byID {
		if len(prompts) > 1 {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > 0 {
		id := ids[0]
		paths := make([]string, 0, len(byID[id]))
		for _, prompt := range byID[id] {
			paths = append(paths, prompt.prompt.Path)
		}
		sort.Strings(paths)
		return conflictResolution{}, &DuplicatePromptIDError{ID: id, Paths: paths}
	}

	winners := make(map[string]discoveredPrompt, len(byID))
	for id, prompts := range byID {
		if len(prompts) == 0 {
			continue
		}
		winners[id] = prompts[0]
	}
	return conflictResolution{Winners: winners}, nil
}

func sortedWinnerEntries(winners map[string]discoveredPrompt) []discoveredPrompt {
	ids := make([]string, 0, len(winners))
	for id := range winners {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]discoveredPrompt, 0, len(ids))
	for _, id := range ids {
		out = append(out, winners[id])
	}
	return out
}
