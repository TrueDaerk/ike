package app

// scratch_section_test.go covers the app-side wiring of the explorer's
// Scratches section (#1963/#1965): the wheel routed to the section under the
// pointer, and the MRU store pushed in as the section's "last opened" column.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
	"ike/internal/scratch"
)

// seedScratches writes n scratch files into the sandboxed store and refreshes
// the explorer's section, returning their paths.
func seedScratches(t *testing.T, m Model, n int) []string {
	t.Helper()
	dir, err := scratch.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("s%02d.txt", i))
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	m.explorer().RefreshScratches()
	return paths
}

// sectionRowY returns the absolute screen row of the section's first body row.
func sectionRowY(t *testing.T, m Model) int {
	t.Helper()
	r, ok := m.lay.Panes[pane.ExplorerKey]
	if !ok {
		t.Fatal("setup: the explorer pane has no rect")
	}
	exp := m.explorer()
	for y := 0; y < r.H; y++ {
		if exp.ScratchDividerHit(0, y) {
			return r.Y + m.contentYOff(pane.ExplorerKey) + y + 1
		}
	}
	t.Fatal("setup: no Scratches divider found in the explorer pane")
	return 0
}

// TestExplorerWheelScrollsScratchSection (#1965): a wheel over the Scratches
// section scrolls the section, while a wheel over the tree leaves it alone.
func TestExplorerWheelScrollsScratchSection(t *testing.T) {
	m := newSized()
	seedScratches(t, m, 20)
	if got := len(m.explorer().ScratchEntries()); got != 20 {
		t.Fatalf("setup: %d section rows, want 20", got)
	}
	y := sectionRowY(t, m)
	x := m.lay.Panes[pane.ExplorerKey].X + paneContentX
	m = step(m, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	if got := m.explorer().ScratchTop(); got != wheelLines {
		t.Fatalf("wheel over the section: top = %d want %d", got, wheelLines)
	}
	m = step(m, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})
	if got := m.explorer().ScratchTop(); got != 0 {
		t.Fatalf("wheel back up: top = %d want 0", got)
	}
	// The tree region above the divider keeps the tree's own scroll.
	m = step(m, tea.MouseWheelMsg{X: x, Y: m.lay.Panes[pane.ExplorerKey].Y + paneContentY, Button: tea.MouseWheelDown})
	if got := m.explorer().ScratchTop(); got != 0 {
		t.Fatalf("a tree wheel scrolled the section: top = %d", got)
	}
}

// TestScratchSectionLastOpenedFromMRU (#1965): opening a scratch stamps the
// MRU store, and the section's age column reads that time — not the mtime.
func TestScratchSectionLastOpenedFromMRU(t *testing.T) {
	m := newSized()
	paths := seedScratches(t, m, 1)
	// Age the file well past the open we are about to record.
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(paths[0], old, old); err != nil {
		t.Fatal(err)
	}
	m.explorer().RefreshScratches()
	tm, _ := m.openPath(paths[0], false)
	m = tm.(Model)
	got := m.lastOpenedTimes()
	if ts, ok := got[paths[0]]; !ok || time.Since(ts) > time.Minute {
		t.Fatalf("opening a scratch must stamp the MRU store, got %v (ok=%v)", ts, ok)
	}
	if got := m.explorer().ScratchAgeFor(paths[0]); got != "now" {
		t.Fatalf("the age column must follow the MRU stamp, got %q want %q", got, "now")
	}
}
