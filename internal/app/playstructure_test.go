package app

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// playstructure_test.go covers the playground's structure strip (#2793): the
// entries, the width it takes off the result, the click and keyboard jumps,
// the viewport highlight and the cases that hide it.

// bigObject is a result far taller than the pane: 40 keys, each an object of
// five members.
func bigObject() string {
	var b strings.Builder
	b.WriteString("{")
	for i := 0; i < 40; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `"k%02d":{"a":1,"b":2,"c":3,"d":4,"e":5}`, i)
	}
	b.WriteString("}")
	return b.String()
}

// structureOn dispatches playground.structure.
func structureOn(m Model) Model {
	tm, cmd := m.Update(TogglePlayStructureMsg{})
	return drainCmd(tm.(Model), cmd)
}

// playResultWidth is the hosting pane's interior width.
func playResultWidth(m Model) int {
	r := m.lay.Panes[m.play.paneKey]
	return paneInterior(r.W, paneChromeW)
}

// clickResult presses the left button at content-local x/y of the playground.
func clickResult(m Model, x, y int) Model {
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	r := m.lay.Panes[m.play.paneKey]
	tm, cmd := m.Update(tea.MouseClickMsg{X: r.X + paneContentX + x, Y: r.Y + m.contentYOff(m.play.paneKey) + y, Button: tea.MouseLeft})
	return drainCmd(tm.(Model), cmd)
}

// TestPlayStructureItemsAndWidth: an object result lists its keys, the strip
// takes its cells off the result editor, and the rendered rows carry them.
func TestPlayStructureItemsAndWidth(t *testing.T) {
	m := openJQ(t, playApp(t, bigObject()))
	m = setProgram(m, ".")
	width := playResultWidth(m)
	if width < playStripMinPane {
		t.Fatalf("test setup: pane interior %d is below the strip threshold", width)
	}
	if len(m.play.outline) != 40 || m.play.outline[0].Label != "k00" || m.play.outline[39].Label != "k39" {
		t.Fatalf("outline = %+v", m.play.outline)
	}
	if m.playStripW(width) != 0 {
		t.Fatal("the strip is off by default")
	}
	m = structureOn(m)
	sw := m.playStripW(width)
	if sw != playStripMinW { // `k00` plus separator and pad is under the floor
		t.Fatalf("strip width = %d, want the label plus separator and pad", sw)
	}
	body := ansi.Strip(m.playInlineBody(width))
	rows := strings.Split(body, "\n")
	last := rows[len(rows)-1]
	if ansi.StringWidth(last) != width {
		t.Errorf("a result row is %d cells, want the pane's %d", ansi.StringWidth(last), width)
	}
	if !strings.Contains(body, "│ k00") {
		t.Errorf("the strip's first entry is missing:\n%s", body)
	}
	if m.play.resultEd.Width() != width-sw {
		t.Errorf("the result editor was not narrowed for the strip")
	}
	if !m.play.stripFocus {
		t.Error("the toggle shows the strip and gives it the keyboard")
	}
}

// TestPlayStructureClickJumps: a click on an entry scrolls its node into view
// and puts the caret on it; a click in the result text beside the strip still
// lands in the result.
func TestPlayStructureClickJumps(t *testing.T) {
	m := openJQ(t, playApp(t, bigObject()))
	m = setProgram(m, ".")
	m = structureOn(m)
	width := playResultWidth(m)
	sw := m.playStripW(width)
	_ = m.playInlineBody(width)       // render once: the strip window is laid out
	m = clickResult(m, width-sw+2, 5) // the sixth entry: k05
	line, col := m.play.resultEd.CursorPos()
	if want := m.play.outline[5].Line; line != want || col != 2 {
		t.Fatalf("caret = %d:%d, want %d:2 (k05's key)", line, col, want)
	}
	if first, last := m.play.resultEd.VisibleLines(); line < first || line > last {
		t.Errorf("the node is not in view: %d outside %d..%d", line, first, last)
	}
	if !m.play.bufFocus || m.play.stripFocus {
		t.Error("a jump hands the keyboard to the result")
	}
	// Jump far down: the strip window follows the viewport.
	m.jumpPlayStrip(39)
	_ = m.playInlineBody(width)
	if m.play.stripTop == 0 {
		t.Error("the strip window must follow the viewport to the last key")
	}
	body := ansi.Strip(m.playInlineBody(width))
	if !strings.Contains(body, "k39") {
		t.Errorf("the last key is not drawn after jumping to it:\n%s", body)
	}
	// A click in the text area (left of the strip) is the result's.
	m = clickResult(m, 3, 0)
	if m.play.stripFocus {
		t.Error("a text click must not focus the strip")
	}
}

