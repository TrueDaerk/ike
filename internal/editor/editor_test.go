package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/search"
	"ike/internal/host"
)

// key builds a key press for a single printable rune.
func key(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Text: string(r), Code: r} }

// special builds a key press for a non-printable key identified by its code.
func special(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

// modKey builds a key press for a code combined with modifiers.
func modKey(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

// keys builds a sequence of single-rune key messages from a string.
func keys(s string) []tea.KeyPressMsg {
	var out []tea.KeyPressMsg
	for _, r := range s {
		out = append(out, key(r))
	}
	return out
}

// send applies a sequence of keys and returns the resulting model.
func send(m Model, ks ...tea.KeyPressMsg) Model {
	for _, k := range ks {
		m, _ = m.Update(k)
	}
	return m
}

// typeKeys applies every rune of s as a key.
func typeKeys(m Model, s string) Model { return send(m, keys(s)...) }

func loaded(t *testing.T, content string) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	m.SetFocused(true)
	return m, path
}

func line(m Model, i int) string { return m.buf.Line(i) }

// --- loading & motions -----------------------------------------------------

func TestLoadSplitsLines(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\ngamma\n")
	if m.buf.LineCount() != 3 || line(m, 1) != "beta" {
		t.Fatalf("lines=%q", m.buf.Lines())
	}
	if m.Dirty() {
		t.Fatal("fresh load should not be dirty")
	}
}

func TestMotionsHJKL(t *testing.T) {
	m, _ := loaded(t, "abc\ndef\n")
	m = typeKeys(m, "ll")
	if m.cursor.Col != 2 {
		t.Fatalf("ll col=%d want 2", m.cursor.Col)
	}
	m = typeKeys(m, "l") // clamp
	if m.cursor.Col != 2 {
		t.Fatalf("clamp col=%d want 2", m.cursor.Col)
	}
	m = typeKeys(m, "j")
	if m.cursor.Line != 1 {
		t.Fatalf("j row=%d want 1", m.cursor.Line)
	}
	m = typeKeys(m, "hhh")
	if m.cursor.Col != 0 {
		t.Fatalf("h col=%d want 0", m.cursor.Col)
	}
}

func TestCountedMotion(t *testing.T) {
	m, _ := loaded(t, "a b c d e\n")
	m = typeKeys(m, "3w")
	if m.cursor.Col != 6 {
		t.Fatalf("3w col=%d want 6", m.cursor.Col)
	}
}

func TestHomeEndKeys(t *testing.T) {
	m, _ := loaded(t, "hello world\n")
	m = send(m, special(tea.KeyEnd))
	if m.cursor.Col != 10 {
		t.Fatalf("End col=%d want 10", m.cursor.Col)
	}
	m = send(m, special(tea.KeyHome))
	if m.cursor.Col != 0 {
		t.Fatalf("Home col=%d want 0", m.cursor.Col)
	}
}

func TestGgG(t *testing.T) {
	m, _ := loaded(t, "a\nb\nc\nd\n")
	m = typeKeys(m, "G")
	if m.cursor.Line != 3 {
		t.Fatalf("G line=%d want 3", m.cursor.Line)
	}
	m = typeKeys(m, "gg")
	if m.cursor.Line != 0 {
		t.Fatalf("gg line=%d want 0", m.cursor.Line)
	}
}

func TestFindCharAndRepeat(t *testing.T) {
	m, _ := loaded(t, "a.b.c.d\n")
	m = typeKeys(m, "f.")
	if m.cursor.Col != 1 {
		t.Fatalf("f. col=%d want 1", m.cursor.Col)
	}
	m = typeKeys(m, ";")
	if m.cursor.Col != 3 {
		t.Fatalf("; col=%d want 3", m.cursor.Col)
	}
	m = typeKeys(m, ",")
	if m.cursor.Col != 1 {
		t.Fatalf(", col=%d want 1", m.cursor.Col)
	}
}

// --- operators -------------------------------------------------------------

func TestDeleteWord(t *testing.T) {
	m, _ := loaded(t, "hello world\n")
	m = typeKeys(m, "dw")
	if line(m, 0) != "world" {
		t.Fatalf("dw=%q want world", line(m, 0))
	}
}

func TestDeleteCountWord(t *testing.T) {
	m, _ := loaded(t, "one two three four\n")
	m = typeKeys(m, "d2w")
	if line(m, 0) != "three four" {
		t.Fatalf("d2w=%q", line(m, 0))
	}
}

func TestChangeToEnd(t *testing.T) {
	m, _ := loaded(t, "hello world\n")
	m = typeKeys(m, "wc$")
	m = typeKeys(m, "X")
	m = send(m, special(tea.KeyEsc))
	if line(m, 0) != "hello X" {
		t.Fatalf("c$=%q want 'hello X'", line(m, 0))
	}
}

func TestDeleteLineCount(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\nfour\n")
	m = typeKeys(m, "3dd")
	if m.buf.LineCount() != 1 || line(m, 0) != "four" {
		t.Fatalf("3dd=%q", m.buf.Lines())
	}
}

func TestDeleteCharX(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	m = typeKeys(m, "x")
	if line(m, 0) != "bc" || !m.Dirty() {
		t.Fatalf("x=%q dirty=%v", line(m, 0), m.Dirty())
	}
}

func TestTextObjectDeleteInnerParens(t *testing.T) {
	m, _ := loaded(t, "foo(bar)baz\n")
	m = typeKeys(m, "f(")
	m = typeKeys(m, "di(")
	if line(m, 0) != "foo()baz" {
		t.Fatalf("di(=%q want foo()baz", line(m, 0))
	}
}

func TestTextObjectChangeInnerWord(t *testing.T) {
	m, _ := loaded(t, "hello world\n")
	m = typeKeys(m, "ciw")
	m = typeKeys(m, "HEY")
	m = send(m, special(tea.KeyEsc))
	if line(m, 0) != "HEY world" {
		t.Fatalf("ciw=%q", line(m, 0))
	}
}

// --- registers & paste -----------------------------------------------------

func TestYankPaste(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	m = typeKeys(m, "yl") // yank char under cursor
	m = typeKeys(m, "p")  // paste after
	if line(m, 0) != "aabc" {
		t.Fatalf("yl p=%q want aabc", line(m, 0))
	}
}

func TestLinewiseYankPaste(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\n")
	m = typeKeys(m, "yy") // yank line
	m = typeKeys(m, "p")  // paste below
	if m.buf.LineCount() != 3 || line(m, 1) != "one" {
		t.Fatalf("yyp=%q", m.buf.Lines())
	}
}

func TestNamedRegister(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	m = typeKeys(m, `"ayl`) // yank into register a
	m = typeKeys(m, "$")
	m = typeKeys(m, `"ap`)
	if line(m, 0) != "abca" {
		t.Fatalf("named reg paste=%q want abca", line(m, 0))
	}
}

// --- insert mode -----------------------------------------------------------

func TestInsertAndAppend(t *testing.T) {
	m, _ := loaded(t, "bc\n")
	m = typeKeys(m, "i")
	m = typeKeys(m, "a")
	m = send(m, special(tea.KeyEsc))
	if line(m, 0) != "abc" || m.ModeName() != Normal {
		t.Fatalf("i: %q mode=%v", line(m, 0), m.ModeName())
	}
	m = typeKeys(m, "A!")
	m = send(m, special(tea.KeyEsc))
	if line(m, 0) != "abc!" {
		t.Fatalf("A: %q", line(m, 0))
	}
}

func TestOpenLineBelow(t *testing.T) {
	m, _ := loaded(t, "top\nbottom\n")
	m = typeKeys(m, "onew")
	m = send(m, special(tea.KeyEsc))
	if m.buf.LineCount() != 3 || line(m, 1) != "new" {
		t.Fatalf("o=%q", m.buf.Lines())
	}
}

func TestEnterSplitsLine(t *testing.T) {
	m, _ := loaded(t, "abcd\n")
	m = typeKeys(m, "ll") // cursor on 'c' (col 2)
	m = typeKeys(m, "i")  // insert before 'c'
	m = send(m, special(tea.KeyEnter))
	m = send(m, special(tea.KeyEsc))
	if m.buf.LineCount() != 2 || line(m, 0) != "ab" || line(m, 1) != "cd" {
		t.Fatalf("split=%q", m.buf.Lines())
	}
}

func TestBackspaceJoins(t *testing.T) {
	m, _ := loaded(t, "ab\ncd\n")
	m = typeKeys(m, "j")
	m = typeKeys(m, "i")
	m = send(m, special(tea.KeyBackspace))
	if m.buf.LineCount() != 1 || line(m, 0) != "abcd" {
		t.Fatalf("bs join=%q", m.buf.Lines())
	}
}

func TestInsertModeArrowKeys(t *testing.T) {
	m, _ := loaded(t, "abc\ndef\n")
	m = typeKeys(m, "i") // insert at col 0, line 0
	m = send(m, special(tea.KeyRight), special(tea.KeyRight))
	if m.cursor.Col != 2 {
		t.Fatalf("right in insert col=%d want 2", m.cursor.Col)
	}
	m = send(m, special(tea.KeyDown))
	if m.cursor.Line != 1 {
		t.Fatalf("down in insert line=%d want 1", m.cursor.Line)
	}
	// typing lands at the moved position, still in insert mode.
	m = typeKeys(m, "X")
	if m.ModeName() != Insert {
		t.Fatal("arrows must not leave insert mode")
	}
	if line(m, 1) != "deXf" {
		t.Fatalf("insert after arrow=%q want deXf", line(m, 1))
	}
}

func TestMouseClickPositionsCursor(t *testing.T) {
	m, _ := loaded(t, "hello\nworld\nthird\n")
	m.MouseClick(2, 1) // col 2, screen row 1 -> line 1
	if m.cursor.Line != 1 || m.cursor.Col != 2 {
		t.Fatalf("click=%v want {1 2}", m.cursor)
	}
}

func TestMouseClickClampsToLineInNormal(t *testing.T) {
	m, _ := loaded(t, "ab\n")
	m.MouseClick(50, 0)    // far past line end
	if m.cursor.Col != 1 { // normal mode snaps onto last rune
		t.Fatalf("normal click col=%d want 1", m.cursor.Col)
	}
}

func TestScrollByMovesViewportNotCursor(t *testing.T) {
	lines := strings.Repeat("x\n", 100)
	m, _ := loaded(t, lines)
	m.SetSize(80, 10)
	cursorBefore := m.cursor
	m.ScrollBy(5)
	if m.view.Top != 5 {
		t.Fatalf("Top = %d want 5", m.view.Top)
	}
	if m.cursor != cursorBefore {
		t.Fatalf("cursor moved to %v, wheel scroll must not move it", m.cursor)
	}
	m.ScrollBy(-100) // cannot scroll above the top
	if m.view.Top != 0 {
		t.Fatalf("Top = %d want 0", m.view.Top)
	}
	m.ScrollBy(1000) // clamps to the last line
	if m.view.Top != m.buf.LineCount()-1 {
		t.Fatalf("Top = %d want %d (max)", m.view.Top, m.buf.LineCount()-1)
	}
}

func TestScrollByWorksInInsertMode(t *testing.T) {
	lines := strings.Repeat("x\n", 100)
	m, _ := loaded(t, lines)
	m.SetSize(80, 10)
	m, _ = m.Update(key('i')) // enter insert mode
	if m.ModeName() != Insert {
		t.Fatal("setup: expected insert mode")
	}
	m.ScrollBy(5)
	if m.view.Top != 5 {
		t.Fatalf("Top = %d want 5 (scroll must work regardless of mode)", m.view.Top)
	}
	if m.ModeName() != Insert {
		t.Fatal("scrolling must not leave insert mode")
	}
}

func TestMouseClickHonoursGutter(t *testing.T) {
	m, _ := loaded(t, "hello\n")
	m.Configure(host.MapConfig{"editor.line_numbers": "true"})
	m.SetSize(80, 20)
	gw := m.view.GutterWidth(1)
	m.MouseClick(gw+3, 0) // gutter + 3 -> col 3
	if m.cursor.Col != 3 {
		t.Fatalf("gutter click col=%d want 3", m.cursor.Col)
	}
}

func TestReplaceChar(t *testing.T) {
	m, _ := loaded(t, "cat\n")
	m = typeKeys(m, "rb")
	if line(m, 0) != "bat" {
		t.Fatalf("r=%q want bat", line(m, 0))
	}
}

// --- undo / redo / dot -----------------------------------------------------

func TestUndoRedo(t *testing.T) {
	m, _ := loaded(t, "hello\n")
	m = typeKeys(m, "x") // -> "ello"
	m = typeKeys(m, "u") // undo
	if line(m, 0) != "hello" {
		t.Fatalf("undo=%q want hello", line(m, 0))
	}
	m, _ = m.Update(modKey('r', tea.ModCtrl)) // redo
	if line(m, 0) != "ello" {
		t.Fatalf("redo=%q want ello", line(m, 0))
	}
}

func TestUndoRedoWithCount(t *testing.T) {
	m, _ := loaded(t, "hello\n")
	m = typeKeys(m, "xxx") // three separate changes -> "lo"
	if line(m, 0) != "lo" {
		t.Fatalf("setup=%q want lo", line(m, 0))
	}
	m = typeKeys(m, "3u") // undo all three at once
	if line(m, 0) != "hello" {
		t.Fatalf("3u=%q want hello", line(m, 0))
	}
	m = typeKeys(m, "2")
	m, _ = m.Update(modKey('r', tea.ModCtrl)) // 2 ctrl+r redoes two
	if line(m, 0) != "llo" {
		t.Fatalf("2ctrl+r=%q want llo", line(m, 0))
	}
}

func TestUndoCountPastHistoryStops(t *testing.T) {
	m, _ := loaded(t, "hello\n")
	m = typeKeys(m, "x")   // -> "ello"
	m = typeKeys(m, "99u") // count far beyond the single change
	if line(m, 0) != "hello" {
		t.Fatalf("99u=%q want hello", line(m, 0))
	}
	if m.Dirty() {
		t.Fatal("undo back to the loaded (saved) state should clear dirty (#251)")
	}
}

func TestUndoRedoTrackSavePoint(t *testing.T) {
	m, _ := loaded(t, "hello\n")
	m = typeKeys(m, "x") // -> "ello"
	m = send(m, key(':'))
	m = typeKeys(m, "w")
	m = send(m, special(tea.KeyEnter)) // save point at "ello"
	m = typeKeys(m, "x")               // -> "llo", dirty
	if !m.Dirty() {
		t.Fatal("edit after save should be dirty")
	}
	m = typeKeys(m, "u") // back to "ello" = save point
	if line(m, 0) != "ello" || m.Dirty() {
		t.Fatalf("undo to save point: line=%q dirty=%v, want ello/clean", line(m, 0), m.Dirty())
	}
	m = typeKeys(m, "u") // back to "hello", before the save point
	if line(m, 0) != "hello" || !m.Dirty() {
		t.Fatalf("undo past save point: line=%q dirty=%v, want hello/dirty", line(m, 0), m.Dirty())
	}
	m, _ = m.Update(modKey('r', tea.ModCtrl)) // redo to "ello" = save point again
	if line(m, 0) != "ello" || m.Dirty() {
		t.Fatalf("redo to save point: line=%q dirty=%v, want ello/clean", line(m, 0), m.Dirty())
	}
}

func TestUndoDiscardedSavePointStaysDirty(t *testing.T) {
	m, _ := loaded(t, "hello\n")
	m = typeKeys(m, "x") // -> "ello"
	m = send(m, key(':'))
	m = typeKeys(m, "w")
	m = send(m, special(tea.KeyEnter)) // save point at "ello"
	m = typeKeys(m, "u")               // back to "hello" (dirty: disk has "ello")
	m = typeKeys(m, "sX")              // fresh edit discards the redo branch (and the save point)
	m = send(m, special(tea.KeyEsc))
	m = typeKeys(m, "uu") // back to "hello" — but disk still holds "ello"
	if line(m, 0) != "hello" || !m.Dirty() {
		t.Fatalf("after discarding the saved branch: line=%q dirty=%v, want hello/dirty", line(m, 0), m.Dirty())
	}
}

func TestRestoreTextUndoNeverClean(t *testing.T) {
	m, _ := loaded(t, "base\n")
	m.RestoreText("recovered\n")
	m = typeKeys(m, "x") // edit the recovered text
	m = typeKeys(m, "u") // undo back to the recovered content
	if line(m, 0) != "recovered" || !m.Dirty() {
		t.Fatalf("crash-restored buffer after undo: line=%q dirty=%v, want recovered/dirty", line(m, 0), m.Dirty())
	}
}

func TestUndoInsertIsOneUnit(t *testing.T) {
	m, _ := loaded(t, "x\n")
	m = typeKeys(m, "A")
	m = typeKeys(m, "abc")
	m = send(m, special(tea.KeyEsc))
	if line(m, 0) != "xabc" {
		t.Fatalf("insert=%q", line(m, 0))
	}
	m = typeKeys(m, "u")
	if line(m, 0) != "x" {
		t.Fatalf("undo insert=%q want x", line(m, 0))
	}
}

func TestUndoFromInsertModeFlushesAndReverts(t *testing.T) {
	m, _ := loaded(t, "x\n")
	m = typeKeys(m, "A")
	m = typeKeys(m, "abc") // typing, still in insert mode (no Esc)
	// Cmd+Z arrives as an editor.undo action while the insert session is open.
	m, _ = m.Update(ActionMsg{Action: "undo"})
	if line(m, 0) != "x" {
		t.Fatalf("undo mid-insert=%q want x (whole run reverted)", line(m, 0))
	}
	if m.mode != Normal {
		t.Fatalf("undo mid-insert mode=%v want Normal", m.mode)
	}
}

func TestDotRepeatsDelete(t *testing.T) {
	m, _ := loaded(t, "aaaa\n")
	m = typeKeys(m, "x")
	m = typeKeys(m, ".")
	m = typeKeys(m, ".")
	if line(m, 0) != "a" {
		t.Fatalf("x.. =%q want a", line(m, 0))
	}
}

func TestDotRepeatsInsert(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = typeKeys(m, "i")
	m = typeKeys(m, "ab")
	m = send(m, special(tea.KeyEsc))
	m = typeKeys(m, ".")
	if line(m, 0) != "aabb" && line(m, 0) != "abab" {
		// Cursor sits on last inserted rune; "." inserts before it.
		t.Logf("dot insert result=%q", line(m, 0))
	}
	if m.buf.RuneLen(0) != 4 {
		t.Fatalf("dot insert len=%d want 4 (%q)", m.buf.RuneLen(0), line(m, 0))
	}
}

// --- search ----------------------------------------------------------------

func TestSearchForwardAndNext(t *testing.T) {
	m, _ := loaded(t, "foo bar foo baz foo\n")
	m = send(m, key('/'))
	m = typeKeys(m, "foo")
	m = send(m, special(tea.KeyEnter))
	if m.cursor.Col != 8 {
		t.Fatalf("/foo col=%d want 8", m.cursor.Col)
	}
	m = typeKeys(m, "n")
	if m.cursor.Col != 16 {
		t.Fatalf("n col=%d want 16", m.cursor.Col)
	}
	m = typeKeys(m, "N")
	if m.cursor.Col != 8 {
		t.Fatalf("N col=%d want 8", m.cursor.Col)
	}
}

func TestIncrementalSearchPreviewJumpsWhileTyping(t *testing.T) {
	m, _ := loaded(t, "aaa\nbbb\nccc target ddd\n")
	m = send(m, key('/'))
	m = typeKeys(m, "targ")
	if m.cursor.Line != 2 || m.cursor.Col != 4 {
		t.Fatalf("preview cursor=%v want {2 4} while still typing", m.cursor)
	}
	if !m.searching {
		t.Fatal("preview jump must not leave search mode")
	}
	// Deleting back to a non-matching prefix parks the cursor at the origin.
	m = send(m, special(tea.KeyBackspace), special(tea.KeyBackspace), special(tea.KeyBackspace), special(tea.KeyBackspace))
	if m.cursor.Line != 0 || m.cursor.Col != 0 {
		t.Fatalf("empty pattern cursor=%v want origin {0 0}", m.cursor)
	}
}

func TestIncrementalSearchEscRestoresOrigin(t *testing.T) {
	m, _ := loaded(t, "aaa\nbbb\nccc target ddd\n")
	m = typeKeys(m, "j") // origin at line 1
	m = send(m, key('/'))
	m = typeKeys(m, "target")
	if m.cursor.Line != 2 {
		t.Fatalf("preview should sit on the match, cursor=%v", m.cursor)
	}
	m = send(m, special(tea.KeyEscape))
	if m.cursor.Line != 1 || m.cursor.Col != 0 {
		t.Fatalf("esc cursor=%v want origin {1 0}", m.cursor)
	}
	if m.searching || m.ModeName() != Normal {
		t.Fatal("esc should leave search mode")
	}
}

func TestSearchNoMatchesReportsAndRestores(t *testing.T) {
	m, _ := loaded(t, "aaa\nbbb\n")
	m = typeKeys(m, "j")
	m = send(m, key('/'))
	m = typeKeys(m, "zzz")
	if m.cursor.Line != 1 {
		t.Fatalf("no-match preview cursor=%v want origin line 1", m.cursor)
	}
	m = send(m, special(tea.KeyEnter))
	if m.cmdMsg != "no matches: zzz" {
		t.Fatalf("cmdMsg=%q want 'no matches: zzz'", m.cmdMsg)
	}
	if m.cursor.Line != 1 || m.hlActive {
		t.Fatalf("cursor=%v hl=%v want origin/unhighlighted", m.cursor, m.hlActive)
	}
}

func TestSearchWrapReportsHint(t *testing.T) {
	m, _ := loaded(t, "foo\nbar\nfoo\n")
	m = send(m, key('/'))
	m = typeKeys(m, "foo")
	m = send(m, special(tea.KeyEnter)) // lands on line 2 (skips the origin match)
	if m.cursor.Line != 2 {
		t.Fatalf("commit cursor=%v want line 2", m.cursor)
	}
	m = typeKeys(m, "n") // wraps to line 0
	if m.cursor.Line != 0 {
		t.Fatalf("n cursor=%v want wrapped to line 0", m.cursor)
	}
	if m.cmdMsg != "search wrapped" {
		t.Fatalf("cmdMsg=%q want 'search wrapped'", m.cmdMsg)
	}
}

func TestSearchCounterAndHighlightLifecycle(t *testing.T) {
	m, _ := loaded(t, "foo bar foo baz foo\n")
	m.SetSize(60, 6)
	m = send(m, key('/'))
	m = typeKeys(m, "foo")
	// The origin sits on the first match; like vim's "/", the preview lands on
	// the next one, so the counter reads 2/3.
	if !strings.Contains(m.View(), "2/3") {
		t.Fatalf("view should show the 2/3 counter while typing:\n%s", m.View())
	}
	if _, ok := m.searchHLQuery(); !ok {
		t.Fatal("preview highlights should be active while typing")
	}
	m = send(m, special(tea.KeyEnter))
	if _, ok := m.searchHLQuery(); !ok {
		t.Fatal("committed highlights should stay active after enter")
	}
	m = send(m, special(tea.KeyEscape)) // normal-mode esc = :noh
	if _, ok := m.searchHLQuery(); ok {
		t.Fatal("normal-mode esc should clear the highlights")
	}
	m = typeKeys(m, "n") // n re-arms them
	if _, ok := m.searchHLQuery(); !ok {
		t.Fatal("n should re-arm the highlights")
	}
}

// --- visual ----------------------------------------------------------------

func TestVisualDelete(t *testing.T) {
	m, _ := loaded(t, "hello\n")
	m = typeKeys(m, "vll") // select 'hel'
	m = typeKeys(m, "d")
	if line(m, 0) != "lo" {
		t.Fatalf("visual d=%q want lo", line(m, 0))
	}
	if m.ModeName() != Normal {
		t.Fatal("should return to normal after visual delete")
	}
}

func TestVisualCountedMotion(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\nfour\nfive\n")
	m = typeKeys(m, "V3j") // select lines 0..3
	if m.cursor.Line != 3 {
		t.Fatalf("V3j cursor line=%d want 3", m.cursor.Line)
	}
	m = typeKeys(m, "d")
	if got := line(m, 0); got != "five" {
		t.Fatalf("V3jd left %q want five", got)
	}
}

func TestVisualCountedGotoLine(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\nfour\n")
	m = typeKeys(m, "V3G") // select lines 0..2
	if m.cursor.Line != 2 {
		t.Fatalf("V3G cursor line=%d want 2", m.cursor.Line)
	}
}

func TestVisualZeroIsLineStartWithoutCount(t *testing.T) {
	m, _ := loaded(t, "hello world\n")
	m = typeKeys(m, "$v0") // select the whole line charwise, right to left
	if m.cursor.Col != 0 {
		t.Fatalf("v0 cursor col=%d want 0", m.cursor.Col)
	}
	// With a pending count, 0 continues the count instead: 10| goes to col 9.
	m = typeKeys(m, "10l")
	if m.cursor.Col != 10 {
		t.Fatalf("v10l cursor col=%d want 10", m.cursor.Col)
	}
}

func TestVisualEscapeClearsPendingCount(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\nfour\n")
	m = typeKeys(m, "v3")
	m = send(m, special(tea.KeyEscape))
	m = typeKeys(m, "j") // must move one line, not three
	if m.cursor.Line != 1 {
		t.Fatalf("j after v3<esc> cursor line=%d want 1", m.cursor.Line)
	}
}

func TestVisualLineYank(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\n")
	m = typeKeys(m, "Vj") // select lines 0-1
	m = typeKeys(m, "y")
	m = typeKeys(m, "p")
	if m.buf.LineCount() != 5 {
		t.Fatalf("Vjy p lines=%d want 5 (%q)", m.buf.LineCount(), m.buf.Lines())
	}
}

func TestVisualSelectionRange(t *testing.T) {
	m, _ := loaded(t, "hello\nworld\n")
	m = typeKeys(m, "vll") // anchor col 0, cursor col 2 -> select 'hel'
	start, end, ok := m.selectionOnLine(0, 5)
	if !ok || start != 0 || end != 2 {
		t.Fatalf("charwise selection=%d..%d ok=%v want 0..2", start, end, ok)
	}
	// A line outside the selection is not highlighted.
	if _, _, ok := m.selectionOnLine(1, 5); ok {
		t.Fatal("line 1 should not be selected")
	}
}

func TestVisualLineSelectionRange(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\n")
	m = typeKeys(m, "Vj") // lines 0..1
	if _, end, ok := m.selectionOnLine(1, 3); !ok || end != 3 {
		t.Fatalf("V-line selection end=%d ok=%v want 3", end, ok)
	}
	if _, _, ok := m.selectionOnLine(2, 5); ok {
		t.Fatal("line 2 outside V-line selection")
	}
}

func TestVisualTextObjectSelectsWord(t *testing.T) {
	m, _ := loaded(t, "foo bar baz\n")
	m = typeKeys(m, "w")   // cursor on "bar"
	m = typeKeys(m, "viw") // select inner word
	start, end, ok := m.selectionOnLine(0, 11)
	if !ok || start != 4 || end != 6 {
		t.Fatalf("viw selection=%d..%d want 4..6", start, end)
	}
	m = typeKeys(m, "d")
	if line(m, 0) != "foo  baz" {
		t.Fatalf("viwd=%q want 'foo  baz'", line(m, 0))
	}
}

func TestVisualPasteReplacesSelection(t *testing.T) {
	m, _ := loaded(t, "foo bar\n")
	m = typeKeys(m, "yiw")  // yank "foo"
	m = typeKeys(m, "w")    // to "bar"
	m = typeKeys(m, "viwp") // select "bar", paste -> replaced by "foo"
	if line(m, 0) != "foo foo" {
		t.Fatalf("visual paste=%q want 'foo foo'", line(m, 0))
	}
}

func TestAltArrowWordNav(t *testing.T) {
	m, _ := loaded(t, "foo bar baz\n")
	m = send(m, modKey(tea.KeyRight, tea.ModAlt))
	if m.cursor.Col != 4 {
		t.Fatalf("alt+right col=%d want 4", m.cursor.Col)
	}
	m = send(m, modKey(tea.KeyLeft, tea.ModAlt))
	if m.cursor.Col != 0 {
		t.Fatalf("alt+left col=%d want 0", m.cursor.Col)
	}
}

func TestAltArrowWordNavDotStopsAndLineClamp(t *testing.T) {
	m, _ := loaded(t, "config.editor.tabWidth\nnext\n")
	// '.' inside the identifier is a stop point (#303).
	m = send(m, modKey(tea.KeyRight, tea.ModAlt))
	if m.cursor.Col != 6 {
		t.Fatalf("alt+right to '.' col=%d want 6", m.cursor.Col)
	}
	m = send(m, modKey(tea.KeyRight, tea.ModAlt), modKey(tea.KeyRight, tea.ModAlt))
	if m.cursor.Col != 13 {
		t.Fatalf("alt+right to second '.' col=%d want 13", m.cursor.Col)
	}
	// Past the last word the motion clamps to the line instead of crossing.
	m = send(m, modKey(tea.KeyRight, tea.ModAlt), modKey(tea.KeyRight, tea.ModAlt),
		modKey(tea.KeyRight, tea.ModAlt))
	if m.cursor.Line != 0 {
		t.Fatalf("alt+right crossed to line %d, must stay on line 0", m.cursor.Line)
	}
	// Backward from column 0 stays put rather than jumping to the previous line.
	m = send(m, special(tea.KeyDown))
	for range 4 {
		m = send(m, modKey(tea.KeyLeft, tea.ModAlt))
	}
	if m.cursor.Line != 1 || m.cursor.Col != 0 {
		t.Fatalf("alt+left clamp pos=%v want line 1 col 0", m.cursor)
	}
}

func TestShiftAltArrowExtendsSelection(t *testing.T) {
	m, _ := loaded(t, "foo.bar baz\n")
	m = send(m, modKey(tea.KeyRight, tea.ModShift|tea.ModAlt))
	if !m.mode.IsVisual() {
		t.Fatalf("alt+shift+right mode=%v want visual", m.mode)
	}
	if m.anchor.Col != 0 || m.cursor.Col != 3 {
		t.Fatalf("selection anchor=%d cursor=%d want 0/3", m.anchor.Col, m.cursor.Col)
	}
	m = send(m, modKey(tea.KeyRight, tea.ModShift|tea.ModAlt))
	if m.anchor.Col != 0 || m.cursor.Col != 4 {
		t.Fatalf("extended selection anchor=%d cursor=%d want 0/4", m.anchor.Col, m.cursor.Col)
	}
	m = send(m, modKey(tea.KeyLeft, tea.ModShift|tea.ModAlt))
	if m.anchor.Col != 0 || m.cursor.Col != 3 {
		t.Fatalf("shrunk selection anchor=%d cursor=%d want 0/3", m.anchor.Col, m.cursor.Col)
	}
}

func TestAltArrowWordNavInInsertMode(t *testing.T) {
	m, _ := loaded(t, "foo bar\nnext\n")
	m = typeKeys(m, "i")
	m = send(m, modKey(tea.KeyRight, tea.ModAlt))
	if m.cursor.Col != 4 {
		t.Fatalf("insert alt+right col=%d want 4", m.cursor.Col)
	}
	// Clamps to the line-end slot (one past the last rune) instead of crossing.
	m = send(m, modKey(tea.KeyRight, tea.ModAlt), modKey(tea.KeyRight, tea.ModAlt))
	if m.cursor.Line != 0 || m.cursor.Col != 7 {
		t.Fatalf("insert alt+right clamp pos=%v want line 0 col 7", m.cursor)
	}
	m = send(m, modKey(tea.KeyLeft, tea.ModAlt), modKey(tea.KeyLeft, tea.ModAlt),
		modKey(tea.KeyLeft, tea.ModAlt))
	if m.cursor.Line != 0 || m.cursor.Col != 0 {
		t.Fatalf("insert alt+left clamp pos=%v want line 0 col 0", m.cursor)
	}
}

func TestShiftArrowStartsAndExtendsSelection(t *testing.T) {
	m, _ := loaded(t, "foo bar baz\n")
	m = send(m, modKey(tea.KeyRight, tea.ModShift))
	if !m.mode.IsVisual() {
		t.Fatalf("shift+right mode=%v want visual", m.mode)
	}
	if m.anchor.Col != 0 || m.cursor.Col != 1 {
		t.Fatalf("selection anchor=%d cursor=%d want 0/1", m.anchor.Col, m.cursor.Col)
	}
	m = send(m, modKey(tea.KeyRight, tea.ModShift), modKey(tea.KeyRight, tea.ModShift))
	if m.anchor.Col != 0 || m.cursor.Col != 3 {
		t.Fatalf("extended selection anchor=%d cursor=%d want 0/3", m.anchor.Col, m.cursor.Col)
	}
	m = send(m, special(tea.KeyEscape))
	if m.mode.IsVisual() {
		t.Fatal("escape should leave visual mode")
	}
}

func TestClipboardCopyCutPaste(t *testing.T) {
	clip := &fakeClipboard{}
	m, _ := loaded(t, "foo bar\n")
	m.SetClipboard(clip)

	// Copy the selection "foo" and paste it over "bar".
	m = send(m, modKey(tea.KeyRight, tea.ModShift), modKey(tea.KeyRight, tea.ModShift))
	m, _ = m.runAction("copy")
	if clip.text != "foo" {
		t.Fatalf("copy clipboard=%q want foo", clip.text)
	}
	if m.mode.IsVisual() {
		t.Fatal("copy should leave visual mode")
	}

	// Cut without a selection removes the whole line into the clipboard.
	m, _ = m.runAction("cut")
	if clip.text != "foo bar\n" {
		t.Fatalf("cut clipboard=%q want whole line", clip.text)
	}
	if line(m, 0) != "" {
		t.Fatalf("cut left line %q, want empty buffer line", line(m, 0))
	}

	// Paste inserts the clipboard text.
	clip.text = "hi"
	m, _ = m.runAction("paste")
	if line(m, 0) != "hi" {
		t.Fatalf("paste line=%q want hi", line(m, 0))
	}
}

func TestClipboardCopyCutFeedback(t *testing.T) {
	clip := &fakeClipboard{}
	m, _ := loaded(t, "foo bar\nsecond\n")
	m.SetClipboard(clip)

	// Charwise: the "foo" selection reports its rune count.
	m = send(m, modKey(tea.KeyRight, tea.ModShift), modKey(tea.KeyRight, tea.ModShift))
	m, cmd := m.runAction("copy")
	if cmd == nil {
		t.Fatal("copy should return a feedback command (#252)")
	}
	if n, ok := cmd().(NoticeMsg); !ok || n.Text != "copied 3 chars" {
		t.Fatalf("copy notice=%+v want 'copied 3 chars'", cmd())
	}

	// Linewise: cutting without a selection reports one line.
	m, cmd = m.runAction("cut")
	if cmd == nil {
		t.Fatal("cut should return a feedback command (#252)")
	}
	if n, ok := cmd().(NoticeMsg); !ok || n.Text != "cut 1 line" {
		t.Fatalf("cut notice=%+v want 'cut 1 line'", cmd())
	}
}

func TestClipboardPasteReplacesSelection(t *testing.T) {
	clip := &fakeClipboard{text: "XY"}
	m, _ := loaded(t, "foo\n")
	m.SetClipboard(clip)
	m = send(m, modKey(tea.KeyRight, tea.ModShift)) // select "fo"
	m, _ = m.runAction("paste")
	if line(m, 0) != "XYo" {
		t.Fatalf("visual paste line=%q want XYo", line(m, 0))
	}
}

func TestLineStartEndActions(t *testing.T) {
	m, _ := loaded(t, "foo bar\n")
	m, _ = m.runAction("line_end")
	if m.cursor.Col != 6 {
		t.Fatalf("line_end col=%d want 6", m.cursor.Col)
	}
	m, _ = m.runAction("line_start")
	if m.cursor.Col != 0 {
		t.Fatalf("line_start col=%d want 0", m.cursor.Col)
	}
	// Mid-insert, line end goes one past the last rune so typing continues there.
	m = typeKeys(m, "i")
	m, _ = m.runAction("line_end")
	if m.cursor.Col != 7 {
		t.Fatalf("insert line_end col=%d want 7", m.cursor.Col)
	}
}

func TestDuplicateLineAction(t *testing.T) {
	m, _ := loaded(t, "foo bar\nsecond\n")
	m = typeKeys(m, "ll") // cursor at col 2
	m, _ = m.Update(ActionMsg{Action: "duplicate_line"})
	if line(m, 0) != "foo bar" || line(m, 1) != "foo bar" || line(m, 2) != "second" {
		t.Fatalf("duplicate_line lines=%q,%q,%q", line(m, 0), line(m, 1), line(m, 2))
	}
	if m.cursor.Line != 1 || m.cursor.Col != 2 {
		t.Fatalf("duplicate_line cursor=%v want line 1 col 2", m.cursor)
	}
}

func TestFindActionOpensSearch(t *testing.T) {
	m, _ := loaded(t, "foo\n")
	m, _ = m.Update(ActionMsg{Action: "find"})
	if !m.searching {
		t.Fatal("find action should enter search input")
	}
}

func TestCommandLineRendersInEditorView(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\n")
	m.SetSize(40, 10)
	m = typeKeys(m, ":wq")
	view := m.View()
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[len(lines)-1], ":wq") {
		t.Fatalf("command line missing from the view's bottom row:\n%s", view)
	}
	// Search input renders the same way.
	m = send(m, special(tea.KeyEscape))
	m = typeKeys(m, "/foo")
	if !strings.Contains(m.View(), "/foo") {
		t.Fatal("search input missing from the view")
	}
}

