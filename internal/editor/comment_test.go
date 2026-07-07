package editor

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/lang"
)

func init() {
	// A private test language so comment tests never depend on the compiled-in
	// language plugins (which live under plugins/ and are not imported here).
	lang.Register(lang.Language{ID: "ctest", Extensions: []string{"ctest"}, LineComment: "//", BlockComment: [2]string{"/*", "*/"}})
	lang.Register(lang.Language{ID: "ctest-nb", Extensions: []string{"ctestnb"}, LineComment: "#"})
	lang.Register(lang.Language{ID: "ctest-none", Extensions: []string{"ctestnone"}})
}

// loadedExt is loaded with a caller-chosen file extension, so lang.Comments
// resolves the test language.
func loadedExt(t *testing.T, ext, content string) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f."+ext)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	m.SetFocused(true)
	return m
}

func TestCommentLineToggleAndCursorAdvance(t *testing.T) {
	m := loadedExt(t, "ctest", "alpha\nbeta\n")
	m, _ = m.runAction("comment_line")
	if got := line(m, 0); got != "// alpha" {
		t.Fatalf("comment: %q", got)
	}
	if m.cursor.Line != 1 {
		t.Fatalf("single-line toggle must advance the cursor, line=%d", m.cursor.Line)
	}
	// Toggling the now-commented line back (from line 1, move up first).
	m = send(m, key('k'))
	m, _ = m.runAction("comment_line")
	if got := line(m, 0); got != "alpha" {
		t.Fatalf("uncomment: %q", got)
	}
}

func TestCommentLineSelectionMixedAndIndent(t *testing.T) {
	m := loadedExt(t, "ctest", "\tone\n\t\t// two\n\tthree\n")
	// Linewise-select all three lines: V, j, j.
	m = send(m, key('V'), key('j'), key('j'))
	m, _ = m.runAction("comment_line")
	// Mixed range: uncommented lines gain a marker at the minimal indent (one
	// tab); the already-commented line is untouched.
	if got := line(m, 0); got != "\t// one" {
		t.Fatalf("line 0: %q", got)
	}
	if got := line(m, 1); got != "\t\t// two" {
		t.Fatalf("line 1 must stay untouched: %q", got)
	}
	if got := line(m, 2); got != "\t// three" {
		t.Fatalf("line 2: %q", got)
	}
	if !m.mode.IsVisual() || m.anchor.Line != 0 || m.cursor.Line != 2 {
		t.Fatalf("selection must be preserved, mode=%v anchor=%v cursor=%v", m.mode, m.anchor, m.cursor)
	}
	// Now fully commented: a second toggle uncomments every line.
	m, _ = m.runAction("comment_line")
	if line(m, 0) != "\tone" || line(m, 1) != "\t\ttwo" || line(m, 2) != "\tthree" {
		t.Fatalf("uncomment all: %q", m.buf.Lines())
	}
}

func TestCommentLineOneUndoUnitAndDotRepeat(t *testing.T) {
	m := loadedExt(t, "ctest", "one\ntwo\nthree\n")
	m = send(m, key('V'), key('j'))
	m, _ = m.runAction("comment_line")
	if line(m, 0) != "// one" || line(m, 1) != "// two" {
		t.Fatalf("comment: %q", m.buf.Lines())
	}
	m = send(m, special(27)) // Esc leaves visual
	m = send(m, key('u'))
	if line(m, 0) != "one" || line(m, 1) != "two" {
		t.Fatalf("one undo must revert the whole toggle: %q", m.buf.Lines())
	}
	// Dot repeat: comment line 0, then '.' on line 1 (cursor advanced there).
	m = typeKeys(m, "gg")
	m, _ = m.runAction("comment_line")
	m = send(m, key('.'))
	if line(m, 0) != "// one" || line(m, 1) != "// two" {
		t.Fatalf("dot repeat: %q", m.buf.Lines())
	}
}

func TestCommentLineCommitsInsertSession(t *testing.T) {
	m := loadedExt(t, "ctest", "one\n")
	m = send(m, key('i'))
	m = typeKeys(m, "X")
	m, _ = m.runAction("comment_line")
	if m.mode != Normal {
		t.Fatalf("insert session must commit first, mode=%v", m.mode)
	}
	if got := line(m, 0); got != "// Xone" {
		t.Fatalf("toggle after insert commit: %q", got)
	}
}

func TestCommentLineNoSyntaxNotifies(t *testing.T) {
	m := loadedExt(t, "ctestnone", "one\n")
	m, cmd := m.runAction("comment_line")
	if got := line(m, 0); got != "one" {
		t.Fatalf("no-syntax toggle must not edit: %q", got)
	}
	if cmd == nil {
		t.Fatal("no-syntax toggle must produce feedback")
	}
	if n, ok := cmd().(NoticeMsg); !ok || n.Text == "" {
		t.Fatalf("expected a NoticeMsg, got %#v", cmd())
	}
}

func TestCommentLineSkipsBlankLines(t *testing.T) {
	m := loadedExt(t, "ctestnb", "one\n\ntwo\n")
	m = send(m, key('V'), key('j'), key('j'))
	m, _ = m.runAction("comment_line")
	if line(m, 0) != "# one" || line(m, 1) != "" || line(m, 2) != "# two" {
		t.Fatalf("blank line must stay blank: %q", m.buf.Lines())
	}
	m, _ = m.runAction("comment_line")
	if line(m, 0) != "one" || line(m, 1) != "" || line(m, 2) != "two" {
		t.Fatalf("uncomment: %q", m.buf.Lines())
	}
}
