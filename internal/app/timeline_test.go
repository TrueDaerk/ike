package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/timeline"
	"ike/internal/vcs"
)

// timeline_test.go covers the per-file Timeline (#1916) end to end in the app:
// the merged list over both sources, diffing an entry against the buffer and
// two entries against each other (across sources), restoring a snapshot, the
// source filter, and the degenerate files (untracked / never saved).

// timelineRepo builds a git repo whose f.txt carries two commits, opens it in
// a fresh model with the VCS snapshot wired up, and returns the model plus the
// file path. The caller records snapshots itself.
func timelineRepo(t *testing.T) (Model, string) {
	t.Helper()
	return timelineRepoIn(t, newSized())
}

// timelineRepoIn is timelineRepo over an already-built model, so a test can
// bring its own settings.
func timelineRepoIn(t *testing.T, m Model) (Model, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")
	if err := os.WriteFile(path, []byte("v1\nv2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "second")

	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	m.vcs.snap = &vcs.Snapshot{Root: dir, Branch: "main"}
	return m, path
}

// openTimelineWithGit opens the Timeline and runs the git window its command
// requests, so the returned model holds the fully merged list.
func openTimelineWithGit(t *testing.T, m Model) Model {
	t.Helper()
	tm, cmd := m.Update(TimelineMsg{})
	m = tm.(Model)
	if !m.timelineOpen() {
		t.Fatal("the timeline did not open")
	}
	msg, ok := msgOf[vcs.FileLogMsg](cmd)
	if !ok {
		t.Fatalf("no git window requested: %v", cmdMsgs(cmd))
	}
	return step(m, msg)
}

// focusFileTab returns the focused pane to its first file-backed editor tab.
// A diff opens as a content tab of that very pane since #2507, so a command
// that acts on "the focused file" needs the file tab active again.
func focusFileTab(m Model) Model {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil {
		return m
	}
	for i := 0; i < inst.TabCount(); i++ {
		if inst.TabEditor(i) != nil {
			m.switchTab(inst, i)
			break
		}
	}
	return m
}

// replaceBuffer overwrites a buffer through the normal edit path, so the
// change is one undoable unit like a restore.
func replaceBuffer(ed *editor.Model, text string) {
	lines := strings.Split(ed.Text(), "\n")
	last := lines[len(lines)-1]
	ed.ApplyTextEdits([]editor.TextEdit{{
		EndLine: len(lines) - 1, EndCol: len([]rune(last)), Text: text,
	}})
}

// msgOf extracts the first message of type T from the command tree a dispatch
// produced — every command dispatch is a batch since the command-executed
// signal (#679), so a bare type assertion on cmd() would see the batch.
func msgOf[T any](cmd tea.Cmd) (T, bool) {
	for _, msg := range cmdMsgs(cmd) {
		if typed, ok := msg.(T); ok {
			return typed, true
		}
	}
	var zero T
	return zero, false
}

// pressTimeline feeds one key into the open Timeline and returns the model plus
// the command the action produced.
func pressTimeline(m Model, key string) (Model, tea.Cmd) {
	press := tea.KeyPressMsg{Code: rune(key[0]), Text: key}
	if key == "enter" {
		press = tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	tm, cmd := m.Update(press)
	return tm.(Model), cmd
}

func TestTimelineMergesSnapshotsAndCommits(t *testing.T) {
	m, path := timelineRepo(t)
	m.recordLocalHistory(path) // one snapshot of the committed content
	m = openTimelineWithGit(t, m)

	if got := len(m.tl.merged); got != 3 {
		t.Fatalf("merged entries = %d, want 3 (1 snapshot + 2 commits): %+v", got, m.tl.merged)
	}
	// Chronological order, newest first — the snapshot was taken after both
	// commits, and at an equal timestamp it still ranks as the later event.
	if m.tl.merged[0].Source != timeline.Local {
		t.Fatalf("first entry = %+v, want the snapshot", m.tl.merged[0])
	}
	for i := 1; i < len(m.tl.merged); i++ {
		if m.tl.merged[i].Time.After(m.tl.merged[i-1].Time) {
			t.Fatalf("entry %d is newer than its predecessor: %+v", i, m.tl.merged)
		}
	}
	if m.tl.merged[1].Subject != "second" || m.tl.merged[2].Subject != "init" {
		t.Fatalf("commit order = %+v", m.tl.merged[1:])
	}
	// The list renders both sources with their own icon and no stale loader.
	body := m.renderTimeline()
	if !strings.Contains(body, timeline.Local.Icon()) || !strings.Contains(body, timeline.Git.Icon()) {
		t.Fatalf("rendered list misses a source icon:\n%s", body)
	}
	if strings.Contains(body, "loading git history") {
		t.Fatalf("loader still shown after the window arrived:\n%s", body)
	}

	// y on a commit copies its full hash; on a snapshot it explains itself.
	var copied string
	old := clipboardWrite
	clipboardWrite = func(text string) { copied = text }
	t.Cleanup(func() { clipboardWrite = old })
	m, _ = pressTimeline(m, "j") // move onto the newest commit
	m, _ = pressTimeline(m, "y")
	if copied != m.tl.merged[1].Hash || len(copied) != 40 {
		t.Fatalf("copied %q, want the full sha of %+v", copied, m.tl.merged[1])
	}
	copied = ""
	m, _ = pressTimeline(m, "k") // back onto the snapshot
	m, _ = pressTimeline(m, "y")
	if copied != "" {
		t.Fatalf("a snapshot has no commit hash, but %q was copied", copied)
	}

	// esc closes the picker without touching the panes.
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m = tm.(Model); m.timelineOpen() {
		t.Fatal("esc did not close the timeline")
	}
}

func TestTimelineFilterCycles(t *testing.T) {
	m, path := timelineRepo(t)
	m.recordLocalHistory(path)
	m = openTimelineWithGit(t, m)

	m, _ = pressTimeline(m, "f") // both → local
	if m.tl.filter != timeline.LocalOnly || len(m.tl.merged) != 1 {
		t.Fatalf("local filter: %q with %d entries", m.tl.filter, len(m.tl.merged))
	}
	m, _ = pressTimeline(m, "f") // local → git
	if m.tl.filter != timeline.GitOnly || len(m.tl.merged) != 2 {
		t.Fatalf("git filter: %q with %d entries", m.tl.filter, len(m.tl.merged))
	}
	// The selection stays in range while the list shrinks under it.
	if m.tl.sel < 0 || m.tl.sel >= len(m.tl.merged) {
		t.Fatalf("selection %d out of range for %d entries", m.tl.sel, len(m.tl.merged))
	}
	m, _ = pressTimeline(m, "f") // git → both
	if m.tl.filter != timeline.Both || len(m.tl.merged) != 3 {
		t.Fatalf("back to both: %q with %d entries", m.tl.filter, len(m.tl.merged))
	}
}

// TestTimelineDefaultFilterFromSettings guards the history.timeline_source
// setting: the Timeline opens on the configured filter.
func TestTimelineDefaultFilterFromSettings(t *testing.T) {
	m, path := timelineRepoIn(t, newWithSettings(t, "[history]\ntimeline_source = \"git\"\n"))
	m.recordLocalHistory(path)
	m = openTimelineWithGit(t, m)
	if m.tl.filter != timeline.GitOnly {
		t.Fatalf("filter = %q, want git from the setting", m.tl.filter)
	}
	if len(m.tl.merged) != 2 {
		t.Fatalf("git-only list = %+v", m.tl.merged)
	}
}

func TestTimelineDiffAgainstBufferBothSources(t *testing.T) {
	m, path := timelineRepo(t)
	m.recordLocalHistory(path) // snapshot of "v1\nv2\n"
	m = openTimelineWithGit(t, m)
	ed := m.editorForPath(path)
	if ed == nil {
		t.Fatal("setup: no editor for the file")
	}

	// enter on the snapshot: snapshot (left) vs the live buffer (right).
	m2, cmd := pressTimeline(m, "enter")
	msg, ok := msgOf[timelineDiffMsg](cmd)
	if !ok {
		t.Fatalf("enter produced no diff: %v", cmdMsgs(cmd))
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if msg.left != "v1\nv2" || msg.right != ed.Text() || !msg.editable {
		t.Fatalf("snapshot-vs-buffer = %+v", msg)
	}
	if m2.timelineOpen() {
		t.Fatal("the picker must close when a diff opens")
	}
	m2 = step(m2, msg)
	inst, _, _, ok := m2.diffSlot()
	if !ok {
		t.Fatal("no diff pane opened")
	}
	if left, right := inst.Diff().Titles(); !strings.HasPrefix(left, "f.txt @ ") || right != "f.txt" {
		t.Fatalf("diff titles = %q / %q", left, right)
	}

	// enter on the oldest commit: its blob (left) vs the buffer. The picker
	// closed with the diff (the modal shell is shared state), so re-open it —
	// over the file tab the diff tab now sits beside (#2507).
	m = openTimelineWithGit(t, focusFileTab(m2))
	m, _ = pressTimeline(m, "G") // jump to the oldest entry ("init")
	if got := m.tl.merged[m.tl.sel]; got.Source != timeline.Git || got.Subject != "init" {
		t.Fatalf("selected %+v, want the init commit", got)
	}
	m, cmd = pressTimeline(m, "enter")
	msg, ok = msgOf[timelineDiffMsg](cmd)
	if !ok {
		t.Fatalf("enter on a commit produced no diff: %v", cmdMsgs(cmd))
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if msg.left != "v1" || msg.right != ed.Text() {
		t.Fatalf("commit-vs-buffer = %+v", msg)
	}
}

func TestTimelineDiffBetweenEntriesAcrossSources(t *testing.T) {
	m, path := timelineRepo(t)
	m.recordLocalHistory(path)
	m = openTimelineWithGit(t, m)

	// d without a mark explains itself instead of diffing.
	m2, cmd := pressTimeline(m, "d")
	if _, ok := msgOf[timelineDiffMsg](cmd); ok {
		t.Fatal("d without a mark must not diff")
	}
	if !m2.timelineOpen() {
		t.Fatal("d without a mark must keep the picker open")
	}

	// Mark the snapshot, select the oldest commit, diff across the sources.
	m, _ = pressTimeline(m, "m")
	if !m.tl.marked || m.tl.mark.Source != timeline.Local {
		t.Fatalf("mark = %+v (marked=%v)", m.tl.mark, m.tl.marked)
	}
	if !strings.Contains(m.renderTimeline(), "*") {
		t.Fatal("the marked row must be flagged in the list")
	}
	m, _ = pressTimeline(m, "G")
	m, cmd = pressTimeline(m, "d")
	msg, ok := msgOf[timelineDiffMsg](cmd)
	if !ok {
		t.Fatalf("d with a mark produced no diff: %v", cmdMsgs(cmd))
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	// The older entry goes left, so the diff reads forward in time — here the
	// commit blob against the newer snapshot.
	if msg.left != "v1" || msg.right != "v1\nv2" {
		t.Fatalf("commit-vs-snapshot = %+v", msg)
	}
	if msg.editable {
		t.Fatal("neither side is the working tree: the diff must stay read-only")
	}
	m = step(m, msg)
	inst, _, _, ok := m.diffSlot()
	if !ok {
		t.Fatal("no diff pane opened")
	}
	if inst.Diff().Editable() {
		t.Fatal("an entry-vs-entry diff must not be editable")
	}
	if got := inst.Diff().HunkCount(); got != 1 {
		t.Fatalf("hunks = %d, want 1", got)
	}
}

func TestTimelineRestoreSnapshotIsUndoable(t *testing.T) {
	m, path := timelineRepo(t)
	m.recordLocalHistory(path) // snapshot of "v1\nv2\n"
	ed := m.editorForPath(path)
	if ed == nil {
		t.Fatal("setup: no editor for the file")
	}
	// Diverge the buffer from the snapshot.
	m = openTimelineWithGit(t, m)
	replaceBuffer(ed, "wrecked")

	m, cmd := pressTimeline(m, "r")
	msg, ok := msgOf[timelineRestoreMsg](cmd)
	if !ok {
		t.Fatalf("r produced no restore: %v", cmdMsgs(cmd))
	}
	if msg.err != nil || msg.text != "v1\nv2" {
		t.Fatalf("restore msg = %+v", msg)
	}
	m = step(m, msg)
	if got := m.editorForPath(path).Text(); got != "v1\nv2" {
		t.Fatalf("buffer after restore = %q", got)
	}
	// One undoable change: a single undo brings the pre-restore content back.
	ed = m.editorForPath(path)
	*ed, _ = ed.Update(editor.ActionMsg{Action: "undo"})
	if got := ed.Text(); got != "wrecked" {
		t.Fatalf("buffer after undo = %q, want the pre-restore content", got)
	}

	// A commit entry rejects the restore action with an explanation.
	m = openTimelineWithGit(t, m)
	m, _ = pressTimeline(m, "G")
	m, cmd = pressTimeline(m, "r")
	if _, ok := msgOf[timelineRestoreMsg](cmd); ok {
		t.Fatal("restore must not run on a commit entry")
	}
	if !m.timelineOpen() {
		t.Fatal("a rejected restore must keep the picker open")
	}
}

func TestTimelineDegenerateFiles(t *testing.T) {
	// A tracked file that was never saved in this session: git entries only.
	m, path := timelineRepo(t)
	m = openTimelineWithGit(t, m)
	if len(m.tl.local) != 0 || len(m.tl.merged) != 2 {
		t.Fatalf("file without snapshots = %d local / %d merged", len(m.tl.local), len(m.tl.merged))
	}

	// An untracked file: snapshots only, and no git window is requested.
	m, path = timelineRepo(t)
	m.vcs.snap.Files = map[string]vcs.FileStatus{"f.txt": vcs.StatusUntracked}
	m.recordLocalHistory(path)
	tm, cmd := m.Update(TimelineMsg{})
	m = tm.(Model)
	if !m.timelineOpen() {
		t.Fatal("the timeline must open for an untracked file")
	}
	if _, ok := msgOf[vcs.FileLogMsg](cmd); ok {
		t.Fatal("an untracked file must not query git")
	}
	if len(m.tl.merged) != 1 || m.tl.merged[0].Source != timeline.Local {
		t.Fatalf("untracked timeline = %+v", m.tl.merged)
	}
	if m.tl.more || m.tl.loading {
		t.Fatal("no git window is pending for an untracked file")
	}

	// Neither history: a notification instead of an empty picker.
	m = newSized()
	empty := filepath.Join(t.TempDir(), "fresh.txt")
	if err := os.WriteFile(empty, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ = m.openPath(empty, false)
	m = tm.(Model)
	tm, cmd = m.Update(TimelineMsg{})
	m = tm.(Model)
	if _, ok := msgOf[vcs.FileLogMsg](cmd); m.timelineOpen() || ok {
		t.Fatal("a file with neither snapshots nor commits must not open the timeline")
	}
}

// TestTimelineIncrementalLoading guards the paging path: the first window
// reports more, and walking toward the end pulls the next one in.
func TestTimelineIncrementalLoading(t *testing.T) {
	m, path := timelineRepo(t)
	m.recordLocalHistory(path)
	tm, _ := m.Update(TimelineMsg{})
	m = tm.(Model)
	// A one-commit window over a two-commit history: older history remains.
	first := vcs.FileLogCmd(m.tl.root, m.tl.rel, 0, 1)().(vcs.FileLogMsg)
	if !first.HasMore {
		t.Fatalf("setup: first window = %+v", first)
	}
	m = step(m, first)
	if len(m.tl.git) != 1 || !m.tl.more || m.tl.loading {
		t.Fatalf("after the first window: %d commits, more=%v loading=%v",
			len(m.tl.git), m.tl.more, m.tl.loading)
	}
	if !strings.Contains(m.renderTimeline(), "L loads more") {
		t.Fatal("the list must announce that older commits are unloaded")
	}
	// L requests the next window; the arriving entries append in order.
	m, cmd := pressTimeline(m, "L")
	next, ok := msgOf[vcs.FileLogMsg](cmd)
	if !ok {
		t.Fatalf("L requested no window: %v", cmdMsgs(cmd))
	}
	if next.Offset != 1 || len(next.Entries) != 1 || next.HasMore {
		t.Fatalf("second window = %+v", next)
	}
	m = step(m, next)
	if len(m.tl.git) != 2 || m.tl.more {
		t.Fatalf("after the second window: %d commits, more=%v", len(m.tl.git), m.tl.more)
	}
	if m.tl.merged[1].Subject != "second" || m.tl.merged[2].Subject != "init" {
		t.Fatalf("merged commit order = %+v", m.tl.merged)
	}
	// Nothing left to load: L is a friendly no-op.
	_, cmd = pressTimeline(m, "L")
	if _, ok := msgOf[vcs.FileLogMsg](cmd); ok {
		t.Fatal("L must not re-query an exhausted history")
	}
}

// TestTimelineGitErrorKeepsSnapshots guards the failure path: a failing git
// window leaves the local half usable instead of closing the view.
func TestTimelineGitErrorKeepsSnapshots(t *testing.T) {
	m, path := timelineRepo(t)
	m.recordLocalHistory(path)
	tm, _ := m.Update(TimelineMsg{})
	m = tm.(Model)
	m = step(m, vcs.FileLogMsg{Path: m.tl.rel, Err: os.ErrNotExist})
	if !m.timelineOpen() {
		t.Fatal("a git error must not close the timeline")
	}
	if len(m.tl.merged) != 1 || m.tl.loading || m.tl.more {
		t.Fatalf("after the git error: %+v (loading=%v more=%v)", m.tl.merged, m.tl.loading, m.tl.more)
	}
	// A window for another file is ignored outright.
	before := len(m.tl.git)
	m = step(m, vcs.FileLogMsg{Path: "other.txt", Entries: []vcs.FileLogEntry{{
		LogEntry: vcs.LogEntry{Hash: "x", Time: time.Now()},
	}}})
	if len(m.tl.git) != before {
		t.Fatal("a window for another file must be ignored")
	}
}
