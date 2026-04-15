// Package store discovers prompts on disk and resolves them by ID.
//
// ID is the filename stem (DECISIONS.md §3). Duplicate stems are a hard error
// (§4). Keybind validation is delegated to internal/keybind.
package store

// Summary is the light-weight view of a prompt used for listings.
type Summary struct {
	ID          string
	Title       string
	Description string
	Tags        []string
	Key         string
	Path        string
}

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
