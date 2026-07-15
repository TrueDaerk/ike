package app

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
)

// inputcoalesce.go keeps a burst of mouse events from starving keystrokes (#602).
//
// bubbletea's event loop reads one message at a time from an unbuffered channel
// and runs Update + a full View render for every one, with no lookahead. So a
// scroll/drag burst — one MouseWheelMsg or MouseMotionMsg per notch/cell — makes
// a key typed right after wait behind dozens of Update+render passes. The IDE is
// "not missing events" but feels frozen.
//
// The coalescer is a tea.WithFilter hook: it absorbs wheel/motion events and
// returns nil, which makes bubbletea skip both Update and the render for them, so
// the queue drains at channel speed and a following key is reached at once. A
// short timer re-injects the folded events as one coalescedInputMsg, applied in a
// single pass (one render), preserving net scroll distance. Every other message —
// keys, mouse press/release/click, resize, paste — passes straight through.

// coalesceInterval bounds how long wheel/motion events accumulate before the
// batch is re-injected: about one 60fps frame — long enough to fold a burst,
// short enough that scrolling still tracks the wheel.
const coalesceInterval = 16 * time.Millisecond

// coalescedInputMsg carries mouse events folded from a burst. Update replays the
// wheel notches in order and then the latest motion, so the whole burst costs one
// render instead of one per event.
type coalescedInputMsg struct {
	wheels []tea.MouseWheelMsg
	motion *tea.MouseMotionMsg
}

// MouseCoalescer holds the accumulator shared between the filter (called on the
// event-loop goroutine) and the flush timer. It is safe for concurrent use.
type MouseCoalescer struct {
	mu         sync.Mutex
	send       func(tea.Msg)
	wheels     []tea.MouseWheelMsg
	motion     tea.MouseMotionMsg
	haveMotion bool
	armed      bool
}

// NewMouseCoalescer returns a coalescer with no sender yet; wire the program's
// Send with SetSender once the program exists.
func NewMouseCoalescer() *MouseCoalescer { return &MouseCoalescer{} }

// SetSender wires the program's Send so the flush timer can re-inject the folded
// batch. Until it is set the filter still absorbs events but cannot flush.
func (c *MouseCoalescer) SetSender(send func(tea.Msg)) {
	c.mu.Lock()
	c.send = send
	c.mu.Unlock()
}

// Filter is the tea.WithFilter hook. Wheel and motion events are absorbed (return
// nil → bubbletea skips Update+render for them); everything else passes through
// untouched, so keys and clicks are never delayed or dropped.
func (c *MouseCoalescer) Filter(_ tea.Model, msg tea.Msg) tea.Msg {
	switch m := msg.(type) {
	case tea.MouseWheelMsg:
		c.absorb(func() { c.wheels = append(c.wheels, m) })
		return nil
	case tea.MouseMotionMsg:
		c.absorb(func() { c.motion, c.haveMotion = m, true })
		return nil
	default:
		return msg
	}
}

// absorb records an event under the lock and arms the flush timer.
func (c *MouseCoalescer) absorb(record func()) {
	c.mu.Lock()
	record()
	if !c.armed && c.send != nil {
		c.armed = true
		time.AfterFunc(coalesceInterval, c.flush)
	}
	c.mu.Unlock()
}

// flush drains the accumulator and re-injects it as one coalescedInputMsg. A
// drain that finds nothing (already emptied) is a no-op.
func (c *MouseCoalescer) flush() {
	c.mu.Lock()
	wheels := c.wheels
	c.wheels = nil
	var motion *tea.MouseMotionMsg
	if c.haveMotion {
		m := c.motion
		motion = &m
		c.haveMotion = false
	}
	c.armed = false
	send := c.send
	c.mu.Unlock()
	if send == nil || (len(wheels) == 0 && motion == nil) {
		return
	}
	send(coalescedInputMsg{wheels: wheels, motion: motion})
}

// overlayCapturesKeyboard reports whether a modal overlay or prompt owns the
// keyboard right now — mirroring the guard chain in the KeyPressMsg handler. When
// true, a paste is not routed into the (hidden) editor/terminal below.
func (m Model) overlayCapturesKeyboard() bool {
	return m.settings.IsOpen() || (m.menuEnabled() && m.menu.IsOpen()) ||
		m.finder.IsOpen() || m.todo.IsOpen() || m.undoTree.IsOpen() ||
		m.commitUI.IsOpen() || m.callhier.IsOpen() || m.palette.IsOpen() ||
		m.shell.IsOpen() || m.recoveryOpen() || m.onboardingOpen() ||
		m.conflictOpen() || m.revertPromptOpen() || m.depEditPromptOpen() ||
		m.switchPromptOpen() || m.closePromptOpen() || m.renameOpen() ||
		m.lspRenameOpen() || m.explorerCapturing()
}

// handlePaste routes a bracketed-paste block (#603) to the focused editable
// surface: a terminal pane through its bracketed-paste path, otherwise the
// focused editor as a single block insert. It is a no-op while a modal overlay
// owns the keyboard (the paste would otherwise land in a hidden buffer).
func (m Model) handlePaste(text string) (tea.Model, tea.Cmd) {
	if text == "" || m.overlayCapturesKeyboard() {
		return m, nil
	}
	if m.terminalFocused() {
		if inst := m.panes.FocusedInstance(); inst != nil {
			if term := inst.ActiveTerminal(); term != nil {
				term.PasteText(text)
			}
		}
		return m, nil
	}
	inst := m.panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindEditor {
		return m, nil
	}
	ed := inst.Editor()
	if ed == nil {
		return m, nil
	}
	ed.PasteText(text)
	return m, nil
}

// applyCoalescedInput replays a folded mouse burst in a single Update pass: the
// wheel notches in arrival order, then the latest motion. One render covers the
// whole burst.
func (m Model) applyCoalescedInput(msg coalescedInputMsg) (tea.Model, tea.Cmd) {
	var tm tea.Model = m
	var cmds []tea.Cmd
	for _, w := range msg.wheels {
		mm, ok := tm.(Model)
		if !ok {
			return tm, tea.Batch(cmds...)
		}
		var cmd tea.Cmd
		tm, cmd = mm.handleMouse(mouseEvent{Mouse: w.Mouse(), action: mouseWheel})
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if msg.motion != nil {
		if mm, ok := tm.(Model); ok {
			var cmd tea.Cmd
			tm, cmd = mm.handleMouse(mouseEvent{Mouse: msg.motion.Mouse(), action: mouseMotion})
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	return tm, tea.Batch(cmds...)
}
