// Package keybind resolves the final TUI keybind map from frontmatter
// declarations plus the auto-assign pool (DECISIONS.md §16, §17).
package keybind

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
	ID  string
	Key string // raw frontmatter value; empty means auto-assign
}
