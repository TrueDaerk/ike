package diff

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// texts builds a pair with one change at line 11 of 30 same lines, leaving a
// long unchanged run on both sides.
func collapseTexts() (string, string) {
	var l, r []string
	for i := 1; i <= 30; i++ {
		line := "line"
		l = append(l, line)
		r = append(r, line)
	}
	r[10] = "CHANGED"
	return strings.Join(l, "\n") + "\n", strings.Join(r, "\n") + "\n"
}

func collapseModel(t *testing.T) Model {
	t.Helper()
	m := New("diff", "l", "r", "", nil)
	m.SetSize(100, 60)
	m.SetContents(collapseTexts())
	return m
}

func press(m *Model, s string) {
	msg := tea.KeyPressMsg{Code: rune(s[0]), Text: s}
	m.handleKey(msg)
}

func TestCollapsedContextFoldsUnchangedRuns(t *testing.T) {
	m := collapseModel(t)
	v := ansi.Strip(strings.Join(m.lines, "\n"))
	if !strings.Contains(v, "unchanged lines") {
		t.Fatalf("no separator rendered:\n%s", v)
	}
	// Two gaps: before (10-line run, ctx 3 → 7 hidden) and after the change.
	if len(m.gaps) != 2 {
		t.Fatalf("gaps = %+v", m.gaps)
	}
	if got := m.gaps[0]; got.start != 0 || got.end != 7 {
		t.Fatalf("leading gap = %+v (file edge keeps no context)", got)
	}
	// Collapsed view is much shorter than the full 31 rows.
	if len(m.lines) >= 31 {
		t.Fatalf("collapsed lines = %d", len(m.lines))
	}
	// Hunk navigation still lands on the change.
	press(&m, "n")
	if m.CurrentHunk() != 0 {
		t.Fatal("hunk nav broken while collapsed")
	}
}

func TestCollapseToggleAndExpand(t *testing.T) {
	m := collapseModel(t)
	collapsedLen := len(m.lines)

	press(&m, "c") // show all
	if len(m.lines) != 31 {
		t.Fatalf("full view lines = %d, want 31", len(m.lines))
	}
	press(&m, "c") // collapse again
	if len(m.lines) != collapsedLen {
		t.Fatalf("re-collapse lines = %d, want %d", len(m.lines), collapsedLen)
	}

	// o expands the nearest gap only.
	press(&m, "o")
	if len(m.lines) <= collapsedLen || len(m.lines) >= 31 {
		t.Fatalf("after one expand: %d lines (collapsed %d, full 31)", len(m.lines), collapsedLen)
	}
	press(&m, "o")
	if len(m.lines) != 31 {
		t.Fatalf("after both expands: %d lines, want 31", len(m.lines))
	}
	// New contents reset the expansion.
	m.SetContents(collapseTexts())
	if len(m.lines) != collapsedLen {
		t.Fatalf("SetContents did not reset gaps: %d", len(m.lines))
	}
}

func TestCollapseDisabledByNegativeContext(t *testing.T) {
	m := collapseModel(t)
	m.SetContext(-1)
	if len(m.lines) != 31 || m.Collapsed() {
		t.Fatalf("ctx<0 must disable collapsing (lines=%d)", len(m.lines))
	}
}

func TestCollapseUnifiedSeparators(t *testing.T) {
	m := collapseModel(t)
	m.SetUnified(true)
	v := ansi.Strip(strings.Join(m.lines, "\n"))
	if !strings.Contains(v, "unchanged lines") {
		t.Fatal("unified view lost the separators")
	}
	if !strings.Contains(v, "CHANGED") {
		t.Fatal("unified view lost the change")
	}
}
