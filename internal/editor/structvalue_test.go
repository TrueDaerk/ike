package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
)

// structvalue_test.go covers the editor half of gy / gY (#2499): the chords
// reach the command, the copy lands in the clipboard register and in the
// history, and a buffer with nothing structural under the caret declines with
// a notice instead of copying air. The per-language extraction itself is
// tested against the real grammars in internal/structval.

// TestYankValueNoGrammar (#2499): in a plain buffer — no grammar, so no chain
// — both commands report that there is no value and leave the registers alone.
func TestYankValueNoGrammar(t *testing.T) {
	m, _ := loaded(t, "just some prose\n")
	m.cursor = buffer.Position{Line: 0, Col: 5}
	for _, outer := range []bool{false, true} {
		cmd := m.yankStructuralValue(outer)
		if got := noticeIn(t, cmd); got != "no structural value under the cursor" {
			t.Fatalf("outer=%v notice = %q", outer, got)
		}
		if got := m.regs.Get('"').Text; got != "" {
			t.Fatalf("outer=%v yanked %q, want nothing", outer, got)
		}
	}
}

// TestYankValueLargeFile (#2499): whole-buffer analyses are off in large-file
// mode, and the notice says so rather than pretending the file has no values.
func TestYankValueLargeFile(t *testing.T) {
	m, _ := loaded(t, "just some prose\n")
	m.largeFile = true
	if got := noticeIn(t, m.yankStructuralValue(false)); !strings.Contains(got, "large-file") {
		t.Fatalf("notice = %q, want the large-file explanation", got)
	}
}

// TestCopiedValueLabel (#2499): the toast leads with the first line of what
// was copied and closes with the size hint.
func TestCopiedValueLabel(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"x", `copied "x" (1 char)`},
		{"hello", `copied "hello" (5 chars)`},
		{"a\nb\n", "copied \"a\" (2 lines, 4 chars)"},
		{strings.Repeat("z", 50), `copied "` + strings.Repeat("z", 39) + `…" (50 chars)`},
	} {
		if got := copiedValueLabel(tc.text); got != tc.want {
			t.Errorf("copiedValueLabel(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}
