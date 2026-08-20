package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/changefeed"
	"ike/internal/diff"
	"ike/internal/host"
	"ike/internal/watch"
)

// externalWrite rewrites path behind IKE's back and feeds the watcher event
// the real watcher would have produced, through the root model's Update — the
// same route a coding agent's write takes.
func externalWrite(t *testing.T, m Model, path, content string) Model {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: path})
	return tm.(Model)
}

// TestChangeFeedRecordsExternalWriteWithDiff covers the headline acceptance
// criterion: an external write to a tracked file produces a feed entry whose
// mini-diff shows what changed, with the pre-change content taken off the open
// buffer before the auto-reload overwrites it.
func TestChangeFeedRecordsExternalWriteWithDiff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.openPath(path, false)
	m = tm.(Model)

	m = externalWrite(t, m, path, "one\nAGENT\n")

	if m.feed.Len() != 1 {
		t.Fatalf("feed = %d entries after an external write, want 1", m.feed.Len())
	}
	e, ok := m.feed.Get(absTestPath(path))
	if !ok {
		t.Fatalf("no entry for %s; feed holds %+v", path, m.feed.Entries())
	}
	if e.Kind != changefeed.Changed {
		t.Fatalf("Kind = %v, want Changed", e.Kind)
	}
	if e.Origin != changefeed.FromBuffer {
		t.Fatalf("Origin = %v, want FromBuffer (the open buffer held the previous content)", e.Origin)
	}
	if e.Before != "one\ntwo" {
		t.Fatalf("Before = %q, want the pre-change buffer text", e.Before)
	}

	m.openChangeFeed()
	if !m.changeFeedOpen() {
		t.Fatal("the change-feed panel did not open")
	}
	if m.cfErr != "" {
		t.Fatalf("cfErr = %q, want a diff instead", m.cfErr)
	}
	if len(m.cfDiff.Hunks) == 0 {
		t.Fatal("the selected entry produced no diff hunks")
	}
	if !diffMentions(m.cfDiff, "AGENT") {
		t.Fatalf("the diff does not show the external line; rows = %+v", m.cfDiff.Rows)
	}
	// The panel renders through the shell content, width-aware (#1969).
	body := m.changeFeedBody(120)
	if !strings.Contains(body, "AGENT") || !strings.Contains(body, baseName(path)) {
		t.Fatalf("panel body missing the file or the change:\n%s", body)
	}
}

// TestChangeFeedIgnoresOwnSaves: IKE's own writes must never look like an
// agent's. The watcher stamps a save epoch on every write; the feed re-asks it
// before recording.
func TestChangeFeedIgnoresOwnSaves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = openDirty(t, m, path)
	m = drainKey(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}) // save

	// The save stamped the epoch; the echo the filesystem produces for it
	// arrives as an ordinary event.
	tm, _ := m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: path})
	m = tm.(Model)

	if m.feed.Len() != 0 {
		t.Fatalf("own save landed in the feed: %+v", m.feed.Entries())
	}
}

// TestChangeFeedSkipsNonFileEvents: directory, repository-metadata and
// settings events name no project file the user edited.
func TestChangeFeedSkipsNonFileEvents(t *testing.T) {
	dir := t.TempDir()
	m := newSized()
	for _, kind := range []watch.Kind{watch.DirChanged, watch.GitChanged, watch.ConfigChanged} {
		tm, _ := m.Update(watch.EventMsg{Kind: kind, Path: filepath.Join(dir, "x")})
		m = tm.(Model)
	}
	if m.feed.Len() != 0 {
		t.Fatalf("non-file events landed in the feed: %+v", m.feed.Entries())
	}
}