// TestPlayStructureKeyboard: ↓ / enter in the focused strip jump, esc hands
// the keyboard back where it came from; the chord toggles through focus to
// hidden.
func TestPlayStructureKeyboard(t *testing.T) {
	m := openJQ(t, playApp(t, bigObject()))
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	m = setProgram(m, ".")
	m = drainKey(m, tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl | tea.ModAlt})
	if !m.playStructure || !m.play.stripFocus {
		t.Fatalf("ctrl+alt+g must show and focus the strip (status %q)", m.play.status)
	}
	if m.play.program.Text != "." {
		t.Fatalf("the chord leaked into the query line: %q", m.play.program.Text)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = drainKey(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.play.stripSel != 2 {
		t.Fatalf("selection = %d, want 2", m.play.stripSel)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if line, _ := m.play.resultEd.CursorPos(); line != m.play.outline[2].Line {
		t.Fatalf("enter put the caret on %d, want %d", line, m.play.outline[2].Line)
	}
	if m.play.stripFocus || !m.play.bufFocus {
		t.Fatal("enter hands the keyboard to the result")
	}
	// From the query line: esc returns there.
	m.play.setBufFocus(false)
	m = structureOn(m) // on, not focused: focus it
	if !m.play.stripFocus {
		t.Fatal("the toggle focuses a shown strip")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.play.stripFocus || m.play.bufFocus || !m.playOpen() {
		t.Fatal("esc returns to the query line without closing the playground")
	}
	m = structureOn(m)
	m = structureOn(m) // focused: hide
	if m.playStructure || m.play.stripFocus || m.playStripW(playResultWidth(m)) != 0 {
		t.Fatal("the toggle on a focused strip hides it")
	}
	if m.play.bufFocus {
		t.Error("hiding returns the keyboard to the query line it came from")
	}
}

// TestPlayStructureHides: a scalar result lists nothing and the strip gives
// the result its whole width; a pane below the threshold hides it too.
func TestPlayStructureHides(t *testing.T) {
	m := openJQ(t, playApp(t, bigObject()))
	m = setProgram(m, ".")
	m = structureOn(m)
	width := playResultWidth(m)
	m = setProgram(m, ".k00.a")
	if len(m.play.outline) != 0 || m.playStripW(width) != 0 {
		t.Fatalf("a scalar result must hide the strip: %+v", m.play.outline)
	}
	if m.play.stripFocus {
		t.Error("the strip cannot keep the keyboard with nothing to list")
	}
	if w := m.play.resultEd.Width(); w != width {
		t.Errorf("result editor is %d cells, want the pane's full %d", w, width)
	}
	m = setProgram(m, ".")
	if m.playStripW(width) == 0 {
		t.Fatal("the strip comes back with the structure")
	}
	if m.playStripW(playStripMinPane-1) != 0 {
		t.Error("a pane below the threshold hides the strip")
	}
}

// TestPlayStructureYQ: the yq dialect lists its mapping's keys the same way.
func TestPlayStructureYQ(t *testing.T) {
	m := openYQ(t, yqApp(t, "kind: Deployment\nspec:\n  replicas: 2\n"))
	m = setProgram(m, ".")
	if len(m.play.outline) != 2 || m.play.outline[1].Label != "spec" {
		t.Fatalf("yq outline = %+v", m.play.outline)
	}
}
