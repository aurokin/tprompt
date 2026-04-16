package config

import "fmt"

// DeliveryFlags holds optional CLI flag values. A nil pointer means the flag
// was not provided, so the next precedence level applies.
type DeliveryFlags struct {
	Mode     *string
	Enter    *bool
	Sanitize *string
}

// Delivery is the fully-resolved set of delivery settings after applying
// precedence: CLI flags → prompt frontmatter → config file → built-in defaults.
type Delivery struct {
	Mode     string
	Enter    bool
	Sanitize string
}

// FrontmatterDefaults captures per-prompt delivery defaults from frontmatter.
type FrontmatterDefaults struct {
	Mode  string
	Enter *bool
}

// ResolveDelivery applies the four-level precedence chain to produce a final
// Delivery. It validates the resolved mode and sanitize values.
func ResolveDelivery(cfg Resolved, fm FrontmatterDefaults, flags DeliveryFlags) (Delivery, error) {
	d := Delivery{
		Mode:     cfg.DefaultMode,
		Enter:    cfg.DefaultEnter,
		Sanitize: cfg.Sanitize,
	}

	if fm.Mode != "" {
		d.Mode = fm.Mode
	}
	if fm.Enter != nil {
		d.Enter = *fm.Enter
	}

	if flags.Mode != nil {
		d.Mode = *flags.Mode
	}
	if flags.Enter != nil {
		d.Enter = *flags.Enter
	}
	if flags.Sanitize != nil {
		d.Sanitize = *flags.Sanitize
	}

	switch d.Mode {
	case "paste", "type":
	default:
		return Delivery{}, fmt.Errorf("invalid delivery mode %q: must be paste or type", d.Mode)
	}

	switch d.Sanitize {
	case "off", "safe", "strict":
	default:
		return Delivery{}, fmt.Errorf("invalid sanitize mode %q: must be off, safe, or strict", d.Sanitize)
	}

	return d, nil
}
