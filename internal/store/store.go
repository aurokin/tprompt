// Package store discovers prompts on disk and resolves them by ID.
//
// ID is the filename stem (DECISIONS.md §3). Duplicate stems are a hard error
// (§4). Keybind validation is delegated to internal/keybind.
package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hsadler/tprompt/internal/keybind"
	"github.com/hsadler/tprompt/internal/promptmeta"
)

// Summary is the light-weight view of a prompt used for listings.
type Summary struct {
	ID          string
	Title       string
	Description string
	Tags        []string
	Key         string
	KeySource   KeySource
	Path        string
}

// KeySource identifies how a prompt's resolved board key was assigned.
type KeySource string

const (
	KeySourceExplicit KeySource = "explicit"
	KeySourceAuto     KeySource = "auto"
	KeySourceOverflow KeySource = "overflow"
)

// DeliveryDefaults captures per-prompt delivery defaults from frontmatter.
type DeliveryDefaults struct {
	Mode  string
	Enter *bool
}

// Prompt is a fully-loaded prompt including body.
type Prompt struct {
	Summary
	Body     string
	Defaults DeliveryDefaults
}

// Store is the interface defined in docs/implementation/interfaces.md.
type Store interface {
	Discover() error
	Resolve(id string) (Prompt, error)
	List() ([]Summary, error)
}

// DuplicatePromptIDError reports multiple markdown files resolving to the same
// filename-stem prompt ID.
type DuplicatePromptIDError struct {
	ID    string
	Paths []string
}

func (e *DuplicatePromptIDError) Error() string {
	return fmt.Sprintf("duplicate prompt ID detected: %s: %s", e.ID, strings.Join(e.Paths, ", "))
}

// InvalidPromptModeError reports an unsupported frontmatter mode default.
type InvalidPromptModeError struct {
	Path  string
	Value string
}

func (e *InvalidPromptModeError) Error() string {
	return fmt.Sprintf("invalid delivery mode %q in %s: must be one of paste, type", e.Value, e.Path)
}

// NotFoundError reports a prompt lookup miss.
type NotFoundError struct {
	ID string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("prompt %q not found", e.ID)
}

// PromptsDirMissingError reports that the configured prompts directory does not
// exist or is not a directory at discovery time.
type PromptsDirMissingError struct {
	Path string
}

func (e *PromptsDirMissingError) Error() string {
	return fmt.Sprintf("prompts directory missing: %s", e.Path)
}

// FSStore is the filesystem-backed Phase 1 store implementation.
type FSStore struct {
	root     string
	reserved map[rune]string
	pool     []rune

	promptsByID map[string]Prompt
	summaries   []Summary
}

// NewFS returns a store that discovers prompts from the given directory.
func NewFS(root string, reserved map[rune]string, pool []rune) *FSStore {
	reservedCopy := make(map[rune]string, len(reserved))
	for key, action := range reserved {
		reservedCopy[key] = action
	}
	poolCopy := append([]rune(nil), pool...)

	return &FSStore{
		root:     root,
		reserved: reservedCopy,
		pool:     poolCopy,
	}
}

func (s *FSStore) Discover() error {
	root, err := filepath.Abs(s.root)
	if err != nil {
		s.clearCache()
		return fmt.Errorf("resolve prompts directory: %w", err)
	}

	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		s.clearCache()
		return &PromptsDirMissingError{Path: root}
	}

	entries, err := discoverPromptFiles(root)
	if err != nil {
		s.clearCache()
		return err
	}

	if err := validateUniqueIDs(entries); err != nil {
		s.clearCache()
		return err
	}

	assignment, err := keybind.Resolve(promptInputs(entries), s.reserved, s.pool)
	if err != nil {
		s.clearCache()
		return err
	}

	promptsByID := make(map[string]Prompt, len(entries))
	summaries := make([]Summary, 0, len(entries))
	for _, entry := range entries {
		if resolvedKey, ok := resolvedKeyForPrompt(assignment.Bindings, entry.prompt.ID); ok {
			entry.prompt.Key = string(resolvedKey)
			if entry.hasKey {
				entry.prompt.KeySource = KeySourceExplicit
			} else {
				entry.prompt.KeySource = KeySourceAuto
			}
		} else {
			entry.prompt.KeySource = KeySourceOverflow
		}
		promptsByID[entry.prompt.ID] = entry.prompt
		summaries = append(summaries, entry.prompt.Summary)
	}

	sort.Slice(summaries, func(i, j int) bool { return summaries[i].ID < summaries[j].ID })
	s.promptsByID = promptsByID
	s.summaries = summaries
	return nil
}

func (s *FSStore) clearCache() {
	s.promptsByID = nil
	s.summaries = nil
}