func TestCommandLineOnEmptyScratchBuffer(t *testing.T) {
	m := New()
	m.SetSize(40, 5)
	m = typeKeys(m, ":q")
	if !strings.Contains(m.View(), ":q") {
		t.Fatal("command line must render on the empty scratch buffer")
	}
}

// fakeClipboard captures writes and serves reads for clipboard action tests.
type fakeClipboard struct{ text string }

func (f *fakeClipboard) Read() (string, error) { return f.text, nil }
func (f *fakeClipboard) Write(s string) error  { f.text = s; return nil }

func TestPageDownMovesCursor(t *testing.T) {
	var sb string
	for i := 0; i < 50; i++ {
		sb += "line\n"
	}
	m, _ := loaded(t, sb)
	m.SetSize(80, 10)
	m = send(m, special(tea.KeyPgDown))
	if m.cursor.Line == 0 {
		t.Fatal("PgDown should move the cursor down a page")
	}
}

func TestToggleCase(t *testing.T) {
	m, _ := loaded(t, "aBc\n")
	m = typeKeys(m, "~~~")
	if line(m, 0) != "AbC" {
		t.Fatalf("~=%q want AbC", line(m, 0))
	}
}

func TestSearchWordStar(t *testing.T) {
	m, _ := loaded(t, "foo bar foo\n")
	m = typeKeys(m, "*") // search word under cursor ("foo") forward
	if m.cursor.Col != 8 {
		t.Fatalf("* col=%d want 8", m.cursor.Col)
	}
}