// TestChangeFeedHidesWatcherNoise: the feed follows the watcher's own ignore
// rules, so a vendored directory churning cannot bury the real changes.
func TestChangeFeedHidesWatcherNoise(t *testing.T) {
	dir := t.TempDir()
	noise := filepath.Join(dir, "node_modules", "pkg")
	if err := os.MkdirAll(noise, 0o755); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m.feed.Ignore = func(path string) bool { return watch.Ignored(dir, path) }

	m = externalWrite(t, m, filepath.Join(noise, "index.js"), "x")
	if m.feed.Len() != 0 {
		t.Fatalf("ignored path landed in the feed: %+v", m.feed.Entries())
	}

	real := filepath.Join(dir, "main.go")
	m = externalWrite(t, m, real, "package main")
	if m.feed.Len() != 1 {
		t.Fatalf("feed = %d entries, want the one real change", m.feed.Len())
	}
}

// TestChangeFeedCoalescesAndCaps: an agent rewriting the same file is one row,
// and the configured cap bounds the session's list.
func TestChangeFeedCoalescesAndCaps(t *testing.T) {
	dir := t.TempDir()
	m := newSized()
	m.host.SetConfig(host.MapConfig{"files.change_feed_limit": "3"})

	path := filepath.Join(dir, "a.txt")
	for _, content := range []string{"1", "2", "3"} {
		m = externalWrite(t, m, path, content)
	}
	if m.feed.Len() != 1 {
		t.Fatalf("feed = %d entries after three writes to one file, want 1", m.feed.Len())
	}
	if e, _ := m.feed.Get(absTestPath(path)); e.Count != 3 {
		t.Fatalf("Count = %d, want 3", e.Count)
	}

	for _, name := range []string{"b.txt", "c.txt", "d.txt"} {
		m = externalWrite(t, m, filepath.Join(dir, name), "x")
	}
	if m.feed.Len() != 3 {
		t.Fatalf("feed = %d entries, want the cap 3", m.feed.Len())
	}
	if _, ok := m.feed.Get(absTestPath(path)); ok {
		t.Fatal("the oldest entry survived the cap")
	}
}

// TestChangeFeedSurvivesPaneSwitches: the feed lives on the root model, not on
// the panel, so closing it and moving focus around leaves it intact.
func TestChangeFeedSurvivesPaneSwitches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	m = externalWrite(t, m, path, "AGENT\n")

	m.openChangeFeed()
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape}) // close the panel
	if m.changeFeedOpen() {
		t.Fatal("esc left the panel open")
	}
	for range 3 {
		m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab}) // cycle pane focus
	}
	if m.feed.Len() != 1 {
		t.Fatalf("feed = %d entries after pane switches, want 1", m.feed.Len())
	}
	m.openChangeFeed()
	if !m.changeFeedOpen() || len(m.cfEntries) != 1 {
		t.Fatalf("re-opened panel shows %d entries, want 1", len(m.cfEntries))
	}
}

// TestChangeFeedFollowsLiveChanges: the agent keeps writing while the panel is
// open, so the list folds new events in and the selection stays on the file it
// was on rather than sliding down as rows prepend.
func TestChangeFeedFollowsLiveChanges(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	m := newSized()
	m = externalWrite(t, m, a, "a\n")

	m.openChangeFeed()
	if len(m.cfEntries) != 1 || m.cfSel != 0 {
		t.Fatalf("panel opened with %d entries at %d, want 1 at 0", len(m.cfEntries), m.cfSel)
	}
	m = externalWrite(t, m, b, "b\n")

	if len(m.cfEntries) != 2 {
		t.Fatalf("open panel shows %d entries, want the new one folded in", len(m.cfEntries))
	}
	if sel, _ := m.changeFeedSel(); sel.Path != absTestPath(a) {
		t.Fatalf("selection moved to %q, want it pinned to %q", sel.Path, a)
	}
}

