package editor

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"ike/internal/host"
)

// key builds a tea.KeyMsg for a single rune.
func key(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// special builds a tea.KeyMsg for a non-rune key.
func special(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

// keys builds a sequence of single-rune key messages from a string.
func keys(s string) []tea.KeyMsg {
	var out []tea.KeyMsg
	for _, r := range s {
		out = append(out, key(r))
	}
	return out
}

// send applies a sequence of keys and returns the resulting model.
func send(m Model, ks ...tea.KeyMsg) Model {
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
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlR}) // redo
	if line(m, 0) != "ello" {
		t.Fatalf("redo=%q want ello", line(m, 0))
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

func TestShiftArrowWordNav(t *testing.T) {
	m, _ := loaded(t, "foo bar baz\n")
	m = send(m, special(tea.KeyShiftRight))
	if m.cursor.Col != 4 {
		t.Fatalf("shift+right col=%d want 4", m.cursor.Col)
	}
	m = send(m, special(tea.KeyShiftLeft))
	if m.cursor.Col != 0 {
		t.Fatalf("shift+left col=%d want 0", m.cursor.Col)
	}
}

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
