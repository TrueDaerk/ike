package theme

import (
	"image/color"
	"math"
	"testing"
)

// relativeLuminance implements the WCAG 2.x relative-luminance formula
// (https://www.w3.org/TR/WCAG21/#dfn-relative-luminance).
func relativeLuminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	lin := func(v uint32) float64 {
		s := float64(v) / 0xffff
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// contrastRatio returns the WCAG contrast ratio between two colors, in [1, 21].
func contrastRatio(a, b color.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// minTextContrast is the WCAG AA threshold for normal text. Every built-in
// theme must clear it on the fg/bg slot pairs the chrome actually renders;
// new themes that ship unreadable pairs fail this test.
const minTextContrast = 4.5

// TestBuiltinThemeContrast audits every built-in theme across the semantic
// fg/bg slot pairs used by the UI chrome (issue #384).
func TestBuiltinThemeContrast(t *testing.T) {
	pairs := []struct {
		name string
		fg   func(*Palette) color.Color
		bg   func(*Palette) color.Color
	}{
		{"Foreground/Background", func(p *Palette) color.Color { return p.Foreground }, func(p *Palette) color.Color { return p.Background }},
		{"Foreground/Surface", func(p *Palette) color.Color { return p.Foreground }, func(p *Palette) color.Color { return p.Surface }},
		{"Foreground/Panel", func(p *Palette) color.Color { return p.Foreground }, func(p *Palette) color.Color { return p.Panel }},
		{"SelectionText/Selection", func(p *Palette) color.Color { return p.SelectionText }, func(p *Palette) color.Color { return p.Selection }},
		{"SelectionText/Primary", func(p *Palette) color.Color { return p.SelectionText }, func(p *Palette) color.Color { return p.Primary }},
		{"Accent/Surface", func(p *Palette) color.Color { return p.Accent }, func(p *Palette) color.Color { return p.Surface }},
		{"Secondary/Surface", func(p *Palette) color.Color { return p.Secondary }, func(p *Palette) color.Color { return p.Surface }},
		{"Secondary/Panel", func(p *Palette) color.Color { return p.Secondary }, func(p *Palette) color.Color { return p.Panel }},
		{"Success/Surface", func(p *Palette) color.Color { return p.Success }, func(p *Palette) color.Color { return p.Surface }},
		{"Warning/Surface", func(p *Palette) color.Color { return p.Warning }, func(p *Palette) color.Color { return p.Surface }},
		{"Warning/Panel", func(p *Palette) color.Color { return p.Warning }, func(p *Palette) color.Color { return p.Panel }},
		{"Error/Surface", func(p *Palette) color.Color { return p.Error }, func(p *Palette) color.Color { return p.Surface }},
		{"Error/Panel", func(p *Palette) color.Color { return p.Error }, func(p *Palette) color.Color { return p.Panel }},
		{"Info/Surface", func(p *Palette) color.Color { return p.Info }, func(p *Palette) color.Color { return p.Surface }},
		{"Info/Panel", func(p *Palette) color.Color { return p.Info }, func(p *Palette) color.Color { return p.Panel }},
		{"Hint/Surface", func(p *Palette) color.Color { return p.Hint }, func(p *Palette) color.Color { return p.Surface }},
		{"Hint/Panel", func(p *Palette) color.Color { return p.Hint }, func(p *Palette) color.Color { return p.Panel }},
	}
	for _, th := range Builtins() {
		p := NewPalette(th)
		t.Run(th.Name, func(t *testing.T) {
			for _, pair := range pairs {
				ratio := contrastRatio(pair.fg(p), pair.bg(p))
				if ratio < minTextContrast {
					t.Errorf("%s: contrast %.2f:1, want >= %.1f:1", pair.name, ratio, minTextContrast)
				}
			}
		})
	}
}

// Thresholds for the full-matrix audit below.
const (
	// minDimContrast is the floor for text that is deliberately low-emphasis
	// (comments, punctuation, inlay hints, deleted-file rows). It still has to
	// be legible, just quieter than body text.
	minDimContrast = 3.5
	// maxOverlayContrast caps how far an overlay background (visual selection,
	// ruler stripe, occurrence mark, diff line) may drift from Surface. Keeping
	// overlays close in luminance is what guarantees that syntax colors chosen
	// against Surface stay readable once an overlay is painted underneath them.
	maxOverlayContrast = 1.35
	// maxSelectionContrast is the same cap for the two "strong" backgrounds —
	// the list selection and the completion primary row — which are allowed a
	// slightly bolder step because they also carry SelectionText.
	maxSelectionContrast = 1.5
	// minOverlayTextContrast is the resulting floor for dim text painted over
	// an overlay (minDimContrast / maxSelectionContrast, rounded down).
	minOverlayTextContrast = 2.3
)

// dimTextSlots are the slots held to the tier's dim threshold instead of its
// text threshold. A tier may set both to the same number, which is exactly what
// "no dim class" means for the high-contrast themes.
var dimTextSlots = map[string]bool{
	"InlayHint":           true,
	"VCSDeleted":          true,
	"capture:comment":     true,
	"capture:punctuation": true,
	"file:lock":           true,
	"file:go.sum":         true, // shares the lock tint (#1366)
	"file:log":            true, // shares the lock tint (#1621)
	"file:diff":           true, // shares the lock tint (#1630)
	"file:patch":          true, // shares the lock tint (#1630)
}

// contrastTier is the threshold set one theme is audited against. Themes may
// aim above the baseline floor; the audit then holds them to what they aim for,
// so a stricter theme cannot silently regress to merely passing (#1229).
type contrastTier struct {
	text        float64 // fg on Background/Surface/Panel
	dim         float64 // same, for dimTextSlots
	overlay     float64 // max drift of an overlay background from Surface
	selection   float64 // same, for the two strong backgrounds
	overlayText float64 // dim text over the strongest overlay
}

// baselineTier is the readability floor every built-in must clear (#1226).
var baselineTier = contrastTier{
	text:        minTextContrast,
	dim:         minDimContrast,
	overlay:     maxOverlayContrast,
	selection:   maxSelectionContrast,
	overlayText: minOverlayTextContrast,
}

// aaaTier is the stricter target of the two high-contrast themes (#1229):
// WCAG AAA everywhere, no dim class (dim == text), and overlays capped at
// 1.2:1 — so the worst pair in the theme is still ~5.8:1 (7 / 1.2).
var aaaTier = contrastTier{
	text:        7,
	dim:         7,
	overlay:     1.2,
	selection:   1.2,
	overlayText: 5.8,
}

// tierByTheme overrides the baseline per theme name; everything absent here
// keeps baselineTier, so adding a strict theme never tightens the others.
var tierByTheme = map[string]contrastTier{
	"high-contrast-dark":  aaaTier,
	"high-contrast-light": aaaTier,
}

func tierFor(name string) contrastTier {
	if t, ok := tierByTheme[name]; ok {
		return t
	}
	return baselineTier
}

// TestBuiltinThemeFullContrast is the exhaustive version of the audit above: it
// walks every text color a theme can paint (chrome, diagnostics, VCS status,
// syntax captures, explorer file colors) against every background it can land
// on, and asserts that no theme ships a dark-on-dark or light-on-light pair.
func TestBuiltinThemeFullContrast(t *testing.T) {
	for _, th := range Builtins() {
		p := NewPalette(th)
		tier := tierFor(th.Name)
		t.Run(th.Name, func(t *testing.T) {
			// eps absorbs the rounding loss of storing tuned colors as 8-bit hex.
			const eps = 0.02
			check := func(name string, fg, bg color.Color, want float64) {
				t.Helper()
				if r := contrastRatio(fg, bg); r < want-eps {
					t.Errorf("%s: contrast %.2f:1, want >= %.2f:1", name, r, want)
				}
			}
			// Overlay backgrounds must stay near Surface.
			for _, o := range []struct {
				name string
				c    color.Color
				max  float64
			}{
				{"SelectionMuted", p.SelectionMuted, tier.overlay},
				{"Ruler", p.Ruler, tier.overlay},
				{"OccurrenceRead", p.OccurrenceRead, tier.overlay},
				{"OccurrenceWrite", p.OccurrenceWrite, tier.overlay},
				{"DiffAdded", p.DiffAdded, tier.overlay},
				{"DiffRemoved", p.DiffRemoved, tier.overlay},
				{"DiffChanged", p.DiffChanged, tier.overlay},
				{"Selection", p.Selection, tier.selection},
				{"Primary", p.Primary, tier.selection},
			} {
				if r := contrastRatio(o.c, p.Surface); r > o.max+eps {
					t.Errorf("%s/Surface: overlay contrast %.2f:1, want <= %.2f:1", o.name, r, o.max)
				}
			}
			// Every text color against every background it can land on. Chrome
			// text reaches all three base surfaces; syntax captures only ever
			// paint the editor body (Surface); explorer file colors paint the
			// pane body and the Panel-backed hover row.
			type named struct {
				name string
				c    color.Color
			}
			chromeBases := []named{
				{"Background", p.Background}, {"Surface", p.Surface}, {"Panel", p.Panel},
			}
			captureBases := []named{{"Surface", p.Surface}}
			fileBases := []named{{"Surface", p.Surface}, {"Panel", p.Panel}}
			overlays := []named{
				{"Selection", p.Selection}, {"SelectionMuted", p.SelectionMuted},
				{"Ruler", p.Ruler}, {"OccurrenceRead", p.OccurrenceRead},
				{"OccurrenceWrite", p.OccurrenceWrite}, {"DiffAdded", p.DiffAdded},
				{"DiffRemoved", p.DiffRemoved}, {"DiffChanged", p.DiffChanged},
			}
			audit := func(fg named, bases []named) {
				want := tier.text
				if dimTextSlots[fg.name] {
					want = tier.dim
				}
				for _, b := range bases {
					check(fg.name+"/"+b.name, fg.c, b.c, want)
				}
				for _, b := range overlays {
					check(fg.name+"/"+b.name, fg.c, b.c, tier.overlayText)
				}
			}
			for _, f := range []named{
				{"Foreground", p.Foreground}, {"Accent", p.Accent}, {"Secondary", p.Secondary},
				{"Success", p.Success}, {"Warning", p.Warning}, {"Error", p.Error},
				{"Info", p.Info}, {"Hint", p.Hint}, {"InlayHint", p.InlayHint},
				{"VCSModified", p.VCSModified}, {"VCSAdded", p.VCSAdded},
				{"VCSUntracked", p.VCSUntracked}, {"VCSDeleted", p.VCSDeleted},
				{"VCSConflicted", p.VCSConflicted},
			} {
				audit(f, chromeBases)
			}
			for k, v := range p.Captures {
				audit(named{"capture:" + k, Resolve(v)}, captureBases)
			}
			for k, v := range p.Files {
				audit(named{"file:" + k, Resolve(v)}, fileBases)
			}
			// The two strong backgrounds also carry SelectionText.
			check("SelectionText/Selection", p.SelectionText, p.Selection, tier.text)
			check("SelectionText/Primary", p.SelectionText, p.Primary, tier.text)
		})
	}
}

func TestContrastRatioFormula(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"#ffffff", "#000000", 21},
		{"#ffffff", "#ffffff", 1},
		{"#777777", "#ffffff", 4.48}, // canonical WCAG example just below AA
	}
	for _, c := range cases {
		got := contrastRatio(Resolve(c.a), Resolve(c.b))
		if math.Abs(got-c.want) > 0.02 {
			t.Errorf("contrastRatio(%s, %s) = %.2f, want %.2f", c.a, c.b, got, c.want)
		}
	}
}

// TestHighContrastThemesUseStrictTier guards #1229 from both sides: the two
// high-contrast built-ins are registered and audited at the AAA tier (a rename
// or a dropped map entry would silently demote them to the baseline), and no
// other built-in was tightened along with them.
func TestHighContrastThemesUseStrictTier(t *testing.T) {
	strict := map[string]bool{"high-contrast-dark": false, "high-contrast-light": false}
	for _, th := range Builtins() {
		if _, ok := strict[th.Name]; ok {
			strict[th.Name] = true
			if tierFor(th.Name) != aaaTier {
				t.Errorf("%s must be audited at the AAA tier", th.Name)
			}
			continue
		}
		if tierFor(th.Name) != baselineTier {
			t.Errorf("%s must keep the baseline tier: adding a strict theme may not tighten the others", th.Name)
		}
	}
	for name, found := range strict {
		if !found {
			t.Errorf("built-in theme %q missing from Builtins()", name)
		}
	}
}
