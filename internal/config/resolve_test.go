package config

import "testing"

func TestResolveDeliveryUsesConfigDefaults(t *testing.T) {
	cfg := Resolved{DefaultMode: "paste", DefaultEnter: false, Sanitize: "off"}
	d, err := ResolveDelivery(cfg, FrontmatterDefaults{}, DeliveryFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Mode != "paste" || d.Enter != false || d.Sanitize != "off" {
		t.Fatalf("got %+v", d)
	}
}

func TestResolveDeliveryFrontmatterOverridesConfig(t *testing.T) {
	cfg := Resolved{DefaultMode: "paste", DefaultEnter: false, Sanitize: "off"}
	enterTrue := true
	fm := FrontmatterDefaults{Mode: "type", Enter: &enterTrue}
	d, err := ResolveDelivery(cfg, fm, DeliveryFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Mode != "type" {
		t.Fatalf("Mode = %q, want type", d.Mode)
	}
	if !d.Enter {
		t.Fatal("Enter = false, want true")
	}
}

func TestResolveDeliveryFlagsOverrideFrontmatter(t *testing.T) {
	cfg := Resolved{DefaultMode: "paste", DefaultEnter: false, Sanitize: "off"}
	enterTrue := true
	fm := FrontmatterDefaults{Mode: "type", Enter: &enterTrue}

	pasteMode := "paste"
	enterFalse := false
	strict := "strict"
	flags := DeliveryFlags{Mode: &pasteMode, Enter: &enterFalse, Sanitize: &strict}

	d, err := ResolveDelivery(cfg, fm, flags)
	if err != nil {
		t.Fatal(err)
	}
	if d.Mode != "paste" {
		t.Fatalf("Mode = %q, want paste", d.Mode)
	}
	if d.Enter {
		t.Fatal("Enter = true, want false")
	}
	if d.Sanitize != "strict" {
		t.Fatalf("Sanitize = %q, want strict", d.Sanitize)
	}
}

func TestResolveDeliveryPartialFrontmatter(t *testing.T) {
	cfg := Resolved{DefaultMode: "paste", DefaultEnter: false, Sanitize: "off"}
	fm := FrontmatterDefaults{Mode: "type"}
	d, err := ResolveDelivery(cfg, fm, DeliveryFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Mode != "type" {
		t.Fatalf("Mode = %q, want type", d.Mode)
	}
	if d.Enter != false {
		t.Fatal("Enter should fall through to config default")
	}
}

func TestResolveDeliveryPartialFlags(t *testing.T) {
	cfg := Resolved{DefaultMode: "paste", DefaultEnter: false, Sanitize: "off"}
	safe := "safe"
	flags := DeliveryFlags{Sanitize: &safe}
	d, err := ResolveDelivery(cfg, FrontmatterDefaults{}, flags)
	if err != nil {
		t.Fatal(err)
	}
	if d.Mode != "paste" {
		t.Fatalf("Mode = %q, want paste", d.Mode)
	}
	if d.Sanitize != "safe" {
		t.Fatalf("Sanitize = %q, want safe", d.Sanitize)
	}
}

func TestResolveDeliveryRejectsInvalidMode(t *testing.T) {
	cfg := Resolved{DefaultMode: "yolo", DefaultEnter: false, Sanitize: "off"}
	_, err := ResolveDelivery(cfg, FrontmatterDefaults{}, DeliveryFlags{})
	if err == nil {
		t.Fatal("want error for invalid mode, got nil")
	}
}

func TestResolveDeliveryRejectsInvalidSanitize(t *testing.T) {
	cfg := Resolved{DefaultMode: "paste", DefaultEnter: false, Sanitize: "maybe"}
	_, err := ResolveDelivery(cfg, FrontmatterDefaults{}, DeliveryFlags{})
	if err == nil {
		t.Fatal("want error for invalid sanitize, got nil")
	}
}