// TestChangeFeedRevertRestoresPreChangeContent: the revert action is confirmed
// first and then restores the pre-change content through the local-history
// restore path — one undoable edit, disk untouched until the save.
func TestChangeFeedRevertRestoresPreChangeContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	m = externalWrite(t, m, path, "AGENT WROTE THIS\n")

	if ed := m.activeEditor(); ed == nil || ed.Text() != "AGENT WROTE THIS" {
		t.Fatalf("buffer = %q, want the auto-reloaded external content", m.activeEditor().Text())
	}

	m.openChangeFeed()
	m = keyChangeFeed(t, m, "r")
	if !m.changeFeedRevertOpen() {
		t.Fatal("revert did not ask for confirmation first")
	}
	if m.activeEditor().Text() != "AGENT WROTE THIS" {
		t.Fatal("the buffer changed before the confirmation was answered")
	}

	// esc cancels without restoring anything.
	tm, _ = m.updateChangeFeedRevert(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.activeEditor().Text() != "AGENT WROTE THIS" {
		t.Fatal("cancelling the confirmation still reverted")
	}

	m.openChangeFeed()
	m = keyChangeFeed(t, m, "r")
	tm, _ = m.updateChangeFeedRevert(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)

	ed := m.activeEditor()
	if got := ed.Text(); got != "mine" {
		t.Fatalf("buffer after revert = %q, want the pre-change content %q", got, "mine")
	}
	if !ed.Dirty() {
		t.Fatal("the revert must mark the buffer dirty — the file on disk is untouched until a save")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "AGENT WROTE THIS\n" {
		t.Fatalf("file on disk = %q, want the external content untouched", data)
	}
	if _, ok := m.feed.Get(absTestPath(path)); ok {
		t.Fatal("the reverted entry stayed in the feed")
	}
}

// TestChangeFeedRevertViaLocalHistory: with no open buffer to read the
// pre-change content from, the newest local-history snapshot supplies it — the
// #1023 store is the fallback the issue calls for.
func TestChangeFeedRevertViaLocalHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("saved by ike\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m.lhStore.Record(path, []byte("saved by ike\n")) // what IKE last wrote there

	m = externalWrite(t, m, path, "AGENT\n") // the file is not open anywhere

	e, ok := m.feed.Get(absTestPath(path))
	if !ok {
		t.Fatalf("no feed entry; feed holds %+v", m.feed.Entries())
	}
	if e.Origin != changefeed.FromSnapshot {
		t.Fatalf("Origin = %v, want FromSnapshot", e.Origin)
	}
	if e.Before != "saved by ike" {
		t.Fatalf("Before = %q, want the snapshot content", e.Before)
	}

	m.openChangeFeed()
	if m.cfErr != "" || len(m.cfDiff.Hunks) == 0 {
		t.Fatalf("no diff from the snapshot: cfErr=%q hunks=%d", m.cfErr, len(m.cfDiff.Hunks))
	}
	m = keyChangeFeed(t, m, "r")
	tm, cmd := m.updateChangeFeedRevert(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(tm.(Model), cmd)

	// The revert opened the file to restore into it.
	ed := m.editorForPath(path)
	if ed == nil {
		t.Fatal("revert did not open the file it restores into")
	}
	if got := ed.Text(); got != "saved by ike" {
		t.Fatalf("buffer after revert = %q, want the snapshot content", got)
	}
}

// TestChangeFeedRevertUndeletesRemovedFile: a file an agent deleted has no
// buffer to restore into, so the revert writes it back to disk and opens it.
func TestChangeFeedRevertUndeletesRemovedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m.lhStore.Record(path, []byte("keep me\n"))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(watch.EventMsg{Kind: watch.FileRemoved, Path: path})
	m = tm.(Model)

	e, ok := m.feed.Get(absTestPath(path))
	if !ok || e.Kind != changefeed.Removed {
		t.Fatalf("entry = %+v, ok = %v; want a Removed entry", e, ok)
	}

	m.openChangeFeed()
	// The mini-diff reads the disk, not the (possibly still open) buffer: the
	// file holds nothing now, so every line shows as removed.
	if m.cfErr != "" || len(m.cfDiff.Hunks) == 0 {
		t.Fatalf("no diff for the deleted file: cfErr=%q hunks=%d", m.cfErr, len(m.cfDiff.Hunks))
	}
	for _, row := range m.cfDiff.Rows {
		if row.Kind != diff.RowRemoved {
			t.Fatalf("diff row %+v, want every row removed for a deleted file", row)
		}
	}

	m = keyChangeFeed(t, m, "r")
	tm, cmd := m.updateChangeFeedRevert(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(tm.(Model), cmd)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the file was not written back: %v", err)
	}
	if string(data) != "keep me\n" {
		t.Fatalf("restored file = %q, want %q", data, "keep me\n")
	}
	if m.editorForPath(path) == nil {
		t.Fatal("the restored file was not opened")
	}
}

// TestChangeFeedOpenAndDismiss: enter opens the file, x drops the row, c
// clears the feed.
func TestChangeFeedOpenAndDismiss(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	m := newSized()
	m = externalWrite(t, m, a, "a\n")
	m = externalWrite(t, m, b, "b\n")

	// x drops the selected (newest) row and leaves the panel usable.
	m.openChangeFeed()
	m = keyChangeFeed(t, m, "x")
	if m.feed.Len() != 1 {
		t.Fatalf("feed = %d after x, want 1", m.feed.Len())
	}
	if !m.changeFeedOpen() || len(m.cfEntries) != 1 {
		t.Fatalf("panel after x: open=%v entries=%d, want open with 1", m.changeFeedOpen(), len(m.cfEntries))
	}

	// enter opens the remaining file in an editor.
	tm, cmd := m.updateChangeFeed(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(tm.(Model), cmd)
	if m.changeFeedOpen() {
		t.Fatal("enter left the modal in front of the pane it opened")
	}
	if m.editorForPath(a) == nil {
		t.Fatalf("enter did not open %s", a)
	}

	// c clears the whole feed.
	m.openChangeFeed()
	m = keyChangeFeed(t, m, "c")
	if m.feed.Len() != 0 || m.changeFeedOpen() {
		t.Fatalf("after c: len=%d open=%v, want 0 and closed", m.feed.Len(), m.changeFeedOpen())
	}
}

// TestChangeFeedReloadsConflictedBuffer: a dirty buffer is never silently
// reloaded — it goes stale, and the feed's reload action is how the user drops
// their edits in favour of the external version.
func TestChangeFeedReloadsConflictedBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = openDirty(t, m, path) // buffer: "Xone", dirty
	m = externalWrite(t, m, path, "AGENT\n")

	ed := m.activeEditor()
	if !ed.Stale() {
		t.Fatal("the dirty buffer was not marked stale by the external change")
	}
	if ed.Text() == "AGENT" {
		t.Fatal("a dirty buffer must never be silently reloaded")
	}

	m.openChangeFeed()
	tm, cmd := m.updateChangeFeed(tea.KeyPressMsg{Code: 'R', Text: "R"})
	m = drainCmd(tm.(Model), cmd)

	if got := m.editorForPath(path).Text(); got != "AGENT" {
		t.Fatalf("buffer after reload = %q, want the on-disk %q", got, "AGENT")
	}
	if m.editorForPath(path).Dirty() {
		t.Fatal("the reloaded buffer is still dirty")
	}
}

// TestChangeFeedRefusedActionKeepsPanelOpen: an action that cannot apply —
// reloading a file that is not open — explains itself without tearing down the
// list the user was reading.
func TestChangeFeedRefusedActionKeepsPanelOpen(t *testing.T) {
	dir := t.TempDir()
	m := newSized()
	m = externalWrite(t, m, filepath.Join(dir, "a.txt"), "a\n") // never opened

	m.openChangeFeed()
	m = keyChangeFeed(t, m, "R")
	if !m.changeFeedOpen() {
		t.Fatal("a reload that cannot happen closed the panel")
	}
	if !strings.Contains(lastNotice(m), "is not open") {
		t.Fatalf("notice = %q, want the reason the reload is unavailable", lastNotice(m))
	}
}

// TestChangeFeedDiffPane: d lands the before/after pair in the reusable diff
// pane instead of the panel's mini-diff.
func TestChangeFeedDiffPane(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	m = externalWrite(t, m, path, "AGENT\n")

	m.openChangeFeed()
	tm, cmd := m.updateChangeFeed(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m = drainCmd(tm.(Model), cmd)
	if m.changeFeedOpen() {
		t.Fatal("d left the modal in front of the diff pane")
	}
	if _, _, _, ok := m.diffSlot(); !ok {
		t.Fatal("d did not open a diff pane")
	}
}

// TestChangeFeedCreatedEntryHasNoRevert: a file that did not exist before has
// no previous version — the panel says so instead of offering a revert that
// would have to invent one.
func TestChangeFeedCreatedEntryHasNoRevert(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(path, []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(watch.EventMsg{Kind: watch.FileCreated, Path: path})
	m = tm.(Model)

	e, ok := m.feed.Get(absTestPath(path))
	if !ok || e.Kind != changefeed.Created || e.HasBefore() {
		t.Fatalf("entry = %+v, ok = %v; want a Created entry without pre-change content", e, ok)
	}
	m.openChangeFeed()
	if m.cfErr == "" {
		t.Fatal("the panel promised a diff for a created file")
	}
	m = keyChangeFeed(t, m, "r")
	if m.changeFeedRevertOpen() {
		t.Fatal("revert was offered for a file with no previous version")
	}
	if !m.changeFeedOpen() {
		t.Fatal("a revert that cannot happen closed the panel anyway")
	}
	if !strings.Contains(lastNotice(m), "no previous version") {
		t.Fatalf("notice = %q, want the reason the revert is unavailable", lastNotice(m))
	}
}

// TestChangeFeedEmptyNotifies: the command explains itself rather than opening
// a blank panel.
func TestChangeFeedEmptyNotifies(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(ChangeFeedMsg{}) // the command routes to openChangeFeed
	m = tm.(Model)
	if m.changeFeedOpen() {
		t.Fatal("an empty feed opened a panel")
	}
	m = newSized()
	m.openChangeFeed()
	if !strings.Contains(strings.ToLower(lastNotice(m)), "no external file changes") {
		t.Fatalf("notice = %q, want the empty-feed explanation", lastNotice(m))
	}
}

// TestChangeFeedLimitZeroDisables: files.change_feed_limit = 0 is the off
// switch — nothing is recorded.
func TestChangeFeedLimitZeroDisables(t *testing.T) {
	dir := t.TempDir()
	m := newSized()
	m.host.SetConfig(host.MapConfig{"files.change_feed_limit": "0"})

	m = externalWrite(t, m, filepath.Join(dir, "a.txt"), "x")
	if m.feed.Len() != 0 {
		t.Fatalf("feed = %d entries with the feed disabled, want 0", m.feed.Len())
	}
}

// lastNotice returns the text of the most recent notification the model
// raised, draining the host's queue.
func lastNotice(m Model) string {
	pending := m.host.DrainNotifications()
	if len(pending) == 0 {
		return ""
	}
	return pending[len(pending)-1].Text
}

// absTestPath mirrors the absolute spelling the watcher (and therefore the
// feed) keys entries by.
func absTestPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// keyChangeFeed feeds one key into the open panel and runs its commands.
func keyChangeFeed(t *testing.T, m Model, key string) Model {
	t.Helper()
	if !m.changeFeedOpen() {
		t.Fatal("the change-feed panel is not open")
	}
	tm, cmd := m.updateChangeFeed(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
	return drainCmd(tm.(Model), cmd)
}

// diffMentions reports whether text appears on either side of the diff.
func diffMentions(res diff.Result, text string) bool {
	for _, row := range res.Rows {
		if strings.Contains(row.Left, text) || strings.Contains(row.Right, text) {
			return true
		}
	}
	return false
}
