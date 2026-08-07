package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// TestListNavKeys guards #887: pgup/pgdn/home/end work everywhere listNav is
// wired.
func TestListNavKeys(t *testing.T) {
	sel := 0
	if !listNav("pgdown", &sel, 50, 10) || sel != 10 {
		t.Fatalf("pgdown sel = %d", sel)
	}
	if !listNav("end", &sel, 50, 10) || sel != 49 {
		t.Fatalf("end sel = %d", sel)
	}
	if !listNav("home", &sel, 50, 10) || sel != 0 {
		t.Fatalf("home sel = %d", sel)
	}
	if listNav("x", &sel, 50, 10) {
		t.Fatal("unrelated keys must not consume")
	}
}

// TestListNavWrapsStepsClampsPages guards #1666: single steps wrap around the
// list, page jumps clamp — the semantics every selectable list shares.
func TestListNavWrapsStepsClampsPages(t *testing.T) {
	sel := 0
	if !listNav("up", &sel, 5, 3) || sel != 4 {
		t.Fatalf("up on the first row = %d, want 4", sel)
	}
	if !listNav("down", &sel, 5, 3) || sel != 0 {
		t.Fatalf("down on the last row = %d, want 0", sel)
	}
	if !listNav("k", &sel, 5, 3) || sel != 4 {
		t.Fatalf("k on the first row = %d, want 4", sel)
	}
	if !listNav("j", &sel, 5, 3) || sel != 0 {
		t.Fatalf("j on the last row = %d, want 0", sel)
	}
	if !listNav("pgup", &sel, 5, 3) || sel != 0 {
		t.Fatalf("pgup on the first row must clamp, got %d", sel)
	}
	// A settings page's single-letter keys are actions, so g/G stay free.
	if listNav("g", &sel, 5, 3) || listNav("G", &sel, 5, 3) {
		t.Fatal("g/G must stay available as page action keys")
	}
	// Empty and single-entry lists must not move or panic.
	empty := 0
	if listNav("down", &empty, 0, 3) || empty != 0 {
		t.Fatalf("empty list consumed a key, sel = %d", empty)
	}
	one := 0
	for _, k := range []string{"up", "down", "pgup", "pgdown", "home", "end"} {
		if !listNav(k, &one, 1, 3) || one != 0 {
			t.Fatalf("one-entry list after %q: sel = %d", k, one)
		}
	}
}

// TestNavPageSizeFollowsRenderedHeight guards #1666: a page's pgup/pgdn jump
// is its last rendered height, not a fixed row count.
func TestNavPageSizeFollowsRenderedHeight(t *testing.T) {
	var n navRows
	if n.navPageSize() != navPage {
		t.Fatalf("unrendered page size = %d, want the %d fallback", n.navPageSize(), navPage)
	}
	n.setRows(24)
	if n.navPageSize() != 24 {
		t.Fatalf("page size = %d, want 24", n.navPageSize())
	}
}

// TestSpaceTogglesBool guards #887: space flips a boolean row like enter.
func TestSpaceTogglesBool(t *testing.T) {
	m := mouseModel(t)
	m.focus = formColumn
	m.sel = 0 // ui.menu_bar (Bool)
	before := m.value("ui.menu_bar")
	m.Update(key("space"))
	if m.value("ui.menu_bar") == before {
		t.Fatal("space must toggle the boolean")
	}
}

// TestChordCaptureSubPanel guards #887: schema Chord entries capture through
// the shared sub-panel — multi-step, enter confirms, esc cancels.
func TestChordCaptureSubPanel(t *testing.T) {
	restoreConfig(t)
	pages := []Page{{Title: "Keys", Entries: []Entry{
		{Key: "palette.toggle_key", Type: Chord, Title: "Palette key", Scope: config.UserScope},
	}}}
	m := New(pages, testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.focus = formColumn
	m.Update(key("enter"))
	if !m.SubOpen() {
		t.Fatal("a chord entry must open the capture sub-panel")
	}
	m.Update(keyRune('g'))
	m.Update(keyRune('p'))
	apply(t, m.Update(key("enter")))
	if m.SubOpen() {
		t.Fatal("apply must close the capture")
	}
	if got := liveValue("palette.toggle_key"); got != "g p" {
		t.Fatalf("captured chord = %q, want \"g p\"", got)
	}
	// Esc cancels without writing.
	m.Update(key("enter"))
	m.Update(keyRune('z'))
	m.Update(key("esc"))
	if m.SubOpen() || liveValue("palette.toggle_key") != "g p" {
		t.Fatal("esc must cancel the capture without writing")
	}
}

// TestKeyHelpOverlay guards #887: "?" lists the shared keys plus the active
// page's.
func TestKeyHelpOverlay(t *testing.T) {
	m, _ := searchModel(t) // schema pages + toolchain
	m.cat = len(m.pages) - 1
	m.focus = formColumn
	m.Update(key("?"))
	if !m.SubOpen() {
		t.Fatal("? must open the key help")
	}
	v := m.View()
	if !strings.Contains(v, "write-scope") || !strings.Contains(v, "new Python environment") {
		t.Fatalf("key help must list shared + page keys:\n%s", v)
	}
	m.Update(key("esc"))
	if m.SubOpen() {
		t.Fatal("esc must close the help")
	}
}
