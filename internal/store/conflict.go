package store

import (
	"sort"

	"github.com/hsadler/tprompt/internal/promptsource"
)

type ConflictPolicy string

const (
	ConflictPolicyGlobal  ConflictPolicy = "global"
	ConflictPolicyProject ConflictPolicy = "project"
)

type sourcePrompts struct {
	Source  promptsource.Source
	Prompts []discoveredPrompt
}

type conflictResolution struct {
	Winners map[string]discoveredPrompt
	Shadows []discoveredPrompt
}

func resolveConflicts(sources []sourcePrompts, policy ConflictPolicy) (conflictResolution, error) {
	if policy == "" {
		policy = ConflictPolicyGlobal
	}
	byID := make(map[string][]discoveredPrompt)
	for _, source := range sources {
		for _, prompt := range source.Prompts {
			byID[prompt.prompt.ID] = append(byID[prompt.prompt.ID], prompt)
		}
	}

	ids := make([]string, 0, len(byID))
	for id, prompts := range byID {
		if duplicateWithinTier(prompts) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > 0 {
		id := ids[0]
		paths := duplicateTierPaths(byID[id])
		return conflictResolution{}, &DuplicatePromptIDError{ID: id, Paths: paths}
	}

	winners := make(map[string]discoveredPrompt, len(byID))
	var shadows []discoveredPrompt
	for id, prompts := range byID {
		if len(prompts) == 0 {
			continue
		}
		winner, losers := chooseWinner(prompts, policy)
		winners[id] = winner
		shadows = append(shadows, losers...)
	}
	return conflictResolution{Winners: winners, Shadows: shadows}, nil
}

func duplicateTierPaths(prompts []discoveredPrompt) []string {
	pathsByScope := map[promptsource.Scope][]string{}
	for _, prompt := range prompts {
		scope := promptsource.Scope(prompt.prompt.Scope)
		if scope == "" {
			scope = promptsource.ScopeGlobal
		}
		pathsByScope[scope] = append(pathsByScope[scope], prompt.prompt.Path)
	}
	scopes := make([]string, 0, len(pathsByScope))
	for scope, paths := range pathsByScope {
		if len(paths) > 1 {
			scopes = append(scopes, string(scope))
		}
	}
	sort.Strings(scopes)
	if len(scopes) == 0 {
		return nil
	}
	paths := pathsByScope[promptsource.Scope(scopes[0])]
	sort.Strings(paths)
	return paths
}

func duplicateWithinTier(prompts []discoveredPrompt) bool {
	countByScope := map[promptsource.Scope]int{}
	for _, prompt := range prompts {
		scope := promptsource.Scope(prompt.prompt.Scope)
		if scope == "" {
			scope = promptsource.ScopeGlobal
		}
		countByScope[scope]++
		if countByScope[scope] > 1 {
			return true
		}
	}
	return false
}

func chooseWinner(prompts []discoveredPrompt, policy ConflictPolicy) (discoveredPrompt, []discoveredPrompt) {
	if len(prompts) == 1 {
		return prompts[0], nil
	}

	wantScope := promptsource.ScopeGlobal
	if policy == ConflictPolicyProject {
		wantScope = promptsource.ScopeProject
	}

	winnerIdx := 0
	for i, prompt := range prompts {
		if promptsource.Scope(prompt.prompt.Scope) == wantScope {
			winnerIdx = i
			break
		}
	}

	losers := make([]discoveredPrompt, 0, len(prompts)-1)
	for i, prompt := range prompts {
		if i == winnerIdx {
			continue
		}
		losers = append(losers, prompt)
	}
	return prompts[winnerIdx], losers
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
