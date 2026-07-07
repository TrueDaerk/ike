package theme

import (
	"reflect"
	"testing"

	"charm.land/lipgloss/v2"
)

// TestBuiltinsComplete: every built-in defines every ui slot, a non-empty
// captures map covering the default capture set, and the required file keys.
func TestBuiltinsComplete(t *testing.T) {
	def := Default()
	for _, th := range Builtins() {
		ui := reflect.ValueOf(th.UI)
		for i := 0; i < ui.NumField(); i++ {
			if ui.Field(i).String() == "" {
				t.Errorf("%s: ui slot %s is empty", th.Name, ui.Type().Field(i).Name)
			}
		}
		for capture := range def.Captures {
			if th.Captures[capture] == "" {
				t.Errorf("%s: capture %q missing", th.Name, capture)
			}
		}
		for _, key := range []string{"dir", "default"} {
			if th.Files[key] == "" {
				t.Errorf("%s: file key %q missing", th.Name, key)
			}
		}
	}
}

func TestBuiltinNamesUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, th := range Builtins() {
		if th.Name == "" {
			t.Fatal("built-in with empty name")
		}
		if seen[th.Name] {
			t.Errorf("duplicate built-in name %q", th.Name)
		}
		seen[th.Name] = true
	}
}

func TestSelect(t *testing.T) {
	if th, ok := Select("tokyo-night", nil); !ok || th.Name != "tokyo-night" {
		t.Errorf("Select(tokyo-night) = %q, %v", th.Name, ok)
	}
	// Empty name is the implicit default, not a warning case.
	if th, ok := Select("", nil); !ok || th.Name != DefaultName {
		t.Errorf("Select(\"\") = %q, %v", th.Name, ok)
	}
	// Unknown name falls back to default and reports not-found.
	if th, ok := Select("no-such-theme", nil); ok || th.Name != DefaultName {
		t.Errorf("Select(unknown) = %q, %v", th.Name, ok)
	}
	// Extra (plugin) themes are found and shadow built-ins, last wins.
	extra := []Theme{{Name: "custom"}, {Name: "nord", UI: UI{Accent: "#123456"}}}
	if th, ok := Select("custom", extra); !ok || th.Name != "custom" {
		t.Errorf("Select(custom) = %q, %v", th.Name, ok)
	}
	if th, _ := Select("nord", extra); th.UI.Accent != "#123456" {
		t.Errorf("plugin theme should shadow built-in, got accent %q", th.UI.Accent)
	}
}

// TestNewPaletteFallback: sparse themes backfill ui slots and maps from default.
func TestNewPaletteFallback(t *testing.T) {
	p := NewPalette(Theme{Name: "sparse", UI: UI{Accent: "#101010"}})
	def := NewPalette(Default())
	if p.Accent != lipgloss.Color("#101010") {
		t.Errorf("explicit slot not honored: %v", p.Accent)
	}
	if p.Background != def.Background {
		t.Errorf("empty slot should fall back to default: %v != %v", p.Background, def.Background)
	}
	if len(p.Captures) == 0 || len(p.Files) == 0 {
		t.Error("nil captures/files should fall back to default maps")
	}
}

func TestResolve(t *testing.T) {
	cases := map[string]string{
		"red":     "#ff5555",
		"RED":     "#ff5555",
		"grey":    "#8a8a8a",
		"orange":  "#ff8700",
		"#1f6feb": "#1f6feb", // hex passes through
	}
	for token, want := range cases {
		if got := Resolve(token); got != lipgloss.Color(want) {
			t.Errorf("Resolve(%q) = %v, want %v", token, got, want)
		}
	}
	if got := Resolve("39"); got != lipgloss.Color("39") {
		t.Errorf("ANSI index should pass through, got %v", got)
	}
}
