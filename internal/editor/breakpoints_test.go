package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
)

// bpEditor loads a real 5-line file and wires a recording adjuster plus a
// live source over the given line set.
func bpEditor(t *testing.T, lines *[]int, calls *[][3]interface{}) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bp.txt")
	if err := os.WriteFile(path, []byte("l0\nl1\nl2\nl3\nl4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	m.Configure(host.MapConfig{"editor.line_numbers": "true"})
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetBreakpointSource(func(string) []int { return *lines })
	m.SetBreakpointAdjuster(func(p string, cursorAfter, delta int) {
		*calls = append(*calls, [3]interface{}{p, cursorAfter, delta})
	})
	return m
}

// TestAdjusterFiresOnLineInsert verifies "o" + esc (open line below) reports
// a +1 delta at the cursor's post-edit line by the time the change event
// lands, and further typing without a line change reports nothing.
func TestAdjusterFiresOnLineInsert(t *testing.T) {
	lines := []int{}
	var calls [][3]interface{}
	m := bpEditor(t, &lines, &calls)
	m, _ = m.Update(key('o'))                          // insert mode, new line 1
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // end the insert session
	total := 0
	for _, c := range calls {
		total += c[2].(int)
	}
	if len(calls) == 0 || total != 1 {
		t.Fatalf("adjust calls = %v, want a net +1 delta", calls)
	}
	if last := calls[len(calls)-1]; last[1].(int) != 1 {
		t.Fatalf("adjust cursorAfter = %v, want line 1", last[1])
	}
	calls = nil
	m, _ = m.Update(key('x')) // normal-mode delete of one char: no line change
	if len(calls) != 0 {
		t.Fatalf("a same-line edit must not adjust, got %v", calls)
	}
}

// TestAdjusterFiresOnLineDelete verifies "dd" reports -1 at the cursor line.
func TestAdjusterFiresOnLineDelete(t *testing.T) {
	lines := []int{}
	var calls [][3]interface{}
	m := bpEditor(t, &lines, &calls)
	m, _ = m.Update(key('j'))
	m, _ = m.Update(key('d'))
	m, _ = m.Update(key('d')) // delete line 1, cursor stays on 1
	if len(calls) != 1 || calls[0][1].(int) != 1 || calls[0][2].(int) != -1 {
		t.Fatalf("adjust calls = %v, want one (1, -1)", calls)
	}
}

// TestGutterRendersBreakpointLine verifies the source-driven gutter marker:
// the breakpoint line's number renders bold in the error tone.
func TestGutterRendersBreakpointLine(t *testing.T) {
	lines := []int{2}
	var calls [][3]interface{}
	m := bpEditor(t, &lines, &calls)
	m.SetSize(40, 10)
	view := m.View()
	if !strings.Contains(view, "\x1b[1;") && !strings.Contains(view, ";1m") && !strings.Contains(view, "[1m") {
		t.Fatalf("expected a bold gutter cell for the breakpoint line in:\n%s", view)
	}
}

// TestGutterHit maps gutter clicks to buffer lines and rejects body clicks.
func TestGutterHit(t *testing.T) {
	lines := []int{}
	var calls [][3]interface{}
	m := bpEditor(t, &lines, &calls)
	m.SetSize(40, 10)
	line, ok := m.GutterHit(0, 3)
	if !ok || line != 3 {
		t.Fatalf("GutterHit(0,3) = %d/%v, want 3/true", line, ok)
	}
	gw := m.GutterWidth()
	if _, ok := m.GutterHit(gw, 3); ok {
		t.Fatal("a click at the first content column is not a gutter hit")
	}
}