func (s *FSStore) Resolve(id string) (Prompt, error) {
	if err := s.ensureDiscovered(); err != nil {
		return Prompt{}, err
	}
	prompt, ok := s.promptsByID[id]
	if !ok {
		return Prompt{}, &NotFoundError{ID: id}
	}
	return clonePrompt(prompt), nil
}

func (s *FSStore) List() ([]Summary, error) {
	if err := s.ensureDiscovered(); err != nil {
		return nil, err
	}
	return cloneSummaries(s.summaries), nil
}

func (s *FSStore) ensureDiscovered() error {
	if s.promptsByID != nil {
		return nil
	}
	return s.Discover()
}

type discoveredPrompt struct {
	prompt Prompt
	rawKey string
	hasKey bool
}

func discoverPromptFiles(root string) ([]discoveredPrompt, error) {
	entries := make([]discoveredPrompt, 0)
	err := filepath.WalkDir(root, promptFileWalker(root, &entries))
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].prompt.ID < entries[j].prompt.ID })
	if err := validatePromptDefaults(entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func promptFileWalker(root string, entries *[]discoveredPrompt) fs.WalkDirFunc {
	return func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if shouldSkipPath(root, path, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}

		entry, err := loadPromptFile(path)
		if err != nil {
			return err
		}
		*entries = append(*entries, entry)
		return nil
	}
}

func shouldSkipPath(root, path string, d fs.DirEntry) bool {
	return path != root && isHidden(filepath.Base(path))
}

func loadPromptFile(path string) (discoveredPrompt, error) {
	content, err := readPromptFile(path)
	if err != nil {
		return discoveredPrompt{}, fmt.Errorf("read prompt %s: %w", path, err)
	}
	parsed, err := promptmeta.Parse(content)
	if err != nil {
		return discoveredPrompt{}, fmt.Errorf("parse prompt %s: %w", path, err)
	}
	sanitizeMeta(&parsed.Meta)
	return buildDiscoveredPrompt(path, parsed), nil
}

func buildDiscoveredPrompt(path string, parsed promptmeta.Parsed) discoveredPrompt {
	return discoveredPrompt{
		prompt: Prompt{
			Summary: Summary{
				ID:          strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
				Title:       parsed.Meta.Title,
				Description: parsed.Meta.Description,
				Tags:        append([]string(nil), parsed.Meta.Tags...),
				Path:        path,
			},
			Body: parsed.Body,
			Defaults: DeliveryDefaults{
				Mode:  parsed.Meta.Mode,
				Enter: parsed.Meta.Enter,
			},
		},
		rawKey: keyValue(parsed.Meta.Key),
		hasKey: parsed.Meta.KeyDeclared,
	}
}

func readPromptFile(path string) ([]byte, error) {
	// Path comes from WalkDir rooted at the configured prompts directory.
	//nolint:gosec // G304: bounded project file discovery, not arbitrary user input
	return os.ReadFile(path)
}

func validateUniqueIDs(entries []discoveredPrompt) error {
	byID := make(map[string][]string)
	for _, entry := range entries {
		byID[entry.prompt.ID] = append(byID[entry.prompt.ID], entry.prompt.Path)
	}

	ids := make([]string, 0, len(byID))
	for id, paths := range byID {
		if len(paths) < 2 {
			continue
		}
		sort.Strings(paths)
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return nil
	}
	id := ids[0]
	return &DuplicatePromptIDError{ID: id, Paths: byID[id]}
}

func promptInputs(entries []discoveredPrompt) []keybind.Input {
	inputs := make([]keybind.Input, 0, len(entries))
	for _, entry := range entries {
		inputs = append(inputs, keybind.Input{
			ID:     entry.prompt.ID,
			Key:    entry.rawKey,
			HasKey: entry.hasKey,
			Path:   entry.prompt.Path,
		})
	}
	return inputs
}

func validatePromptDefaults(entries []discoveredPrompt) error {
	for _, entry := range entries {
		switch entry.prompt.Defaults.Mode {
		case "", "paste", "type":
		default:
			return &InvalidPromptModeError{
				Path:  entry.prompt.Path,
				Value: entry.prompt.Defaults.Mode,
			}
		}
	}
	return nil
}

func clonePrompt(prompt Prompt) Prompt {
	prompt.Summary = cloneSummary(prompt.Summary)
	prompt.Defaults.Enter = cloneBoolPtr(prompt.Defaults.Enter)
	return prompt
}

func cloneSummaries(summaries []Summary) []Summary {
	cloned := make([]Summary, 0, len(summaries))
	for _, summary := range summaries {
		cloned = append(cloned, cloneSummary(summary))
	}
	return cloned
}

func cloneSummary(summary Summary) Summary {
	summary.Tags = append([]string(nil), summary.Tags...)
	return summary
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func resolvedKeyForPrompt(bindings map[rune]string, promptID string) (rune, bool) {
	for key, id := range bindings {
		if id == promptID {
			return key, true
		}
	}
	return 0, false
}

func isHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}

func keyValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
