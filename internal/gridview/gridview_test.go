package gridview

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

// stripANSI drops SGR sequences so tests can read the text a row carries.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

var pal = theme.DefaultPalette()

func TestDataRowPadsCellsAndSeparates(t *testing.T) {
	cells := []Cell{{Text: "1"}, {Text: "user-1"}, {Text: "x"}}
	got := stripANSI(DataRow(pal, cells, []int{2, 8, 1}, 0, 40, false, false))
	if want := " 1   user-1    x"; got != want {
		t.Fatalf("row = %q, want %q", got, want)
	}
}

func TestDataRowDrawsNullAsGlyphFaint(t *testing.T) {
	cells := []Cell{{Text: "1"}, {Null: true}, {Text: "z"}}
	row := DataRow(pal, cells, []int{1, 3, 1}, 0, 40, false, false)
	if got := stripANSI(row); got != " 1  "+NullCell+"    z" {
		t.Fatalf("row = %q", got)
	}
	faint := lipgloss.NewStyle().Faint(true).Render(NullCell + "    ")
	if !strings.Contains(row, faint) {
		t.Fatalf("null cell must render faint with its padding and separator:\n%q\nwant chunk %q", row, faint)
	}
}

func TestDataRowSelectedFillsTheBudget(t *testing.T) {
	cells := []Cell{{Text: "a"}, {Text: "b"}}
	for _, focused := range []bool{true, false} {
		row := DataRow(pal, cells, []int{1, 1}, 0, 12, true, focused)
		if n := lipgloss.Width(row); n != 12 {
			t.Errorf("focused=%v: selected row spans %d cells, want the whole budget 12:\n%q", focused, n, row)
		}
		bg := pal.SelectionMuted
		if focused {
			bg = pal.Selection
		}
		want := lipgloss.NewStyle().Foreground(pal.Foreground).Background(bg).Bold(focused).Render(" ")
		if !strings.HasPrefix(row, want) {
			t.Errorf("focused=%v: leading cell must carry the selection background:\n got %q\nwant prefix %q", focused, row, want)
		}
	}
	plain := DataRow(pal, cells, []int{1, 1}, 0, 12, false, true)
	if n := lipgloss.Width(plain); n != 5 {
		t.Errorf("an unselected row stops after its cells: width %d, want 5", n)
	}
}

func TestDataRowHonoursColumnOffsetAndBudget(t *testing.T) {
	cells := []Cell{{Text: "first"}, {Text: "second"}, {Text: "third"}}
	widths := []int{5, 6, 5}
	if got := stripANSI(DataRow(pal, cells, widths, 1, 40, false, false)); got != " second  third" {
		t.Fatalf("colOff 1 = %q", got)
	}
	// Budget of 9 fits the space plus "second  " exactly; 8 clips the chunk
	// with an ellipsis, and the third column never starts.
	if got := stripANSI(DataRow(pal, cells, widths, 1, 9, false, false)); got != " second  " {
		t.Fatalf("budget 9 = %q", got)
	}
	if got := stripANSI(DataRow(pal, cells, widths, 1, 8, false, false)); got != " second…" {
		t.Fatalf("budget 8 = %q", got)
	}
	if got := lipgloss.Width(DataRow(pal, cells, widths, 0, 9, false, false)); got != 9 {
		t.Fatalf("a clipped row spans %d cells, want exactly the budget", got)
	}
}

func TestHeaderRowIsBoldAccentAndClipped(t *testing.T) {
	labels := []string{"id", "name ▼", "note"}
	widths := []int{2, 6, 4}
	row := HeaderRow(pal, labels, widths, 0, 40)
	want := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent).Render(" id  name ▼  note")
	if row != want {
		t.Fatalf("header:\n got %q\nwant %q", row, want)
	}
	if got := stripANSI(HeaderRow(pal, labels, widths, 1, 40)); got != " name ▼  note" {
		t.Fatalf("colOff 1 = %q", got)
	}
	if got := stripANSI(HeaderRow(pal, labels, widths, 0, 6)); got != " id  …" {
		t.Fatalf("clipped = %q", got)
	}
}

func TestSidebarFillsBlankLinesBelowTheList(t *testing.T) {
	row := func(i, w int) string { return PadTo(strings.Repeat("r", i+1), w) }
	got := Sidebar(1, 4, 3, 4, row)
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want height 4:\n%q", len(lines), got)
	}
	if lines[0] != "rr  " || lines[1] != "rrr " {
		t.Errorf("rows from top=1: %q", lines[:2])
	}
	for _, l := range lines[2:] {
		if l != "    " {
			t.Errorf("filler line %q, want 4 blanks", l)
		}
	}
	if strings.HasSuffix(got, "\n") {
		t.Errorf("no trailing newline expected")
	}
	if Sidebar(0, 0, 3, 4, row) != "" {
		t.Errorf("zero height renders nothing")
	}
}

func TestPadToAndClipTo(t *testing.T) {
	if got := PadTo("ab", 4); got != "ab  " {
		t.Errorf("PadTo = %q", got)
	}
	if got := PadTo("abcdef", 4); got != "abc…" {
		t.Errorf("PadTo over = %q", got)
	}
	if got := ClipTo("abc", 3); got != "abc" {
		t.Errorf("ClipTo fits = %q", got)
	}
	if got := ClipTo("abcd", 3); got != "ab…" {
		t.Errorf("ClipTo = %q", got)
	}
	if got := ClipTo("abcd", 0); got != "" {
		t.Errorf("ClipTo 0 = %q", got)
	}
	if got := ClipTo("héllo", 3); got != "hé…" {
		t.Errorf("ClipTo counts runes, = %q", got)
	}
}
