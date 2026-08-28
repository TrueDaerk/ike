package theme

import (
	"image/color"
	"testing"
)

// diffemph_test.go guards the intra-line emphasis backgrounds (#2170): a
// changed range inside a diff line must read as a stronger patch of its own
// side's colour, without leaving the readability envelope the line
// backgrounds live in.

// sameColor compares two resolved colours by their RGBA components.
func sameColor(a, b color.Color) bool {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	return ar == br && ag == bg && ab == bb
}

// TestDiffEmphDistinctFromLine: in every built-in, each emphasis background
// differs from the line background it sits in — otherwise the refinement is
// invisible — and stays on that side's hue rather than borrowing the other's.
func TestDiffEmphDistinctFromLine(t *testing.T) {
	for _, th := range Builtins() {
		p := NewPalette(th)
		t.Run(th.Name, func(t *testing.T) {
			for _, c := range []struct {
				name       string
				emph, base color.Color
			}{
				{"added", p.DiffAddedEmph, p.DiffAdded},
				{"removed", p.DiffRemovedEmph, p.DiffRemoved},
			} {
				if sameColor(c.emph, c.base) {
					t.Errorf("%s: emphasis equals the line background", c.name)
				}
				// The step must be visible but bounded: a perceptible
				// luminance step, at most emphHeadroom beyond the line
				// background's own drift from Surface.
				if r := ContrastRatio(c.emph, c.base); r < 1.03 {
					t.Errorf("%s: emphasis/line contrast %.3f:1, want >= 1.03:1", c.name, r)
				}
				maxRatio := ContrastRatio(c.base, p.Surface)*emphHeadroom + 0.02
				if r := ContrastRatio(c.emph, p.Surface); r > maxRatio {
					t.Errorf("%s: emphasis/Surface %.2f:1 leaves the envelope (max %.2f:1)", c.name, r, maxRatio)
				}
			}
			if sameColor(p.DiffAddedEmph, p.DiffRemovedEmph) {
				t.Error("added and removed emphasis are the same colour")
			}
		})
	}
}

// TestDiffEmphSparseAndExplicit: a theme that declares neither slot still gets
// per-side emphasis derived from its own diff backgrounds, and a theme that
// declares them keeps exactly what it declared.
func TestDiffEmphSparseAndExplicit(t *testing.T) {
	sparse := NewPalette(Theme{Name: "sparse", UI: UI{Surface: "#202020", Success: "#00ff00", Error: "#ff0000"}})
	if sameColor(sparse.DiffAddedEmph, sparse.DiffAdded) || sameColor(sparse.DiffRemovedEmph, sparse.DiffRemoved) {
		t.Error("sparse theme: emphasis did not derive from the line backgrounds")
	}
	explicit := NewPalette(Theme{Name: "explicit", UI: UI{DiffAddedEmph: "#123456", DiffRemovedEmph: "#654321"}})
	if !sameColor(explicit.DiffAddedEmph, Resolve("#123456")) {
		t.Errorf("DiffAddedEmph = %v, want the declared token", explicit.DiffAddedEmph)
	}
	if !sameColor(explicit.DiffRemovedEmph, Resolve("#654321")) {
		t.Errorf("DiffRemovedEmph = %v, want the declared token", explicit.DiffRemovedEmph)
	}
}
