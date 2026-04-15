package store

import "testing"

// TestStoreInterfaceShape is a compile-time anchor: if the Store interface
// loses a method, dependent packages break here instead of at a call site.
func TestStoreInterfaceShape(t *testing.T) {
	var _ Store = (*stubStore)(nil)
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

type stubStore struct{}

func (stubStore) Discover() error                { return nil }
func (stubStore) Resolve(string) (Prompt, error) { return Prompt{}, nil }
func (stubStore) List() ([]Summary, error)       { return nil, nil }
