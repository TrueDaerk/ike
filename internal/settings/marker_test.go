package settings

// marker_test.go covers the selection marker of #1689: the settings lists
// prefix their selected row with the "❯" chevron Search Everywhere / Recent
// Files use, accent-coloured while the column has focus and muted otherwise,
// with unselected rows keeping the same indent so nothing jumps sideways.

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
)

// colLine returns the [x, x+w) slice of the first stripped view line whose
// slice contains want — i.e. the row of one specific column, so a title bar or
// a neighbouring column carrying the same word cannot be mistaken for it.
func colLine(t *testing.T, view string, x, w int, want string) string {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(view), "\n") {
		r := []rune(line)
		if y < bodyTop || len(r) < x+w {
			continue // above the body: the title bar and the breadcrumb
		}
		if cell := string(r[x : x+w]); strings.Contains(cell, want) {
			return cell
		}
	}
	t.Fatalf("no row of the column at x=%d contains %q:\n%s", x, want, view)
	return ""
}

// railLine returns the rail row carrying want.
func railLine(t *testing.T, view, want string) string {
	t.Helper()
	return colLine(t, view, 1, catWidth, want)
}

// TestRowMarkerGlyphAndColour: an unselected row pays the marker's width in
// blanks, a selected one draws the chevron — accent while focused, the muted
// border colour otherwise.
func TestRowMarkerGlyphAndColour(t *testing.T) {
	pal := theme.DefaultPalette()
	plain := lipgloss.NewStyle()

	if got := rowMarker(pal, false, true, plain); ansi.Strip(got) != "  " {
		t.Fatalf("unselected marker = %q, want %d blanks", ansi.Strip(got), markerWidth)
	}
	focused := rowMarker(pal, true, true, plain)
	muted := rowMarker(pal, true, false, plain)
	for name, got := range map[string]string{"focused": focused, "unfocused": muted} {
		if s := ansi.Strip(got); s != "❯ " {
			t.Fatalf("%s marker = %q, want the chevron", name, s)
		}
		if lipgloss.Width(got) != markerWidth {
			t.Fatalf("%s marker width = %d, want %d", name, lipgloss.Width(got), markerWidth)
		}
	}
	if want := plain.Foreground(pal.Accent).Bold(true).Render("❯ "); focused != want {
		t.Fatalf("focused marker = %q, want the accent-coloured %q", focused, want)
	}
	if want := plain.Foreground(pal.Border).Bold(true).Render("❯ "); muted != want {
		t.Fatalf("unfocused marker = %q, want the muted %q", muted, want)
	}
}

// TestRowMarkerKeepsSelectionBackground: on a selected row the marker sits on
// the selection band, so the band has no hole in its first cells.
func TestRowMarkerKeepsSelectionBackground(t *testing.T) {
	pal := theme.DefaultPalette()
	bg := lipgloss.NewStyle().Background(pal.Selection)
	got := rowMarker(pal, true, true, bg)
	if got == rowMarker(pal, true, true, lipgloss.NewStyle()) {
		t.Fatal("a selected marker must carry the row's selection background")
	}
}

// TestRailMarksSelectedPage: the rail's selected page carries the chevron and
// no other rail row does; arrowing down moves the marker with the selection.
func TestRailMarksSelectedPage(t *testing.T) {
	restoreConfig(t)
	m := New(sectionPages(), testOpts(t))
	m.SetSize(120, 20)
	m.Open()

	if l := railLine(t, m.View(), "Editor"); !strings.HasPrefix(strings.TrimRight(l, " "), "❯ Editor") {
		t.Fatalf("the selected page must carry the chevron, got %q", l)
	}
	if l := railLine(t, m.View(), "Appearance"); strings.Contains(l, "❯") {
		t.Fatalf("an unselected page must not carry a chevron, got %q", l)
	}
	m.Update(key("down"))
	if l := railLine(t, m.View(), "Appearance"); !strings.HasPrefix(strings.TrimRight(l, " "), "❯ Appearance") {
		t.Fatalf("the marker must follow the selection, got %q", l)
	}
	if l := railLine(t, m.View(), "Editor"); strings.Contains(l, "❯") {
		t.Fatalf("the previous page must drop its chevron, got %q", l)
	}
}

// TestMarkerKeepsRowAlignment: selected and unselected labels start in the
// same column, so moving the selection never shifts the text sideways.
func TestMarkerKeepsRowAlignment(t *testing.T) {
	restoreConfig(t)
	m := New(sectionPages(), testOpts(t))
	m.SetSize(120, 20)
	m.Open()

	// Rune-counted, not byte-counted: the chevron is multi-byte, and it is the
	// cell column that must not move.
	col := func(page, label string) int {
		t.Helper()
		line := railLine(t, m.View(), page)
		return len([]rune(line[:strings.Index(line, label)]))
	}
	selEditor, unselAppearance := col("Editor", "Editor"), col("Appearance", "Appearance")
	m.Update(key("down"))
	unselEditor, selAppearance := col("Editor", "Editor"), col("Appearance", "Appearance")

	if selEditor != unselEditor {
		t.Fatalf("Editor label moved from column %d to %d when deselected", selEditor, unselEditor)
	}
	if selAppearance != unselAppearance {
		t.Fatalf("Appearance label moved from column %d to %d when selected", unselAppearance, selAppearance)
	}
	if selEditor != markerWidth {
		t.Fatalf("labels start at column %d, want %d (after the marker)", selEditor, markerWidth)
	}
}

// TestFormColumnMarksSelectedEntry: the settings column marks its selected
// entry too, and the row keeps the column's full width.
func TestFormColumnMarksSelectedEntry(t *testing.T) {
	restoreConfig(t)
	m := New(gridPages(), testOpts(t))
	m.SetSize(140, 24)
	m.Open()
	m.focus, m.sel = formColumn, 1
	m.syncEditor()

	l := colLine(t, m.View(), formX(), formWidth, "Tab width")
	if !strings.Contains(l, "❯ Tab width") {
		t.Fatalf("the selected entry must carry the chevron, got %q", l)
	}
	if got := colLine(t, m.View(), formX(), formWidth, "Menu bar"); strings.Contains(got, "❯ Menu bar") {
		t.Fatalf("an unselected entry must not carry a chevron, got %q", got)
	}
	// The marker is taken out of the row's width, not added to it: the rail,
	// the separator and the settings column still occupy the same cells.
	rowW := lipgloss.Width(m.renderEntry(row{kind: rowEntry, entry: gridPages()[0].Entries[1]}, true, false, formWidth))
	if rowW != formWidth {
		t.Fatalf("entry row width = %d, want the column's %d", rowW, formWidth)
	}
}

// TestFormMarkerIsMutedWhileRailFocused: the chevron only goes accent for the
// column that owns the keys.
func TestFormMarkerIsMutedWhileRailFocused(t *testing.T) {
	restoreConfig(t)
	m := New(gridPages(), testOpts(t))
	m.SetSize(140, 24)
	m.Open()
	r := row{kind: rowEntry, entry: gridPages()[0].Entries[0]}

	m.focus = catColumn
	unfocused := m.renderEntry(r, true, false, formWidth)
	m.focus = formColumn
	focused := m.renderEntry(r, true, false, formWidth)
	if unfocused == focused {
		t.Fatal("the settings column's marker must change colour with focus")
	}
}
