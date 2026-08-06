package shotpng

import (
	"os"
	"runtime"
	"testing"
)

// TestLoadFontsEmbeddedFallback guards the CI path: with no font file named,
// the embedded Go Mono set loads and yields usable cell metrics.
func TestLoadFontsEmbeddedFallback(t *testing.T) {
	f, err := LoadFonts(FontSpec{Size: 14})
	if err != nil {
		t.Fatalf("LoadFonts: %v", err)
	}
	if f.CellW <= 0 || f.CellH <= 0 {
		t.Fatalf("cell metrics = %dx%d, want positive", f.CellW, f.CellH)
	}
	if f.Ascent <= 0 || f.Ascent > f.CellH {
		t.Errorf("ascent = %d, want inside the cell height %d", f.Ascent, f.CellH)
	}
	// The default line height is 1.25 em: 14 * 1.25 = 17.5, rounded up.
	if want := 18; f.CellH != want {
		t.Errorf("cell height = %d, want %d for a 14px em", f.CellH, want)
	}
}

// TestLoadFontsMissingFile reports a bad path instead of silently rendering
// with a substitute font — a screenshot run must fail loudly.
func TestLoadFontsMissingFile(t *testing.T) {
	_, err := LoadFonts(FontSpec{Regular: FontFile{Path: "/nope/none.ttf"}})
	if err == nil {
		t.Fatal("want an error for a missing font file")
	}
}

// TestLoadFontsUnknownCollectionName guards the by-name face lookup: naming a
// face the collection does not hold is an error, not face 0.
func TestLoadFontsUnknownCollectionName(t *testing.T) {
	spec := DefaultFontSpec(0)
	if spec.Regular.Path == "" {
		t.Skip("no system font on this machine")
	}
	_, err := LoadFonts(FontSpec{Regular: FontFile{Path: spec.Regular.Path, Name: "No-Such-Face"}})
	if err == nil {
		t.Fatal("want an error for an unknown face name")
	}
}

// TestDefaultFontSpecPlatform checks the platform default resolves to a font
// that exists — the screenshots are generated with it.
func TestDefaultFontSpecPlatform(t *testing.T) {
	spec := DefaultFontSpec(0)
	if spec.Regular.Path == "" {
		t.Skipf("no known system monospace font on %s", runtime.GOOS)
	}
	if _, err := os.Stat(spec.Regular.Path); err != nil {
		t.Fatalf("default font %q: %v", spec.Regular.Path, err)
	}
	if _, err := LoadFonts(spec); err != nil {
		t.Fatalf("loading the platform default: %v", err)
	}
}

// TestFaceForFallsBack guards the per-glyph fallback: a rune the monospace
// face lacks must be looked up in the fallback fonts before giving up.
func TestFaceForFallsBack(t *testing.T) {
	f, err := LoadFonts(DefaultFontSpec(14))
	if err != nil {
		t.Fatalf("LoadFonts: %v", err)
	}
	// 'a' is in every face, so the regular face must answer for it.
	if face, _ := f.faceFor('a', false, false); face != f.regular {
		t.Error("plain 'a' must use the regular face")
	}
	if len(f.fallbacks) == 0 {
		t.Skip("no fallback fonts on this machine")
	}
	// A rune the primary face lacks must resolve to a fallback, if any has it.
	const rare = '✿'
	if f.regular.has(rare) {
		t.Skip("the primary face carries the probe rune")
	}
	face, _ := f.faceFor(rare, false, false)
	if face == f.regular {
		found := false
		for _, fb := range f.fallbacks {
			if fb.has(rare) {
				found = true
			}
		}
		if found {
			t.Error("a fallback carries the rune but faceFor returned the regular face")
		}
	}
}
