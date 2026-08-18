package dataview

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// profile_test.go covers the pane's half of the column profile (#1940): the
// popup, its async/cancel path, and the routing rules that keep a late result
// from replacing what the user is looking at.

// gridPane returns a pane over the fixture database with the grid focused and
// the profiled column selected by colOff steps of l.
func gridPane(t *testing.T, src *fakeSource) Model {
	t.Helper()
	m := fakePane(t, src)
	feed(t, &m, key("tab")) // sidebar → grid
	return m
}

func TestProfilePopupOpensAsyncAndLandsInThePane(t *testing.T) {
	src := &fakeSource{rows: 1200, est: 1200}
	src.block = make(chan struct{})
	m := gridPane(t, src)

	cmd := m.Update(key("P"))
	if cmd == nil {
		t.Fatal("P must return the background profile command")
	}
	// The popup is up before the backend has answered: the grid never waits.
	if !m.ProfileOpen() || !m.ProfileRunning() {
		t.Fatal("the popup must open pending")
	}
	if view := stripANSI(m.View()); !strings.Contains(view, "profiling id") {
		t.Fatalf("a pending profile must say so:\n%s", view)
	}
	if src.profiles != 0 {
		t.Fatal("nothing may run before the runtime executes the command")
	}

	close(src.block)
	pump(t, &m, cmd)
	if m.ProfileRunning() {
		t.Fatal("the landed result must clear the pending state")
	}
	if src.profiles != 1 || src.profiled[0] != "big.id|" {
		t.Fatalf("the profile ran for %v, want big.id unfiltered", src.profiled)
	}
	view := stripANSI(m.View())
	for _, want := range []string{"column: id", "rows", "distinct", "top", "esc close"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the popup is missing %q:\n%s", want, view)
		}
	}
}

func TestProfilePopupOwnsTheKeyboardAndEscCancels(t *testing.T) {
	src := &fakeSource{rows: 1200, est: 1200}
	src.block = make(chan struct{})
	defer close(src.block)
	m := gridPane(t, src)
	before := m.PageOffset()

	cmd := m.Update(key("P"))
	// Grid keys are inert under the popup: n would page a grid the user
	// cannot see.
	feed(t, &m, key("n"))
	if m.PageOffset() != before {
		t.Fatal("the popup must own the keyboard")
	}
	// esc closes the popup and cancels the query behind it — the command's
	// context is done, so the fake returns without waiting for its release.
	feed(t, &m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.ProfileOpen() {
		t.Fatal("esc must close the popup")
	}
	pump(t, &m, cmd) // the cancelled profile lands into a closed popup
	if m.ProfileOpen() {
		t.Fatal("a result whose popup is gone must be dropped")
	}
	// The grid is live again.
	feed(t, &m, key("n"))
	if m.PageOffset() == before {
		t.Fatal("closing the popup must give the grid its keys back")
	}
}

func TestProfileDropsASupersededResult(t *testing.T) {
	src := &fakeSource{rows: 1200, est: 1200}
	m := gridPane(t, src)

	first := m.Update(key("P")) // id
	feed(t, &m, tea.KeyPressMsg{Code: tea.KeyEscape})
	second := m.Update(key("P"))
	pump(t, &m, first) // the stale one lands after the fresh popup opened
	pump(t, &m, second)
	if !m.ProfileOpen() || m.ProfileRunning() {
		t.Fatal("the second profile must be the one showing")
	}
	if src.profiles != 2 {
		t.Fatalf("profiles = %d, want 2", src.profiles)
	}
}

func TestProfileCarriesTheActiveFilter(t *testing.T) {
	src := &fakeSource{rows: 1200, est: 1200}
	m := gridPane(t, src)
	feed(t, &m, key("/"))
	for _, r := range "id > 5" {
		feed(t, &m, key(string(r)))
	}
	feed(t, &m, key("enter"))

	pump(t, &m, m.Update(key("P")))
	if len(src.profiled) == 0 || !strings.HasSuffix(src.profiled[len(src.profiled)-1], "|WHERE id > 5") {
		t.Fatalf("the profile must run under the grid's filter, got %v", src.profiled)
	}
}

func TestProfileCopyEmitsTheRenderedText(t *testing.T) {
	src := &fakeSource{rows: 1200, est: 1200}
	m := gridPane(t, src)
	pump(t, &m, m.Update(key("P")))

	msgs := feed(t, &m, key("y"))
	if len(msgs) != 1 {
		t.Fatalf("y must emit one copy message, got %v", msgs)
	}
	copyMsg, ok := msgs[0].(CopyMsg)
	if !ok {
		t.Fatalf("y must emit a CopyMsg, got %T", msgs[0])
	}
	if !strings.Contains(copyMsg.Text, "column: id") || !strings.Contains(copyMsg.Text, "distinct") {
		t.Fatalf("the copied text must be the shown profile:\n%s", copyMsg.Text)
	}
}

func TestProfileErrorStaysInThePopup(t *testing.T) {
	src := &fakeSource{rows: 1200, est: 1200, profileErr: errors.New("no such column: id")}
	m := gridPane(t, src)
	pump(t, &m, m.Update(key("P")))

	view := stripANSI(m.View())
	if !strings.Contains(view, "no such column") {
		t.Fatalf("the engine's error belongs in the popup:\n%s", view)
	}
	// Nothing to copy from a failed profile.
	if msgs := feed(t, &m, key("y")); len(msgs) != 0 {
		t.Fatalf("a failed profile has nothing to copy, got %v", msgs)
	}
}

func TestProfileMsgRunsTheSameActionAsTheKey(t *testing.T) {
	src := &fakeSource{rows: 1200, est: 1200}
	m := gridPane(t, src)
	feed(t, &m, ProfileMsg{})
	if !m.ProfileOpen() || m.ProfileColumn() != "id" {
		t.Fatalf("the palette command must profile the focused column, got %q", m.ProfileColumn())
	}
	if src.profiles != 1 {
		t.Fatalf("profiles = %d, want 1", src.profiles)
	}
}

func TestClosingThePaneCancelsARunningProfile(t *testing.T) {
	src := &fakeSource{rows: 1200, est: 1200}
	src.block = make(chan struct{})
	defer close(src.block)
	m := gridPane(t, src)

	cmd := m.Update(key("P"))
	m.Close()
	if m.ProfileOpen() {
		t.Fatal("closing the pane must close the popup")
	}
	pump(t, &m, cmd) // returns through the cancelled context, not the block
}
