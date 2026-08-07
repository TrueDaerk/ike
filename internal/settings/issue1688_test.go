package settings

// issue1688_test.go covers the theme picker's row preview (#1688): every
// theme.name option row is painted in that theme's own background/foreground
// from the row start up to the palette swatch strip of #1664, while the
// selection stays visible on top of arbitrary theme colors.

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
)

// sgr is the SGR parameter run a foreground (38) or background (48) colour
// contributes to a rendered cell. Comparing whole styled strings would be
// brittle: lipgloss re-emits a style per cell when a row is width-clipped, so
// the tests look for the colour itself instead.
func sgr(layer int, c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%d;2;%d;%d;%d", layer, r>>8, g>>8, b>>8)
}

// themeEntry returns the theme.name entry BasePages builds for themes.
func themeEntry(t *testing.T, themes []string) Entry {
	t.Helper()
	for _, p := range BasePages(themes, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == "theme.name" {
				return e
			}
		}
	}
	t.Fatal("BasePages must carry a theme.name entry")
	return Entry{}
}

// TestThemeRowColorsResolve: a registered theme yields its palette's
// foreground/background, an unknown name resolves to nothing, and the memoized
// callback is stable.
func TestThemeRowColorsResolve(t *testing.T) {
	rc := themeRowColorsFn(nil)
	fg, bg, ok := rc("default")
	if !ok {
		t.Fatal("the default theme must resolve row colors")
	}
	def, _ := theme.Select("default", nil)
	p := theme.NewPalette(def)
	if fg != p.Foreground || bg != p.Background {
		t.Fatalf("row colors = (%v,%v), want the palette's (%v,%v)", fg, bg, p.Foreground, p.Background)
	}
	if _, _, ok := rc("no-such-theme"); ok {
		t.Fatal("an unknown theme must not resolve row colors")
	}
	if fg2, bg2, _ := rc("default"); fg2 != fg || bg2 != bg {
		t.Fatal("the memoized row colors must be stable")
	}
	if _, _, ok := rc("tokyo-night"); !ok {
		t.Fatal("tokyo-night must resolve row colors")
	}
}

// TestThemedEditorRowPaintsLabelNotTail: the label block carries the option's
// colors up to the swatch strip, the strip keeps its own, and the row fits the
// given width.
func TestThemedEditorRowPaintsLabelNotTail(t *testing.T) {
	restoreConfig(t)
	m := New(gridPages(), testOpts(t))
	fg, bg := lipgloss.Color("#ff0000"), lipgloss.Color("#0000ff")
	strip := lipgloss.NewStyle().Foreground(m.theme().Accent).Render("██")

	row := m.themedEditorRow(40, false, "● dracula", strip, fg, bg)
	if w := lipgloss.Width(row); w != 40 {
		t.Fatalf("row width = %d, want the full 40 columns", w)
	}
	plain := ansi.Strip(row)
	if !strings.Contains(plain, "● dracula") || !strings.HasSuffix(plain, "██") {
		t.Fatalf("row must read as label … strip, got %q", plain)
	}
	if !strings.Contains(row, sgr(38, fg)) || !strings.Contains(row, sgr(48, bg)) {
		t.Fatalf("the label block must be painted in the option's colors:\n%q", row)
	}
	// The themed block ends before the strip: the tail is appended untouched.
	if !strings.HasSuffix(row, strip) {
		t.Fatalf("the row must end with the untouched swatch strip:\n%q", row)
	}
	if head := strings.TrimSuffix(row, strip); strings.Count(head, sgr(48, bg)) == 0 {
		t.Fatalf("the option's background must cover the row up to the strip:\n%q", head)
	}
}

