package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/ui"
)

// pushTestLayer stacks a transient shell titled heading over the open floats.
func pushTestLayer(m Model, heading string) *ui.Floating {
	f := ui.New(ui.Config{})
	f.SetContent(ui.ModelContent{Heading: heading, Body: func() string { return heading + " body" }})
	m.floats.Push(f)
	return f
}

// TestFloatingStackLayersOverShell guards #1237 end-to-end: a transient layer
// pushed over the open help shell renders on top of it in the frame, and esc
// dismisses one layer at a time — the pushed layer first, the base shell on
// the second press.
func TestFloatingStackLayersOverShell(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(ShowKeymapHelpMsg{})
	m = tm.(Model)
	if !m.shell.IsOpen() {
		t.Fatal("precondition: help shell open")
	}
	top := pushTestLayer(m, "TOPMOST")

	if m.floats.Depth() != 2 || m.floats.Top() != top {
		t.Fatal("pushed layer must be the topmost of two open layers")
	}
	frame := ansi.Strip(m.render())
	if !strings.Contains(frame, "TOPMOST") {
		t.Fatal("pushed layer missing from the frame")
	}
	if !strings.Contains(frame, "HELP") {
		t.Fatal("base help layer must stay visible under the pushed layer")
	}

	// First esc pops only the topmost layer; the base shell survives.
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if top.IsOpen() {
		t.Fatal("first esc must close the pushed layer")
	}
	if !m.shell.IsOpen() || m.floats.Top() != m.shell {
		t.Fatal("base shell must survive the first esc and become top again")
	}
	// Second esc closes the base shell.
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.shell.IsOpen() || m.floats.IsOpen() {
		t.Fatal("second esc must close the base shell")
	}
}

// TestFloatingStackOutsideClickPopsOneLayer guards the per-layer outside-click
// (#116 under #1237): a press outside the topmost layer pops only that layer;
// a press inside keeps it open. The base shell needs its own outside press.
func TestFloatingStackOutsideClickPopsOneLayer(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(ShowKeymapHelpMsg{})
	m = tm.(Model)
	top := pushTestLayer(m, "TOPMOST")

	press := func(x, y int) tea.MouseClickMsg {
		return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
	}
	// Dead center hits the (centered) topmost layer: everything stays open.
	tm, _ = m.Update(press(m.width/2, m.height/2))
	m = tm.(Model)
	if !top.IsOpen() || !m.shell.IsOpen() {
		t.Fatal("click inside the topmost layer must not close any layer")
	}
	// A corner press is outside the topmost layer: it pops only that layer.
	tm, _ = m.Update(press(0, m.height-1))
	m = tm.(Model)
	if top.IsOpen() {
		t.Fatal("outside click must pop the topmost layer")
	}
	if !m.shell.IsOpen() {
		t.Fatal("outside click must not reach the base layer")
	}
	// The next outside press closes the now-topmost base shell.
	tm, _ = m.Update(press(0, m.height-1))
	m = tm.(Model)
	if m.shell.IsOpen() {
		t.Fatal("second outside click must close the base shell")
	}
}
