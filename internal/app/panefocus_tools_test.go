package app

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
)

// panefocus_tools_test.go guards the focus-by-number chords against the tool
// windows (#2493): the numbers in the title bars promise that ctrl+N goes
// there from wherever the keyboard currently is, and a tool pane must not be
// able to eat the chord — least of all the terminal, which used to forward it
// to the shell so it died silently (#275).

// toolWindowKind is one focusable tool window in the table below: the pane
// kind it registers as, and how a test adds one to the layout.
type toolWindowKind struct {
	kind pane.Kind
	add  func(m *Model) string
}

// toolWindowKinds lists the tool windows the focus chord must escape from. The
// terminal is missing on purpose — it is opened through its own message so a
// real session backs it — and covered by its own case in the table test.
func toolWindowKinds() map[string]toolWindowKind {
	return map[string]toolWindowKind{
		"vcs":         {pane.KindVCS, func(m *Model) string { return m.activeWS().Panes.AddVCS() }},
		"debug":       {pane.KindDebug, func(m *Model) string { return m.activeWS().Panes.AddDebug() }},
		"problems":    {pane.KindProblems, func(m *Model) string { return m.activeWS().Panes.AddProblems() }},
		"structure":   {pane.KindStructure, func(m *Model) string { return m.activeWS().Panes.AddStructure() }},
		"usages":      {pane.KindUsages, func(m *Model) string { return m.activeWS().Panes.AddUsages() }},
		"http":        {pane.KindHTTP, func(m *Model) string { return m.activeWS().Panes.AddHTTP() }},
		"breakpoints": {pane.KindBreakpoints, func(m *Model) string { return m.activeWS().Panes.AddBreakpoints() }},
		"tests":       {pane.KindTests, func(m *Model) string { return m.activeWS().Panes.AddTests() }},
		"issues":      {pane.KindIssues, func(m *Model) string { return m.activeWS().Panes.AddIssues() }},
		"dom":         {pane.KindDOM, func(m *Model) string { return m.activeWS().Panes.AddDOM() }},
		"xdoctor":     {pane.KindDoctor, func(m *Model) string { return m.activeWS().Panes.AddDoctor() }},
		"lspdoctor":   {pane.KindLSPDoctor, func(m *Model) string { return m.activeWS().Panes.AddLSPDoctor() }},
		"deps":        {pane.KindDeps, func(m *Model) string { return m.activeWS().Panes.AddDeps() }},
		"time":        {pane.KindTime, func(m *Model) string { return m.activeWS().Panes.AddTime() }},
	}
}

// focusChordKey is the chord these tests bind pane.focus<n> to. The ctrl+digit
// defaults ship on macOS only (#2407) and the bug under test is the routing,
// not the spelling — so the table binds a platform-neutral chord and runs
// everywhere; TestPaneFocusChordFocusesThatPane guards the defaults.
var focusChordKey = tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}

// toolWindowApp is a sized app whose focus chord is bound to pane.focus<n>.
func toolWindowApp(t *testing.T, n int) Model {
	t.Helper()
	return numberedApp(t, host.MapConfig{"keymap.bindings.ctrl+y": "pane.focus" + strconv.Itoa(n)})
}

// focusToolWindow adds tw's pane below the editor, focuses it and re-layouts,
// returning the new pane's key.
func focusToolWindow(t *testing.T, m *Model, tw toolWindowKind) string {
	t.Helper()
	target := m.activeEditorKey()
	if target == "" {
		t.Fatal("precondition: the default layout should include an editor pane")
	}
	key := tw.add(m)
	if !m.insertToolPane(key, target, layout.ZoneBottom) {
		t.Fatalf("could not insert the %s tool window", key)
	}
	m.setFocus(key)
	m.layout()
	if inst := m.activeWS().Panes.FocusedInstance(); inst == nil || inst.Kind() != tw.kind {
		t.Fatalf("precondition: %s should be the focused tool window", key)
	}
	return key
}

