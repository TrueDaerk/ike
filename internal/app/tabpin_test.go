package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/pane"
	"ike/internal/theme"
)

// tabpin_test.go covers pinned tabs (#1172): the toggle command, persistence
// through the layout store, the LRU-eviction and Close-Others exemptions, the
// pin prefix in the tab bar (render + hit geometry) and the context-menu
// label state.

func TestTogglePinRoundTripsThroughLayout(t *testing.T) {
	conf := t.TempDir()
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")

	m := fixedDirApp(t, conf)
	m = openApp2(t, m, a, b)
	// b is active: pin it.
	m = dispatch(t, m, TabTogglePinMsg{})
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst.TabPinned(0) || !inst.TabPinned(1) {
		t.Fatalf("toggle must pin the active tab only: %v %v", inst.TabPinned(0), inst.TabPinned(1))
	}

	m2 := fixedDirApp(t, conf)
	inst2 := m2.activeWS().Panes.Get(key)
	if inst2 == nil || inst2.Kind() != pane.KindEditor || inst2.TabCount() != 2 {
		t.Fatal("the editor pane must restore with both tabs")
	}
	if inst2.TabPinned(0) || !inst2.TabPinned(1) {
		t.Fatalf("pin state must survive a restart: %v %v", inst2.TabPinned(0), inst2.TabPinned(1))
	}

	// Unpin, restart again: the pin must not resurrect.
	m2 = dispatch(t, m2, TabSelectMsg{Index: 1})
	m2 = dispatch(t, m2, TabTogglePinMsg{})
	m3 := fixedDirApp(t, conf)
	if inst3 := m3.activeWS().Panes.Get(key); inst3.TabPinned(1) {
		t.Fatal("an unpinned tab must restore unpinned")
	}
}

func TestTabLimitEvictionSkipsPinned(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "a\n")
	b := writeTemp(t, dir, "b.txt", "b\n")
	c := writeTemp(t, dir, "c.txt", "c\n")
	m := newSized()
	withTabLimit(t, 2)
	m = openApp2(t, m, a)
	m = dispatch(t, m, TabTogglePinMsg{}) // pin a, the LRU-to-be
	m = openApp2(t, m, b, c)

	inst := m.activeWS().Panes.FocusedInstance()
	if inst.TabCount() != 2 {
		t.Fatalf("tabs = %v, want 2 after eviction", tabPaths(inst))
	}
	for _, p := range tabPaths(inst) {
		if p == b {
			t.Fatalf("eviction must skip the pinned LRU and close b, tabs = %v", tabPaths(inst))
		}
	}
	if inst.TabForPath(a) < 0 {
		t.Fatalf("the pinned tab must survive, tabs = %v", tabPaths(inst))
	}
}

func TestTabLimitAllPinnedExceedsLimit(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "a\n")
	b := writeTemp(t, dir, "b.txt", "b\n")
	c := writeTemp(t, dir, "c.txt", "c\n")
	m := newSized()
	withTabLimit(t, 2)
	m = openApp2(t, m, a)
	m = dispatch(t, m, TabTogglePinMsg{})
	m = openApp2(t, m, b)
	m = dispatch(t, m, TabTogglePinMsg{})
	m = openApp2(t, m, c)

	inst := m.activeWS().Panes.FocusedInstance()
	if inst.TabCount() != 3 {
		// Documented overflow: with every other tab pinned nothing is
		// evictable, so the limit is exceeded rather than a pin overridden.
		t.Fatalf("all-pinned pane must exceed the limit, tabs = %v", tabPaths(inst))
	}
}

func TestCloseOthersKeepsPinnedTabs(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "a\n")
	b := writeTemp(t, dir, "b.txt", "b\n")
	c := writeTemp(t, dir, "c.txt", "c\n")
	m := newSized()
	m = openApp2(t, m, a)
	m = dispatch(t, m, TabTogglePinMsg{}) // pin a
	m = openApp2(t, m, b, c)              // c active

	m = dispatch(t, m, TabCloseOthersMsg{})
	inst := m.activeWS().Panes.FocusedInstance()
	paths := tabPaths(inst)
	if len(paths) != 2 || inst.TabForPath(a) < 0 || inst.TabForPath(c) < 0 {
		t.Fatalf("close-others must keep the pinned and the active tab, got %v", paths)
	}
	if inst.TabForPath(b) >= 0 {
		t.Fatalf("close-others must still close unpinned clean tabs, got %v", paths)
	}
}

func TestTabLabelsCarryPinPrefix(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "a\n")
	b := writeTemp(t, dir, "b.txt", "b\n")
	m := newSized()
	m = openApp2(t, m, a)
	m = dispatch(t, m, TabTogglePinMsg{})
	m = openApp2(t, m, b)

	labels := tabLabels(m.activeWS().Panes.FocusedInstance())
	if !strings.HasPrefix(labels[0], tabPinPrefix) {
		t.Fatalf("pinned tab label must carry the pin prefix, got %q", labels[0])
	}
	if strings.HasPrefix(labels[1], tabPinPrefix) {
		t.Fatalf("unpinned tab label must not carry the prefix, got %q", labels[1])
	}
}

func TestRenderTabBarPinPrefixKeepsGeometry(t *testing.T) {
	pal := theme.DefaultPalette()
	labels := []string{tabPinPrefix + "a.go", "b.go"}
	bar := renderTabBar(labels, 0, 60, pal)
	plain := ansi.Strip(bar)
	if !strings.Contains(plain, "• a.go ✕ │ b.go ✕") {
		t.Fatalf("pinned segment must render prefix, label and ✕, got %q", plain)
	}
	// The rendered cells must match what tabHit assumes: the ✕ of the pinned
	// segment sits one pad after the (prefixed) label.
	lw := ansi.StringWidth(labels[0])
	if idx, onClose := tabHit(labels, 0, 60, 1+lw+1); idx != 0 || !onClose {
		t.Fatalf("✕ cell of the pinned segment must hit-test as close, got %d %v", idx, onClose)
	}
	// The pin glyph cell belongs to the segment but is not the close zone.
	if idx, onClose := tabHit(labels, 0, 60, 1); idx != 0 || onClose {
		t.Fatalf("pin glyph cell must select the tab, not close it, got %d %v", idx, onClose)
	}
	// The second segment's ✕: separator after segment 0 (width lw+2+tabCloseW).
	pos := lw + 2 + tabCloseW + 1 // past segment 0 and the │
	lw2 := ansi.StringWidth(labels[1])
	if idx, onClose := tabHit(labels, 0, 60, pos+1+lw2+1); idx != 1 || !onClose {
		t.Fatalf("✕ cell of the second segment must stay aligned, got %d %v", idx, onClose)
	}
}

func TestTabContextItemsPinLabel(t *testing.T) {
	find := func(pinned bool) string {
		for _, it := range tabContextItems(pinned) {
			if it.Command == "editor.tab.togglePin" {
				return it.Title
			}
		}
		return ""
	}
	if got := find(false); got != "Pin Tab" {
		t.Fatalf("unpinned tab menu entry = %q, want Pin Tab", got)
	}
	if got := find(true); got != "Unpin Tab" {
		t.Fatalf("pinned tab menu entry = %q, want Unpin Tab", got)
	}
}
