package editor

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/search"
	"ike/internal/histories"
)

// searchprefill_test.go covers #2826: "/", "?" and editor.find open the search
// line prefilled — selected — with the last committed query, so refining it
// needs no retyping, while a visual-selection prefill (#2063) still wins.

// TestSlashPrefillsLastQuery: /foo<enter>/ opens "foo" selected with the
// preview on a match; typing replaces the selection.
func TestSlashPrefillsLastQuery(t *testing.T) {
	m, _ := loaded(t, "alpha\nfoo one\nbeta\nfoo two\n")
	m = commitSearchLine(m, "foo")
	m.cursor = buffer.Position{Line: 2}

	m = typeKeys(m, "/")
	if m.cmdline != "foo" || m.cmdCur != 3 {
		t.Fatalf("prefill: cmdline=%q cmdCur=%d, want %q 3", m.cmdline, m.cmdCur, "foo")
	}
	if m.cmdSelStart != 0 || m.cmdSelEnd != 3 {
		t.Fatalf("prefill selection=[%d,%d), want [0,3)", m.cmdSelStart, m.cmdSelEnd)
	}
	if m.preview.Pattern != "foo" || m.cursor.Line != 3 {
		t.Fatalf("preview=%q on line %d, want foo previewed on line 3", m.preview.Pattern, m.cursor.Line)
	}
	m = typeKeys(m, "b")
	if m.cmdline != "b" || m.cmdSelStart != m.cmdSelEnd {
		t.Fatalf("typing over the prefill: cmdline=%q, want %q unselected", m.cmdline, "b")
	}
}

// TestSlashPrefillEndAppends: end drops the selection and keeps the text, so
// typing refines the previous query.
func TestSlashPrefillEndAppends(t *testing.T) {
	m, _ := loaded(t, "foo bar\nfoo\n")
	m = commitSearchLine(m, "foo")
	m = typeKeys(m, "/")
	m = send(m, special(tea.KeyEnd))
	m = typeKeys(m, " bar")
	if m.cmdline != "foo bar" {
		t.Fatalf("end + typing = %q, want %q", m.cmdline, "foo bar")
	}
	m = send(m, special(tea.KeyEnter))
	if m.query.Pattern != "foo bar" {
		t.Fatalf("committed %q, want the refined query", m.query.Pattern)
	}
}

// TestQuestionPrefillsLastQuery: "?" prefills the same way, whichever
// direction committed the query.
func TestQuestionPrefillsLastQuery(t *testing.T) {
	m, _ := loaded(t, "foo\nbar\nfoo\n")
	m = commitSearchLine(m, "foo")
	m = typeKeys(m, "?")
	if m.cmdline != "foo" || m.cmdSelEnd != 3 || m.searchDir != search.Backward {
		t.Fatalf("? prefill: cmdline=%q sel=%d dir=%v", m.cmdline, m.cmdSelEnd, m.searchDir)
	}
	m = send(m, special(tea.KeyEnter))
	m = typeKeys(m, "/")
	if m.cmdline != "foo" || m.searchDir != search.Forward {
		t.Fatalf("/ after ? prefill: cmdline=%q dir=%v", m.cmdline, m.searchDir)
	}
}

// TestPrefillKeepsMarkers: the prefill is the typed line, markers included,
// so a case-sensitive regex search comes back as it was committed.
func TestPrefillKeepsMarkers(t *testing.T) {
	m, _ := loaded(t, "Foo\nfoo\n")
	m = commitSearchLine(m, `\v\CF.o`)
	m = typeKeys(m, "/")
	if m.cmdline != `\v\CF.o` {
		t.Fatalf("prefill = %q, want the committed line with its markers", m.cmdline)
	}
}

// TestVisualSelectionBeatsLastQuery: a single-line visual selection still
// seeds the line (#2063) over the last query.
func TestVisualSelectionBeatsLastQuery(t *testing.T) {
	m, _ := loaded(t, "foo bar baz\n")
	m = commitSearchLine(m, "foo")
	m.cursor = buffer.Position{Line: 0, Col: 4}
	m.enterVisual(Visual)
	m.cursor = buffer.Position{Line: 0, Col: 6}
	m, _ = m.Update(ActionMsg{Action: "find"})
	if m.cmdline != "bar" {
		t.Fatalf("prefill = %q, want the visual selection", m.cmdline)
	}
}

// TestPrefillEscRestoresOrigin: esc from a prefilled line (whose preview
// moved cursor and viewport) restores both to where "/" was pressed (#255).
func TestPrefillEscRestoresOrigin(t *testing.T) {
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = "line"
	}
	lines[50] = "target"
	m, _ := loaded(t, strings.Join(lines, "\n")+"\n")
	m.SetSize(40, 10)
	m = commitSearchLine(m, "target")
	m = typeKeys(m, "gg")
	m = send(m, special(tea.KeyDown), special(tea.KeyDown))
	origin, top := m.cursor, m.view.Top

	m = typeKeys(m, "/")
	if m.cursor.Line != 50 {
		t.Fatalf("preview cursor line = %d, want the match on 50", m.cursor.Line)
	}
	m = send(m, special(tea.KeyEscape))
	if m.cursor != origin || m.view.Top != top {
		t.Fatalf("esc: cursor=%v top=%d, want %v %d", m.cursor, m.view.Top, origin, top)
	}
}

// TestFindActionPrefillsLastQuery: editor.find (cmd+f) opens the same line
// and follows the same rule.
func TestFindActionPrefillsLastQuery(t *testing.T) {
	m, _ := loaded(t, "foo\nbar\n")
	m = commitSearchLine(m, "bar")
	m, _ = m.Update(ActionMsg{Action: "find"})
	if m.cmdline != "bar" || m.cmdSelEnd != 3 {
		t.Fatalf("find prefill: cmdline=%q sel=%d, want bar selected", m.cmdline, m.cmdSelEnd)
	}
}

// TestPrefillFallsBackToProjectHistory: an editor that committed nothing yet
// opens with the project's last search — the newest search-history entry.
func TestPrefillFallsBackToProjectHistory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "histories.json")
	other, _ := loaded(t, "alpha\n")
	other.SetHistories(histories.NewAt(file))
	_ = commitSearchLine(other, "alpha")

	m, _ := loaded(t, "alpha\n")
	m.SetHistories(histories.NewAt(file))
	m = typeKeys(m, "/")
	if m.cmdline != "alpha" || m.cmdSelEnd != 5 {
		t.Fatalf("fallback prefill: cmdline=%q sel=%d, want alpha selected", m.cmdline, m.cmdSelEnd)
	}
}

// TestPrefillFallsBackToSeededQuery: without history, a query the app
// seeded (#2623, the project's last search) prefills the line.
func TestPrefillFallsBackToSeededQuery(t *testing.T) {
	m, _ := loaded(t, "alpha\n")
	m.SeedSearch(search.Compile("alp", false, search.CaseSmart), search.Forward)
	m = typeKeys(m, "/")
	if m.cmdline != "alp" {
		t.Fatalf("seeded prefill = %q, want alp", m.cmdline)
	}
}

// TestNoPrefillWithoutQuery: a fresh editor opens the line empty.
func TestNoPrefillWithoutQuery(t *testing.T) {
	m, _ := loaded(t, "alpha\n")
	m = typeKeys(m, "/")
	if m.cmdline != "" || m.cmdSelEnd != 0 {
		t.Fatalf("fresh line: cmdline=%q sel=%d, want empty", m.cmdline, m.cmdSelEnd)
	}
}
