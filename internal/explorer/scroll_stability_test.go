package explorer

// scroll_stability_test.go guards #1140: a wheel scroll moves the viewport
// without the cursor, and content-driven rebuilds (watcher refresh, poll
// re-scan, config apply) must leave that viewport alone — only deliberate
// cursor motion (keys, click, reveal, user-initiated pendingSel snaps) pulls
// the window back to the selection. The former clampScroll was cursor-anchored
// and ran from those non-cursor paths, yanking the viewport back.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"ike/internal/host"
	"ike/internal/watch"
)

// scrollTree builds a flat directory with n files (f00..fNN).
func scrollTree(t *testing.T, n int) string {
	t.Helper()
	root := t.TempDir()
	for i := 0; i < n; i++ {
		mustWrite(t, filepath.Join(root, fmt.Sprintf("f%02d", i)), "x")
	}
	return root
}

// scrolledModel returns a loaded model of 30 rows in a 6-row pane, cursor on
// the last row, wheel-scrolled back to the top.
func scrolledModel(t *testing.T) Model {
	t.Helper()
	m := mounted(t, scrollTree(t, 30), 40, 6)
	m.moveCursor(len(m.rows)) // cursor to the bottom; viewport follows
	if m.offset == 0 {
		t.Fatal("precondition: moving to the bottom should scroll")
	}
	m.ScrollBy(-1000) // wheel back to the top, cursor stays at the bottom
	if m.offset != 0 {
		t.Fatalf("precondition: wheel-up should reach offset 0, got %d", m.offset)
	}
	return m
}

// TestWheelScrollSurvivesWatcherRefresh: an external-refresh re-scan (the
// watcher path, which snaps the cursor onto its entry via pendingSel) must
// not pull the viewport back to the off-screen selection.
func TestWheelScrollSurvivesWatcherRefresh(t *testing.T) {
	m := scrolledModel(t)
	cursorBefore := m.rows[m.cursor].path
	m, _ = pumpScans(m.Update(watch.EventMsg{Kind: watch.DirChanged, Path: m.Root()}))
	if m.offset != 0 {
		t.Fatalf("watcher refresh moved the viewport: offset = %d, want 0", m.offset)
	}
	if got := m.rows[m.cursor].path; got != cursorBefore {
		t.Fatalf("cursor left its entry across the refresh: %q -> %q", cursorBefore, got)
	}
}

// TestWheelScrollSurvivesPollRescan: a poll-driven re-scan (no pendingSel at
// all) keeps the viewport too.
func TestWheelScrollSurvivesPollRescan(t *testing.T) {
	m := scrolledModel(t)
	m, _ = pumpScans(m.Update(pollMsg{changed: []string{m.Root()}}))
	if m.offset != 0 {
		t.Fatalf("poll re-scan moved the viewport: offset = %d, want 0", m.offset)
	}
}

// TestWheelScrollSurvivesConfigApply: a live config change that rebuilds the
// rows (a sort change) keeps the wheel-scrolled viewport.
func TestWheelScrollSurvivesConfigApply(t *testing.T) {
	m := scrolledModel(t)
	m.Configure(host.MapConfig{"explorer.sort": "modified"})
	if m.offset != 0 {
		t.Fatalf("config apply moved the viewport: offset = %d, want 0", m.offset)
	}
}

// TestWheelScrollSurvivesResize: SetSize is a geometry change, not a cursor
// move — the viewport only clamps into the new range.
func TestWheelScrollSurvivesResize(t *testing.T) {
	m := scrolledModel(t)
	m.SetSize(40, 8)
	if m.offset != 0 {
		t.Fatalf("resize moved the viewport: offset = %d, want 0", m.offset)
	}
}

// TestCursorMotionStillReframes: pressing a movement key after a wheel scroll
// pulls the viewport back to the cursor — deliberate motion keeps following.
func TestCursorMotionStillReframes(t *testing.T) {
	m := scrolledModel(t)
	m, _ = m.Update(CursorMoveMsg{Delta: -1})
	_, textH, _, _, _ := m.viewport()
	if m.cursor < m.offset || m.cursor >= m.offset+textH {
		t.Fatalf("cursor %d not in window [%d,%d) after key motion", m.cursor, m.offset, m.offset+textH)
	}
	if m.offset == 0 {
		t.Fatal("key motion near the bottom should have scrolled the viewport back down")
	}
}

// TestUserSnapFollowsCursor: a user-initiated pendingSel snap (snapCursorTo,
// the file-op / toggle-hidden / reveal path) scrolls the landed cursor into
// view on the next rebuild.
func TestUserSnapFollowsCursor(t *testing.T) {
	m := scrolledModel(t)
	last := m.rows[len(m.rows)-1].path
	m.snapCursorTo(last)
	m.rebuild()
	_, textH, _, _, _ := m.viewport()
	if m.cursor < m.offset || m.cursor >= m.offset+textH {
		t.Fatalf("snapped cursor %d not in window [%d,%d)", m.cursor, m.offset, m.offset+textH)
	}
}

// TestRebuildClampsOutOfRangeOffset: when rows vanish under a scrolled
// viewport (deleted / collapsed), the offset still clamps into the valid
// range — clampOffset keeps the essential bound the old clampScroll enforced.
func TestRebuildClampsOutOfRangeOffset(t *testing.T) {
	root := scrollTree(t, 30)
	m := mounted(t, root, 40, 6)
	m.ScrollBy(1000) // bottom
	if m.offset == 0 {
		t.Fatal("precondition: should be scrolled")
	}
	// Delete most files on disk and let the watcher-style refresh land.
	for i := 5; i < 30; i++ {
		if err := os.Remove(filepath.Join(root, fmt.Sprintf("f%02d", i))); err != nil {
			t.Fatal(err)
		}
	}
	m, _ = pumpScans(m.Update(watch.EventMsg{Kind: watch.DirChanged, Path: root}))
	_, textH, _, _, _ := m.viewport()
	maxOff := len(m.rows) - textH
	if maxOff < 0 {
		maxOff = 0
	}
	if m.offset > maxOff || m.offset < 0 {
		t.Fatalf("offset %d outside [0,%d] after rows shrank", m.offset, maxOff)
	}
}
