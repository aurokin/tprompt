package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aurokin/tprompt/internal/keybind"
	"github.com/aurokin/tprompt/internal/promptsource"
	"github.com/aurokin/tprompt/internal/prompttmpl"
	"github.com/google/go-cmp/cmp"
)

func TestStoreInterfaceShape(t *testing.T) {
	var _ Store = (*FSStore)(nil)
}

func TestPromptCarriesDeliveryDefaults(t *testing.T) {
	enter := false
	prompt := Prompt{
		Summary: Summary{ID: "code-review"},
		Body:    "body",
		Defaults: DeliveryDefaults{
			Mode:  "paste",
			Enter: &enter,
		},
	}

	if prompt.Defaults.Mode != "paste" {
		t.Fatalf("Defaults.Mode = %q, want %q", prompt.Defaults.Mode, "paste")
	}
	if prompt.Defaults.Enter == nil || *prompt.Defaults.Enter != enter {
		t.Fatalf("Defaults.Enter = %v, want %v", prompt.Defaults.Enter, enter)
	}
}

func TestFSStoreDiscoversPromptsRecursivelyAndResolvesKeys(t *testing.T) {
	dir := t.TempDir()
	writePrompt(t, dir, "code-review.md", `---
title: Code Review
description: Deep review
tags: [review, code]
key: C
mode: paste
enter: false
---

Review this code.
`)
	writePrompt(t, dir, filepath.Join("nested", "deep-review.md"), `---
title: Deep Review
description: Multi-pass review
---

Go deeper.
`)
	writePrompt(t, dir, "notes.txt", "ignore me")
	writePrompt(t, dir, filepath.Join(".hidden", "ignored.md"), "hidden")
	writePrompt(t, dir, ".hidden-root.md", "hidden")

	store := NewFS(dir, map[rune]string{'p': "clipboard"}, []rune("1c2"))
	summaries, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	wantSummaries := []Summary{
		{
			ID:          "code-review",
			Title:       "Code Review",
			Description: "Deep review",
			Tags:        []string{"review", "code"},
			Key:         "c",
			KeySource:   KeySourceExplicit,
			Path:        filepath.Join(dir, "code-review.md"),
			Scope:       "global",
		},
		{
			ID:          "deep-review",
			Title:       "Deep Review",
			Description: "Multi-pass review",
			Key:         "1",
			KeySource:   KeySourceAuto,
			Path:        filepath.Join(dir, "nested", "deep-review.md"),
			Scope:       "global",
		},
	}
	if diff := cmp.Diff(wantSummaries, summaries); diff != "" {
		t.Fatalf("List() mismatch (-want +got):\n%s", diff)
	}

	prompt, err := store.Resolve("code-review")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if prompt.Body != "Review this code." {
		t.Fatalf("Body = %q, want %q", prompt.Body, "Review this code.")
	}
	if prompt.Defaults.Mode != "paste" {
		t.Fatalf("Defaults.Mode = %q, want %q", prompt.Defaults.Mode, "paste")
	}
	if prompt.Defaults.Enter == nil || *prompt.Defaults.Enter {
		t.Fatalf("Defaults.Enter = %v, want false", prompt.Defaults.Enter)
	}
}

func TestFSStoreDetectsDuplicatePromptIDs(t *testing.T) {
	dir := t.TempDir()
	writePrompt(t, dir, filepath.Join("one", "code-review.md"), "one\n")
	writePrompt(t, dir, filepath.Join("two", "code-review.md"), "two\n")

	store := NewFS(dir, nil, []rune("123"))
	err := store.Discover()
	if err == nil {
		t.Fatal("want error, got nil")
	}

	var dupErr *DuplicatePromptIDError
	if !errors.As(err, &dupErr) {
		t.Fatalf("want DuplicatePromptIDError, got %T", err)
	}
	if dupErr.ID != "code-review" {
		t.Fatalf("ID = %q, want %q", dupErr.ID, "code-review")
	}
}

