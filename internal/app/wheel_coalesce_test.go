package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// wheel_coalesce_test.go covers mouse-wheel event coalescing (#238): a burst of
// queued wheel events accumulates in the model and applies in a single update
// pass, so the UI reacts once instead of replaying every stale event.

// wheelApp opens one editor with enough lines to scroll and returns the model
// plus a screen cell inside the editor's content area (below the tab bar).
func wheelApp(t *testing.T) (Model, int, int) {
	t.Helper()
	dir := t.TempDir()
	p := writeTemp(t, dir, "long.txt", strings.Repeat("line\n", 200))
	m := openApp(t, p)
	r, ok := m.lay.Panes[m.activeWS().Panes.Focused()]
	if !ok {
		t.Fatal("setup: focused pane has no rect")
	}
	return m, r.X + paneContentX, r.Y + paneContentY + 2
}

// raw feeds a message through Update without the step helper's implicit wheel
// flush, so pending state stays observable.
func raw(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	tm, cmd := m.Update(msg)
	return tm.(Model), cmd
}

func TestWheelBurstCoalescesIntoOneFlush(t *testing.T) {
	m, x, y := wheelApp(t)
	ed := m.activeWS().Panes.FocusedInstance().Editor()

	var cmds [3]tea.Cmd
	for i := range cmds {
		m, cmds[i] = raw(t, m, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	}
	if top, _ := ed.ScrollOffset(); top != 0 {
		t.Fatalf("wheel events must not apply before the flush, top=%d", top)
	}
	if len(m.pendingWheel) != 1 || m.pendingWheel[0].count != 3 {
		t.Fatalf("identical events must merge into one batch, got %+v", m.pendingWheel)
	}
	if cmds[0] == nil {
		t.Fatal("the first wheel event must schedule a flush")
	}
	if cmds[1] != nil || cmds[2] != nil {
		t.Fatal("follow-up wheel events must not schedule extra flushes")
	}

	m, _ = raw(t, m, wheelFlushMsg{})
	if top, _ := ed.ScrollOffset(); top != 3*wheelLines {
		t.Fatalf("flush must apply the whole batch at once, top=%d want %d", top, 3*wheelLines)
	}
	if len(m.pendingWheel) != 0 || m.wheelFlushQueued {
		t.Fatal("flush must clear the pending state")
	}
}

func TestWheelDirectionChangeKeepsOrder(t *testing.T) {
	m, x, y := wheelApp(t)
	ed := m.activeWS().Panes.FocusedInstance().Editor()

	for i := 0; i < 3; i++ {
		m, _ = raw(t, m, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	}
	m, _ = raw(t, m, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})
	if len(m.pendingWheel) != 2 {
		t.Fatalf("a direction change must start a new batch, got %+v", m.pendingWheel)
	}
	m, _ = raw(t, m, wheelFlushMsg{})
	if top, _ := ed.ScrollOffset(); top != 2*wheelLines {
		t.Fatalf("flush must replay batches in order, top=%d want %d", top, 2*wheelLines)
	}
}

func TestNonWheelEventFlushesPendingWheel(t *testing.T) {
	m, x, y := wheelApp(t)
	ed := m.activeWS().Panes.FocusedInstance().Editor()

	m, _ = raw(t, m, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	// A click arriving behind the wheel burst must see the scrolled viewport:
	// the pending batch flushes before the click is handled. Click far enough
	// from the top edge that the cursor lands outside the scroll margin and
	// the viewport stays put.
	m, _ = raw(t, m, tea.MouseClickMsg{X: x, Y: y + 6, Button: tea.MouseLeft})
	if top, _ := ed.ScrollOffset(); top != wheelLines {
		t.Fatalf("a non-wheel event must flush pending wheels first, top=%d want %d", top, wheelLines)
	}
	if len(m.pendingWheel) != 0 {
		t.Fatal("the inline flush must clear the pending state")
	}

	// The originally scheduled flush arrives later and must be a no-op.
	m, _ = raw(t, m, wheelFlushMsg{})
	if top, _ := ed.ScrollOffset(); top != wheelLines {
		t.Fatalf("a stale flush must not scroll again, top=%d", top)
	}
	_ = m
}