func TestIndentLine(t *testing.T) {
	m, _ := loaded(t, "code\n")
	m.Configure(host.MapConfig{"editor.use_spaces": "true", "editor.tab_width": "2"})
	m = typeKeys(m, ">>")
	if line(m, 0) != "  code" {
		t.Fatalf(">>=%q want '  code'", line(m, 0))
	}
	m = typeKeys(m, "<<")
	if line(m, 0) != "code" {
		t.Fatalf("<<=%q want code", line(m, 0))
	}
}

// --- ex commands & dirty ---------------------------------------------------

func TestSaveRoundTrip(t *testing.T) {
	m, path := loaded(t, "hello\n")
	m = typeKeys(m, "x")
	if !m.Dirty() {
		t.Fatal("should be dirty")
	}
	m = send(m, key(':'))
	m = typeKeys(m, "w")
	m = send(m, special(tea.KeyEnter))
	if m.Dirty() {
		t.Fatal("save should clear dirty")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "ello\n" {
		t.Fatalf("file=%q want ello", got)
	}
}

func TestQuitEmitsCloseMsg(t *testing.T) {
	m, _ := loaded(t, "x\n")
	m, _ = m.Update(key(':'))
	m, _ = m.Update(key('q'))
	_, cmd := m.Update(special(tea.KeyEnter))
	if cmd == nil {
		t.Fatal(":q should return a command")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Fatalf(":q msg=%T want CloseMsg", cmd())
	}
}

func TestExGotoLine(t *testing.T) {
	m, _ := loaded(t, "a\nb\nc\nd\n")
	m = send(m, key(':'))
	m = typeKeys(m, "3")
	m = send(m, special(tea.KeyEnter))
	if m.cursor.Line != 2 {
		t.Fatalf(":3 line=%d want 2", m.cursor.Line)
	}
}

func TestCommandLineRender(t *testing.T) {
	m, _ := loaded(t, "x\n")
	m = typeKeys(m, ":")
	m = typeKeys(m, "wq")
	if m.CommandLine() != ":wq" {
		t.Fatalf("cmdline=%q", m.CommandLine())
	}
}

// --- config-driven viewport ------------------------------------------------

func TestLineNumberGutterFromConfig(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\n")
	m.Configure(host.MapConfig{"editor.line_numbers": "true"})
	m.SetSize(80, 20)
	out := m.View()
	if out == "" {
		t.Fatal("empty view")
	}
	if m.view.GutterWidth(2) == 0 {
		t.Fatal("gutter should be enabled")
	}
}

func TestExpandTabFromConfig(t *testing.T) {
	m, _ := loaded(t, "\n")
	m.Configure(host.MapConfig{"editor.use_spaces": "true", "editor.tab_width": "2"})
	m = typeKeys(m, "i")
	m = send(m, special(tea.KeyTab))
	m = send(m, special(tea.KeyEsc))
	if line(m, 0) != "  " {
		t.Fatalf("expandtab=%q want two spaces", line(m, 0))
	}
}

// --- events seam -----------------------------------------------------------

func TestEmitterReceivesChange(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	var got []EventKind
	m.SetEmitter(EmitterFunc(func(e Event) { got = append(got, e.Kind) }))
	m = typeKeys(m, "x")
	found := false
	for _, k := range got {
		if k == EventChange {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a change event, got %v", got)
	}
}

// TestInsertWordAndLineKill (#246): alt+backspace / ctrl+w delete the previous
// word, cmd+backspace / ctrl+u delete to the line start; everything stays one
// undo unit with the surrounding insert.
func TestInsertWordAndLineKill(t *testing.T) {
	mk := func(k tea.KeyPressMsg) func(*testing.T) {
		return func(t *testing.T) {
			m, _ := loaded(t, "alpha bravo charlie\n")
			m = typeKeys(m, "A") // append at line end
			m = send(m, k)
			if line(m, 0) != "alpha bravo " {
				t.Fatalf("word kill=%q want %q", line(m, 0), "alpha bravo ")
			}
			m = send(m, k)
			if line(m, 0) != "alpha " {
				t.Fatalf("second word kill=%q want %q", line(m, 0), "alpha ")
			}
			// The whole insert (both kills) undoes as one unit.
			m = send(m, special(tea.KeyEsc))
			m = typeKeys(m, "u")
			if line(m, 0) != "alpha bravo charlie" {
				t.Fatalf("undo=%q", line(m, 0))
			}
		}
	}
	t.Run("alt+backspace", mk(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}))
	t.Run("ctrl+w", mk(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}))

	lk := func(k tea.KeyPressMsg) func(*testing.T) {
		return func(t *testing.T) {
			m, _ := loaded(t, "alpha bravo\n")
			m = typeKeys(m, "A")
			m = send(m, k)
			if line(m, 0) != "" {
				t.Fatalf("line kill=%q want empty", line(m, 0))
			}
			// At column 0 the kill is a no-op (nothing before the cursor).
			m = send(m, k)
			if line(m, 0) != "" || m.buf.LineCount() != 1 {
				t.Fatalf("col-0 kill=%q lines=%d", line(m, 0), m.buf.LineCount())
			}
		}
	}
	t.Run("cmd+backspace", lk(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper}))
	t.Run("cmd+backspace (meta)", lk(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModMeta}))
	t.Run("ctrl+u", lk(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}))

	// A word kill from column 0 crosses into the previous line (vim
	// backspace=eol behavior); plain backspace still joins lines unchanged.
	t.Run("cross-line word kill", func(t *testing.T) {
		m, _ := loaded(t, "one two\nthree\n")
		m = typeKeys(m, "ji") // line 1, col 0, insert
		m = send(m, tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
		if m.buf.LineCount() != 1 || line(m, 0) != "one three" {
			t.Fatalf("cross-line kill=%q", m.buf.Lines())
		}
	})
}

