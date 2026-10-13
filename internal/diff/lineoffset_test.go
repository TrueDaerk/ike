package diff

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// lineoffset_test.go covers the per-side line offsets (#2956): an excerpt
// numbers its rows the way the file does, and enter lands on the file line.

func TestLineOffsetsShiftGutterAndJump(t *testing.T) {
	m := testModel(t, "a\nold\nc", "a\nnew\nc")
	m.SetLineOffsets(100, 1000)
	m.SetContents("a\nold\nc", "a\nnew\nc")
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "102") || !strings.Contains(view, "1002") {
		t.Fatalf("gutter must show the shifted numbers:\n%s", view)
	}
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter must jump")
	}
	if msg := cmd().(JumpMsg); msg.Line != 1002 {
		t.Fatalf("jump line = %d, want 1002", msg.Line)
	}
	// A retarget is another comparison: the numbering resets.
	m.Retarget("l", "r", "", "/tmp/right.txt", "", "", false)
	if l, r := m.LineOffsets(); l != 0 || r != 0 {
		t.Fatalf("offsets after retarget = %d/%d", l, r)
	}
}
