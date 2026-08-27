package app

// settledearlyout_test.go guards the settled pass's constant work (#2187):
// the reconcile steps at the end of every Update pass run once per *message*,
// so a message flood must not pay O(panes × tabs) — nor a single allocation —
// while nothing they care about is open.

import (
	"strings"
	"testing"
)

func TestImageSyncSkipsWorkWithoutImagePanes(t *testing.T) {
	m := newSized()
	if m.activeWS().Panes.ImagesMinted() {
		t.Fatal("setup: a fresh workspace has no image panes")
	}
	if cmd := m.imageSyncCmd(); cmd != nil {
		t.Fatal("no image pane, no reconcile cmd")
	}
	if allocs := testing.AllocsPerRun(50, func() { m.imageSyncCmd() }); allocs > 0 {
		t.Errorf("imageSyncCmd runs per message: %v allocs without image panes, want 0", allocs)
	}
	// With an image pane open the pass does its work again.
	tm, _ := m.Update(OpenImageMsg{Path: writeTestPNG(t)})
	m = tm.(Model)
	if !m.activeWS().Panes.ImagesMinted() {
		t.Fatal("opening an image preview must mint an image pane")
	}
	m.gfxQueried = false // the open pass already probed; ask for it again
	if raw := rawStrings(m.imageSyncCmd()); !strings.Contains(raw, "a=q") {
		t.Errorf("an open image pane must still emit the Kitty probe, got %q", raw)
	}
}

func TestBreadcrumbLayoutSkipsWorkWithoutSymbols(t *testing.T) {
	m := newSized()
	if m.crumbSig != "" {
		t.Fatalf("setup: no pane shows a breadcrumb row, sig = %q", m.crumbSig)
	}
	if allocs := testing.AllocsPerRun(50, func() { m.syncBreadcrumbLayout() }); allocs > 0 {
		t.Errorf("syncBreadcrumbLayout runs per message: %v allocs with no symbols, want 0", allocs)
	}
	if m.crumbSig != "" {
		t.Errorf("the early-out must not invent a signature, got %q", m.crumbSig)
	}
}

func TestDomSyncSkipsWorkWithoutInspector(t *testing.T) {
	m := newSized()
	if m.domPanel() != nil || m.domHLPath != "" {
		t.Fatal("setup: no DOM inspector and no highlighted file")
	}
	if cmd := m.domSyncCmd(); cmd != nil {
		t.Fatal("no inspector, no sync cmd")
	}
	if allocs := testing.AllocsPerRun(50, func() { m.domSyncCmd() }); allocs > 0 {
		t.Errorf("domSyncCmd runs per message: %v allocs with no inspector, want 0", allocs)
	}
}
