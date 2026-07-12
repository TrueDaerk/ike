package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/commitui"
	"ike/internal/host"
	"ike/internal/registry"
	"ike/internal/vcs"
	"ike/internal/watch"
)

func vcsApp(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	return NewWith(registry.New(), host.MapConfig{})
}

func TestVCSBranchSegment(t *testing.T) {
	m := vcsApp(t)
	if got := m.branchSegment(); got != "" {
		t.Fatalf("no snapshot: segment = %q, want hidden", got)
	}
	m.vcs.snap = &vcs.Snapshot{Root: "/r", Branch: "main"}
	if got := m.branchSegment(); got != "⎇ main" {
		t.Errorf("segment = %q", got)
	}
	m.vcs.snap = &vcs.Snapshot{Root: "/r", Branch: "main", Ahead: 2, Behind: 1}
	if got := m.branchSegment(); got != "⎇ main ↑2 ↓1" {
		t.Errorf("diverged segment = %q", got)
	}
}

func TestVCSSnapshotReachesExplorer(t *testing.T) {
	m := vcsApp(t)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)
	snap := vcs.NewSnapshot("/r", map[string]vcs.FileStatus{"a.go": vcs.StatusModified})
	out, _ = m.Update(vcs.SnapshotMsg{Snap: snap})
	m = out.(Model)
	if m.VCSSnapshot() != snap {
		t.Fatal("snapshot not stored on the model")
	}
}

func TestVCSMarksCmdGatesOnStatus(t *testing.T) {
	m := vcsApp(t)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)
	dir := t.TempDir()
	path := writeTemp(t, dir, "f.go", "x\n")
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	ed := m.activeEditor()
	if ed == nil || ed.Path() != path {
		t.Fatal("setup: file not open")
	}

	// No snapshot / clean file: the command resolves to a clearing message,
	// never a git subprocess.
	msg, ok := m.vcsMarksCmd(ed)().(vcs.MarksMsg)
	if !ok || msg.Path != path || msg.Marks != nil {
		t.Fatalf("clean-file marks cmd = %#v", msg)
	}

	// Untracked stays clearing; modified goes through RefreshMarks (which on
	// this fake root fails and also resolves to a clear — the gate is what's
	// under test, the git call is covered in internal/vcs).
	m.vcs.snap = vcs.NewSnapshot(dir, map[string]vcs.FileStatus{"f.go": vcs.StatusUntracked})
	if msg := m.vcsMarksCmd(ed)().(vcs.MarksMsg); msg.Marks != nil {
		t.Fatalf("untracked marks = %#v", msg)
	}
	m.vcs.snap = vcs.NewSnapshot(dir, map[string]vcs.FileStatus{"f.go": vcs.StatusModified})
	if msg := m.vcsMarksCmd(ed)().(vcs.MarksMsg); msg.Path != path {
		t.Fatalf("modified marks path = %q", msg.Path)
	}
}

func TestCommitDialogLifecycle(t *testing.T) {
	m := vcsApp(t)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)

	// Outside a repo the command degrades to a hint.
	out, _ = m.Update(OpenCommitMsg{})
	m = out.(Model)
	if m.commitUI.IsOpen() {
		t.Fatal("dialog must not open without a snapshot")
	}

	m.vcs.snap = &vcs.Snapshot{Root: "/r", Branch: "main",
		Entries: []vcs.FileEntry{{Path: "a.go", Status: vcs.StatusModified, X: '.', Y: 'M'}}}
	out, _ = m.Update(OpenCommitMsg{})
	m = out.(Model)
	if !m.commitUI.IsOpen() {
		t.Fatal("dialog should open on a repo")
	}

	// While open, keys go to the dialog: space on the first row emits the
	// stage toggle, which the app answers with a git command.
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("stage toggle produced no command")
	}
	tgl, ok := cmd().(commitui.ToggleMsg)
	if !ok || tgl.Path != "a.go" || !tgl.Stage {
		t.Fatalf("toggle = %#v", tgl)
	}

	// A successful commit closes the dialog, clears the message, toasts.
	out, _ = m.Update(vcs.CommitDoneMsg{Hash: "abc1234", Summary: "feat: x"})
	m = out.(Model)
	if m.commitUI.IsOpen() || m.commitUI.Message() != "" {
		t.Fatal("successful commit must close and clear")
	}

	// A refresh with a nil snapshot closes a reopened dialog.
	out, _ = m.Update(OpenCommitMsg{})
	m = out.(Model)
	out, _ = m.Update(vcs.SnapshotMsg{Snap: nil})
	m = out.(Model)
	if m.commitUI.IsOpen() {
		t.Fatal("losing the repo must close the dialog")
	}
}

func TestVCSWatcherEventArmsDebounce(t *testing.T) {
	m := vcsApp(t)
	_, cmd := m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: "x.go"})
	if cmd == nil || !m.vcs.tickArmed {
		t.Fatalf("watcher event must arm the vcs debounce tick (cmd=%v armed=%v)", cmd, m.vcs.tickArmed)
	}
	// A second event while armed must not arm another tick.
	if c := m.scheduleVCSRefresh(); c != nil {
		t.Fatal("second trigger while armed must not schedule again")
	}
}

func TestVCSSaveInvalidateArmsDebounce(t *testing.T) {
	m := vcsApp(t)
	if _, cmd := m.Update(vcsInvalidateMsg{}); cmd == nil || !m.vcs.tickArmed {
		t.Fatal("invalidate must arm the vcs debounce tick")
	}
}

func TestVCSTickRunsSerializedRefresh(t *testing.T) {
	m := vcsApp(t)
	m.vcs.tickArmed = true
	if _, cmd := m.Update(vcsTickMsg{}); cmd == nil {
		t.Fatal("tick must launch the refresh")
	}
	if m.vcs.tickArmed || !m.vcs.refreshing {
		t.Fatalf("after tick: armed=%v refreshing=%v", m.vcs.tickArmed, m.vcs.refreshing)
	}
	// A tick arriving mid-flight queues exactly one follow-up instead of a
	// second subprocess.
	if _, cmd := m.Update(vcsTickMsg{}); cmd != nil {
		t.Fatal("mid-flight tick must not launch a second refresh")
	}
	if !m.vcs.dirty {
		t.Fatal("mid-flight tick must mark the state dirty")
	}

	snap := &vcs.Snapshot{Root: "/r", Branch: "main"}
	_, cmd := m.Update(vcs.SnapshotMsg{Snap: snap})
	if m.VCSSnapshot() != snap {
		t.Fatal("snapshot not stored")
	}
	if cmd == nil || !m.vcs.refreshing || m.vcs.dirty {
		t.Fatalf("dirty state must chain a follow-up refresh (cmd=%v refreshing=%v dirty=%v)",
			cmd, m.vcs.refreshing, m.vcs.dirty)
	}
	// The follow-up completes with no further trigger: the chain stops.
	if _, cmd := m.Update(vcs.SnapshotMsg{Snap: nil}); cmd != nil {
		t.Fatal("clean completion must not refresh again")
	}
	if m.VCSSnapshot() != nil {
		t.Fatal("nil snapshot (not a repo) must replace the old one")
	}
}
