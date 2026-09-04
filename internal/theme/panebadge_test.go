package theme

import (
	"image/color"
	"testing"
)

// panebadge_test.go guards the pane-number pill (#2496): the badge is only
// worth drawing if the digit reads at a glance, so both pill/digit pairs must
// clear the text-contrast floor and both pills must stand out from the pane
// body they sit on.

// minBadgeOnSurface is how far a pill background has to sit from the pane
// surface. A filled chip does not need text contrast, but it does have to be
// visibly a chip — that is the entire point of the badge.
const minBadgeOnSurface = 1.35

// TestPaneBadgeContrast: every built-in theme renders both pills legibly.
func TestPaneBadgeContrast(t *testing.T) {
	for _, th := range Builtins() {
		p := NewPalette(th)
		t.Run(th.Name, func(t *testing.T) {
			for _, c := range []struct {
				name   string
				fg, bg color.Color
			}{
				{"focused", p.PaneBadgeText, p.PaneBadge},
				{"unfocused", p.PaneBadgeMutedText, p.PaneBadgeMuted},
			} {
				if r := ContrastRatio(c.fg, c.bg); r < minTextContrast {
					t.Errorf("%s: digit/pill contrast %.2f:1, want >= %.1f:1", c.name, r, minTextContrast)
				}
				if r := ContrastRatio(c.bg, p.Surface); r < minBadgeOnSurface {
					t.Errorf("%s: pill/Surface contrast %.2f:1, want >= %.2f:1", c.name, r, minBadgeOnSurface)
				}
			}
			if sameColor(p.PaneBadge, p.PaneBadgeMuted) {
				t.Error("focused and unfocused pills are the same colour")
			}
		})
	}
}

// TestPaneBadgeSparseAndExplicit: a theme declaring none of the slots still
// gets a derived, legible pair; a theme declaring them keeps what it declared.
func TestPaneBadgeSparseAndExplicit(t *testing.T) {
	sparse := NewPalette(Theme{Name: "sparse", UI: UI{Surface: "#202020", Foreground: "#e0e0e0", Accent: "#00a0ff"}})
	if sameColor(sparse.PaneBadge, sparse.Surface) || sameColor(sparse.PaneBadgeMuted, sparse.Surface) {
		t.Error("a sparse theme must still derive pills distinct from its surface")
	}
	if r := ContrastRatio(sparse.PaneBadgeText, sparse.PaneBadge); r < minTextContrast {
		t.Errorf("sparse focused digit contrast %.2f:1, want >= %.1f:1", r, minTextContrast)
	}

	explicit := NewPalette(Theme{Name: "explicit", UI: UI{
		PaneBadge: "#ff0000", PaneBadgeText: "#ffffff",
		PaneBadgeMuted: "#00ff00", PaneBadgeMutedText: "#000000",
	}})
	for _, c := range []struct {
		name string
		got  color.Color
		want string
	}{
		{"PaneBadge", explicit.PaneBadge, "#ff0000"},
		{"PaneBadgeText", explicit.PaneBadgeText, "#ffffff"},
		{"PaneBadgeMuted", explicit.PaneBadgeMuted, "#00ff00"},
		{"PaneBadgeMutedText", explicit.PaneBadgeMutedText, "#000000"},
	} {
		if !sameColor(c.got, Resolve(c.want)) {
			t.Errorf("%s = %v, want the declared %s", c.name, c.got, c.want)
		}
	}
}