func TestDuplicatePromptIDErrorNamesAllPaths(t *testing.T) {
	err := &DuplicatePromptIDError{
		ID:    "code-review",
		Paths: []string{"/prompts/agents/code-review.md", "/prompts/reviews/code-review.md"},
	}
	want := "duplicate prompt ID detected: code-review\n" +
		"- /prompts/agents/code-review.md\n" +
		"- /prompts/reviews/code-review.md\n" +
		"prompt IDs are filename stems and must be unique; rename one file to resolve"
	if got := err.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestFSStoreDiscoversThroughSymlinkedRoot(t *testing.T) {
	target := t.TempDir()
	writePrompt(t, target, "alpha.md", "body\n")
	link := filepath.Join(t.TempDir(), "prompts")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink(): %v", err)
	}

	store := NewFS(link, nil, []rune("1"))
	if err := store.Discover(); err != nil {
		t.Fatalf("Discover(): %v", err)
	}
	summaries, err := store.List()
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != "alpha" {
		t.Fatalf("summaries = %+v, want one prompt with ID alpha", summaries)
	}
}

func TestFSStoreRejectsSymlinkedRootToFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.md")
	if err := os.WriteFile(target, []byte("body\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	link := filepath.Join(dir, "prompts")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink(): %v", err)
	}

	store := NewFS(link, nil, []rune("1"))
	err := store.Discover()
	var missingErr *PromptsDirMissingError
	if !errors.As(err, &missingErr) {
		t.Fatalf("want PromptsDirMissingError, got %T: %v", err, err)
	}
}

func TestCountPromptFiles(t *testing.T) {
	dir := t.TempDir()
	writePrompt(t, dir, "alpha.md", "a\n")
	writePrompt(t, dir, filepath.Join("nested", "bravo.md"), "b\n")
	writePrompt(t, dir, filepath.Join(".hidden", "charlie.md"), "c\n")
	writePrompt(t, dir, "notes.txt", "not a prompt\n")

	count, err := CountPromptFiles(promptsource.Source{Path: dir, Scope: promptsource.ScopeGlobal})
	if err != nil {
		t.Fatalf("CountPromptFiles(): %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	missing, err := CountPromptFiles(promptsource.Source{
		Path:     filepath.Join(dir, "absent"),
		Scope:    promptsource.ScopeGlobal,
		Optional: true,
	})
	if err != nil {
		t.Fatalf("CountPromptFiles(missing optional): %v", err)
	}
	if missing != 0 {
		t.Fatalf("missing optional count = %d, want 0", missing)
	}
}

func TestFSStoreSurfacesKeybindValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		reserved map[rune]string
		files    map[string]string
		assert   func(*testing.T, error)
	}{
		{
			name: "duplicate",
			files: map[string]string{
				"alpha.md": "---\nkey: x\n---\na\n",
				"bravo.md": "---\nkey: X\n---\nb\n",
			},
			assert: func(t *testing.T, err error) {
				t.Helper()
				var dupErr *keybind.DuplicateKeybindError
				if !errors.As(err, &dupErr) {
					t.Fatalf("want DuplicateKeybindError, got %T", err)
				}
			},
		},
		{
			name:     "reserved",
			reserved: map[rune]string{'p': "clipboard"},
			files: map[string]string{
				"alpha.md": "---\nkey: P\n---\na\n",
			},
			assert: func(t *testing.T, err error) {
				t.Helper()
				var reservedErr *keybind.ReservedKeybindError
				if !errors.As(err, &reservedErr) {
					t.Fatalf("want ReservedKeybindError, got %T", err)
				}
			},
		},
		{
			name: "malformed",
			files: map[string]string{
				"alpha.md": "---\nkey: ctrl+x\n---\na\n",
			},
			assert: func(t *testing.T, err error) {
				t.Helper()
				var malformed *keybind.MalformedKeybindError
				if !errors.As(err, &malformed) {
					t.Fatalf("want MalformedKeybindError, got %T", err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range tc.files {
				writePrompt(t, dir, rel, content)
			}

			store := NewFS(dir, tc.reserved, []rune("123"))
			err := store.Discover()
			if err == nil {
				t.Fatal("want error, got nil")
			}
			tc.assert(t, err)
		})
	}
}

func TestFSStoreRejectsInvalidPromptMode(t *testing.T) {
	dir := t.TempDir()
	writePrompt(t, dir, "alpha.md", "---\nmode: turbo\n---\na\n")

	store := NewFS(dir, nil, []rune("123"))
	err := store.Discover()
	if err == nil {
		t.Fatal("want error, got nil")
	}

	var modeErr *InvalidPromptModeError
	if !errors.As(err, &modeErr) {
		t.Fatalf("want InvalidPromptModeError, got %T", err)
	}
	if modeErr.Value != "turbo" {
		t.Fatalf("Value = %q, want %q", modeErr.Value, "turbo")
	}
}

func TestFSStoreCarriesTemplateVariables(t *testing.T) {
	dir := t.TempDir()
	writePrompt(t, dir, "templated.md", `---
variables:
  - name: issue-id
    label: Issue
    required: true
  - name: focus
    default: correctness
---
Review {{issue-id}} for {{focus}}.
`)

	s := NewFS(dir, nil, []rune("123"))
	prompt, err := s.Resolve("templated")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := []prompttmpl.Variable{
		{Name: "issue-id", Label: "Issue", Required: true},
		{Name: "focus", Default: "correctness"},
	}
	if diff := cmp.Diff(want, prompt.Variables); diff != "" {
		t.Fatalf("Variables mismatch (-want +got):\n%s", diff)
	}
}

func TestFSStoreRejectsInvalidTemplate(t *testing.T) {
	tests := map[string]struct {
		body string
		want any
	}{
		"bad variable": {
			body: "---\nvariables:\n  - name: Issue\n---\nbody\n",
			want: &prompttmpl.InvalidVariableError{},
		},
		"unused variable": {
			body: "---\nvariables:\n  - name: issue\n  - name: focus\n---\n{{issue}}\n",
			want: &prompttmpl.InvalidVariableError{},
		},
		"unknown placeholder": {
			body: "---\nvariables:\n  - name: issue\n---\n{{focus}}\n",
			want: &prompttmpl.InvalidPlaceholderError{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writePrompt(t, dir, "alpha.md", tc.body)

			s := NewFS(dir, nil, []rune("123"))
			err := s.Discover()
			if err == nil {
				t.Fatal("want error, got nil")
			}
			switch tc.want.(type) {
			case *prompttmpl.InvalidVariableError:
				var got *prompttmpl.InvalidVariableError
				if !errors.As(err, &got) {
					t.Fatalf("err = %T: %v, want InvalidVariableError", err, err)
				}
			case *prompttmpl.InvalidPlaceholderError:
				var got *prompttmpl.InvalidPlaceholderError
				if !errors.As(err, &got) {
					t.Fatalf("err = %T: %v, want InvalidPlaceholderError", err, err)
				}
			}
		})
	}
}

func TestFSStoreTreatsEmptyOrNullKeysAsAbsent(t *testing.T) {
	tests := map[string]string{
		"implicit-null": "---\nkey:\n---\na\n",
		"explicit-null": "---\nkey: null\n---\na\n",
		"empty-string":  "---\nkey: \"\"\n---\na\n",
	}

	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writePrompt(t, dir, "alpha.md", content)

			store := NewFS(dir, nil, []rune("123"))
			summaries, err := store.List()
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(summaries) != 1 {
				t.Fatalf("len(List()) = %d, want 1", len(summaries))
			}
			if summaries[0].KeySource != KeySourceAuto {
				t.Fatalf("KeySource = %q, want %q", summaries[0].KeySource, KeySourceAuto)
			}
			if summaries[0].Key != "1" {
				t.Fatalf("Key = %q, want %q", summaries[0].Key, "1")
			}
		})
	}
}

