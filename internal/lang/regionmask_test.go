package lang

import "testing"

// TestRegionMasksOffsetsIntoTheHost (#2598): the embedded language's mask
// producer sees only the region's own lines, and its spans come back in host
// coordinates.
func TestRegionMasksOffsetsIntoTheHost(t *testing.T) {
	Register(Language{ID: "maskable", Masks: func(lines []string) []Span {
		var out []Span
		for i, line := range lines {
			if line == "secret" {
				out = append(out, Span{Line: i, StartCol: 0, EndCol: 6, Capture: "secret.value", Replace: "••••"})
			}
		}
		return out
	}})
	Register(Language{ID: "plain"})

	lines := []string{"host", "secret", "secret", "host"}
	got := RegionMasks(lines, []Region{{Lang: "maskable", StartLine: 1, EndLine: 2, EndCol: 6}})
	if len(got) != 2 {
		t.Fatalf("masks = %+v, want one per region line", got)
	}
	if got[0].Line != 1 || got[1].Line != 2 {
		t.Errorf("masks = %+v, want host lines 1 and 2", got)
	}
	if got[0].Replace != "••••" {
		t.Errorf("mask must keep its stand-in, got %+v", got[0])
	}
	// A language without a producer, and an unregistered one, contribute
	// nothing rather than failing the pass.
	if got := RegionMasks(lines, []Region{
		{Lang: "plain", StartLine: 1, EndLine: 2},
		{Lang: "nosuchlang", StartLine: 1, EndLine: 2},
	}); len(got) != 0 {
		t.Errorf("masks = %+v, want none", got)
	}
}

// TestRegionMasksClampsAndShifts (#2598): a region reaching past the buffer is
// clamped, and one starting mid-line has its columns shifted back into host
// coordinates.
func TestRegionMasksClampsAndShifts(t *testing.T) {
	Register(Language{ID: "firstword", Masks: func(lines []string) []Span {
		return []Span{{Line: 0, StartCol: 0, EndCol: 6, Capture: "secret.value", Replace: "••••"}}
	}})

	lines := []string{"pre: secret"}
	got := RegionMasks(lines, []Region{{Lang: "firstword", StartLine: 0, EndLine: 9, StartCol: 5, EndCol: 11}})
	if len(got) != 1 || got[0].StartCol != 5 || got[0].EndCol != 11 {
		t.Fatalf("masks = %+v, want the span shifted to columns 5–11", got)
	}
	if got := RegionMasks(lines, []Region{{Lang: "firstword", StartLine: 5, EndLine: 6}}); len(got) != 0 {
		t.Errorf("masks = %+v, want none for a region outside the buffer", got)
	}
}
