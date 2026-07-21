package settings

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// fakeSub is a scriptable SubPanel for stack tests.
type fakeSub struct {
	title     string
	capturing bool
	buttons   []Button
	keys      []string
	msgs      []tea.Msg
}

func (f *fakeSub) Title() string     { return f.title }
func (f *fakeSub) Capturing() bool   { return f.capturing }
func (f *fakeSub) Buttons() []Button { return f.buttons }
func (f *fakeSub) View(w, h int) string {
	return "content of " + f.title
}
func (f *fakeSub) Update(key tea.KeyPressMsg) tea.Cmd {
	f.keys = append(f.keys, key.String())
	return nil
}
func (f *fakeSub) Receive(msg tea.Msg) { f.msgs = append(f.msgs, msg) }

func subModel(t *testing.T) *Model {
	t.Helper()
	restoreConfig(t)
	m := New(BasePages([]string{"default"}), testOpts(t))
	m.SetSize(90, 28)
	m.Open()
	return m
}

// TestSubPanelStackEscPopsOneLevel guards #883: esc pops exactly one level;
// the panel closes only from the base level.
func TestSubPanelStackEscPopsOneLevel(t *testing.T) {
	m := subModel(t)
	a, b := &fakeSub{title: "One"}, &fakeSub{title: "Two"}
	m.Push(a)
	m.Push(b)
	if !m.SubOpen() {
		t.Fatal("stack must be open")
	}
	m.Update(key("esc"))
	if m.topSub() != a {
		t.Fatal("esc must pop only the top level")
	}
	m.Update(key("esc"))
	if m.SubOpen() {
		t.Fatal("second esc must pop the last level")
	}
	if !m.IsOpen() {
		t.Fatal("popping the stack must not close the panel")
	}
}

// TestSubPanelBreadcrumb: the sub-panel box carries the full trail.
func TestSubPanelBreadcrumb(t *testing.T) {
	m := subModel(t)
	m.Push(&fakeSub{title: "New Environment"})
	v := m.View()
	if !strings.Contains(v, "Settings › Editor › New Environment") {
		t.Fatalf("breadcrumb missing, view:\n%s", v)
	}
	if !strings.Contains(v, "content of New Environment") {
		t.Fatal("sub-panel content must render")
	}
}

// TestSubPanelButtonKeyAndClick: a button triggers by its key and by a press
// on its cells; disabled buttons stay inert.
func TestSubPanelButtonKeyAndClick(t *testing.T) {
	m := subModel(t)
	ran, dead := 0, 0
	sub := &fakeSub{title: "Pick"}
	sub.buttons = []Button{
		{Label: "Next", Key: "enter", Do: func() tea.Cmd { ran++; return nil }},
		{Label: "Off", Disabled: true, Do: func() tea.Cmd { dead++; return nil }},
	}
	m.Push(sub)
	m.Update(key("enter"))
	if ran != 1 {
		t.Fatalf("button key must run the action, ran=%d", ran)
	}
	// Click the button row: first button spans from x=1 within the row.
	x0, y0, _, h := m.subRect()
	rowY := y0 + 1 + 1 + subContentHeight(h) // border + breadcrumb + content
	m.Click(x0+2, rowY)
	if ran != 2 {
		t.Fatalf("button click must run the action, ran=%d", ran)
	}
	spans := buttonSpans(sub.buttons)
	m.Click(x0+1+spans[1].start, rowY)
	if dead != 0 {
		t.Fatal("disabled button must stay inert")
	}
}

// TestSubPanelCapturingOwnsKeys: a capturing sub-panel sees esc and button
// keys verbatim.
func TestSubPanelCapturingOwnsKeys(t *testing.T) {
	m := subModel(t)
	sub := &fakeSub{title: "Form", capturing: true,
		buttons: []Button{{Label: "Save", Key: "enter", Do: func() tea.Cmd { t.Fatal("stack must not run buttons while capturing"); return nil }}}}
	m.Push(sub)
	m.Update(key("esc"))
	m.Update(key("enter"))
	if len(sub.keys) != 2 || sub.keys[0] != "esc" || sub.keys[1] != "enter" {
		t.Fatalf("capturing panel keys = %v", sub.keys)
	}
	if !m.SubOpen() {
		t.Fatal("stack must not pop a capturing panel on esc")
	}
	if !m.Capturing() {
		t.Fatal("panel must report capturing for the host (resize chords)")
	}
}

// TestSubPanelDeliverReachesStack: async messages reach open sub-panels.
func TestSubPanelDeliverReachesStack(t *testing.T) {
	m := subModel(t)
	sub := &fakeSub{title: "Waiter"}
	m.Push(sub)
	type probeMsg struct{}
	m.Deliver(probeMsg{})
	if len(sub.msgs) != 1 {
		t.Fatalf("sub-panel must receive delivered msgs, got %v", sub.msgs)
	}
}

// TestSubPanelContentClickRoutesLocal: presses inside the content area arrive
// content-local at a SubPanelClicker.
type clickSub struct {
	fakeSub
	gotX, gotY int
}

func (c *clickSub) Click(x, y int) tea.Cmd { c.gotX, c.gotY = x, y; return nil }

func TestSubPanelContentClickRoutesLocal(t *testing.T) {
	m := subModel(t)
	sub := &clickSub{fakeSub: fakeSub{title: "Clicky"}}
	m.Push(sub)
	x0, y0, _, _ := m.subRect()
	m.Click(x0+1+4, y0+1+1+2) // 4 cells in, 2 content rows down
	if sub.gotX != 4 || sub.gotY != 2 {
		t.Fatalf("content click = (%d,%d), want (4,2)", sub.gotX, sub.gotY)
	}
	// A press outside the box is swallowed, not a close.
	m.Click(0, 0)
	if !m.SubOpen() || !m.IsOpen() {
		t.Fatal("outside press must not close anything")
	}
}