func TestFSStoreLoadsFullyStubbedEmptyPrompt(t *testing.T) {
	dir := t.TempDir()
	writePrompt(t, dir, "stub.md", `---
title:
description:
tags: []
key:
mode:
enter:
---

Stubbed body.
`)

	store := NewFS(dir, nil, []rune("123"))
	prompt, err := store.Resolve("stub")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if prompt.Title != "" || prompt.Description != "" {
		t.Fatalf("display fields populated: %#v", prompt.Summary)
	}
	if len(prompt.Tags) != 0 {
		t.Fatalf("Tags = %#v, want empty", prompt.Tags)
	}
	if prompt.KeySource != KeySourceAuto {
		t.Fatalf("KeySource = %q, want %q", prompt.KeySource, KeySourceAuto)
	}
	if prompt.Key != "1" {
		t.Fatalf("Key = %q, want %q", prompt.Key, "1")
	}
	if prompt.Defaults.Mode != "" {
		t.Fatalf("Defaults.Mode = %q, want empty", prompt.Defaults.Mode)
	}
	if prompt.Defaults.Enter != nil {
		t.Fatalf("Defaults.Enter = %v, want nil", prompt.Defaults.Enter)
	}
	if prompt.Body != "Stubbed body." {
		t.Fatalf("Body = %q", prompt.Body)
	}
}

func TestFSStoreEmptyKeyDoesNotCollideWithExplicitKey(t *testing.T) {
	dir := t.TempDir()
	writePrompt(t, dir, "alpha.md", "---\nkey: c\n---\nbody\n")
	writePrompt(t, dir, "bravo.md", "---\nkey: \"\"\n---\nbody\n")

	store := NewFS(dir, nil, []rune("1c2"))
	summaries, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("len(List()) = %d, want 2", len(summaries))
	}

	byID := map[string]Summary{}
	for _, s := range summaries {
		byID[s.ID] = s
	}
	if byID["alpha"].KeySource != KeySourceExplicit || byID["alpha"].Key != "c" {
		t.Fatalf("alpha = %#v, want explicit c", byID["alpha"])
	}
	if byID["bravo"].KeySource != KeySourceAuto || byID["bravo"].Key != "1" {
		t.Fatalf("bravo = %#v, want auto 1", byID["bravo"])
	}
}

func TestFSStoreTreatsLeadingFenceWithoutClosingFenceAsBody(t *testing.T) {
	dir := t.TempDir()
	content := "---\nHeading below\n"
	writePrompt(t, dir, "rule.md", content)

	store := NewFS(dir, nil, []rune("123"))
	prompt, err := store.Resolve("rule")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := "---\nHeading below"
	if prompt.Body != want {
		t.Fatalf("Body = %q, want %q", prompt.Body, want)
	}
	if prompt.Title != "" || prompt.Description != "" || len(prompt.Tags) != 0 {
		t.Fatalf("Summary metadata = %#v, want zero values", prompt.Summary)
	}
}

func TestFSStoreResolveMissingPrompt(t *testing.T) {
	dir := t.TempDir()
	writePrompt(t, dir, "alpha.md", "body\n")

	store := NewFS(dir, nil, []rune("123"))
	_, err := store.Resolve("missing")
	if err == nil {
		t.Fatal("want error, got nil")
	}

	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("want NotFoundError, got %T", err)
	}
}

func TestFSStoreDiscoverReportsMissingRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	store := NewFS(missing, nil, []rune("123"))

	err := store.Discover()
	var missingErr *PromptsDirMissingError
	if !errors.As(err, &missingErr) {
		t.Fatalf("want PromptsDirMissingError, got %T: %v", err, err)
	}
}

func TestFSStoreAutoCreateMissingRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "fresh", "prompts")

	store := NewFSWithAutoCreate(missing, nil, []rune("123"))
	summaries, err := store.List()
	if err != nil {
		t.Fatalf("List on auto-create root: %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("List() = %d summaries, want 0", len(summaries))
	}
	info, err := os.Stat(missing)
	if err != nil {
		t.Fatalf("auto-created root not found: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("auto-created path is not a directory: %s", missing)
	}
}

func TestFSStoreAutoCreateLeavesExistingFilesAlone(t *testing.T) {
	dir := t.TempDir()
	writePrompt(t, dir, "alpha.md", "Alpha body.\n")

	store := NewFSWithAutoCreate(dir, nil, []rune("123"))
	summaries, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != "alpha" {
		t.Fatalf("List() = %v, want [alpha]", summaries)
	}
}

func TestFSStoreAutoCreateReportsCreateError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(blocker, "prompts")

	store := NewFSWithAutoCreate(root, nil, []rune("123"))
	err := store.Discover()
	var createErr *PromptsDirCreateError
	if !errors.As(err, &createErr) {
		t.Fatalf("want PromptsDirCreateError, got %T: %v", err, err)
	}
}

func TestFSStoreNewFSDoesNotAutoCreate(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "fresh")
	store := NewFS(missing, nil, []rune("123"))

	err := store.Discover()
	var missingErr *PromptsDirMissingError
	if !errors.As(err, &missingErr) {
		t.Fatalf("want PromptsDirMissingError, got %T: %v", err, err)
	}
	if _, statErr := os.Stat(missing); statErr == nil {
		t.Fatalf("NewFS unexpectedly created %s", missing)
	}
}

func TestFSStoreDiscoverReportsRootIsFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewFS(file, nil, []rune("123"))

	err := store.Discover()
	var missingErr *PromptsDirMissingError
	if !errors.As(err, &missingErr) {
		t.Fatalf("want PromptsDirMissingError, got %T: %v", err, err)
	}
}

func TestFSStoreTreatsNonMappingFenceAsBody(t *testing.T) {
	dir := t.TempDir()
	content := "---\nHeading\n---\nbody\n"
	writePrompt(t, dir, "rule.md", content)

	store := NewFS(dir, nil, []rune("123"))
	prompt, err := store.Resolve("rule")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := "---\nHeading\n---\nbody"
	if prompt.Body != want {
		t.Fatalf("Body = %q, want %q", prompt.Body, want)
	}
	if prompt.Title != "" || prompt.Description != "" || len(prompt.Tags) != 0 {
		t.Fatalf("Summary metadata = %#v, want zero values", prompt.Summary)
	}
}

func TestFSStoreResolveReturnsClonedPromptData(t *testing.T) {
	dir := t.TempDir()
	writePrompt(t, dir, "alpha.md", `---
title: Alpha
tags: [one, two]
enter: true
---
body
`)

	store := NewFS(dir, nil, []rune("123"))
	prompt, err := store.Resolve("alpha")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	prompt.Tags[0] = "mutated"
	if prompt.Defaults.Enter == nil {
		t.Fatal("Defaults.Enter = nil, want non-nil")
	}
	*prompt.Defaults.Enter = false

	again, err := store.Resolve("alpha")
	if err != nil {
		t.Fatalf("Resolve() second call: %v", err)
	}

	if diff := cmp.Diff([]string{"one", "two"}, again.Tags); diff != "" {
		t.Fatalf("Resolve() tags mutated (-want +got):\n%s", diff)
	}
	if again.Defaults.Enter == nil || !*again.Defaults.Enter {
		t.Fatalf("Resolve() Enter = %v, want true", again.Defaults.Enter)
	}
}

func TestFSStoreListReturnsClonedSummaryData(t *testing.T) {
	dir := t.TempDir()
	writePrompt(t, dir, "alpha.md", `---
title: Alpha
tags: [one, two]
---
body
`)

	store := NewFS(dir, nil, []rune("123"))
	summaries, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("len(List()) = %d, want 1", len(summaries))
	}

	summaries[0].Tags[0] = "mutated"

	again, err := store.List()
	if err != nil {
		t.Fatalf("List() second call: %v", err)
	}

	if diff := cmp.Diff([]string{"one", "two"}, again[0].Tags); diff != "" {
		t.Fatalf("List() tags mutated (-want +got):\n%s", diff)
	}
}