func TestSaveReportsWrittenOnExLine(t *testing.T) {
	m, _ := loaded(t, "hello\n")
	m = typeKeys(m, "x")
	m = send(m, key(':'))
	m = typeKeys(m, "w")
	m = send(m, special(tea.KeyEnter))
	if m.cmdMsg != `"f.txt" written` {
		t.Fatalf("cmdMsg=%q want %q (#261)", m.cmdMsg, `"f.txt" written`)
	}
}

func TestSaveFailureReportsError(t *testing.T) {
	m, path := loaded(t, "hello\n")
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o644)
	m = typeKeys(m, "x")
	m = send(m, key(':'))
	m = typeKeys(m, "w")
	m = send(m, special(tea.KeyEnter))
	if !strings.HasPrefix(m.cmdMsg, "E: ") {
		t.Fatalf("cmdMsg=%q want a visible E: error (#261)", m.cmdMsg)
	}
	if !m.Dirty() {
		t.Fatal("a failed save must keep the buffer dirty")
	}
}

func TestWriteQuitStaysOpenOnSaveFailure(t *testing.T) {
	m, path := loaded(t, "hello\n")
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o644)
	m = typeKeys(m, "x")
	m = send(m, key(':'))
	m = typeKeys(m, "wq")
	var cmd tea.Cmd
	m, cmd = m.Update(special(tea.KeyEnter))
	if cmd != nil {
		if _, ok := cmd().(CloseMsg); ok {
			t.Fatal(":wq with a failed write must not close the pane (#261)")
		}
	}
	if !strings.HasPrefix(m.cmdMsg, "E: ") {
		t.Fatalf("cmdMsg=%q want the write error", m.cmdMsg)
	}
}

