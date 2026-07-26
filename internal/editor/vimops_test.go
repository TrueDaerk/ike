package editor

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// loadedAs is loaded with an explicit file name, for language-aware behavior.
func loadedAs(t *testing.T, name, content string) (Model, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
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

// --- case operators --------------------------------------------------------

func TestCaseOperatorWithMotion(t *testing.T) {
	m, _ := loaded(t, "Hello World\n")
	m = typeKeys(m, "gUw")
	if line(m, 0) != "HELLO World" {
		t.Fatalf("gUw got %q", line(m, 0))
	}
	if m.cursor.Col != 0 {
		t.Fatalf("cursor should stay at span start, col=%d", m.cursor.Col)
	}
	m = typeKeys(m, "guw")
	if line(m, 0) != "hello World" {
		t.Fatalf("guw got %q", line(m, 0))
	}
	m = typeKeys(m, "g~w")
	if line(m, 0) != "HELLO World" {
		t.Fatalf("g~w got %q", line(m, 0))
	}
}

func TestCaseOperatorLinewiseDoubling(t *testing.T) {
	m, _ := loaded(t, "one Two\nthree\n")
	m = typeKeys(m, "gUU")
	if line(m, 0) != "ONE TWO" || line(m, 1) != "three" {
		t.Fatalf("gUU got %q / %q", line(m, 0), line(m, 1))
	}
	m = typeKeys(m, "guu")
	if line(m, 0) != "one two" {
		t.Fatalf("guu got %q", line(m, 0))
	}
	// The repeated-sequence form (gugu) is the same linewise operation.
	m = typeKeys(m, "gUgU")
	if line(m, 0) != "ONE TWO" {
		t.Fatalf("gUgU got %q", line(m, 0))
	}
}

func TestCaseOperatorTextObjectAndDot(t *testing.T) {
	m, _ := loaded(t, "foo bar\n")
	m = typeKeys(m, "gUiw")
	if line(m, 0) != "FOO bar" {
		t.Fatalf("gUiw got %q", line(m, 0))
	}
	m = typeKeys(m, "u") // undo restores
	if line(m, 0) != "foo bar" {
		t.Fatalf("undo got %q", line(m, 0))
	}
}

func TestVisualCaseOps(t *testing.T) {
	m, _ := loaded(t, "foo bar\n")
	m = typeKeys(m, "vllU")
	if line(m, 0) != "FOO bar" {
		t.Fatalf("vllU got %q", line(m, 0))
	}
	if m.mode != Normal {
		t.Fatal("case op should leave visual mode")
	}
	m = typeKeys(m, "v$u")
	if line(m, 0) != "foo bar" {
		t.Fatalf("v$u got %q", line(m, 0))
	}
	m = typeKeys(m, "V~")
	if line(m, 0) != "FOO BAR" {
		t.Fatalf("V~ got %q", line(m, 0))
	}
}

// --- visual J / r ----------------------------------------------------------

func TestVisualJoin(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\n")
	m = typeKeys(m, "VjJ")
	if line(m, 0) != "one two" || line(m, 1) != "three" {
		t.Fatalf("VjJ got %q / %q", line(m, 0), line(m, 1))
	}
}

func TestVisualReplace(t *testing.T) {
	m, _ := loaded(t, "abcdef\n")
	m = typeKeys(m, "vllrx")
	if line(m, 0) != "xxxdef" {
		t.Fatalf("vllrx got %q", line(m, 0))
	}
	if m.mode != Normal {
		t.Fatal("r should leave visual mode")
	}
}

func TestVisualReplaceLinewiseKeepsBreaks(t *testing.T) {
	m, _ := loaded(t, "ab\ncd\n")
	m = typeKeys(m, "Vjr-")
	if line(m, 0) != "--" || line(m, 1) != "--" {
		t.Fatalf("Vjr- got %q / %q", line(m, 0), line(m, 1))
	}
}

// --- gJ --------------------------------------------------------------------

func TestGJoinNoSpace(t *testing.T) {
	m, _ := loaded(t, "one\n  two\n")
	m = typeKeys(m, "gJ")
	if line(m, 0) != "one  two" {
		t.Fatalf("gJ got %q", line(m, 0))
	}
}

// --- ge / gE ---------------------------------------------------------------

func TestWordEndBackward(t *testing.T) {
	m, _ := loaded(t, "foo bar.baz qux\n")
	m = typeKeys(m, "$")
	m = typeKeys(m, "ge") // from 'x' of qux to 'z' of baz
	if m.cursor.Col != 10 {
		t.Fatalf("ge col=%d want 10", m.cursor.Col)
	}
	m = typeKeys(m, "ge") // to '.' (punct run end)
	if m.cursor.Col != 7 {
		t.Fatalf("second ge col=%d want 7", m.cursor.Col)
	}
	m = typeKeys(m, "$gE") // WORD end: 'z' of bar.baz
	if m.cursor.Col != 10 {
		t.Fatalf("gE col=%d want 10", m.cursor.Col)
	}
}

func TestDeleteToWordEndBackward(t *testing.T) {
	m, _ := loaded(t, "foo bar\n")
	m = typeKeys(m, "$dge")
	// Vim's dge spans [previous word end, cursor): "o ba" goes, "for" stays.
	if line(m, 0) != "for" {
		t.Fatalf("dge got %q", line(m, 0))
	}
}

// --- gv / gi ---------------------------------------------------------------

func TestReselectVisual(t *testing.T) {
	m, _ := loaded(t, "abcdef\n")
	m = typeKeys(m, "vll")
	m = send(m, special(tea.KeyEscape))
	if m.mode != Normal {
		t.Fatal("esc should leave visual")
	}
	m = typeKeys(m, "$gv")
	if !m.mode.IsVisual() || m.anchor.Col != 0 || m.cursor.Col != 2 {
		t.Fatalf("gv mode=%v anchor=%v cursor=%v", m.mode, m.anchor, m.cursor)
	}
	m = typeKeys(m, "d")
	if line(m, 0) != "def" {
		t.Fatalf("gvd got %q", line(m, 0))
	}
}

func TestGotoLastInsert(t *testing.T) {
	m, _ := loaded(t, "hello\n")
	m = typeKeys(m, "ax") // insert after 'h'
	m = send(m, special(tea.KeyEscape))
	m = typeKeys(m, "$gi")
	if m.mode != Insert {
		t.Fatal("gi should enter insert mode")
	}
	if m.cursor.Col != 2 {
		t.Fatalf("gi col=%d want 2", m.cursor.Col)
	}
}

// --- text objects ----------------------------------------------------------

func TestParagraphObject(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\n\nthree\nfour\n")
	m = typeKeys(m, "dip")
	if m.buf.LineCount() != 3 || line(m, 0) != "" || line(m, 1) != "three" {
		t.Fatalf("dip got %q", m.buf.Lines())
	}
}

func TestParagraphAroundIncludesBlanks(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\n\n\nthree\n")
	m = typeKeys(m, "dap")
	if line(m, 0) != "three" {
		t.Fatalf("dap got %q", m.buf.Lines())
	}
}

func TestVisualParagraphSelectsLinewise(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\n\nthree\n")
	m = typeKeys(m, "jvip")
	if m.mode != VisualLine {
		t.Fatalf("vip should switch to V-LINE, mode=%v", m.mode)
	}
	if m.anchor.Line != 0 || m.cursor.Line != 1 {
		t.Fatalf("vip lines %d..%d", m.anchor.Line, m.cursor.Line)
	}
}

func TestSentenceObject(t *testing.T) {
	m, _ := loaded(t, "One two. Three four! Five.\n")
	m = typeKeys(m, "fTdis") // cursor on "Three"
	if line(m, 0) != "One two.  Five." {
		t.Fatalf("dis got %q", line(m, 0))
	}
	m, _ = loaded(t, "One two. Three four! Five.\n")
	m = typeKeys(m, "fTdas")
	if line(m, 0) != "One two. Five." {
		t.Fatalf("das got %q", line(m, 0))
	}
}

func TestTagObject(t *testing.T) {
	m, _ := loadedAs(t, "f.html", "<div><p>hi there</p></div>\n")
	m = typeKeys(m, "fhdit") // cursor on 'h' of hi, inside <p>
	if line(m, 0) != "<div><p></p></div>" {
		t.Fatalf("dit got %q", line(m, 0))
	}
	m, _ = loadedAs(t, "f.html", "<div><p>hi there</p></div>\n")
	m = typeKeys(m, "fhdat")
	if line(m, 0) != "<div></div>" {
		t.Fatalf("dat got %q", line(m, 0))
	}
}

func TestTagObjectMultiline(t *testing.T) {
	m, _ := loadedAs(t, "f.html", "<ul>\n  <li>a</li>\n</ul>\n")
	m = typeKeys(m, "jfadat")
	if line(m, 0) != "<ul>" || line(m, 1) != "  " {
		t.Fatalf("dat got %q", m.buf.Lines())
	}
}

func TestBracketAliases(t *testing.T) {
	m, _ := loaded(t, "a (one) {two} b\n")
	m = typeKeys(m, "fodib")
	if line(m, 0) != "a () {two} b" {
		t.Fatalf("dib got %q", line(m, 0))
	}
	m = typeKeys(m, "ftdaB")
	if line(m, 0) != "a ()  b" {
		t.Fatalf("daB got %q", line(m, 0))
	}
}

// --- z scrolling and ZZ/ZQ -------------------------------------------------

func TestScrollZCommands(t *testing.T) {
	content := ""
	for i := 0; i < 100; i++ {
		content += "line\n"
	}
	m, _ := loaded(t, content)
	m.view.ScrollOff = 0
	m = typeKeys(m, "50G")
	m = typeKeys(m, "zt")
	if m.view.Top != 49 {
		t.Fatalf("zt top=%d want 49", m.view.Top)
	}
	m = typeKeys(m, "zz")
	h := m.view.Height()
	if m.view.Top != 49-h/2 {
		t.Fatalf("zz top=%d want %d", m.view.Top, 49-h/2)
	}
	m = typeKeys(m, "zb")
	if m.view.Top != 49-h+1 {
		t.Fatalf("zb top=%d want %d", m.view.Top, 49-h+1)
	}
}

func TestZZSavesAndCloses(t *testing.T) {
	m, path := loaded(t, "hello\n")
	m = typeKeys(m, "x")
	m, _ = m.Update(key('Z'))
	_, cmd := m.Update(key('Z'))
	if cmd == nil {
		t.Fatal("ZZ should produce a command")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Fatalf("ZZ msg=%T want CloseMsg", cmd())
	}
	b, _ := os.ReadFile(path)
	if string(b) != "ello\n" {
		t.Fatalf("ZZ should save, file=%q", b)
	}
}

func TestZQClosesWithoutSaving(t *testing.T) {
	m, path := loaded(t, "hello\n")
	m = typeKeys(m, "x")
	m, _ = m.Update(key('Z'))
	_, cmd := m.Update(key('Q'))
	if cmd == nil {
		t.Fatal("ZQ should produce a command")
	}
	msg, ok := cmd().(CloseMsg)
	if !ok || !msg.Force {
		t.Fatalf("ZQ msg=%#v want CloseMsg{Force:true}", cmd())
	}
	b, _ := os.ReadFile(path)
	if string(b) != "hello\n" {
		t.Fatalf("ZQ must not save, file=%q", b)
	}
}

// --- = reindent ------------------------------------------------------------

func TestReindentOperator(t *testing.T) {
	// "tabtest" (registered in langindent_test.go) indents after ":".
	m, _ := loadedAs(t, "f.tabtest", "run:\nx = 1\n")
	m = typeKeys(m, "j==")
	if line(m, 1) != "\tx = 1" {
		t.Fatalf("== got %q", line(m, 1))
	}
}

func TestReindentVisual(t *testing.T) {
	m, _ := loadedAs(t, "f.tabtest", "run:\n        x = 1\n")
	m = typeKeys(m, "VG=")
	if line(m, 1) != "\tx = 1" {
		t.Fatalf("V= got %q", m.buf.Lines())
	}
}

// --- gq reflow -------------------------------------------------------------

func TestReflowOperator(t *testing.T) {
	m, _ := loaded(t, "alpha beta gamma delta epsilon zeta\n")
	m.textWidth = 20
	m = typeKeys(m, "gqq")
	if line(m, 0) != "alpha beta gamma" || line(m, 1) != "delta epsilon zeta" {
		t.Fatalf("gqq got %q", m.buf.Lines())
	}
}

func TestReflowKeepsCommentLeader(t *testing.T) {
	m, _ := loaded(t, "// alpha beta gamma delta epsilon\n")
	m.textWidth = 20
	m = typeKeys(m, "gqq")
	if line(m, 0) != "// alpha beta gamma" || line(m, 1) != "// delta epsilon" {
		t.Fatalf("gqq got %q", m.buf.Lines())
	}
}

func TestReflowMotionParagraph(t *testing.T) {
	m, _ := loaded(t, "one two three four five\n\nkeep\n")
	m.textWidth = 10
	m = typeKeys(m, "gqip")
	if line(m, 0) != "one two" || m.buf.Line(m.buf.LineCount()-1) != "keep" {
		t.Fatalf("gqip got %q", m.buf.Lines())
	}
}

// --- display-line motions --------------------------------------------------

func TestDisplayMotionsNoWrap(t *testing.T) {
	m, _ := loaded(t, "hello world\nnext\n")
	m = typeKeys(m, "llg0")
	if m.cursor.Col != 0 {
		t.Fatalf("g0 col=%d", m.cursor.Col)
	}
	m = typeKeys(m, "g$")
	if m.cursor.Col != 10 {
		t.Fatalf("g$ col=%d want 10", m.cursor.Col)
	}
	m = typeKeys(m, "gj")
	if m.cursor.Line != 1 {
		t.Fatalf("gj line=%d want 1", m.cursor.Line)
	}
	m = typeKeys(m, "gk")
	if m.cursor.Line != 0 {
		t.Fatalf("gk line=%d want 0", m.cursor.Line)
	}
}

func TestDisplayMotionsSoftWrap(t *testing.T) {
	m, _ := loaded(t, "aaaa bbbb cccc dddd eeee ffff gggg hhhh\n")
	m.SetSize(20, 10) // narrow view forces wrapping
	m.softWrap = true
	m = typeKeys(m, "gj")
	if m.cursor.Line != 0 || m.cursor.Col == 0 {
		t.Fatalf("gj should move within the line: %v", m.cursor)
	}
	col := m.cursor.Col
	m = typeKeys(m, "g0")
	if m.cursor.Col == 0 || m.cursor.Col > col {
		t.Fatalf("g0 should land on the segment start, col=%d", m.cursor.Col)
	}
	seg := m.cursor.Col
	m = typeKeys(m, "g$")
	if m.cursor.Col < seg {
		t.Fatalf("g$ col=%d below segment start %d", m.cursor.Col, seg)
	}
}

func TestUnknownGSequenceCancelsPending(t *testing.T) {
	m, _ := loaded(t, "one two\n")
	m = typeKeys(m, "dgx") // invalid g-sequence must drop the pending d
	m = typeKeys(m, "w")
	if line(m, 0) != "one two" {
		t.Fatalf("pending operator leaked: %q", line(m, 0))
	}
	if m.cursor.Col != 4 {
		t.Fatalf("w should have moved the cursor, col=%d", m.cursor.Col)
	}
}

// --- gf --------------------------------------------------------------------

func TestOpenFileUnderCursor(t *testing.T) {
	m, path := loaded(t, "see other.txt here\n")
	m = typeKeys(m, "fo")
	var cmd tea.Cmd
	m, _ = m.Update(key('g'))
	_, cmd = m.Update(key('f'))
	if cmd == nil {
		t.Fatal("gf should produce a command")
	}
	msg, ok := cmd().(OpenPathMsg)
	if !ok {
		t.Fatalf("gf msg=%T", cmd())
	}
	if msg.Path != "other.txt" || msg.From != path {
		t.Fatalf("gf msg=%#v", msg)
	}
}
