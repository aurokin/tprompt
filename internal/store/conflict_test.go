package store

import (
	"errors"
	"sort"
	"testing"

	"github.com/aurokin/tprompt/internal/promptsource"
	"github.com/google/go-cmp/cmp"
)

func TestResolveConflictsGlobalOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		sources     []sourcePrompts
		wantWinners map[string]string
		wantShadows []string
	}{
		{
			name: "winners include prompts from all global sources",
			sources: []sourcePrompts{
				{
					Source: promptsource.Source{Path: "/primary", Scope: promptsource.ScopeGlobal},
					Prompts: []discoveredPrompt{
						conflictPrompt("bravo", "/primary/bravo.md"),
					},
				},
				{
					Source: promptsource.Source{Path: "/team", Scope: promptsource.ScopeGlobal, Optional: true},
					Prompts: []discoveredPrompt{
						conflictPrompt("alpha", "/team/alpha.md"),
					},
				},
			},
			wantWinners: map[string]string{
				"alpha": "/team/alpha.md",
				"bravo": "/primary/bravo.md",
			},
			wantShadows: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveConflicts(tc.sources, ConflictPolicyGlobal)
			if err != nil {
				t.Fatalf("resolveConflicts: %v", err)
			}
			if diff := cmp.Diff(tc.wantWinners, winnerPathsByID(got.Winners)); diff != "" {
				t.Fatalf("winners mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantShadows, shadowPaths(got.Shadows)); diff != "" {
				t.Fatalf("shadows mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResolveConflictsRejectsCrossGlobalDuplicates(t *testing.T) {
	t.Parallel()

	_, err := resolveConflicts([]sourcePrompts{
		{
			Source: promptsource.Source{Path: "/primary", Scope: promptsource.ScopeGlobal},
			Prompts: []discoveredPrompt{
				conflictPrompt("alpha", "/primary/alpha.md"),
			},
		},
		{
			Source: promptsource.Source{Path: "/team", Scope: promptsource.ScopeGlobal, Optional: true},
			Prompts: []discoveredPrompt{
				conflictPrompt("alpha", "/team/alpha.md"),
			},
		},
	}, ConflictPolicyGlobal)

	var dupErr *DuplicatePromptIDError
	if !errors.As(err, &dupErr) {
		t.Fatalf("want DuplicatePromptIDError, got %T: %v", err, err)
	}
	if dupErr.ID != "alpha" {
		t.Fatalf("ID = %q, want alpha", dupErr.ID)
	}
	wantPaths := []string{"/primary/alpha.md", "/team/alpha.md"}
	if diff := cmp.Diff(wantPaths, dupErr.Paths); diff != "" {
		t.Fatalf("Paths mismatch (-want +got):\n%s", diff)
	}
}

func conflictPrompt(id, path string) discoveredPrompt {
	return discoveredPrompt{
		prompt: Prompt{
			Summary: Summary{
				ID:    id,
				Path:  path,
				Scope: "global",
			},
		},
	}
}

func TestResolveConflictsCrossTierPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		policy      ConflictPolicy
		wantWinner  string
		wantShadows []string
	}{
		{
			name:        "global wins by default",
			policy:      ConflictPolicyGlobal,
			wantWinner:  "/global/alpha.md",
			wantShadows: []string{"/project/alpha.md"},
		},
		{
			name:        "project wins when configured",
			policy:      ConflictPolicyProject,
			wantWinner:  "/project/alpha.md",
			wantShadows: []string{"/global/alpha.md"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveConflicts([]sourcePrompts{
				{
					Source: promptsource.Source{Path: "/global", Scope: promptsource.ScopeGlobal},
					Prompts: []discoveredPrompt{
						conflictPromptWithScope("alpha", "/global/alpha.md", "global"),
					},
				},
				{
					Source: promptsource.Source{Path: "/project", Scope: promptsource.ScopeProject},
					Prompts: []discoveredPrompt{
						conflictPromptWithScope("alpha", "/project/alpha.md", "project"),
					},
				},
			}, tc.policy)
			if err != nil {
				t.Fatalf("resolveConflicts: %v", err)
			}
			if got.Winners["alpha"].prompt.Path != tc.wantWinner {
				t.Fatalf("winner = %q, want %q", got.Winners["alpha"].prompt.Path, tc.wantWinner)
			}
			if diff := cmp.Diff(tc.wantShadows, shadowPaths(got.Shadows)); diff != "" {
				t.Fatalf("shadows mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func conflictPromptWithScope(id, path, scope string) discoveredPrompt {
	prompt := conflictPrompt(id, path)
	prompt.prompt.Scope = scope
	return prompt
}

func winnerPathsByID(winners map[string]discoveredPrompt) map[string]string {
	out := make(map[string]string, len(winners))
	for id, prompt := range winners {
		out[id] = prompt.prompt.Path
	}
	return out
}

func shadowPaths(shadows []discoveredPrompt) []string {
	if len(shadows) == 0 {
		return nil
	}
	out := make([]string, 0, len(shadows))
	for _, prompt := range shadows {
		out = append(out, prompt.prompt.Path)
	}
	return out
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
