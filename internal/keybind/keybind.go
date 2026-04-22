// Package keybind resolves the final TUI keybind map from frontmatter
// declarations plus the auto-assign pool (DECISIONS.md §16, §17).
package keybind

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Assignment maps resolved keybind characters to prompt IDs. Overflow holds
// prompts that could not fit on the board and are reachable only via search.
type Assignment struct {
	Bindings map[rune]string
	Overflow []string
}

// Resolver is a pure function over a prompt set and configuration.
type Resolver interface {
	Resolve(prompts []Input, reserved map[rune]string, pool []rune) (Assignment, error)
}

// Input is the subset of prompt data Resolver needs (avoids importing store).
type Input struct {
	ID     string
	Key    string
	HasKey bool
	Path   string
}

// DuplicateKeybindError reports the full set of prompts that declared the same
// keybind in frontmatter.
type DuplicateKeybindError struct {
	Key   rune
	IDs   []string
	Paths []string
}

func (e *DuplicateKeybindError) Error() string {
	if len(e.Paths) == 0 {
		return fmt.Sprintf("duplicate keybind %q declared", string(e.Key))
	}
	return fmt.Sprintf("duplicate keybind %q declared by: %s", string(e.Key), strings.Join(e.Paths, ", "))
}

// ReservedKeybindError reports a frontmatter key colliding with a reserved
// action key such as clipboard or search.
type ReservedKeybindError struct {
	Key    rune
	Path   string
	Action string
}

func (e *ReservedKeybindError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("frontmatter key %q collides with reserved key for %s", string(e.Key), e.Action)
	}
	return fmt.Sprintf("frontmatter key %q in %s collides with reserved key for %s", string(e.Key), e.Path, e.Action)
}

// MalformedKeybindError reports a raw `key:` value that is not a single
// printable character.
type MalformedKeybindError struct {
	Value string
	Path  string
}

func (e *MalformedKeybindError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("invalid frontmatter key %q: must be a single printable character", e.Value)
	}
	return fmt.Sprintf("invalid frontmatter key %q in %s: must be a single printable character", e.Value, e.Path)
}

type resolver struct{}

// NewResolver returns the Phase 1 keybind resolver implementation.
func NewResolver() Resolver { return resolver{} }

func (resolver) Resolve(prompts []Input, reserved map[rune]string, pool []rune) (Assignment, error) {
	assignment := Assignment{Bindings: make(map[rune]string)}
	normalizedReserved := normalizeReserved(reserved)
	availablePool := normalizePool(pool, normalizedReserved)

	frontmatterByKey, auto, err := groupPrompts(prompts, normalizedReserved)
	if err != nil {
		return Assignment{}, err
	}
	if err := bindFrontmatter(assignment.Bindings, frontmatterByKey); err != nil {
		return Assignment{}, err
	}
	assignAutomatic(&assignment, auto, availablePool)

	return assignment, nil
}

func groupPrompts(prompts []Input, reserved map[rune]string) (map[rune][]Input, []Input, error) {
	frontmatterByKey := make(map[rune][]Input)
	auto := make([]Input, 0, len(prompts))

	for _, prompt := range prompts {
		if !prompt.HasKey {
			auto = append(auto, prompt)
			continue
		}

		key, err := normalizePromptKey(prompt.Key)
		if err != nil {
			return nil, nil, &MalformedKeybindError{Value: prompt.Key, Path: prompt.Path}
		}
		if action, ok := reserved[key]; ok {
			return nil, nil, &ReservedKeybindError{Key: key, Path: prompt.Path, Action: action}
		}
		frontmatterByKey[key] = append(frontmatterByKey[key], prompt)
	}

	return frontmatterByKey, auto, nil
}

func bindFrontmatter(bindings map[rune]string, frontmatterByKey map[rune][]Input) error {
	for _, key := range sortedKeys(frontmatterByKey) {
		bound := frontmatterByKey[key]
		if len(bound) > 1 {
			return duplicateKeybindError(key, bound)
		}
		bindings[key] = bound[0].ID
	}
	return nil
}

func sortedKeys(frontmatterByKey map[rune][]Input) []rune {
	keys := make([]rune, 0, len(frontmatterByKey))
	for key := range frontmatterByKey {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func duplicateKeybindError(key rune, bound []Input) error {
	ids := make([]string, 0, len(bound))
	paths := make([]string, 0, len(bound))
	for _, prompt := range bound {
		ids = append(ids, prompt.ID)
		paths = append(paths, prompt.Path)
	}
	sort.Strings(ids)
	sort.Strings(paths)
	return &DuplicateKeybindError{Key: key, IDs: ids, Paths: paths}
}

func assignAutomatic(assignment *Assignment, auto []Input, pool []rune) {
	sort.Slice(auto, func(i, j int) bool { return auto[i].ID < auto[j].ID })

	nextPool := 0
	for _, prompt := range auto {
		key, ok := nextAvailableKey(assignment.Bindings, pool, &nextPool)
		if !ok {
			assignment.Overflow = append(assignment.Overflow, prompt.ID)
			continue
		}
		assignment.Bindings[key] = prompt.ID
	}
}

func nextAvailableKey(bindings map[rune]string, pool []rune, nextPool *int) (rune, bool) {
	for *nextPool < len(pool) {
		key := pool[*nextPool]
		*nextPool = *nextPool + 1
		if _, taken := bindings[key]; taken {
			continue
		}
		return key, true
	}
	return 0, false
}

func normalizePromptKey(raw string) (rune, error) {
	if raw == "" {
		return 0, fmt.Errorf("empty key")
	}
	if !utf8.ValidString(raw) {
		return 0, fmt.Errorf("invalid utf-8")
	}
	if utf8.RuneCountInString(raw) != 1 {
		return 0, fmt.Errorf("want single rune")
	}

	key, _ := utf8.DecodeRuneInString(raw)
	if !unicode.IsPrint(key) {
		return 0, fmt.Errorf("not printable")
	}

	return unicode.ToLower(key), nil
}

func normalizeReserved(reserved map[rune]string) map[rune]string {
	if len(reserved) == 0 {
		return nil
	}
	out := make(map[rune]string, len(reserved))
	for key, action := range reserved {
		out[unicode.ToLower(key)] = action
	}
	return out
}

func normalizePool(pool []rune, reserved map[rune]string) []rune {
	out := make([]rune, 0, len(pool))
	seen := make(map[rune]struct{}, len(pool))
	for _, key := range pool {
		key = unicode.ToLower(key)
		if _, isReserved := reserved[key]; isReserved {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

// Resolve is a convenience wrapper around the default resolver.
func Resolve(prompts []Input, reserved map[rune]string, pool []rune) (Assignment, error) {
	return NewResolver().Resolve(prompts, reserved, pool)
}