func TestFSStoreIncludesOverflowPromptsWithoutAssignedKeys(t *testing.T) {
	dir := t.TempDir()
	writePrompt(t, dir, "alpha.md", "alpha\n")
	writePrompt(t, dir, "bravo.md", "bravo\n")

	store := NewFS(dir, nil, []rune("1"))
	summaries, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	want := []Summary{
		{ID: "alpha", Key: "1", KeySource: KeySourceAuto, Path: filepath.Join(dir, "alpha.md"), Scope: "global"},
		{ID: "bravo", Key: "", KeySource: KeySourceOverflow, Path: filepath.Join(dir, "bravo.md"), Scope: "global"},
	}
	if diff := cmp.Diff(want, summaries); diff != "" {
		t.Fatalf("List() mismatch (-want +got):\n%s", diff)
	}
}

func TestMultiSourceStoreConcatenatesOrderedGlobalSources(t *testing.T) {
	primary := t.TempDir()
	additional := t.TempDir()
	writePrompt(t, primary, "bravo.md", "bravo\n")
	writePrompt(t, additional, "alpha.md", "alpha\n")

	store := NewMultiSource([]promptsource.Source{
		{Path: primary, Scope: promptsource.ScopeGlobal},
		{Path: additional, Scope: promptsource.ScopeGlobal, Optional: true},
	}, nil, []rune("12"))

	summaries, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	want := []Summary{
		{ID: "alpha", Key: "1", KeySource: KeySourceAuto, Path: filepath.Join(additional, "alpha.md"), Scope: "global"},
		{ID: "bravo", Key: "2", KeySource: KeySourceAuto, Path: filepath.Join(primary, "bravo.md"), Scope: "global"},
	}
	if diff := cmp.Diff(want, summaries); diff != "" {
		t.Fatalf("List() mismatch (-want +got):\n%s", diff)
	}
}

func TestMultiSourceStoreSkipsMissingOptionalSources(t *testing.T) {
	primary := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing")
	writePrompt(t, primary, "alpha.md", "alpha\n")

	store := NewMultiSource([]promptsource.Source{
		{Path: primary, Scope: promptsource.ScopeGlobal},
		{Path: missing, Scope: promptsource.ScopeGlobal, Optional: true},
	}, nil, []rune("1"))

	summaries, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != "alpha" {
		t.Fatalf("List() = %#v, want only alpha", summaries)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Fatalf("optional source was unexpectedly created: %s", missing)
	}
}

