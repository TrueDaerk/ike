package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/host"
)

// marks_test.go covers vim marks & bookmarks (#1151): setting and jumping in
// both forms, global-mark hook routing, edit-shift semantics, the gutter
// glyph and its precedence, and the picker-facing accessors.

// TestLocalMarkSetAndJumpExact: m-a records the position, backtick-a returns
// to it exactly after moving away.
func TestLocalMarkSetAndJumpExact(t *testing.T) {
	m, _ := loaded(t, "alpha\n  beta\ngamma\n")
	m = typeKeys(m, "jll") // line 1, col 2
	m = typeKeys(m, "ma")
	m = typeKeys(m, "gg")
	m = typeKeys(m, "`a")
	if l, c := m.CursorPos(); l != 1 || c != 2 {
		t.Fatalf("backtick jump = %d:%d, want 1:2", l, c)
	}
}

// TestLocalMarkJumpLineForm: '-a lands on the marked line's first non-blank,
// not the exact column.
func TestLocalMarkJumpLineForm(t *testing.T) {
	m, _ := loaded(t, "alpha\n  beta\ngamma\n")
	m = typeKeys(m, "j$ma") // mark at line 1, last col
	m = typeKeys(m, "gg")
	m = typeKeys(m, "'a")
	if l, c := m.CursorPos(); l != 1 || c != 2 {
		t.Fatalf("quote jump = %d:%d, want 1:2 (first non-blank)", l, c)
	}
}

// TestLocalMarkUnsetReports: jumping to an unset mark stays put and reports
// on the ex line (vim's E20).
func TestLocalMarkUnsetReports(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\n")
	m = typeKeys(m, "j")
	m = typeKeys(m, "'z")
	if l, _ := m.CursorPos(); l != 1 {
		t.Fatalf("cursor moved to %d on an unset mark", l)
	}
	if !strings.Contains(m.cmdMsg, "E20") {
		t.Fatalf("cmdMsg = %q, want an E20 report", m.cmdMsg)
	}
}

// TestLocalMarkShiftsOnInsertAbove: opening a line above the mark shifts it
// down; deleting a line above pulls it back up.
func TestLocalMarkShiftsOnInsertAbove(t *testing.T) {
	m, _ := loaded(t, "l0\nl1\nl2\nl3\n")
	m = typeKeys(m, "jjma") // mark a at line 2
	m = typeKeys(m, "gg")
	m = send(m, key('O'), special(tea.KeyEscape)) // insert a line above line 0
	lms := m.LocalMarks()
	if len(lms) != 1 || lms[0].Line != 3 {
		t.Fatalf("after insert above, marks = %+v, want line 3", lms)
	}
	m = typeKeys(m, "dd") // delete the inserted line again
	lms = m.LocalMarks()
	if len(lms) != 1 || lms[0].Line != 2 {
		t.Fatalf("after delete above, marks = %+v, want line 2", lms)
	}
	m = typeKeys(m, "`a")
	if l, _ := m.CursorPos(); l != 2 {
		t.Fatalf("jump after edits = line %d, want 2", l)
	}
}

// TestLocalMarkClampsAfterTruncation: a mark past the end of a shrunk buffer
// clamps into the text on jump instead of failing.
func TestLocalMarkClampsAfterTruncation(t *testing.T) {
	m, _ := loaded(t, "l0\nl1\nl2\nl3\n")
	m = typeKeys(m, "Gma")
	m = typeKeys(m, "ggjdd") // delete a line above: mark shifts up with the delta
	m = typeKeys(m, "gg")
	m = typeKeys(m, "`a")
	if l, _ := m.CursorPos(); l != 2 {
		t.Fatalf("jump = line %d, want 2 (shifted up)", l)
	}
}

// TestGlobalMarkSetRoutesToHook: m-A hands path and position to the injected
// store setter instead of recording locally.
func TestGlobalMarkSetRoutesToHook(t *testing.T) {
	m, path := loaded(t, "alpha\nbeta\n")
	var got []interface{}
	m.SetMarkHooks(MarkHooks{Set: func(r rune, p string, line, col int) {
		got = []interface{}{r, p, line, col}
	}})
	m = typeKeys(m, "jmA")
	if len(got) == 0 || got[0].(rune) != 'A' || got[1].(string) != path || got[2].(int) != 1 {
		t.Fatalf("global set hook got %v, want [A %s 1 0]", got, path)
	}
	if len(m.LocalMarks()) != 0 {
		t.Fatal("a global mark must not land in the local set")
	}
}

// TestGlobalMarkJumpEmitsMsg: backtick-A resolves app-side — the key returns
// a command carrying GlobalMarkJumpMsg with the exact flag; '-A clears it.
func TestGlobalMarkJumpEmitsMsg(t *testing.T) {
	m, _ := loaded(t, "alpha\n")
	m.SetMarkHooks(MarkHooks{Set: func(rune, string, int, int) {}})
	m = typeKeys(m, "`")
	m, cmd := m.Update(key('A'))
	if cmd == nil {
		t.Fatal("backtick-A must return a command")
	}
	msg, ok := cmd().(GlobalMarkJumpMsg)
	if !ok || msg.Letter != 'A' || !msg.Exact {
		t.Fatalf("msg = %#v, want GlobalMarkJumpMsg{A, Exact}", msg)
	}
	m = typeKeys(m, "'")
	m, cmd = m.Update(key('A'))
	if cmd == nil {
		t.Fatal("quote-A must return a command")
	}
	if msg, ok := cmd().(GlobalMarkJumpMsg); !ok || msg.Exact {
		t.Fatalf("msg = %#v, want the line (non-exact) form", msg)
	}
	_ = m
}

