package explorer

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/host"
)

// TestRootRowPathContext guards #1046: a wide-enough pane renders the root
// row with a home-abbreviated project-path suffix.
func TestRootRowPathContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := New(filepath.Join(home, "proj"))
	m.SetSize(40, 6)
	if got := m.rowText(m.root); !strings.Contains(got, " — ~"+string(filepath.Separator)+"proj") {
		t.Fatalf("root row %q must carry the home-abbreviated path context", got)
	}
	// The rendered view shows the same text — rowText is the single source of
	// truth for what View paints.
	first := stripANSI(strings.Split(m.View(), "\n")[0])
	if !strings.Contains(first, "~"+string(filepath.Separator)+"proj") {
		t.Fatalf("rendered root row %q must show the path context", first)
	}
	// Non-root rows never carry it.
	child := &node{name: "main.go", path: filepath.Join(home, "proj", "main.go"), depth: 1}
	if got := m.rowText(child); strings.Contains(got, "—") {
		t.Fatalf("non-root row %q must not carry path context", got)
	}
}

// TestRootContextSuppressedWhenNarrow guards #1046: a pane narrower than
// minRootContextWidth drops the suffix entirely instead of rendering noise.
func TestRootContextSuppressedWhenNarrow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := New(filepath.Join(home, "proj"))
	m.SetSize(minRootContextWidth-1, 6)
	if got := m.rowText(m.root); strings.Contains(got, "—") {
		t.Fatalf("narrow pane: root row %q must suppress the path context", got)
	}
}

// TestRootContextTruncatedToPane guards #1046: the suffix pre-truncates to the
// pane width, so the root row never widens the content (no horizontal
// scrollbar just because the project path is long).
func TestRootContextTruncatedToPane(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	deep := filepath.Join(home, "some", "very", "deeply", "nested", "project", "directory")
	m := New(deep)
	m.SetSize(32, 6)
	if w := ansi.StringWidth(m.rowText(m.root)); w > 31 {
		t.Fatalf("root row width %d must fit the pane (31 cols after the scrollbar reserve)", w)
	}
	if !strings.HasSuffix(m.rowText(m.root), "…") {
		t.Fatalf("root row %q must end in a truncation ellipsis", m.rowText(m.root))
	}
	_, _, _, needH, _ := m.viewport()
	if needH {
		t.Fatal("the path context alone must not trigger the horizontal scrollbar")
	}
}

// TestAbbrevHome covers the "~" substitution.
func TestAbbrevHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sep := string(filepath.Separator)
	if got := abbrevHome(home); got != "~" {
		t.Errorf("abbrevHome(home) = %q want ~", got)
	}
	if got := abbrevHome(filepath.Join(home, "a", "b")); got != "~"+sep+"a"+sep+"b" {
		t.Errorf("abbrevHome(home/a/b) = %q", got)
	}
	if got := abbrevHome(sep + "elsewhere"); got != sep+"elsewhere" {
		t.Errorf("paths outside home must pass through, got %q", got)
	}
}

// TestIconsOffByDefault guards #1046: without explorer.icons the marker stays
// the two-cell expand caret/blank — no glyph column.
func TestIconsOffByDefault(t *testing.T) {
	m := New(t.TempDir())
	f := &node{name: "main.go", depth: 1}
	_, mark, _, _ := m.rowParts(f)
	if mark != "  " {
		t.Fatalf("icons off: file marker = %q want two blank cells", mark)
	}
	m.Configure(host.MapConfig{})
	if m.icons {
		t.Fatal("explorer.icons must default off")
	}
}

// TestIconsGlyphColumn guards #1046: with explorer.icons on every row gains a
// one-cell class glyph (plus separator) between the expand marker and the
// name, and rowText — the width source of truth — includes it.
func TestIconsGlyphColumn(t *testing.T) {
	m := New(t.TempDir())
	m.Configure(host.MapConfig{"explorer.icons": "true"})
	if !m.icons {
		t.Fatal("explorer.icons=true must enable the glyph column")
	}
	cases := []struct {
		n     *node
		glyph string
	}{
		{&node{name: "main.go"}, classGlyphs[classCode]},
		{&node{name: "README.md"}, classGlyphs[classDoc]},
		{&node{name: "config.toml"}, classGlyphs[classConfig]},
		{&node{name: "logo.png"}, classGlyphs[classImage]},
		{&node{name: "LICENSE"}, classGlyphs[classOther]},
		{&node{name: "sub", isDir: true}, classGlyphs[classDir]},
	}
	for _, c := range cases {
		_, mark, _, _ := m.rowParts(c.n)
		if !strings.HasSuffix(mark, c.glyph+" ") {
			t.Errorf("%s: marker %q must end in glyph %q", c.n.name, mark, c.glyph)
		}
		want := 4 // 2-cell expand marker + glyph + separator
		if w := ansi.StringWidth(mark); w != want {
			t.Errorf("%s: marker width %d want %d", c.n.name, w, want)
		}
		if !strings.Contains(m.rowText(c.n), c.glyph) {
			t.Errorf("%s: rowText must include the glyph", c.n.name)
		}
	}
}

// TestIconsWidthConsistency guards #1046: enabling icons widens rowText and
// contentWidth by exactly the glyph column, keeping clipping/scrollbar math on
// the single source of truth.
func TestIconsWidthConsistency(t *testing.T) {
	m := New(t.TempDir())
	m.SetSize(10, 4) // narrow enough to suppress the root path context
	f := &node{name: "main.go", depth: 1}
	m.rows = []*node{m.root, f}
	m.invalidateWidth() // rows were injected directly (#1096)
	before := m.contentWidth()
	m.icons = true
	m.invalidateWidth() // production toggles go through Configure, which invalidates (#1096)
	if got := m.contentWidth(); got != before+2 {
		t.Fatalf("contentWidth with icons = %d want %d (+2 glyph column)", got, before+2)
	}
	view := stripANSI(strings.Split(m.View(), "\n")[1])
	if !strings.Contains(view, classGlyphs[classCode]) {
		t.Fatalf("rendered row %q must show the code glyph", view)
	}
}

// TestGlyphClassOf covers the classification map's edges.
func TestGlyphClassOf(t *testing.T) {
	cases := map[string]glyphClass{
		"a.GO":     classCode, // case-insensitive
		"a.yml":    classConfig,
		"a.jpeg":   classImage,
		"notes.md": classDoc,
		"Makefile": classOther,
	}
	for name, want := range cases {
		if got := glyphClassOf(&node{name: name}); got != want {
			t.Errorf("glyphClassOf(%s) = %v want %v", name, got, want)
		}
	}
	if got := glyphClassOf(&node{name: "x.go", isDir: true}); got != classDir {
		t.Errorf("directories always classify classDir, got %v", got)
	}
}
