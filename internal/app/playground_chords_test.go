package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/telemetry"
)

// playground_chords_test.go covers the query line's missing chords (#2633):
// cmd+a selects the whole program, ctrl+z undoes the last edit, and a Global
// leader sequence (cmd+k …) runs from the playground exactly as it does with
// an editor focused. Local telemetry had all three recorded as `unbound` —
// the mode owns the keyboard, so nothing downstream could claim them.

// playReady opens the playground over a JSON buffer with the first-start
// dialog out of the way: it eats scripted keys on hosts with no language
// server installed.
func playReady(t *testing.T, body string) Model {
	t.Helper()
	m := openJQ(t, playApp(t, body))
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	return m
}

// cmdKey builds a Command-modified press (the super spelling; EditKey and the
// keymap layer accept meta and super alike).
func cmdKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: tea.ModSuper}
}

// typeKey builds a plain printable press, the shape the query line inserts.
func typeKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// TestPlaygroundSelectAllReplacesQuery: cmd+a selects the whole program, so
// the next typed rune replaces it instead of appending to it.
func TestPlaygroundSelectAllReplacesQuery(t *testing.T) {
	m := playReady(t, `{"foo":1}`)
	m.play.program.Set(".foo")

	m = drainKey(m, cmdKey('a'))
	if !m.play.program.Selected() {
		t.Fatal("cmd+a must select the whole query")
	}
	if m.play.program.Text != ".foo" {
		t.Fatalf("cmd+a must not change the query, got %q", m.play.program.Text)
	}
	m = drainKey(m, typeKey('.'))
	if got := m.play.program.Text; got != "." {
		t.Fatalf("typing over the selection must replace the query, got %q", got)
	}
	if m.play.program.Selected() {
		t.Fatal("the replacement consumes the selection")
	}
}

// TestPlaygroundSelectAllPaintsTheQuery: the selection is visible — the
// highlighting steps aside for the reverse-video the next keystroke acts on.
func TestPlaygroundSelectAllPaintsTheQuery(t *testing.T) {
	m := playReady(t, `{"foo":1}`)
	m.play.program.Set(".foo")
	plain := m.playQueryRow(80)
	m = drainKey(m, cmdKey('a'))
	if sel := m.playQueryRow(80); sel == plain {
		t.Fatalf("an armed select-all must render differently, got %q", sel)
	}
}

// TestPlaygroundUndoRestoresPreviousQuery: ctrl+z walks the query back to
// what it was before the last edit — a run of typing counts as one edit, so
// one ctrl+z is not one rune.
func TestPlaygroundUndoRestoresPreviousQuery(t *testing.T) {
	m := playReady(t, `{"foo":1}`)
	m.play.program.Set(".foo")

	m = drainKey(m, typeKey('['))
	m = drainKey(m, typeKey(']'))
	if got := m.play.program.Text; got != ".foo[]" {
		t.Fatalf("typing must extend the query, got %q", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if got := m.play.program.Text; got != ".foo" {
		t.Fatalf("ctrl+z must restore the query before the typed run, got %q", got)
	}
}

// TestPlaygroundUndoAlsoOnCmdZ: the macOS spelling undoes too — the editor
// binds both, and a field the mode owns must not be the one place where only
// one of them works.
func TestPlaygroundUndoAlsoOnCmdZ(t *testing.T) {
	m := playReady(t, `{"foo":1}`)
	m.play.program.Set(".foo")
	m = drainKey(m, typeKey('x'))
	m = drainKey(m, cmdKey('z'))
	if got := m.play.program.Text; got != ".foo" {
		t.Fatalf("cmd+z must undo the last edit, got %q", got)
	}
}

// TestPlaygroundLeaderChordFromQueryLine is the issue's core case: cmd+k holds
// as a prefix from the query line — not typed into the program, not recorded
// unbound — and the continuation completes the Global sequence.
func TestPlaygroundLeaderChordFromQueryLine(t *testing.T) {
	m := playReady(t, `{"foo":1}`)
	m.play.program.Set(".foo")

	out, cmd := m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModMeta})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("cmd+k must arm the resolver timeout from the query line")
	}
	if !m.keys.Pending() || !m.playChordPending() {
		t.Fatal("cmd+k must leave a partial chord pending, not be swallowed")
	}
	if m.play.program.Text != ".foo" {
		t.Fatalf("the leader must not be typed into the query, got %q", m.play.program.Text)
	}
	// cmd+k z is pane.maximize: the continuation is a plain rune the query
	// line would otherwise insert.
	m = drainKey(m, typeKey('z'))
	if m.keys.Pending() {
		t.Fatal("the continuation must complete the sequence")
	}
	if m.play.program.Text != ".foo" {
		t.Fatalf("the continuation must not be typed into the query, got %q", m.play.program.Text)
	}
	if len(m.lay.Panes) != 1 {
		t.Fatalf("cmd+k z must maximize the pane from the playground, got %v", m.lay.Panes)
	}
	if !m.playOpen() {
		t.Fatal("the sequence must not close the playground")
	}
}

