package app

// copy_states_test.go covers the app-level half of the copy-consistency audit
// of #2062: chords the shell claims before pane dispatch must still reach a
// live selection, in every focus state a selection can survive into.

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/httppane"
	"ike/internal/pane"
)

// selectInHTTPPane focuses the response pane and drags a selection over its
// first body row, returning the model and the pane.
func selectInHTTPPane(t *testing.T, m Model) (Model, *httppane.Model) {
	t.Helper()
	m.setFocus(pane.HTTPKey)
	m.layout()
	r, ok := m.lay.Panes[pane.HTTPKey]
	if !ok {
		t.Fatal("the response pane must be laid out")
	}
	p := m.httpPanel()
	if p == nil {
		t.Fatal("no response pane")
	}
	row := -1
	for i := 0; i < p.Rows(); i++ {
		if p.RowText(i) == "{" {
			row = i
		}
	}
	if row < 0 {
		t.Fatal("no body row found")
	}
	x := r.X + paneContentX + 1
	y := r.Y + paneContentY + row + 1
	m = step(m, press(x, y))
	m = step(m, motion(x+4, y+1))
	m = step(m, release(x+4, y+1))
	if !p.HasSelection() {
		t.Fatal("the drag must select in the response pane")
	}
	return m, p
}

// TestHTTPPaneCtrlCCopiesInsteadOfQuitting: the response pane advertises
// ctrl+c as its copy key (#1266), but the shell's quit binding used to eat it
// first — on a platform without a Cmd key that left the pane no copy chord at
// all, and pressing it quit the IDE over a live selection.
func TestHTTPPaneCtrlCCopiesInsteadOfQuitting(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m, p := selectInHTTPPane(t, out.(Model))

	out, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("ctrl+c over a selection must copy")
	}
	msg := cmd()
	if _, quit := msg.(tea.QuitMsg); quit {
		t.Fatal("ctrl+c over a selection must not quit the IDE")
	}
	copyMsg, ok := msg.(httppane.CopyMsg)
	if !ok {
		t.Fatalf("message type: %T", msg)
	}
	if copyMsg.What != "selection" || copyMsg.Text == "" {
		t.Errorf("copy message: %+v", copyMsg)
	}
	if p.HasSelection() {
		t.Error("copying must clear the selection")
	}

	// With the selection gone the chord is the global quit again — the guard
	// is scoped to a live selection, it does not take the key away.
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c without a selection must still act")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Errorf("ctrl+c without a selection must quit, got %T", cmd())
	}
}

// TestTerminalCopyChordWhileSearching pins the audited-ok half of #2062: the
// terminal's scrollback search consumes every key it sees (#1169), so the copy
// chord only survives because the app reserves it ahead of pane dispatch. A
// refactor that moved the reservation behind routeKey would silently recreate
// #2051 here.
func TestTerminalCopyChordWhileSearching(t *testing.T) {
	var copied string
	orig := clipboardWrite
	clipboardWrite = func(text string) { copied = text }
	t.Cleanup(func() { clipboardWrite = orig })

	m, key := openTestTerminal(t)
	term := m.activeWS().Panes.Get(key).Terminal()
	time.Sleep(200 * time.Millisecond) // prompt on the grid

	term.MousePress(0, 0)
	term.MouseDrag(5, 0)
	term.MouseRelease(5, 0)
	if !term.HasSelection() {
		t.Fatal("the drag must select")
	}
	// cmd+f opens the search from the live view (#1504).
	out, _ := m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModSuper})
	m = out.(Model)
	if !term.Searching() {
		t.Fatal("cmd+f must open the scrollback search")
	}

	out, _ = m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModSuper})
	m = out.(Model)
	if copied == "" {
		t.Fatal("cmd+c must copy the selection while the search is open")
	}
	if term.HasSelection() {
		t.Error("copying must clear the selection")
	}
	if !term.Searching() {
		t.Error("copying must not close the search field")
	}
}

// TestPlaygroundCopyChordFromQueryLine: a selection dragged in the result
// buffer stays on screen when the focus returns to the query line, so the
// copy chord must reach it there too instead of dying in the query input.
func TestPlaygroundCopyChordFromQueryLine(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":[1,2,3]}`))
	m = setProgram(m, ".foo[]")
	clip := &playTestClip{}
	m.play.resultEd.SetClipboard(clip)

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = drainKey(m, tea.KeyPressMsg{Code: 'V', Text: "V"})
	m = drainKey(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab}) // back to the query line
	if m.play.bufFocus {
		t.Fatal("tab must return the focus to the query line")
	}
	if _, has := m.play.resultEd.SelectionText(); !has {
		t.Fatal("the selection must survive the focus change")
	}

	m = drainKey(m, playCopyKey())
	if !strings.Contains(clip.text, "1") || !strings.Contains(clip.text, "2") {
		t.Fatalf("clipboard = %q, want the result buffer's selection", clip.text)
	}
	if strings.Contains(clip.text, "3") {
		t.Errorf("clipboard = %q, must hold only the selection", clip.text)
	}
	if !m.playOpen() {
		t.Error("copying must leave the playground open")
	}
	if m.play.bufFocus {
		t.Error("copying must not move the focus into the result buffer")
	}
	// The query line is untouched: the chord was reserved, not typed.
	if got := m.play.program; got != ".foo[]" {
		t.Errorf("query = %q, want %q", got, ".foo[]")
	}
}

// TestPlaygroundCopyChordWithoutSelectionLeavesQueryLine: without a selection
// the chord falls through to the query line exactly as before.
func TestPlaygroundCopyChordWithoutSelectionLeavesQueryLine(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":[1,2,3]}`))
	m = setProgram(m, ".foo[]")
	clip := &playTestClip{}
	m.play.resultEd.SetClipboard(clip)

	m = drainKey(m, playCopyKey())
	if clip.text != "" {
		t.Errorf("clipboard = %q, want nothing copied", clip.text)
	}
	if got := m.play.program; got != ".foo[]" {
		t.Errorf("query = %q, want %q", got, ".foo[]")
	}
	if m.play.bufFocus {
		t.Error("the focus must stay on the query line")
	}
}
