// Package store discovers prompts on disk and resolves them by ID.
//
// ID is the filename stem (DECISIONS.md §3). Duplicate stems within a tier are
// a hard error (§4); cross-tier project/global collisions are resolved by
// policy. Keybind validation is delegated to internal/keybind.
package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hsadler/tprompt/internal/keybind"
	"github.com/hsadler/tprompt/internal/promptmeta"
	"github.com/hsadler/tprompt/internal/promptsource"
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
	Scope       string
	Shadowed    bool
	ShadowedBy  string
	ShadowPath  string
}

// KeySource identifies how a prompt's resolved board key was assigned.
type KeySource string

const (
	KeySourceExplicit KeySource = "explicit"
	KeySourceAuto     KeySource = "auto"
	KeySourceOverflow KeySource = "overflow"
	KeySourceShadowed KeySource = "shadowed"
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

// PromptsDirCreateError reports that the auto-created default prompts
// directory could not be created (e.g. read-only filesystem, blocking file).
// Surfaced as a usage error (exit 2) so default-dir failures stay consistent
// with explicit prompts_dir failures.
type PromptsDirCreateError struct {
	Path string
	Err  error
}

func (e *PromptsDirCreateError) Error() string {
	return fmt.Sprintf("create prompts directory %s: %v", e.Path, e.Err)
}

func (e *PromptsDirCreateError) Unwrap() error { return e.Err }

// FSStore is the filesystem-backed Phase 1 store implementation.
type FSStore struct {
	sources  []promptsource.Source
	policy   ConflictPolicy
	reserved map[rune]string
	pool     []rune

	promptsByID  map[string]Prompt
	promptsByRef map[string]Prompt
	summaries    []Summary
}

// NewFS returns a store that discovers prompts from the given directory.
// A missing root remains a PromptsDirMissingError on Discover.
func NewFS(root string, reserved map[rune]string, pool []rune) *FSStore {
	return NewMultiSource([]promptsource.Source{{
		Path:  root,
		Scope: promptsource.ScopeGlobal,
	}}, reserved, pool)
}

// NewMultiSource returns a store that discovers prompts from an ordered list
// of sources. Missing optional sources are skipped; missing required sources
// remain PromptsDirMissingError.
func NewMultiSource(sources []promptsource.Source, reserved map[rune]string, pool []rune) *FSStore {
	return NewMultiSourceWithPolicy(sources, ConflictPolicyGlobal, reserved, pool)
}

// NewMultiSourceWithPolicy is like NewMultiSource, but cross-tier
// global/project ID collisions are resolved with policy.
func NewMultiSourceWithPolicy(sources []promptsource.Source, policy ConflictPolicy, reserved map[rune]string, pool []rune) *FSStore {
	reservedCopy := make(map[rune]string, len(reserved))
	for key, action := range reserved {
		reservedCopy[key] = action
	}
	poolCopy := append([]rune(nil), pool...)
	sourceCopy := append([]promptsource.Source(nil), sources...)

	return &FSStore{
		sources:  sourceCopy,
		policy:   policy,
		reserved: reservedCopy,
		pool:     poolCopy,
	}
}

// NewFSWithAutoCreate is like NewFS but creates the prompts directory if it
// does not exist on Discover. Use it for the default-resolved global path;
// explicit user paths must continue to use NewFS so a missing directory
// remains a hard error.
func NewFSWithAutoCreate(root string, reserved map[rune]string, pool []rune) *FSStore {
	return NewMultiSource([]promptsource.Source{{
		Path:               root,
		Scope:              promptsource.ScopeGlobal,
		AutoCreateOnAccess: true,
	}}, reserved, pool)
}

// prepareSource canonicalizes the configured root, optionally creates it when
// auto-create is enabled, and confirms it is a directory. A missing
// non-auto-create root surfaces as PromptsDirMissingError so the existing
// contract for explicit prompts_dir is preserved.
func prepareSource(source promptsource.Source) (string, bool, error) {
	root, err := filepath.Abs(source.Path)
	if err != nil {
		return "", false, fmt.Errorf("resolve prompts directory: %w", err)
	}
	if source.AutoCreateOnAccess {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return "", false, &PromptsDirCreateError{Path: root, Err: err}
		}
	}
	info, statErr := os.Stat(root)
	if statErr != nil {
		if errors.Is(statErr, fs.ErrNotExist) {
			if source.Optional {
				return root, false, nil
			}
			return "", false, &PromptsDirMissingError{Path: root}
		}
		return "", false, fmt.Errorf("stat prompts directory %s: %w", root, statErr)
	}
	if !info.IsDir() {
		return "", false, &PromptsDirMissingError{Path: root}
	}
	return root, true, nil
}

func (s *FSStore) Discover() error {
	sourceResults, err := s.discoverSources()
	if err != nil {
		s.clearCache()
		return err
	}

	resolution, err := resolveConflicts(sourceResults, s.policy)
	if err != nil {
		s.clearCache()
		return err
	}
	if err := s.cacheResolvedPrompts(sortedWinnerEntries(resolution.Winners), resolution.Shadows); err != nil {
		s.clearCache()
		return err
	}
	return nil
}

