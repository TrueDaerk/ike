package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/changefeed"
	"ike/internal/terminal"
	"ike/internal/watch"
)

// changefeed_batch_test.go covers the feed's batch actions and its per-process
// grouping (#2183): reload-all / revert-all over the whole list or a marked
// subset, the confirmation that spells the revert out, and the dirty-buffer
// conflicts a batch must never resolve on its own.

// shellBody renders whatever the modal shell currently holds, so a test can
// assert on a confirmation's text.
func shellBody(m Model) string { return m.shell.Content().Render(80) }

// TestChangeFeedReloadAllSkipsDirtyBuffers: A reloads every entry it can and
// leaves the conflicted ones — a file changed externally *and* edited in IKE
// is a decision only the user can make, so the batch reports it instead.
func TestChangeFeedReloadAllSkipsDirtyBuffers(t *testing.T) {
	dir := t.TempDir()
	clean, conflict := filepath.Join(dir, "clean.txt"), filepath.Join(dir, "conflict.txt")
	for _, p := range []string{clean, conflict} {
		if err := os.WriteFile(p, []byte("one\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newSized()
	tm, _ := m.openPath(clean, false)
	m = tm.(Model)
	m = openDirty(t, m, conflict) // buffer: "Xone", dirty — opened last, nothing autosaved

	m = externalWrite(t, m, clean, "AGENT\n")
	m = externalWrite(t, m, conflict, "AGENT\n")

	m.openChangeFeed()
	m = keyChangeFeed(t, m, "A")

	if !m.changeFeedOpen() {
		t.Fatal("the batch reload closed the panel it reports into")
	}
	if got := m.editorForPath(clean).Text(); got != "AGENT" {
		t.Fatalf("clean buffer = %q, want the reloaded external content", got)
	}
	ed := m.editorForPath(conflict)
	if got := ed.Text(); got != "Xone" {
		t.Fatalf("conflicted buffer = %q, want the user's unsaved edits untouched", got)
	}
	if !ed.Dirty() {
		t.Fatal("the batch dropped the unsaved changes it was supposed to skip")
	}
	notice := lastNotice(m)
	if !strings.Contains(notice, "reloaded 1 file(s)") {
		t.Fatalf("notice = %q, want the count of what was reloaded", notice)
	}
	if !strings.Contains(notice, "unsaved changes") || !strings.Contains(notice, "conflict.txt") {
		t.Fatalf("notice = %q, want the skipped conflict named", notice)
	}
}

// TestChangeFeedReloadAllReportsUnreachableEntries: a file that is not open
// and one that is gone are no more reloadable in a batch than under R, and the
// batch says so rather than pretending it did something.
func TestChangeFeedReloadAllReportsUnreachableEntries(t *testing.T) {
	dir := t.TempDir()
	gone := filepath.Join(dir, "gone.txt")
	if err := os.WriteFile(gone, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = externalWrite(t, m, filepath.Join(dir, "closed.txt"), "x\n") // never opened
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(watch.EventMsg{Kind: watch.FileRemoved, Path: gone})
	m = tm.(Model)

	m.openChangeFeed()
	m = keyChangeFeed(t, m, "A")

	notice := lastNotice(m)
	if !strings.Contains(notice, "reloaded 0 file(s)") {
		t.Fatalf("notice = %q, want an honest zero", notice)
	}
	if !strings.Contains(notice, "not open") || !strings.Contains(notice, "no longer exists") {
		t.Fatalf("notice = %q, want both reasons reported", notice)
	}
}

// TestChangeFeedRevertAllConfirmsWithFileList: V asks first, and the prompt
// names every file it is about to restore — a revert of a whole agent run must
// not be answered from a row count alone.
func TestChangeFeedRevertAllConfirmsWithFileList(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("mine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newSized()
	for _, p := range []string{a, b} {
		tm, _ := m.openPath(p, false)
		m = tm.(Model)
		m = externalWrite(t, m, p, "AGENT\n")
	}

	m.openChangeFeed()
	m = keyChangeFeed(t, m, "V")
	if !m.changeFeedRevertOpen() {
		t.Fatal("revert-all did not ask for confirmation first")
	}
	body := shellBody(m)
	if !strings.Contains(body, "a.txt") || !strings.Contains(body, "b.txt") {
		t.Fatalf("confirmation does not list the affected files:\n%s", body)
	}
	if m.editorForPath(a).Text() != "AGENT" || m.editorForPath(b).Text() != "AGENT" {
		t.Fatal("a buffer changed before the confirmation was answered")
	}

	// esc cancels the whole batch.
	tm, _ := m.updateChangeFeedRevert(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.changeFeedRevertOpen() || m.feed.Len() != 2 {
		t.Fatalf("cancel left revert=%v feed=%d, want closed with both entries", m.changeFeedRevertOpen(), m.feed.Len())
	}
	if m.editorForPath(a).Text() != "AGENT" {
		t.Fatal("cancelling the confirmation still reverted")
	}

	m.openChangeFeed()
	m = keyChangeFeed(t, m, "V")
	tm, cmd := m.updateChangeFeedRevert(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	notice := lastNotice(m) // read before the follow-up commands drain the queue
	m = drainCmd(m, cmd)

	for _, p := range []string{a, b} {
		if got := m.editorForPath(p).Text(); got != "mine" {
			t.Fatalf("%s after revert-all = %q, want the pre-change content", baseName(p), got)
		}
	}
	if m.feed.Len() != 0 {
		t.Fatalf("feed = %d entries after revert-all, want the reverted rows gone", m.feed.Len())
	}
	if !strings.Contains(notice, "reverted 2 file(s)") {
		t.Fatalf("notice = %q, want one report for the whole batch", notice)
	}
}

// TestChangeFeedRevertAllExcludesConflicts: a buffer with unsaved changes is
// left out of the confirmation entirely and named as skipped — the batch never
// overwrites the user's own edits.
func TestChangeFeedRevertAllExcludesConflicts(t *testing.T) {
	dir := t.TempDir()
	plain, conflict := filepath.Join(dir, "plain.txt"), filepath.Join(dir, "conflict.txt")
	for _, p := range []string{plain, conflict} {
		if err := os.WriteFile(p, []byte("mine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newSized()
	tm, _ := m.openPath(plain, false)
	m = tm.(Model)
	m = openDirty(t, m, conflict)
	m = externalWrite(t, m, plain, "AGENT\n")
	m = externalWrite(t, m, conflict, "AGENT\n")

	m.openChangeFeed()
	m = keyChangeFeed(t, m, "V")
	body := shellBody(m)
	if !strings.Contains(body, "plain.txt") {
		t.Fatalf("confirmation dropped the file it can revert:\n%s", body)
	}
	if !strings.Contains(body, "conflict.txt (unsaved changes)") {
		t.Fatalf("confirmation does not report the skipped conflict:\n%s", body)
	}

	tm, cmd := m.updateChangeFeedRevert(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	notice := lastNotice(m) // read before the follow-up commands drain the queue
	m = drainCmd(m, cmd)

	if got := m.editorForPath(conflict).Text(); got != "Xmine" {
		t.Fatalf("conflicted buffer = %q, want the unsaved edits untouched", got)
	}
	if _, ok := m.feed.Get(absTestPath(conflict)); !ok {
		t.Fatal("the skipped entry left the feed although nothing was done to it")
	}
	if !strings.Contains(notice, "conflict.txt") {
		t.Fatalf("notice = %q, want the skipped file reported", notice)
	}
}

// TestChangeFeedMarksScopeTheBatch: space marks rows, and a batch with marks
// applies to exactly those — marking refines the scope, it is not a
// precondition for acting on the whole feed.
func TestChangeFeedMarksScopeTheBatch(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("mine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newSized()
	for _, p := range []string{a, b} {
		tm, _ := m.openPath(p, false)
		m = tm.(Model)
		m = externalWrite(t, m, p, "AGENT\n")
	}

	m.openChangeFeed()
	// The newest entry (b.txt) leads the list; mark it and revert the marks.
	sel, _ := m.changeFeedSel()
	if sel.Path != absTestPath(b) {
		t.Fatalf("selection = %q, want the newest entry %q", sel.Path, b)
	}
	tm, _ := m.updateChangeFeed(tea.KeyPressMsg{Code: ' ', Text: " "})
	m = tm.(Model)
	if m.changeFeedMarked() != 1 {
		t.Fatalf("marked = %d after space, want 1", m.changeFeedMarked())
	}
	if !strings.Contains(m.changeFeedBody(120), "●") {
		t.Fatal("the marked row renders no mark")
	}

	m = keyChangeFeed(t, m, "V")
	body := shellBody(m)
	if !strings.Contains(body, "b.txt") || strings.Contains(body, "a.txt") {
		t.Fatalf("confirmation ignored the marks:\n%s", body)
	}
	tm, cmd := m.updateChangeFeedRevert(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(tm.(Model), cmd)

	if got := m.editorForPath(b).Text(); got != "mine" {
		t.Fatalf("marked file = %q, want it reverted", got)
	}
	if got := m.editorForPath(a).Text(); got != "AGENT" {
		t.Fatalf("unmarked file = %q, want it untouched by the batch", got)
	}
}

// TestChangeFeedGroupsByProcess: where the attribution exists the panel
// renders titled groups, and the entries nothing could be attributed to stay
// together under the plain "unattributed" title.
func TestChangeFeedGroupsByProcess(t *testing.T) {
	m := newSized()
	now := time.Now()
	m.feed.Add(changefeed.Entry{
		Path: "/tmp/agent.go", Time: now, Kind: changefeed.Changed,
		Before: "old", Origin: changefeed.FromSnapshot, Source: "claude",
	})
	m.feed.Add(changefeed.Entry{
		Path: "/tmp/other.go", Time: now, Kind: changefeed.Changed,
		Before: "old", Origin: changefeed.FromSnapshot,
	})
	m.openChangeFeed()

	body := m.changeFeedBody(120)
	if !strings.Contains(body, "claude") || !strings.Contains(body, "unattributed") {
		t.Fatalf("panel does not render the groups:\n%s", body)
	}
	// The attributed group leads, so its file is the first row.
	if sel, _ := m.changeFeedSel(); sel.Path != "/tmp/agent.go" {
		t.Fatalf("first row = %q, want the attributed group first", sel.Path)
	}
	// m marks the selection's whole group, and only that group.
	tm, _ := m.updateChangeFeed(tea.KeyPressMsg{Code: 'm', Text: "m"})
	m = tm.(Model)
	if m.changeFeedMarked() != 1 || !m.cfMarks["/tmp/agent.go"] {
		t.Fatalf("group mark = %v, want only the claude group marked", m.cfMarks)
	}
}

// TestChangeFeedAttributionStaysSilentWithoutACandidate: attribution is best
// effort and refuses to guess — no session running anything is no source, and
// neither is an idle or dead one.
func TestChangeFeedAttributionStaysSilentWithoutACandidate(t *testing.T) {
	m := newSized()
	if got := m.changeFeedSource(); got != "" {
		t.Fatalf("changeFeedSource = %q with nothing running, want no attribution", got)
	}
	if got := terminalSourceName(nil); got != "" {
		t.Fatalf("terminalSourceName(nil) = %q, want empty", got)
	}
	var idle terminal.Model // never spawned: not running, so not a candidate
	idle.SetTool("claude")
	if got := terminalSourceName(&idle); got != "" {
		t.Fatalf("terminalSourceName(idle tool) = %q, want empty", got)
	}
}

// TestChangeFeedUngroupedWithoutAttribution: nothing attributed means no group
// titles at all — a header that says nothing is worse than no header.
func TestChangeFeedUngroupedWithoutAttribution(t *testing.T) {
	dir := t.TempDir()
	m := newSized()
	m = externalWrite(t, m, filepath.Join(dir, "a.txt"), "x\n")
	m.openChangeFeed()

	if m.changeFeedGrouped() {
		t.Fatal("grouping claimed for a feed with no attribution")
	}
	if strings.Contains(m.changeFeedBody(120), "unattributed") {
		t.Fatalf("an unattributed feed grew a group title:\n%s", m.changeFeedBody(120))
	}
}
