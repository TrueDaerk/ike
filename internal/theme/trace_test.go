package theme

import (
	"image/color"
	"math"
	"testing"
)

// hue is c's HSL hue in degrees, and whether c is saturated enough for the
// hue to mean anything.
func hue(c color.Color) (h float64, chromatic bool) {
	r16, g16, b16, _ := c.RGBA()
	r, g, b := float64(r16)/0xffff, float64(g16)/0xffff, float64(b16)/0xffff
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	d := mx - mn
	if d < 0.08 {
		return 0, false
	}
	switch mx {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, true
}

// hueDistance is the shorter way round the colour wheel from a to b.
func hueDistance(a, b float64) float64 {
	d := math.Abs(a - b)
	if d > 180 {
		d = 360 - d
	}
	return d
}

// minTraceHueGap is how far apart on the colour wheel the four main
// agent-trace box kinds must sit in every built-in (#2874): a different hue,
// not just a lighter or darker shade of the same one.
const minTraceHueGap = 30

// TestBuiltinTraceKindsDistinct: every built-in sets the trace box roles
// explicitly, and question, edit, create and answer are pairwise different
// hues in each.
func TestBuiltinTraceKindsDistinct(t *testing.T) {
	for _, th := range Builtins() {
		ui := th.UI
		for name, v := range map[string]string{
			"TracePrompt": ui.TracePrompt, "TraceEdit": ui.TraceEdit,
			"TraceCreate": ui.TraceCreate, "TraceDelete": ui.TraceDelete,
			"TraceAnswer": ui.TraceAnswer,
		} {
			if v == "" {
				t.Errorf("%s: %s is empty", th.Name, name)
			}
		}
		p := NewPalette(th)
		kinds := []struct {
			name string
			c    color.Color
		}{
			{"prompt", p.TracePrompt}, {"edit", p.TraceEdit},
			{"create", p.TraceCreate}, {"answer", p.TraceAnswer},
		}
		for i := range kinds {
			hi, ok := hue(kinds[i].c)
			if !ok {
				t.Errorf("%s: trace %s %v is grey, has no hue", th.Name, kinds[i].name, kinds[i].c)
				continue
			}
			for j := i + 1; j < len(kinds); j++ {
				hj, _ := hue(kinds[j].c)
				if d := hueDistance(hi, hj); d < minTraceHueGap {
					t.Errorf("%s: trace %s and %s hues %.0f° apart, want >= %d°",
						th.Name, kinds[i].name, kinds[j].name, d, minTraceHueGap)
				}
			}
		}
	}
}

// TestTraceRolesFallBack: a theme without the trace roles resolves them to
// its own generic roles — the colours the graph view used before #2874.
func TestTraceRolesFallBack(t *testing.T) {
	sparse := NewPalette(Theme{Name: "sparse", UI: UI{
		Accent: "#111111", Warning: "#222222", Success: "#333333",
		Error: "#444444", Info: "#555555",
	}})
	for _, c := range []struct {
		name      string
		got, want color.Color
	}{
		{"prompt←accent", sparse.TracePrompt, Resolve("#111111")},
		{"edit←warning", sparse.TraceEdit, Resolve("#222222")},
		{"create←success", sparse.TraceCreate, Resolve("#333333")},
		{"delete←error", sparse.TraceDelete, Resolve("#444444")},
		{"answer←info", sparse.TraceAnswer, Resolve("#555555")},
	} {
		if !sameColor(c.got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
	// A theme without even the generic roles lands on the default's.
	def := NewPalette(Default())
	empty := NewPalette(Theme{Name: "empty"})
	if !sameColor(empty.TracePrompt, def.Accent) || !sameColor(empty.TraceAnswer, def.Info) {
		t.Errorf("empty theme: prompt %v answer %v, want default accent %v / info %v",
			empty.TracePrompt, empty.TraceAnswer, def.Accent, def.Info)
	}
	// An explicit role wins over the fallback.
	set := NewPalette(Theme{Name: "set", UI: UI{Accent: "#111111", TracePrompt: "#abcdef"}})
	if !sameColor(set.TracePrompt, Resolve("#abcdef")) {
		t.Errorf("explicit TracePrompt: got %v", set.TracePrompt)
	}
}