// TestPaneFocusChordFromEveryToolWindow (#2493): the chord reaches the numbered
// pane from every tool window, terminal included.
func TestPaneFocusChordFromEveryToolWindow(t *testing.T) {
	for name, tw := range toolWindowKinds() {
		t.Run(name, func(t *testing.T) {
			m := toolWindowApp(t, 1)
			key := focusToolWindow(t, &m, tw)
			order := m.paneNumberOrder()
			if len(order) < 2 || order[0] == key {
				t.Fatalf("precondition: %v should number another pane 1", order)
			}
			m = drainKey(m, focusChordKey)
			if got := m.activeWS().Panes.Focused(); got != order[0] {
				t.Errorf("the focus chord from %s focused %s, want pane 1 (%s)", key, got, order[0])
			}
		})
	}

	// The terminal takes every key raw for the shell (#805) — the layer that
	// swallowed the chord until #2493.
	t.Run("terminal", func(t *testing.T) {
		m := toolWindowApp(t, 1)
		tm, _ := m.Update(TerminalNewMsg{})
		m = tm.(Model)
		key := m.activeWS().Panes.Focused()
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindTerminal {
			t.Fatalf("terminal.new should focus a terminal pane, got %q", key)
		}
		t.Cleanup(inst.CloseTerminalTabs)
		if !m.terminalFocused() {
			t.Fatal("precondition: the terminal should hold the keyboard")
		}
		m.layout()
		order := m.paneNumberOrder()
		if len(order) < 2 || order[0] == key {
			t.Fatalf("precondition: %v should number another pane 1", order)
		}
		m = drainKey(m, focusChordKey)
		if got := m.activeWS().Panes.Focused(); got != order[0] {
			t.Errorf("the focus chord from the terminal focused %s, want pane 1 (%s)", got, order[0])
		}
	})
}

// TestPaneFocusChordFromPopupTerminal (#2493): the popup terminal layer takes
// the chord like a terminal pane, and answering it means handing the keyboard
// down — the layer blurs (#2309) and stays on screen, exactly like clicking
// into the pane below it.
func TestPaneFocusChordFromPopupTerminal(t *testing.T) {
	m := openTestPopupWith(t, toolWindowApp(t, 1))
	if !m.popupLayerFocused() {
		t.Fatal("precondition: the popup layer should own the keyboard")
	}
	order := m.paneNumberOrder()
	if len(order) == 0 {
		t.Fatal("precondition: the layout should number at least one pane")
	}
	m = drainKey(m, focusChordKey)
	if got := m.activeWS().Panes.Focused(); got != order[0] {
		t.Errorf("the focus chord from the popup focused %s, want pane 1 (%s)", got, order[0])
	}
	if m.popupLayerFocused() {
		t.Error("the popup must hand the keyboard down after focusing a pane")
	}
	if !m.popup.open {
		t.Error("the popup must stay on screen, blurred rather than hidden")
	}
}

// TestToolWindowKindsAreComplete keeps that table honest: every kind isToolKind
// counts as a tool window — plus the LSP doctor, which that helper predates —
// is exercised by it. The scan runs well past the last kind, so a kind appended
// to internal/pane is caught without editing a bound here.
func TestToolWindowKindsAreComplete(t *testing.T) {
	covered := map[pane.Kind]bool{
		// The explorer is the chord's *target* in the table rather than a case
		// of its own; the terminal has its hand-built case there.
		pane.KindExplorer: true,
		pane.KindTerminal: true,
	}
	for _, tw := range toolWindowKinds() {
		covered[tw.kind] = true
	}
	const kindScan = 64 // far past the last pane.Kind; the kinds are a dense iota block
	for k := pane.KindExplorer; k < pane.KindExplorer+kindScan; k++ {
		if !isToolKind(k) || covered[k] {
			continue
		}
		t.Errorf("tool window kind %d is not covered by the focus-chord table (#2493)", int(k))
	}
	// isToolKind predates the LSP doctor pane (#2164) and does not list it, so
	// the scan above cannot demand it — this does.
	if !covered[pane.KindLSPDoctor] {
		t.Error("the LSP doctor tool window must stay in the focus-chord table")
	}
}

// TestPaneFocusChordOutOfRangeFromToolWindow (#2493, #275): a chord addressing
// a pane that is not open says so from a tool window too, rather than dying
// silently because a tool holds the keyboard.
func TestPaneFocusChordOutOfRangeFromToolWindow(t *testing.T) {
	m := numberedApp(t, host.MapConfig{
		"keymap.bindings.ctrl+y": "pane.focus" + strconv.Itoa(paneNumberMax),
		// drainKey runs the toast's expiry tick inline, so the assertion reads
		// the history ring rather than the live stack; the short timeout keeps
		// that tick from costing the suite four seconds.
		"notifications.timeout_seconds": "1",
	})
	key := focusToolWindow(t, &m, toolWindowKinds()["problems"])
	m = drainKey(m, focusChordKey)
	if got := m.activeWS().Panes.Focused(); got != key {
		t.Errorf("an out-of-range number moved focus to %s", got)
	}
	found := false
	for _, h := range m.history {
		if strings.Contains(h.text, "focus pane") {
			found = true
		}
	}
	if !found {
		t.Errorf("an out-of-range chord must notify, history = %v", m.history)
	}
}
