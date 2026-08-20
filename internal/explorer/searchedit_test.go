package explorer

// searchedit_test.go guards the speed search's shared line editing (#2002):
// the query used to be append-only, so a typo meant retyping from the end.
// It now runs through ui.EditKey — a movable cursor, word motions, the macOS
// opt/cmd chords — while ctrl+n / ctrl+p keep stepping matches.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func openSearch(t *testing.T, names ...string) Model {
	t.Helper()
	m := searchModel(t, names...)
	return typeText(m, "/")
}

func TestSearchCursorInsertsMidQuery(t *testing.T) {
	m := openSearch(t, "alpha.go", "beta.go")
	m = typeText(m, "apha")
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	m = typeText(m, "l")
	if got := m.search.query; got != "alpha" {
		t.Fatalf("query = %q, want %q", got, "alpha")
	}
	if m.search.pos != 2 {
		t.Fatalf("cursor = %d, want 2", m.search.pos)
	}
	if rowName(m, m.cursor) != "alpha.go" {
		t.Fatalf("cursor row = %q, want alpha.go", rowName(m, m.cursor))
	}
}

func TestSearchHomeEndAndDelete(t *testing.T) {
	m := openSearch(t, "alpha.go", "beta.go")
	m = typeText(m, "beta")
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyHome})
	if m.search.pos != 0 {
		t.Fatalf("home: cursor = %d, want 0", m.search.pos)
	}
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.search.query != "eta" {
		t.Fatalf("delete: query = %q, want %q", m.search.query, "eta")
	}
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnd})
	if m.search.pos != 3 {
		t.Fatalf("end: cursor = %d, want 3", m.search.pos)
	}
}

func TestSearchWordAndLineKills(t *testing.T) {
	m := openSearch(t, "alpha beta.go")
	m = typeText(m, "alpha beta")
	// opt+backspace kills the word before the cursor.
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if m.search.query != "alpha " {
		t.Fatalf("alt+backspace: query = %q, want %q", m.search.query, "alpha ")
	}
	// cmd+backspace kills to the start of the line.
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper})
	if m.search.query != "" || m.search.pos != 0 {
		t.Fatalf("super+backspace: query = %q cursor = %d, want empty", m.search.query, m.search.pos)
	}
}

func TestSearchWordMotion(t *testing.T) {
	m := openSearch(t, "alpha beta.go")
	m = typeText(m, "alpha beta")
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	if m.search.pos != 6 {
		t.Fatalf("alt+left: cursor = %d, want 6", m.search.pos)
	}
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt})
	if m.search.pos != 10 {
		t.Fatalf("alt+right: cursor = %d, want 10", m.search.pos)
	}
}

func TestSearchPasteAtCursor(t *testing.T) {
	m := openSearch(t, "alpha.go", "beta.go")
	m = typeText(m, "ala")
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if !m.Paste("ph") {
		t.Fatal("paste must be consumed by the open search")
	}
	if m.search.query != "alpha" {
		t.Fatalf("query = %q, want %q", m.search.query, "alpha")
	}
	if m.search.pos != 4 {
		t.Fatalf("cursor = %d, want 4", m.search.pos)
	}
	if rowName(m, m.cursor) != "alpha.go" {
		t.Fatalf("paste must re-jump: cursor row = %q", rowName(m, m.cursor))
	}
}

// The match stepper keeps priority over anything the shared editor binds.
func TestSearchCtrlNStillSteps(t *testing.T) {
	m := openSearch(t, "a1.go", "a2.go", "b.go")
	m = typeText(m, "a")
	first := m.cursor
	m = pressKey(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if m.cursor == first {
		t.Fatal("ctrl+n must step to the next match")
	}
	if m.search.query != "a" {
		t.Fatalf("ctrl+n must not edit the query, got %q", m.search.query)
	}
}

// The rendered line carries the cursor where the editing position is.
func TestSearchLineRendersCursorMidQuery(t *testing.T) {
	m := openSearch(t, "alpha.go")
	m = typeText(m, "alpha")
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyHome})
	line := m.searchLine()
	if !strings.Contains(line, "lpha") {
		t.Fatalf("search line lost its text: %q", line)
	}
}
