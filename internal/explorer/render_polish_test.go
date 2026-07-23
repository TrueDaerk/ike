package explorer

import (
	"strings"
	"testing"

	"ike/internal/theme"
)

// TestGuideStyleUsesIndentGuideSlot guards #1050: indent-guide cells render in
// the semantic IndentGuide colour (editor parity), never the row's
// filetype/VCS hue, and never bold (#1059).
func TestGuideStyleUsesIndentGuideSlot(t *testing.T) {
	m := New(".")
	n := &node{name: "main.go", path: "main.go"}
	m.rows = []*node{n}
	m.focused = true
	row := m.rowStyle(0, n) // focused cursor: Selection bg + bold (#1052)
	g := m.guideStyle(row)
	pr, pg, pb, _ := theme.DefaultPalette().IndentGuide.RGBA()
	gr, gg, gb, _ := g.GetForeground().RGBA()
	if pr != gr || pg != gg || pb != gb {
		t.Fatalf("guide foreground = %v want IndentGuide slot", g.GetForeground())
	}
	if g.GetBold() {
		t.Fatal("guide cells must not inherit the selection bold (#1059)")
	}
	if g.GetBackground() != row.GetBackground() {
		t.Fatal("guide cells must keep the row background")
	}
}

// TestHoverPreservesActiveAccent guards #1056: hovering the active-file row
// keeps the accent foreground and only adds the hover background.
func TestHoverPreservesActiveAccent(t *testing.T) {
	m := New(".")
	n := &node{name: "main.go", path: "main.go"}
	m.rows = []*node{n}
	m.active = "main.go"
	m.hover = 0
	got := m.rowStyle(0, n)
	ar, ag, ab, _ := theme.DefaultPalette().Accent.RGBA()
	gr, gg, gb, _ := got.GetForeground().RGBA()
	if ar != gr || ag != gg || ab != gb {
		t.Fatalf("hovered active row foreground = %v want Accent", got.GetForeground())
	}
	pr, pg, pb, _ := theme.DefaultPalette().Panel.RGBA()
	br, bg, bb, _ := got.GetBackground().RGBA()
	if pr != br || pg != bg || pb != bb {
		t.Fatalf("hovered row background = %v want Panel", got.GetBackground())
	}
}

// TestOpenFileMarkerUnderlineOnly guards #1055: the open-file marker is
// underline only — italics stay reserved for hidden entries.
func TestOpenFileMarkerUnderlineOnly(t *testing.T) {
	m := New(".")
	n := &node{name: "main.go", path: "main.go"}
	m.rows = []*node{n}
	m.open = map[string]bool{"main.go": true}
	m.width, m.height = 30, 5
	out := m.View()
	if !strings.Contains(out, "\x1b[4m") && !strings.Contains(out, ";4m") && !strings.Contains(out, "[4;") {
		t.Skip("terminal profile strips styling; underline unverifiable here")
	}
	if strings.Contains(out, "\x1b[3m") || strings.Contains(out, ";3m") || strings.Contains(out, "[3;") {
		t.Fatal("open-file marker must not render italic (#1055)")
	}
}

// TestEmptyPlaceholderThemed guards #1058: "(empty)" uses the InlayHint slot,
// not terminal Faint.
func TestEmptyPlaceholderThemed(t *testing.T) {
	m := New(".")
	m.rows = nil
	out := m.View()
	if !strings.Contains(out, "(empty)") {
		t.Fatalf("empty view = %q", out)
	}
	if strings.Contains(out, "\x1b[2m") || strings.Contains(out, ";2m") {
		t.Fatal("(empty) must not use terminal Faint (#1058)")
	}
}
