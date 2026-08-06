package shotpng

import (
	"fmt"
	"os"
	"runtime"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// FontFile names one face inside a font file. Index selects the face of a
// TrueType collection (.ttc); Name selects it by PostScript name instead,
// which survives a macOS font update reordering the collection.
type FontFile struct {
	Path  string
	Index int
	Name  string
}

// FontSpec is the font set a render uses: the four monospace styles plus
// fallback files consulted for glyphs the main styles do not carry (symbols
// like ● or ✓ live outside Menlo on macOS). An empty Regular.Path loads the
// embedded Go Mono set, which is always available — including on CI, where no
// system font can be assumed.
type FontSpec struct {
	Regular    FontFile
	Bold       FontFile
	Italic     FontFile
	BoldItalic FontFile
	Fallbacks  []FontFile
	// Size is the em size in pixels; 0 falls back to DefaultFontSize.
	Size float64
	// LineHeight is the cell height as a multiple of Size; 0 means 1.25.
	LineHeight float64
}

// DefaultFontSize is the em size used when a spec leaves Size unset. At this
// size a 100x30 frame lands around 1200x1100 px — readable in the docs
// without scaling the image up.
const DefaultFontSize = 15

// fontCandidate is one system monospace family: the file plus the PostScript
// names of its four styles.
type fontCandidate struct {
	path                                   string
	regular, bold, italic, boldItalic      string
	fallbacks                              []string
}

// systemFonts lists the families tried, in order, when no explicit spec is
// given. Only the first existing entry is used; if none exists the embedded Go
// Mono set renders instead.
var systemFonts = map[string][]fontCandidate{
	"darwin": {{
		path:       "/System/Library/Fonts/Menlo.ttc",
		regular:    "Menlo-Regular",
		bold:       "Menlo-Bold",
		italic:     "Menlo-Italic",
		boldItalic: "Menlo-BoldItalic",
		fallbacks: []string{
			"/System/Library/Fonts/Apple Symbols.ttf",
			"/Library/Fonts/Arial Unicode.ttf",
		},
	}},
	"linux": {{
		path:       "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
		bold:       "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf",
		italic:     "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Oblique.ttf",
		boldItalic: "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-BoldOblique.ttf",
	}},
}

// DefaultFontSpec returns the best available monospace set for this platform
// at the given size (0 = DefaultFontSize). It never fails: with no system font
// present the spec is empty, which LoadFonts resolves to embedded Go Mono.
func DefaultFontSpec(size float64) FontSpec {
	spec := FontSpec{Size: size}
	for _, c := range systemFonts[runtime.GOOS] {
		if _, err := os.Stat(c.path); err != nil {
			continue
		}
		if c.regular != "" {
			// One collection file holding all four styles (macOS .ttc).
			spec.Regular = FontFile{Path: c.path, Name: c.regular}
			spec.Bold = FontFile{Path: c.path, Name: c.bold}
			spec.Italic = FontFile{Path: c.path, Name: c.italic}
			spec.BoldItalic = FontFile{Path: c.path, Name: c.boldItalic}
		} else {
			// One file per style (the usual Linux packaging).
			spec.Regular = FontFile{Path: c.path}
			spec.Bold = FontFile{Path: c.bold}
			spec.Italic = FontFile{Path: c.italic}
			spec.BoldItalic = FontFile{Path: c.boldItalic}
		}
		for _, f := range c.fallbacks {
			if _, err := os.Stat(f); err == nil {
				spec.Fallbacks = append(spec.Fallbacks, FontFile{Path: f})
			}
		}
		break
	}
	return spec
}

// styledFace pairs a parsed font with its sized face and a scratch buffer for
// glyph lookups. Not safe for concurrent use — one render at a time.
type styledFace struct {
	font *sfnt.Font
	face font.Face
	buf  sfnt.Buffer
}

// has reports whether the face carries a glyph for r (index 0 is .notdef).
func (s *styledFace) has(r rune) bool {
	if s == nil {
		return false
	}
	g, err := s.font.GlyphIndex(&s.buf, r)
	return err == nil && g != 0
}

// Fonts is a loaded font set with the cell metrics derived from it. Cells are
// integer-sized: every glyph is placed at a cell origin, so the character grid
// stays exactly aligned even when the font's advance is fractional.
type Fonts struct {
	regular    *styledFace
	bold       *styledFace
	italic     *styledFace
	boldItalic *styledFace
	fallbacks  []*styledFace

	// CellW, CellH and Ascent are pixel metrics of one character cell.
	CellW, CellH, Ascent int
	// synthBold marks a set without a real bold face: bold is then faked by
	// drawing the regular glyph twice, one pixel apart.
	synthBold bool
}

// LoadFonts resolves a spec into a usable font set. A missing bold/italic file
// is not an error — the renderer falls back to the regular face (bold gets
// synthesised).
func LoadFonts(spec FontSpec) (*Fonts, error) {
	size := spec.Size
	if size <= 0 {
		size = DefaultFontSize
	}
	lh := spec.LineHeight
	if lh <= 0 {
		lh = 1.25
	}
	f := &Fonts{}
	var err error
	if spec.Regular.Path == "" {
		if f.regular, err = embeddedFace(gomono.TTF, size); err != nil {
			return nil, err
		}
		f.bold, _ = embeddedFace(gomonobold.TTF, size)
		f.italic, _ = embeddedFace(gomonoitalic.TTF, size)
		f.boldItalic, _ = embeddedFace(gomonobolditalic.TTF, size)
	} else {
		if f.regular, err = loadFace(spec.Regular, size); err != nil {
			return nil, fmt.Errorf("regular font: %w", err)
		}
		f.bold, _ = loadFace(spec.Bold, size)
		f.italic, _ = loadFace(spec.Italic, size)
		f.boldItalic, _ = loadFace(spec.BoldItalic, size)
	}
	f.synthBold = f.bold == nil
	for _, fb := range spec.Fallbacks {
		if face, err := loadFace(fb, size); err == nil {
			f.fallbacks = append(f.fallbacks, face)
		}
	}

	metrics := f.regular.face.Metrics()
	advance, ok := f.regular.face.GlyphAdvance('M')
	if !ok {
		return nil, fmt.Errorf("font has no 'M' glyph to measure the cell from")
	}
	f.CellW = ceilPx(advance)
	f.CellH = int(size*lh + 0.5)
	// Centre the font's line box in the cell, so the extra leading is split
	// between the rows instead of hanging below the descenders.
	lineBox := ceilPx(metrics.Ascent) + ceilPx(metrics.Descent)
	f.Ascent = ceilPx(metrics.Ascent) + max(0, (f.CellH-lineBox)/2)
	return f, nil
}

// faceFor picks the face for a style, with fallbacks for glyphs the styled
// face lacks. bold reports whether the caller must synthesise weight.
func (f *Fonts) faceFor(r rune, bold, italic bool) (face *styledFace, synth bool) {
	var order []*styledFace
	switch {
	case bold && italic:
		order = []*styledFace{f.boldItalic, f.bold, f.italic, f.regular}
	case bold:
		order = []*styledFace{f.bold, f.regular}
	case italic:
		order = []*styledFace{f.italic, f.regular}
	default:
		order = []*styledFace{f.regular}
	}
	for _, c := range order {
		if c.has(r) {
			return c, bold && (c == f.regular || c == f.italic)
		}
	}
	for _, c := range f.fallbacks {
		if c.has(r) {
			return c, bold
		}
	}
	return f.regular, bold && f.synthBold
}

// loadFace parses one FontFile at the given size, resolving a collection entry
// by PostScript name when the spec gives one.
func loadFace(ff FontFile, size float64) (*styledFace, error) {
	if ff.Path == "" {
		return nil, fmt.Errorf("no font path")
	}
	data, err := os.ReadFile(ff.Path)
	if err != nil {
		return nil, err
	}
	coll, err := sfnt.ParseCollection(data)
	if err != nil {
		return nil, err
	}
	idx := ff.Index
	if ff.Name != "" {
		idx = -1
		var buf sfnt.Buffer
		for i := 0; i < coll.NumFonts(); i++ {
			fnt, err := coll.Font(i)
			if err != nil {
				continue
			}
			if name, err := fnt.Name(&buf, sfnt.NameIDPostScript); err == nil && name == ff.Name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("%s: no face named %q", ff.Path, ff.Name)
		}
	}
	if idx >= coll.NumFonts() {
		return nil, fmt.Errorf("%s: face %d of %d", ff.Path, idx, coll.NumFonts())
	}
	fnt, err := coll.Font(idx)
	if err != nil {
		return nil, err
	}
	return newFace(fnt, size)
}

// embeddedFace builds a face from one of the compiled-in Go Mono styles.
func embeddedFace(ttf []byte, size float64) (*styledFace, error) {
	fnt, err := opentype.Parse(ttf)
	if err != nil {
		return nil, err
	}
	return newFace(fnt, size)
}

func newFace(fnt *sfnt.Font, size float64) (*styledFace, error) {
	face, err := opentype.NewFace(fnt, &opentype.FaceOptions{
		Size:    size,
		DPI:     72, // size is then plain pixels
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	return &styledFace{font: fnt, face: face}, nil
}

func ceilPx(v fixed.Int26_6) int { return int(v.Ceil()) }
