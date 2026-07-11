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

func TestCommentBlockCharwiseWrapUnwrap(t *testing.T) {
	m := loadedExt(t, "ctest", "alpha bravo charlie\n")
	// Select "bravo" charwise: w, v, e.
	m = send(m, key('w'), key('v'), key('e'))
	m, _ = m.runAction("comment_block")
	if got := line(m, 0); got != "alpha /* bravo */ charlie" {
		t.Fatalf("wrap: %q", got)
	}
	if m.mode != Normal {
		t.Fatalf("block toggle should leave visual mode, mode=%v", m.mode)
	}
	// Re-select the exact wrapped text and toggle again → unwrap.
	m = send(m, key('0'), key('w'), key('v'), key('e'), key('e'), key('e')) // through "/* bravo */"
	m, _ = m.runAction("comment_block")
	if got := line(m, 0); got != "alpha bravo charlie" {
		t.Fatalf("unwrap: %q", got)
	}
}

func TestCommentBlockLinewiseWrapUnwrapAndUndo(t *testing.T) {
	m := loadedExt(t, "ctest", "\tone\n\ttwo\nthree\n")
	m = send(m, key('V'), key('j'))
	m, _ = m.runAction("comment_block")
	want := []string{"\t/*", "\tone", "\ttwo", "\t*/", "three"}
	for i, w := range want {
		if line(m, i) != w {
			t.Fatalf("wrap line %d: %q, want %q (all: %q)", i, line(m, i), w, m.buf.Lines())
		}
	}
	// One undo unit reverts the whole wrap.
	m = send(m, key('u'))
	if line(m, 0) != "\tone" || m.buf.LineCount() != 3 {
		t.Fatalf("undo: %q", m.buf.Lines())
	}
	// Wrap again, then select the wrapped block including markers → unwrap.
	m = typeKeys(m, "gg")
	m = send(m, key('V'), key('j'))
	m, _ = m.runAction("comment_block")
	m = typeKeys(m, "gg")
	m = send(m, key('V'), key('j'), key('j'), key('j'))
	m, _ = m.runAction("comment_block")
	if line(m, 0) != "\tone" || line(m, 1) != "\ttwo" || line(m, 2) != "three" {
		t.Fatalf("unwrap: %q", m.buf.Lines())
	}
}

func TestCommentBlockCurrentLineWrap(t *testing.T) {
	m := loadedExt(t, "ctest", "only\n")
	m, _ = m.runAction("comment_block")
	if line(m, 0) != "/*" || line(m, 1) != "only" || line(m, 2) != "*/" {
		t.Fatalf("current-line wrap: %q", m.buf.Lines())
	}
	if m.cursor.Line != 1 {
		t.Fatalf("cursor should stay on the content line, line=%d", m.cursor.Line)
	}
}

func TestCommentBlockFallsBackToLineComments(t *testing.T) {
	m := loadedExt(t, "ctestnb", "one\n") // '#' line marker, no block pair
	m, _ = m.runAction("comment_block")
	if got := line(m, 0); got != "# one" {
		t.Fatalf("fallback: %q", got)
	}
}

func TestCommentBlockDotRepeat(t *testing.T) {
	m := loadedExt(t, "ctest", "one\ntwo\n")
	m, _ = m.runAction("comment_block")
	// Cursor sits on "one" (line 1 after wrap); move to "two" and repeat.
	m = typeKeys(m, "G")
	m = send(m, key('.'))
	got := m.buf.Lines()
	want := []string{"/*", "one", "*/", "/*", "two", "*/"}
	for i, w := range want {
		if i >= len(got) || got[i] != w {
			t.Fatalf("dot repeat: %q, want %q", got, want)
		}
	}
}

// TestCommentLineCommentsBlankLines guards #428: blank lines inside the range
// get a marker (padded to the marker column) so a commented region has no
// gaps; uncommenting empties them again.
func TestCommentLineCommentsBlankLines(t *testing.T) {
	m := loadedExt(t, "ctestnb", "    one\n\n    two\n")
	m = send(m, key('V'), key('j'), key('j'))
	m, _ = m.runAction("comment_line")
	if line(m, 0) != "    # one" || line(m, 1) != "    #" || line(m, 2) != "    # two" {
		t.Fatalf("comment with blank: %q", m.buf.Lines())
	}
	m, _ = m.runAction("comment_line")
	if line(m, 0) != "    one" || line(m, 1) != "" || line(m, 2) != "    two" {
		t.Fatalf("uncomment: %q", m.buf.Lines())
	}
}

// TestCommentLineBlankOnlyRange guards #428: toggling a blank line is no
// longer a no-op, so repeated single-line cmd+7 (which advances the cursor)
// walks across empty lines.
func TestCommentLineBlankOnlyRange(t *testing.T) {
	m := loadedExt(t, "ctestnb", "# above\n\nbelow\n")
	m = send(m, key('j')) // onto the blank line
	m, _ = m.runAction("comment_line")
	if got := line(m, 1); got != "#" {
		t.Fatalf("blank line comment: %q", got)
	}
	if m.cursor.Line != 2 {
		t.Fatalf("cursor must advance, line=%d", m.cursor.Line)
	}
}

// TestCommentLineAlignsWithCommentAbove guards #428: when the line above the
// range is a line comment, new markers land in its column instead of the
// range's min indent.
func TestCommentLineAlignsWithCommentAbove(t *testing.T) {
	m := loadedExt(t, "ctestnb", "  # first\n    second\n")
	m = send(m, key('j'))
	m, _ = m.runAction("comment_line")
	if got := line(m, 1); got != "  #   second" {
		t.Fatalf("aligned comment: %q", got)
	}
	// No comment above: marker falls back to the line's indent.
	m2 := loadedExt(t, "ctestnb", "above\n    second\n")
	m2 = send(m2, key('j'))
	m2, _ = m2.runAction("comment_line")
	if got := line(m2, 1); got != "    # second" {
		t.Fatalf("indent fallback: %q", got)
	}
}

// TestCommentLineAlignAboveClampsToText ensures a comment column deeper than
// the line's own indent never splits the text.
func TestCommentLineAlignAboveClampsToText(t *testing.T) {
	m := loadedExt(t, "ctestnb", "        # deep\nshallow\n")
	m = send(m, key('j'))
	m, _ = m.runAction("comment_line")
	if got := line(m, 1); got != "# shallow" {
		t.Fatalf("clamped comment: %q", got)
	}
}
