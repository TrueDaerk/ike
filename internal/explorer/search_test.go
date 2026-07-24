package explorer

// search_test.go guards the speed search (#1087): activation via "/" and
// SearchMsg, the incremental prefix-first jump, next/prev stepping with
// wrap-around, esc restoring the cursor, enter keeping it, and the raw-key
// capture that keeps the single-letter file-op keys from firing mid-query.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// searchModel builds a loaded tree with a fixed set of file names.
func searchModel(t *testing.T, names ...string) Model {
	t.Helper()
	root := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(root, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := New(root)
	m.SetSize(40, 12)
	m.applyScan(scanCmd(root)().(ScanDoneMsg))
	m.SetFocused(true)
	return m
}

func pressKey(m Model, msg tea.KeyPressMsg) Model {
	m, _ = m.Update(msg)
	return m
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m = pressKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func rowName(m Model, i int) string {
	if i < 0 || i >= len(m.rows) {
		return ""
	}
	return m.rows[i].name
}

func TestSearchActivation(t *testing.T) {
	m := searchModel(t, "alpha.go", "beta.go")
	if m.Searching() {
		t.Fatal("search must start closed")
	}
	m = typeText(m, "/")
	if !m.Searching() {
		t.Fatal("'/' must open the speed search")
	}
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Searching() {
		t.Fatal("esc must close the search")
	}
	// The registry message opens it too (palette / rebinding path).
	m, _ = m.Update(SearchMsg{})
	if !m.Searching() {
		t.Fatal("SearchMsg must open the speed search")
	}
}

func TestSearchIncrementalJump(t *testing.T) {
	m := searchModel(t, "alpha.go", "beta.go", "gamma.go")
	m = typeText(m, "/be")
	if got := rowName(m, m.cursor); got != "beta.go" {
		t.Fatalf("typing 'be' must land on beta.go, got %q", got)
	}
	// Backspace re-resolves from the anchor; an emptied query returns there.
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.cursor != 0 {
		t.Fatalf("empty query must return to the anchor row, cursor = %d", m.cursor)
	}
	if !m.Searching() {
		t.Fatal("backspace must not close the search")
	}
}

func TestSearchPrefixRanksFirst(t *testing.T) {
	// "market.go" contains "ma" earlier in tree order (rows sort
	// alphabetically after the root), but "ma" as a prefix must win even
	// when the contains-match comes first in scan order.
	m := searchModel(t, "amass.go", "main.go")
	m = typeText(m, "/ma")
	if got := rowName(m, m.cursor); got != "main.go" {
		t.Fatalf("prefix match must outrank contains match, got %q", got)
	}
}

func TestSearchNextPrevWrap(t *testing.T) {
	m := searchModel(t, "note1.txt", "note2.txt", "other.md")
	m = typeText(m, "/note")
	first := m.cursor
	if got := rowName(m, first); got != "note1.txt" {
		t.Fatalf("first match must be note1.txt, got %q", got)
	}
	m = pressKey(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if got := rowName(m, m.cursor); got != "note2.txt" {
		t.Fatalf("ctrl+n must step to note2.txt, got %q", got)
	}
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != first {
		t.Fatalf("next past the last match must wrap to the first, cursor = %d", m.cursor)
	}
	m = pressKey(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if got := rowName(m, m.cursor); got != "note2.txt" {
		t.Fatalf("ctrl+p before the first match must wrap to the last, got %q", got)
	}
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != first {
		t.Fatalf("prev must step back to the first match, cursor = %d", m.cursor)
	}
}

func TestSearchEscRestoresEnterKeeps(t *testing.T) {
	m := searchModel(t, "aaa.go", "bbb.go", "ccc.go")
	m.cursor = 1 // start somewhere non-zero
	start := m.cursor
	m = typeText(m, "/ccc")
	if got := rowName(m, m.cursor); got != "ccc.go" {
		t.Fatalf("jump failed, got %q", got)
	}
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Searching() || m.cursor != start {
		t.Fatalf("esc must close and restore the cursor, searching=%v cursor=%d", m.Searching(), m.cursor)
	}
	m = typeText(m, "/ccc")
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.Searching() {
		t.Fatal("enter must close the search")
	}
	if got := rowName(m, m.cursor); got != "ccc.go" {
		t.Fatalf("enter must keep the cursor on the match, got %q", got)
	}
}

func TestSearchCapturesFileOpKeys(t *testing.T) {
	// While the search is open, printable keys extend the query — the raw
	// fallback file-op-ish keys (d, a, R…) must edit the search, never act
	// on the tree, and no prompt may open.
	m := searchModel(t, "radar.go", "zulu.go")
	m = typeText(m, "/") // open
	m = typeText(m, "radar")
	if m.Prompting() {
		t.Fatal("typing into the search must never open a file-op prompt")
	}
	if m.search == nil || m.search.query != "radar" {
		t.Fatalf("query = %q want %q", m.search.query, "radar")
	}
	if got := rowName(m, m.cursor); got != "radar.go" {
		t.Fatalf("cursor must follow the query, got %q", got)
	}
	// The Searching() capture flag is what routes keys here app-side.
	if !m.Searching() {
		t.Fatal("Searching() must report the open search for the capture path")
	}
}

func TestSearchCaseInsensitive(t *testing.T) {
	m := searchModel(t, "README.md", "main.go")
	m = typeText(m, "/read")
	if got := rowName(m, m.cursor); got != "README.md" {
		t.Fatalf("matching must be case-insensitive, got %q", got)
	}
}

func TestSearchNoMatchKeepsCursorAndShowsMiss(t *testing.T) {
	m := searchModel(t, "alpha.go")
	m = typeText(m, "/alp")
	at := m.cursor
	m = typeText(m, "zzz") // now "alpzzz": no match
	if m.cursor != at {
		t.Fatalf("a miss must leave the cursor put, cursor = %d", m.cursor)
	}
	if v := m.View(); !strings.Contains(v, "no matches") {
		t.Fatalf("footer must show the miss:\n%s", v)
	}
}

func TestSearchFooterAndCounterRender(t *testing.T) {
	m := searchModel(t, "note1.txt", "note2.txt")
	m = typeText(m, "/note")
	v := m.View()
	if !strings.Contains(v, "/note") {
		t.Fatalf("footer must show the query:\n%s", v)
	}
	if !strings.Contains(v, "1/2") {
		t.Fatalf("footer must show the match counter:\n%s", v)
	}
}