func (s *FSStore) discoverSources() ([]sourcePrompts, error) {
	sourceResults := make([]sourcePrompts, 0, len(s.sources))
	for _, source := range s.sources {
		source = normalizeSource(source)
		root, exists, err := prepareSource(source)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}

		entries, err := discoverPromptFiles(root, source)
		if err != nil {
			return nil, err
		}

		if err := validateUniqueIDs(entries); err != nil {
			return nil, err
		}
		sourceResults = append(sourceResults, sourcePrompts{
			Source:  source,
			Prompts: entries,
		})
	}
	return sourceResults, nil
}

func normalizeSource(source promptsource.Source) promptsource.Source {
	if source.Scope == "" {
		source.Scope = promptsource.ScopeGlobal
	}
	return source
}

func (s *FSStore) cacheResolvedPrompts(winners []discoveredPrompt, shadows []discoveredPrompt) error {
	assignment, err := keybind.Resolve(promptInputs(winners), s.reserved, s.pool)
	if err != nil {
		return err
	}

	promptsByID := make(map[string]Prompt, len(winners))
	promptsByRef := make(map[string]Prompt, len(winners)+len(shadows))
	summaries := make([]Summary, 0, len(winners)+len(shadows))
	shadowByWinner := shadowsByWinner(winners, shadows)
	for _, entry := range winners {
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
		if shadow, ok := shadowByWinner[promptRef(entry.prompt.Summary)]; ok {
			entry.prompt.ShadowPath = shadow.prompt.Path
		}
		promptsByID[entry.prompt.ID] = entry.prompt
		promptsByRef[promptRef(entry.prompt.Summary)] = entry.prompt
		summaries = append(summaries, entry.prompt.Summary)
	}
	for _, entry := range shadows {
		winner, ok := winnerForShadow(entry, winners)
		if ok {
			entry.prompt.ShadowedBy = winner.prompt.Path
		}
		entry.prompt.Shadowed = true
		entry.prompt.Key = ""
		entry.prompt.KeySource = KeySourceShadowed
		promptsByRef[promptRef(entry.prompt.Summary)] = entry.prompt
		summaries = append(summaries, entry.prompt.Summary)
	}

	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].ID != summaries[j].ID {
			return summaries[i].ID < summaries[j].ID
		}
		return summaries[i].Scope < summaries[j].Scope
	})
	s.promptsByID = promptsByID
	s.promptsByRef = promptsByRef
	s.summaries = summaries
	return nil
}

func (s *FSStore) clearCache() {
	s.promptsByID = nil
	s.promptsByRef = nil
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

// ResolveScoped resolves a prompt by ID and scope. It is used by the TUI
// search path so shadowed prompts remain selectable without changing
// Resolve(id)'s winner-only CLI contract.
func (s *FSStore) ResolveScoped(id, scope string) (Prompt, error) {
	if scope == "" {
		return s.Resolve(id)
	}
	if err := s.ensureDiscovered(); err != nil {
		return Prompt{}, err
	}
	prompt, ok := s.promptsByRef[promptRef(Summary{ID: id, Scope: scope})]
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

func discoverPromptFiles(root string, source promptsource.Source) ([]discoveredPrompt, error) {
	entries := make([]discoveredPrompt, 0)
	err := filepath.WalkDir(root, promptFileWalker(root, source, &entries))
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].prompt.ID < entries[j].prompt.ID })
	if err := validatePromptDefaults(entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func promptFileWalker(root string, source promptsource.Source, entries *[]discoveredPrompt) fs.WalkDirFunc {
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

		entry, err := loadPromptFile(path, source)
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

func loadPromptFile(path string, source promptsource.Source) (discoveredPrompt, error) {
	content, err := readPromptFile(path)
	if err != nil {
		return discoveredPrompt{}, fmt.Errorf("read prompt %s: %w", path, err)
	}
	parsed, err := promptmeta.Parse(content)
	if err != nil {
		return discoveredPrompt{}, fmt.Errorf("parse prompt %s: %w", path, err)
	}
	sanitizeMeta(&parsed.Meta)
	return buildDiscoveredPrompt(path, source, parsed), nil
}

func buildDiscoveredPrompt(path string, source promptsource.Source, parsed promptmeta.Parsed) discoveredPrompt {
	scope := string(source.Scope)
	if scope == "" {
		scope = string(promptsource.ScopeGlobal)
	}
	return discoveredPrompt{
		prompt: Prompt{
			Summary: Summary{
				ID:          strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
				Title:       parsed.Meta.Title,
				Description: parsed.Meta.Description,
				Tags:        append([]string(nil), parsed.Meta.Tags...),
				Path:        path,
				Scope:       scope,
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

func promptRef(summary Summary) string {
	scope := summary.Scope
	if scope == "" {
		scope = string(promptsource.ScopeGlobal)
	}
	return scope + "\x00" + summary.ID
}

func shadowsByWinner(winners []discoveredPrompt, shadows []discoveredPrompt) map[string]discoveredPrompt {
	out := make(map[string]discoveredPrompt)
	for _, shadow := range shadows {
		if winner, ok := winnerForShadow(shadow, winners); ok {
			out[promptRef(winner.prompt.Summary)] = shadow
		}
	}
	return out
}

func winnerForShadow(shadow discoveredPrompt, winners []discoveredPrompt) (discoveredPrompt, bool) {
	for _, winner := range winners {
		if winner.prompt.ID == shadow.prompt.ID {
			return winner, true
		}
	}
	return discoveredPrompt{}, false
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