// TestThemedEditorRowShowsSelection: the selected row keeps the panel's
// chevron and turns bold+underline, without painting the panel's selection
// band over the previewed colors.
func TestThemedEditorRowShowsSelection(t *testing.T) {
	restoreConfig(t)
	m := New(gridPages(), testOpts(t))
	m.focus = detailColumn
	fg, bg := lipgloss.Color("#ff0000"), lipgloss.Color("#0000ff")

	sel := m.themedEditorRow(40, true, "● dracula", "", fg, bg)
	plain := m.themedEditorRow(40, false, "● dracula", "", fg, bg)
	if !strings.HasPrefix(ansi.Strip(sel), "❯ ") {
		t.Fatalf("the selected row must carry the chevron, got %q", ansi.Strip(sel))
	}
	if strings.Contains(ansi.Strip(plain), "❯") {
		t.Fatalf("an unselected row must not carry a chevron, got %q", ansi.Strip(plain))
	}
	if lipgloss.Width(sel) != lipgloss.Width(plain) {
		t.Fatal("selecting a row must not change its width")
	}
	if !strings.Contains(sel, sgr(48, bg)) {
		t.Fatalf("the selected row must keep previewing the option's background:\n%q", sel)
	}
	if !strings.Contains(sel, ";4m") || strings.Contains(plain, ";4m") {
		t.Fatalf("the selection must underline the themed label:\n%q", sel)
	}
	// The panel's selection colour appears only in the marker cells, ahead of
	// the themed block — it must not band over the preview.
	selBG, themeBG := strings.Index(sel, sgr(48, m.theme().Selection)), strings.Index(sel, sgr(48, bg))
	if selBG < 0 || selBG > themeBG {
		t.Fatalf("the selection band must stay in the marker column:\n%q", sel)
	}
	if strings.Count(sel, sgr(48, m.theme().Selection)) != 1 {
		t.Fatalf("the selection band must not cover the previewed colors:\n%q", sel)
	}
}

// TestThemeEnumRowsUseTheirOwnColors: rendering the theme.name editor paints
// each row in its own theme — every row is a mini-preview — while the swatch
// strip stays at the row's end.
func TestThemeEnumRowsUseTheirOwnColors(t *testing.T) {
	restoreConfig(t)
	names := []string{"default", "tokyo-night"}
	e := themeEntry(t, names)
	if e.RowColors == nil {
		t.Fatal("theme.name must carry RowColors")
	}
	m := New(BasePages(names, nil, nil), testOpts(t))
	rows := newEnumEditor(m, e).View(60, 10)
	if len(rows) < 3 {
		t.Fatalf("want the head plus both option rows, got %d lines", len(rows))
	}
	for i, name := range names {
		row := rows[i+1]
		fg, bg, ok := e.RowColors(name)
		if !ok {
			t.Fatalf("%s must resolve row colors", name)
		}
		if !strings.Contains(row, sgr(38, fg)) || !strings.Contains(row, sgr(48, bg)) {
			t.Fatalf("the %s row must be painted in its own colors:\n%q", name, row)
		}
		if plain := ansi.Strip(row); !strings.Contains(plain, name) || !strings.HasSuffix(plain, "██") {
			t.Fatalf("the %s row must keep its swatch strip at the end:\n%q", name, plain)
		}
	}
	if rows[1] == rows[2] {
		t.Fatal("two themes must not render the same row")
	}
}

// TestUnknownThemeRowKeepsPanelColors: an option the callback cannot resolve
// falls back to the panel's colors but keeps the themed layout, so the marker
// column stays aligned down the list.
func TestUnknownThemeRowKeepsPanelColors(t *testing.T) {
	restoreConfig(t)
	names := []string{"default", "no-such-theme"}
	e := themeEntry(t, names)
	m := New(BasePages(names, nil, nil), testOpts(t))
	rows := newEnumEditor(m, e).View(60, 10)
	if len(rows) < 3 {
		t.Fatalf("want the head plus both option rows, got %d lines", len(rows))
	}
	pal := m.theme()
	if !strings.Contains(rows[2], sgr(48, pal.Background)) || !strings.Contains(rows[2], sgr(38, pal.Foreground)) {
		t.Fatalf("an unresolvable option must render in the panel's colors:\n%q", rows[2])
	}
	if plain := ansi.Strip(rows[2]); strings.Contains(plain, "█") {
		t.Fatalf("an unresolvable option must render no swatch strip: %q", plain)
	}
	for i, row := range rows[1:] {
		if got := lipgloss.Width(row); got != lipgloss.Width(rows[1]) {
			t.Fatalf("row %d width = %d, want every option row to share a width", i, got)
		}
	}
}