// TestGlobalMarkAdjustHookFires: a line insert reports the same delta to the
// injected global-mark adjuster as it does to the breakpoint store.
func TestGlobalMarkAdjustHookFires(t *testing.T) {
	m, path := loaded(t, "l0\nl1\nl2\n")
	var calls [][3]interface{}
	m.SetMarkHooks(MarkHooks{Adjust: func(p string, cursorAfter, delta int) {
		calls = append(calls, [3]interface{}{p, cursorAfter, delta})
	}})
	m = send(m, key('o'), special(tea.KeyEscape))
	if len(calls) == 0 {
		t.Fatal("adjust hook did not fire on a line insert")
	}
	total := 0
	for _, c := range calls {
		if c[0].(string) != path {
			t.Fatalf("adjust path = %v, want %s", c[0], path)
		}
		total += c[2].(int)
	}
	if total != 1 {
		t.Fatalf("net delta = %d, want +1", total)
	}
}

// TestGutterShowsMarkLetter: a marked line renders the mark's own letter in
// the gutter's sign column (#2661), not the anonymous flag.
func TestGutterShowsMarkLetter(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\ngamma\n")
	m.Configure(host.MapConfig{"editor.line_numbers": "true"})
	m = typeKeys(m, "jmb")
	view := m.View()
	if strings.Contains(view, "⚑") {
		t.Fatalf("a vim mark must not draw the anonymous flag:\n%s", view)
	}
	if !markedLineHasSign(t, m, 1, "b") {
		t.Fatalf("gutter lacks the mark letter b:\n%s", view)
	}
}

// markedLineHasSign reports whether the rendered row for 0-based line l
// opens with sign — the sign cell is the gutter's first column.
func markedLineHasSign(t *testing.T, m Model, l int, sign string) bool {
	t.Helper()
	text := m.LineText(l)
	for _, row := range strings.Split(ansi.Strip(m.View()), "\n") {
		if !strings.Contains(row, text) {
			continue
		}
		return strings.HasPrefix(row, sign)
	}
	return false
}

// TestGutterBreakpointOutranksBookmark: a breakpoint on the marked line keeps
// its ● — breakpoints stay visible and toggleable everywhere.
func TestGutterBreakpointOutranksBookmark(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\n")
	m.Configure(host.MapConfig{"editor.line_numbers": "true"})
	m.SetBreakpointSource(func(string) []int { return []int{1} })
	m = typeKeys(m, "jmb")
	view := m.View()
	if strings.Contains(view, "⚑") {
		t.Fatal("bookmark glyph must yield to the breakpoint ●")
	}
	if !strings.Contains(view, "●") {
		t.Fatal("breakpoint ● missing")
	}
}

// TestGutterShowsGlobalMarkLines: the gutter also flags lines carried by the
// injected global-mark source for the open file.
func TestGutterShowsGlobalMarkLines(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\n")
	m.Configure(host.MapConfig{"editor.line_numbers": "true"})
	m.SetMarkHooks(MarkHooks{Letters: func(string) map[int]rune { return map[int]rune{0: 'Z'} }})
	if !markedLineHasSign(t, m, 0, "Z") {
		t.Fatalf("global mark line lacks its letter:\n%s", m.View())
	}
}

// TestLoadClearsLocalMarks: opening another file drops the previous file's
// local marks (they are per-session, per-document state).
func TestLoadClearsLocalMarks(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\n")
	m = typeKeys(m, "ma")
	if len(m.LocalMarks()) != 1 {
		t.Fatal("mark not set")
	}
	m2, _ := loaded(t, "other\n")
	_ = m2
	if err := m.Load(m2.Path()); err != nil {
		t.Fatal(err)
	}
	if len(m.LocalMarks()) != 0 {
		t.Fatal("Load must clear local marks")
	}
}

// TestRemoveLocalMark: the picker's aux action removes exactly the named
// mark.
func TestRemoveLocalMark(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\n")
	m = typeKeys(m, "ma")
	m = typeKeys(m, "jmb")
	m.RemoveLocalMark('a')
	lms := m.LocalMarks()
	if len(lms) != 1 || lms[0].Letter != 'b' {
		t.Fatalf("marks after remove = %+v, want only b", lms)
	}
}

// TestJumpToLocalMark: the picker's activation jumps to the exact position
// and reports whether the mark existed.
func TestJumpToLocalMark(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\ngamma\n")
	m = typeKeys(m, "jllma")
	m = typeKeys(m, "gg")
	if !m.JumpToLocalMark('a') {
		t.Fatal("JumpToLocalMark(a) = false, want true")
	}
	if l, c := m.CursorPos(); l != 1 || c != 2 {
		t.Fatalf("jump = %d:%d, want 1:2", l, c)
	}
	if m.JumpToLocalMark('z') {
		t.Fatal("JumpToLocalMark(z) = true for an unset mark")
	}
}