func TestMultiSourceStoreRejectsOptionalSourceThatIsNotDirectory(t *testing.T) {
	primary := t.TempDir()
	fileSource := filepath.Join(t.TempDir(), "team-prompts")
	writePrompt(t, primary, "alpha.md", "alpha\n")
	if err := os.WriteFile(fileSource, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewMultiSource([]promptsource.Source{
		{Path: primary, Scope: promptsource.ScopeGlobal},
		{Path: fileSource, Scope: promptsource.ScopeGlobal, Optional: true},
	}, nil, []rune("1"))

	err := store.Discover()
	var missingErr *PromptsDirMissingError
	if !errors.As(err, &missingErr) {
		t.Fatalf("want PromptsDirMissingError, got %T: %v", err, err)
	}
	if missingErr.Path != fileSource {
		t.Fatalf("Path = %q, want %q", missingErr.Path, fileSource)
	}
}

func TestMultiSourceStoreRejectsDuplicateIDsWithinOneSource(t *testing.T) {
	primary := t.TempDir()
	additional := t.TempDir()
	writePrompt(t, primary, "alpha.md", "primary\n")
	writePrompt(t, additional, filepath.Join("one", "bravo.md"), "one\n")
	writePrompt(t, additional, filepath.Join("two", "bravo.md"), "two\n")

	store := NewMultiSource([]promptsource.Source{
		{Path: primary, Scope: promptsource.ScopeGlobal},
		{Path: additional, Scope: promptsource.ScopeGlobal, Optional: true},
	}, nil, []rune("12"))

	err := store.Discover()
	var dupErr *DuplicatePromptIDError
	if !errors.As(err, &dupErr) {
		t.Fatalf("want DuplicatePromptIDError, got %T: %v", err, err)
	}
	if dupErr.ID != "bravo" {
		t.Fatalf("ID = %q, want bravo", dupErr.ID)
	}
	wantPaths := sortedStrings([]string{
		filepath.Join(additional, "one", "bravo.md"),
		filepath.Join(additional, "two", "bravo.md"),
	})
	if diff := cmp.Diff(wantPaths, sortedStrings(dupErr.Paths)); diff != "" {
		t.Fatalf("Paths mismatch (-want +got):\n%s", diff)
	}
}

func TestMultiSourceStoreRejectsCrossGlobalDuplicateIDs(t *testing.T) {
	primary := t.TempDir()
	additional := t.TempDir()
	third := t.TempDir()
	writePrompt(t, primary, "alpha.md", "primary\n")
	writePrompt(t, additional, "alpha.md", "additional\n")
	writePrompt(t, third, "alpha.md", "third\n")

	store := NewMultiSource([]promptsource.Source{
		{Path: primary, Scope: promptsource.ScopeGlobal},
		{Path: additional, Scope: promptsource.ScopeGlobal, Optional: true},
		{Path: third, Scope: promptsource.ScopeGlobal, Optional: true},
	}, nil, []rune("1"))

	err := store.Discover()
	var dupErr *DuplicatePromptIDError
	if !errors.As(err, &dupErr) {
		t.Fatalf("want DuplicatePromptIDError, got %T: %v", err, err)
	}
	if dupErr.ID != "alpha" {
		t.Fatalf("ID = %q, want alpha", dupErr.ID)
	}
	wantPaths := sortedStrings([]string{
		filepath.Join(additional, "alpha.md"),
		filepath.Join(primary, "alpha.md"),
		filepath.Join(third, "alpha.md"),
	})
	if diff := cmp.Diff(wantPaths, dupErr.Paths); diff != "" {
		t.Fatalf("Paths mismatch (-want +got):\n%s", diff)
	}
}

func TestMultiSourceStoreShadowsCrossTierDuplicateByPolicy(t *testing.T) {
	tests := []struct {
		name        string
		policy      ConflictPolicy
		wantWinner  string
		wantShadow  string
		wantScope   string
		shadowScope string
	}{
		{
			name:        "global wins",
			policy:      ConflictPolicyGlobal,
			wantWinner:  "global body",
			wantShadow:  "project body",
			wantScope:   "global",
			shadowScope: "project",
		},
		{
			name:        "project wins",
			policy:      ConflictPolicyProject,
			wantWinner:  "project body",
			wantShadow:  "global body",
			wantScope:   "project",
			shadowScope: "global",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			primary := t.TempDir()
			project := t.TempDir()
			writePrompt(t, primary, "alpha.md", "---\nkey: a\n---\nglobal body\n")
			writePrompt(t, project, "alpha.md", "---\nkey: a\n---\nproject body\n")

			store := NewMultiSourceWithPolicy([]promptsource.Source{
				{Path: primary, Scope: promptsource.ScopeGlobal},
				{Path: project, Scope: promptsource.ScopeProject},
			}, tc.policy, nil, []rune("12"))

			summaries, err := store.List()
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(summaries) != 2 {
				t.Fatalf("len(List()) = %d, want 2: %#v", len(summaries), summaries)
			}

			var winner, shadow Summary
			for _, summary := range summaries {
				if summary.Shadowed {
					shadow = summary
				} else {
					winner = summary
				}
			}
			if winner.Scope != tc.wantScope {
				t.Fatalf("winner scope = %q, want %q", winner.Scope, tc.wantScope)
			}
			if shadow.Scope != tc.shadowScope || !shadow.Shadowed {
				t.Fatalf("shadow = %#v, want scope %q shadowed", shadow, tc.shadowScope)
			}
			if winner.ShadowPath == "" || shadow.ShadowedBy == "" {
				t.Fatalf("missing shadow links: winner=%#v shadow=%#v", winner, shadow)
			}
			if winner.Key != "a" || winner.KeySource != KeySourceExplicit {
				t.Fatalf("winner key = %q/%q, want explicit a", winner.Key, winner.KeySource)
			}
			if shadow.Key != "" || shadow.KeySource != KeySourceShadowed {
				t.Fatalf("shadow key = %q/%q, want shadowed without key", shadow.Key, shadow.KeySource)
			}

			resolved, err := store.Resolve("alpha")
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if resolved.Body != tc.wantWinner {
				t.Fatalf("Resolve body = %q, want %q", resolved.Body, tc.wantWinner)
			}
			resolvedShadow, err := store.ResolveScoped("alpha", tc.shadowScope)
			if err != nil {
				t.Fatalf("ResolveScoped shadow: %v", err)
			}
			if resolvedShadow.Body != tc.wantShadow {
				t.Fatalf("ResolveScoped body = %q, want %q", resolvedShadow.Body, tc.wantShadow)
			}
		})
	}
}