// TestPlaygroundLeaderChordFromResultBuffer: the same from the other focus —
// the continuation is a plain key the read-only buffer would take as a motion.
func TestPlaygroundLeaderChordFromResultBuffer(t *testing.T) {
	m := playReady(t, `{"foo":1}`)
	m.play.setBufFocus(true)

	out, _ := m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModMeta})
	m = out.(Model)
	if !m.playChordPending() {
		t.Fatal("cmd+k must pend from the result buffer too")
	}
	m = drainKey(m, typeKey('z'))
	if len(m.lay.Panes) != 1 {
		t.Fatalf("cmd+k z must maximize the pane from the result buffer, got %v", m.lay.Panes)
	}
}

// TestPlaygroundLeaderEscCancels: esc ends a held sequence instead of doubling
// as the playground's own esc, which would close the mode under the user.
func TestPlaygroundLeaderEscCancels(t *testing.T) {
	m := playReady(t, `{"foo":1}`)
	out, _ := m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModMeta})
	m = out.(Model)
	if !m.playChordPending() {
		t.Fatal("cmd+k must pend")
	}
	m = drainKey(m, escKey())
	if m.keys.Pending() {
		t.Fatal("esc must abandon the held sequence")
	}
	if !m.playOpen() {
		t.Fatal("the cancelling esc must not also leave the playground")
	}
}

// TestPlaygroundTerminalPopupChordFromQueryLine: the single-step Global chord
// the telemetry shows the user falling back to (cmd+alt+t) opens the popup
// terminal straight from the query line.
func TestPlaygroundTerminalPopupChordFromQueryLine(t *testing.T) {
	m := playReady(t, `{"foo":1}`)
	m.play.program.Set(".foo")
	m = drainKey(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModMeta | tea.ModAlt})
	if m.play.program.Text != ".foo" {
		t.Fatalf("the chord must not type into the query, got %q", m.play.program.Text)
	}
	if !m.popup.open {
		t.Fatal("terminal.popup must open from the playground query line")
	}
}

// TestPlaygroundChordsNotRecordedUnbound is the telemetry half of the issue
// (#2539's recorder): none of the three chords lands in the usage log as an
// unbound key any more — each is consumed by something that means it.
func TestPlaygroundChordsNotRecordedUnbound(t *testing.T) {
	m := playReady(t, `{"foo":1}`)
	m.play.program.Set(".foo")

	m = drainKey(m, cmdKey('a'))
	m = drainKey(m, tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	out, _ := m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModMeta})
	m = out.(Model)
	m = drainKey(m, typeKey('z'))

	var unbound []string
	for _, ev := range eventsOf(usageEvents(t, m), telemetry.TypeKey) {
		if ev.Data["status"] == "unbound" {
			unbound = append(unbound, ev.Data["chord"]+"/"+ev.Data["context"])
		}
	}
	if len(unbound) != 0 {
		t.Fatalf("the playground's own chords must not be recorded unbound, got %v", unbound)
	}
}
