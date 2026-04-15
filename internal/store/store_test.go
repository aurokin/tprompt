package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hsadler/tprompt/internal/keybind"
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
			Path:        filepath.Join(dir, "code-review.md"),
		},
		{
			ID:          "deep-review",
			Title:       "Deep Review",
			Description: "Multi-pass review",
			Key:         "1",
			Path:        filepath.Join(dir, "nested", "deep-review.md"),
		},
	}
	if diff := cmp.Diff(wantSummaries, summaries); diff != "" {
		t.Fatalf("List() mismatch (-want +got):\n%s", diff)
	}

	prompt, err := store.Resolve("code-review")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if prompt.Body != "Review this code.\n" {
		t.Fatalf("Body = %q, want %q", prompt.Body, "Review this code.\n")
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

func TestFSStoreSurfacesKeybindValidationErrors(t *testing.T) {
	dir := t.TempDir()
	writePrompt(t, dir, "alpha.md", "---\nkey: x\n---\na\n")
	writePrompt(t, dir, "bravo.md", "---\nkey: X\n---\nb\n")

	store := NewFS(dir, nil, []rune("123"))
	err := store.Discover()
	if err == nil {
		t.Fatal("want error, got nil")
	}

	var dupErr *keybind.DuplicateKeybindError
	if !errors.As(err, &dupErr) {
		t.Fatalf("want DuplicateKeybindError, got %T", err)
	}
}

func TestFSStoreRejectsExplicitEmptyOrNullKeys(t *testing.T) {
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
			err := store.Discover()
			if err == nil {
				t.Fatal("want error, got nil")
			}

			var malformed *keybind.MalformedKeybindError
			if !errors.As(err, &malformed) {
				t.Fatalf("want MalformedKeybindError, got %T", err)
			}
		})
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

	if prompt.Body != content {
		t.Fatalf("Body = %q, want %q", prompt.Body, content)
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

func TestFSStoreTreatsNonMappingFenceAsBody(t *testing.T) {
	dir := t.TempDir()
	content := "---\nHeading\n---\nbody\n"
	writePrompt(t, dir, "rule.md", content)

	store := NewFS(dir, nil, []rune("123"))
	prompt, err := store.Resolve("rule")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if prompt.Body != content {
		t.Fatalf("Body = %q, want %q", prompt.Body, content)
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