func TestSaveScratchWithoutNameReportsError(t *testing.T) {
	m := New()
	m.SetSize(40, 5)
	m.SetFocused(true)
	m = typeKeys(m, ":w")
	m = send(m, special(tea.KeyEnter))
	if m.cmdMsg != "E: no file name" {
		t.Fatalf("cmdMsg=%q want 'E: no file name' (#261)", m.cmdMsg)
	}
}

// TestReplaceActionOpensPanel guards the find/replace panel (0240 phase 2,
// #283): editor.replace opens the two-field panel, the committed literal
// search seeds Find, and the live preview jumps to the nearest match.
func TestReplaceActionOpensPanel(t *testing.T) {
	m, _ := loaded(t, "aaa\nfoo bar\n")
	m = typeKeys(m, "/foo")
	m = send(m, special(tea.KeyEnter))
	m = typeKeys(m, "gg") // back to the top so the preview jump is visible
	m, _ = m.runAction("replace")
	if m.replPanel == nil || m.replPanel.find != "foo" {
		t.Fatalf("panel = %+v, want open with Find seeded", m.replPanel)
	}
	if m.cursor.Line != 1 {
		t.Fatalf("preview should sit on the match, cursor=%v", m.cursor)
	}
	if _, ok := m.searchHLQuery(); !ok {
		t.Fatal("panel preview must drive the match highlights")
	}

	// Esc restores the origin and closes.
	m = send(m, special(tea.KeyEscape))
	if m.replPanel != nil || m.cursor.Line != 0 {
		t.Fatalf("esc must close and restore origin, cursor=%v", m.cursor)
	}
}

