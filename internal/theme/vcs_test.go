package theme

import (
	"image/color"
	"math"
	"testing"
)

// TestVCSSlotsFallBackToSemanticHues verifies the VCS status slots (Roadmap
// 0320, #463) resolve for sparse themes: empty slots derive from the theme's
// own semantic colors, explicit tokens win.
func TestVCSSlotsFallBackToSemanticHues(t *testing.T) {
	sparse := NewPalette(Theme{Name: "sparse", UI: UI{
		Info:    "#1111ff",
		Success: "#11ff11",
		Warning: "#ffaa11",
		Error:   "#ff1111",
		Border:  "#555555",
	}})
	pairs := []struct {
		name      string
		got, want any
	}{
		{"modified←info", sparse.VCSModified, Resolve("#1111ff")},
		{"added←success", sparse.VCSAdded, Resolve("#11ff11")},
		{"conflicted←error", sparse.VCSConflicted, Resolve("#ff1111")},
		// #1868: untracked is the violet halfway between error and info,
		// deleted the error red muted halfway toward the border tone.
		{"untracked←error+info", sparse.VCSUntracked, Mix(Resolve("#ff1111"), Resolve("#1111ff"), 0.5)},
		{"deleted←error+border", sparse.VCSDeleted, Mix(Resolve("#ff1111"), Resolve("#555555"), 0.5)},
	}
	for _, p := range pairs {
		if p.got != p.want {
			t.Errorf("%s: got %v want %v", p.name, p.got, p.want)
		}
	}

	explicit := NewPalette(Theme{Name: "explicit", UI: UI{VCSModified: "#abcdef"}})
	if explicit.VCSModified != Resolve("#abcdef") {
		t.Errorf("explicit slot overridden: %v", explicit.VCSModified)
	}

	// A fully empty theme still resolves every slot (default fallbacks).
	empty := NewPalette(Theme{Name: "empty"})
	for name, c := range map[string]any{
		"modified": empty.VCSModified, "added": empty.VCSAdded,
		"untracked": empty.VCSUntracked, "deleted": empty.VCSDeleted,
		"conflicted": empty.VCSConflicted,
	} {
		if c == nil {
			t.Errorf("empty theme: %s slot is nil", name)
		}
	}
}

// colorDistance is the low-cost perceptual RGB distance ("redmean") used to
// assert that two slots read as different colors, not merely different hex —
// the WCAG contrast ratio only compares luminance and would rate two equally
// bright hues identical.
func colorDistance(a, b color.Color) float64 {
	ch := func(c color.Color) (float64, float64, float64) {
		r, g, bl, _ := c.RGBA()
		return float64(r) / 257, float64(g) / 257, float64(bl) / 257
	}
	r1, g1, b1 := ch(a)
	r2, g2, b2 := ch(b)
	rm := (r1 + r2) / 2
	dr, dg, db := r1-r2, g1-g2, b1-b2
	return math.Sqrt((2+rm/256)*dr*dr + 4*dg*dg + (2+(255-rm)/256)*db*db)
}

// TestVCSSlotsReadAsDistinctColors guards #1868: the git-workflow palette —
// blue modified, green added, violet untracked, muted red deleted, red
// conflicted — must stay mutually distinguishable in every built-in, and
// untracked must not read like a plain (open, underlined) file in the theme
// foreground.
func TestVCSSlotsReadAsDistinctColors(t *testing.T) {
	// Measured floor: the tightest built-in pair sits around 62 (see #1868);
	// 50 leaves headroom for tuning without allowing two slots to collapse.
	const minDistance = 50
	for _, th := range Builtins() {
		p := NewPalette(th)
		t.Run(th.Name, func(t *testing.T) {
			slots := []struct {
				name string
				c    color.Color
			}{
				{"modified", p.VCSModified}, {"added", p.VCSAdded},
				{"untracked", p.VCSUntracked}, {"deleted", p.VCSDeleted},
				{"conflicted", p.VCSConflicted},
			}
			for i, a := range slots {
				for _, b := range slots[i+1:] {
					if d := colorDistance(a.c, b.c); d < minDistance {
						t.Errorf("%s/%s: distance %.1f, want >= %d", a.name, b.name, d, minDistance)
					}
				}
			}
			if d := colorDistance(p.VCSUntracked, p.Foreground); d < minDistance {
				t.Errorf("untracked/Foreground: distance %.1f, want >= %d", d, minDistance)
			}
		})
	}
}