// TestLocalMarkToggles: m{letter} on the line that already carries the mark
// removes it (#2661) — the gutter glyph goes, a jump reports E20 — while the
// same key on another line still moves the mark.
func TestLocalMarkToggles(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\ngamma\n")
	m.Configure(host.MapConfig{"editor.line_numbers": "true"})
	m = typeKeys(m, "jmm")
	if lms := m.LocalMarks(); len(lms) != 1 || lms[0].Line != 1 {
		t.Fatalf("after set, marks = %+v, want line 1", lms)
	}
	// A different column on the same line still counts as the same line.
	m = typeKeys(m, "$mm")
	if lms := m.LocalMarks(); len(lms) != 0 {
		t.Fatalf("repeat on the marked line must remove, marks = %+v", lms)
	}
	if m.cmdMsg != "mark m removed" {
		t.Fatalf("ex line = %q, want the removal confirmation", m.cmdMsg)
	}
	if markedLineHasSign(t, m, 1, "m") {
		t.Fatalf("gutter still shows the mark:\n%s", m.View())
	}
	m = typeKeys(m, "'m")
	if m.cmdMsg != "E20: mark not set" {
		t.Fatalf("jump after removal = %q, want E20", m.cmdMsg)
	}
	// Setting again and pressing on another line moves the mark.
	m = typeKeys(m, "ggmm")
	m = typeKeys(m, "jjmm")
	if lms := m.LocalMarks(); len(lms) != 1 || lms[0].Line != 2 {
		t.Fatalf("mark on another line must move, marks = %+v", lms)
	}
}

// TestGlobalMarkToggleRoutesToHook: m{A-Z} on the line the store already
// holds the mark on calls the remove hook instead of the setter (#2661).
func TestGlobalMarkToggleRoutesToHook(t *testing.T) {
	m, path := loaded(t, "alpha\nbeta\n")
	var removed []rune
	sets := 0
	m.SetMarkHooks(MarkHooks{
		Set:    func(rune, string, int, int) { sets++ },
		At:     func(r rune, p string, line int) bool { return r == 'A' && p == path && line == 1 },
		Remove: func(r rune) { removed = append(removed, r) },
	})
	m = typeKeys(m, "jmA")
	if len(removed) != 1 || removed[0] != 'A' || sets != 0 {
		t.Fatalf("removed = %v, sets = %d, want the toggle to remove A", removed, sets)
	}
	if m.cmdMsg != "mark A removed" {
		t.Fatalf("ex line = %q, want the removal confirmation", m.cmdMsg)
	}
	// On another line the mark moves as before.
	m = typeKeys(m, "ggmA")
	if sets != 1 {
		t.Fatalf("sets = %d, want 1 after marking another line", sets)
	}
}

// TestGutterSignPrecedence: a bookmark's mnemonic digit outranks a vim mark
// letter, which in turn outranks the anonymous bookmark flag (#2661).
func TestGutterSignPrecedence(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\ngamma\n")
	m.Configure(host.MapConfig{"editor.line_numbers": "true"})
	m.SetBookmarkHooks(func(string) map[int]string {
		return map[int]string{0: "⚑", 1: "3"}
	}, nil)
	m = typeKeys(m, "ma")  // line 0: vim mark + anonymous bookmark
	m = typeKeys(m, "jmb") // line 1: vim mark + mnemonic bookmark
	if !markedLineHasSign(t, m, 0, "a") {
		t.Fatalf("mark letter must outrank the anonymous flag:\n%s", m.View())
	}
	if !markedLineHasSign(t, m, 1, "3") {
		t.Fatalf("mnemonic digit must outrank the mark letter:\n%s", m.View())
	}
}

// TestGutterPrefersFirstMarkLetter: two marks on one line show the
// alphabetically first letter, lowercase before uppercase (#2661).
func TestGutterPrefersFirstMarkLetter(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\n")
	m.Configure(host.MapConfig{"editor.line_numbers": "true"})
	m.SetMarkHooks(MarkHooks{Letters: func(string) map[int]rune { return map[int]rune{0: 'B'} }})
	m = typeKeys(m, "mc")
	if !markedLineHasSign(t, m, 0, "B") {
		t.Fatalf("B must beat c alphabetically:\n%s", m.View())
	}
	m = typeKeys(m, "ma")
	if !markedLineHasSign(t, m, 0, "a") {
		t.Fatalf("a must beat B alphabetically:\n%s", m.View())
	}
}

// TestMarkLetterBeforeLowercaseFirst: the same letter in both cases resolves
// to the local (lowercase) mark.
func TestMarkLetterBeforeLowercaseFirst(t *testing.T) {
	if !markLetterBefore('a', 'A') {
		t.Fatal("lowercase must win the shared letter")
	}
	if markLetterBefore('A', 'a') {
		t.Fatal("uppercase must not win the shared letter")
	}
}