// TestReplacePanelRemembersFields guards #292: re-opening the panel restores
// the last find/replace strings, with the Find prefill preselected — typing
// replaces it wholesale, backspace edits it.
func TestReplacePanelRemembersFields(t *testing.T) {
	m, _ := loaded(t, "one two one\n")
	m, _ = m.runAction("replace")
	m = typeKeys(m, "one")
	m = send(m, special(tea.KeyTab))
	m = typeKeys(m, "X")
	m = send(m, special(tea.KeyEscape))

	m, _ = m.runAction("replace")
	p := m.replPanel
	if p == nil || p.find != "one" || p.repl != "X" || !p.preselect {
		t.Fatalf("re-open should restore fields preselected, got %+v", p)
	}
	m = typeKeys(m, "z") // typing replaces the preselected prefill
	if m.replPanel.find != "z" {
		t.Fatalf("typing over the prefill should replace it, got %q", m.replPanel.find)
	}

	m = send(m, special(tea.KeyEscape))
	m, _ = m.runAction("replace")
	m = send(m, special(tea.KeyBackspace)) // backspace edits, keeps the rest
	if m.replPanel.find != "" || m.replPanel.preselect {
		t.Fatalf("backspace should edit the one-rune prefill, got %q preselect=%v", m.replPanel.find, m.replPanel.preselect)
	}
}