func TestFSStoreClearsCachedPromptsWhenRediscoveryFails(t *testing.T) {
	dir := t.TempDir()
	writePrompt(t, dir, "alpha.md", "body\n")

	store := NewFS(dir, nil, []rune("123"))
	if err := store.Discover(); err != nil {
		t.Fatalf("Discover(): %v", err)
	}

	writePrompt(t, dir, "bravo.md", "---\nkey: x\n---\nbody\n")
	writePrompt(t, dir, "charlie.md", "---\nkey: X\n---\nbody\n")

	err := store.Discover()
	if err == nil {
		t.Fatal("want rediscovery error, got nil")
	}

	var dupErr *keybind.DuplicateKeybindError
	if !errors.As(err, &dupErr) {
		t.Fatalf("want DuplicateKeybindError, got %T", err)
	}

	if _, err := store.List(); err == nil {
		t.Fatal("List() after failed rediscovery: want error, got nil")
	}
	if _, err := store.Resolve("alpha"); err == nil {
		t.Fatal("Resolve() after failed rediscovery: want error, got nil")
	}
}

func TestFSStoreStripsEscapesFromMetadataButPreservesBody(t *testing.T) {
	dir := t.TempDir()
	bodyBytes := "evil\x1b]0;pwn\x07tail\n"
	writePrompt(t, dir, "escape.md", "---\n"+
		`title: "evil\e]0;pwn\atail"`+"\n"+
		`description: "d\e[?1049hd"`+"\n"+
		`tags: ["ok", "b\e]0;x\aad"]`+"\n"+
		"---\n\n"+bodyBytes)

	store := NewFS(dir, nil, []rune("1"))
	prompt, err := store.Resolve("escape")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if prompt.Title != "eviltail" {
		t.Fatalf("Title = %q, want %q", prompt.Title, "eviltail")
	}
	if prompt.Description != "dd" {
		t.Fatalf("Description = %q, want %q", prompt.Description, "dd")
	}
	wantTags := []string{"ok", "bad"}
	if diff := cmp.Diff(wantTags, prompt.Tags); diff != "" {
		t.Fatalf("Tags mismatch (-want +got):\n%s", diff)
	}
	wantBody := "evil\x1b]0;pwn\x07tail"
	if prompt.Body != wantBody {
		t.Fatalf("Body = %q, want %q (escape sequences in body must be preserved)", prompt.Body, wantBody)
	}
}

func writePrompt(t *testing.T, root, relPath, content string) {
	t.Helper()

	path := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