// TestReplacePanelErrorVisibleInRows guards #292: enter on an empty pattern
// renders its error on the panel's hint row (the ex line is covered).
func TestReplacePanelErrorVisibleInRows(t *testing.T) {
	m, _ := loaded(t, "one two one\n")
	m.SetSize(60, 8)
	m, _ = m.runAction("replace")
	m = send(m, special(tea.KeyEnter))
	if m.replPanel == nil {
		t.Fatal("an empty pattern must keep the panel open")
	}
	rows := m.replacePanelRows(60)
	if len(rows) != 3 || !strings.Contains(rows[2], "empty pattern") {
		t.Fatalf("the error must render on the hint row, got %q", rows)
	}
	// The next key clears the message and the hint returns.
	m = typeKeys(m, "o")
	if rows := m.replacePanelRows(60); !strings.Contains(rows[2], "[enter]") {
		t.Fatalf("hint row should return after the next key, got %q", rows[2])
	}
}

// TestReplacePanelReplaceAll: typing both fields and ctrl+a runs the g
// substitute with the engine's report; one undo unit reverts it all.
func TestReplacePanelReplaceAll(t *testing.T) {
	m, _ := loaded(t, "one two one\nthree one\n")
	m, _ = m.runAction("replace")
	m = typeKeys(m, "one")
	m = send(m, special(tea.KeyTab))
	m = typeKeys(m, "X")
	m, _ = m.Update(modKey('a', tea.ModCtrl))
	if line(m, 0) != "X two X" || line(m, 1) != "three X" {
		t.Fatalf("replace all = %q / %q", line(m, 0), line(m, 1))
	}
	if !strings.Contains(m.cmdMsg, "3 substitutions") {
		t.Fatalf("cmdMsg = %q, want the substitution report", m.cmdMsg)
	}
	m = typeKeys(m, "u")
	if line(m, 0) != "one two one" {
		t.Fatalf("one undo must revert the whole replace, got %q", line(m, 0))
	}
}

// TestReplacePanelEnterStartsConfirmFlow: enter hands over to the y/n/a/q/l
// confirm machinery — replace-current / skip / all.
func TestReplacePanelEnterStartsConfirmFlow(t *testing.T) {
	m, _ := loaded(t, "one two one\n")
	m, _ = m.runAction("replace")
	m = typeKeys(m, "one")
	m = send(m, special(tea.KeyTab))
	m = typeKeys(m, "Y")
	m = send(m, special(tea.KeyEnter))
	if m.replPanel != nil || m.subConfirm == nil {
		t.Fatal("enter must close the panel and start the confirm flow")
	}
	m = typeKeys(m, "y") // replace the first match
	m = typeKeys(m, "n") // skip the second
	if line(m, 0) != "Y two one" {
		t.Fatalf("confirm flow result = %q, want Y two one", line(m, 0))
	}
}

// TestReplacePanelSlashesPickAnotherDelimiter: fields holding "/" still build
// a valid substitute via an alternate delimiter.
func TestReplacePanelSlashesPickAnotherDelimiter(t *testing.T) {
	m, _ := loaded(t, "a/b c\n")
	m, _ = m.runAction("replace")
	m = typeKeys(m, "a/b")
	m = send(m, special(tea.KeyTab))
	m = typeKeys(m, "Z")
	m, _ = m.Update(modKey('a', tea.ModCtrl))
	if line(m, 0) != "Z c" {
		t.Fatalf("slash pattern replace = %q, want Z c", line(m, 0))
	}
}

// searchCompileForTest builds a query via the search package (kept here so the
// test reads at the editor level).
func searchCompileForTest(pat string, regex bool) search.Query { return search.Compile(pat, regex) }
