package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/backup"
)

// recovery_dialog_test.go covers the centered restore dialog (#2160): the diff
// preview per file, the restore-all / discard-all answers, the undoable
// restore path, and esc deferring the whole decision.

// plainShell renders the floating shell and strips styling so assertions read
// the dialog's text.
func plainShell(m Model) string { return ansiSeq.ReplaceAllString(m.shell.View(), "") }

func TestRecoveryDialogShowsDiffPreview(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		f := filepath.Join(dir, "work.txt")
		_ = os.WriteFile(f, []byte("keep\nold line\n"), 0o644)
		_ = svc.Snapshot(backup.Doc{Key: f, Path: f, Text: "keep\nnew line\n"})
	})
	v := plainShell(m)
	if !strings.Contains(v, "- old line") || !strings.Contains(v, "+ new line") {
		t.Fatalf("dialog must preview the snapshot against the file on disk: %q", v)
	}
	if !strings.Contains(v, "restore all") || !strings.Contains(v, "discard all") {
		t.Fatalf("dialog must offer the all-files answers: %q", v)
	}
}

func TestRecoveryDiffFollowsSelection(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		for _, n := range []string{"a.txt", "b.txt"} {
			f := filepath.Join(dir, n)
			_ = os.WriteFile(f, []byte("disk "+n+"\n"), 0o644)
			_ = svc.Snapshot(backup.Doc{Key: f, Path: f, Text: "snap " + n + "\n"})
		}
	})
	if v := plainShell(m); !strings.Contains(v, "+ snap a.txt") {
		t.Fatalf("first selection previews a.txt: %q", v)
	}
	m = answer(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	v := plainShell(m)
	if !strings.Contains(v, "+ snap b.txt") || strings.Contains(v, "+ snap a.txt") {
		t.Fatalf("preview must follow the cursor to b.txt: %q", v)
	}
}

func TestRecoveryDiffEmptyWhenSnapshotMatchesDisk(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		f := filepath.Join(dir, "same.txt")
		_ = os.WriteFile(f, []byte("identical\n"), 0o644)
		_ = svc.Snapshot(backup.Doc{Key: f, Path: f, Text: "identical\n"})
	})
	if v := plainShell(m); !strings.Contains(v, "no differences") {
		t.Fatalf("a snapshot equal to the file on disk must say so: %q", v)
	}
}

func TestRecoveryDiffMissingBaseFile(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		f := filepath.Join(dir, "gone.txt")
		_ = svc.Snapshot(backup.Doc{Key: f, Path: f, Text: "only in the snapshot\n"})
	})
	v := plainShell(m)
	if !strings.Contains(v, "no file on disk") {
		t.Fatalf("a deleted base must be called out: %q", v)
	}
	if !strings.Contains(v, "+ only in the snapshot") {
		t.Fatalf("the whole snapshot reads as added: %q", v)
	}
}

func TestRecoveryRestoreIsUndoable(t *testing.T) {
	var file string
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		file = filepath.Join(dir, "work.txt")
		_ = os.WriteFile(file, []byte("on disk\n"), 0o644)
		_ = svc.Snapshot(backup.Doc{Key: file, Path: file, Text: "recovered"})
	})
	m = answer(m, tea.KeyPressMsg{Code: 'r', Text: "r"})
	ed := m.activeEditor()
	if ed == nil || ed.Text() != "recovered" {
		t.Fatalf("restore must put the recovered text into the buffer, got %q", ed.Text())
	}
	// The restore went through the normal edit path: undo brings the on-disk
	// version back instead of leaving the user stuck with the snapshot.
	tm, _ := m.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	m = tm.(Model)
	if got := m.activeEditor().Text(); got != "on disk" {
		t.Fatalf("undo after restore = %q, want the on-disk content back", got)
	}
}

func TestRecoveryRestoreAll(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		for _, n := range []string{"a.txt", "b.txt"} {
			f := filepath.Join(dir, n)
			_ = os.WriteFile(f, []byte("disk\n"), 0o644)
			_ = svc.Snapshot(backup.Doc{Key: f, Path: f, Text: "recovered " + n})
		}
	})
	m = answer(m, tea.KeyPressMsg{Code: 'R', Text: "R"})
	if m.recoveryOpen() {
		t.Fatal("restore all closes the dialog")
	}
	if remainingSnapshots(t) != 0 {
		t.Fatalf("restore all must consume every snapshot, have %d", remainingSnapshots(t))
	}
	texts := map[string]bool{}
	for _, key := range m.activeWS().Panes.Keys() {
		if ed := m.activeWS().Panes.Get(key).Editor(); ed != nil {
			texts[ed.Text()] = true
		}
	}
	if !texts["recovered b.txt"] {
		t.Fatalf("the last restored file must be open, have %v", texts)
	}
}

func TestRecoveryDiscardAll(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		for _, n := range []string{"a.txt", "b.txt"} {
			f := filepath.Join(dir, n)
			_ = os.WriteFile(f, []byte("disk\n"), 0o644)
			_ = svc.Snapshot(backup.Doc{Key: f, Path: f, Text: "r"})
		}
	})
	m = answer(m, tea.KeyPressMsg{Code: 'D', Text: "D"})
	if m.recoveryOpen() {
		t.Fatal("discard all closes the dialog")
	}
	if remainingSnapshots(t) != 0 {
		t.Fatalf("discard all must remove every snapshot, have %d", remainingSnapshots(t))
	}
	if ed := m.activeEditor(); ed != nil && strings.Contains(ed.Text(), "r") && ed.Dirty() {
		t.Fatal("discard all must not open any recovered buffer")
	}
}

func TestRecoveryEscDefersEveryUndecidedFile(t *testing.T) {
	// esc is a deferral, not a silent discard: every snapshot the user has not
	// answered survives to the next launch (#2160).
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
			f := filepath.Join(dir, n)
			_ = os.WriteFile(f, []byte("disk\n"), 0o644)
			_ = svc.Snapshot(backup.Doc{Key: f, Path: f, Text: "r"})
		}
	})
	m = answer(m, tea.KeyPressMsg{Code: 'd', Text: "d"}) // decide one
	m = answer(m, tea.KeyPressMsg{Code: tea.KeyEscape})  // defer the rest
	if got := remainingSnapshots(t); got != 2 {
		t.Fatalf("esc must keep the 2 undecided snapshots, have %d", got)
	}
	if m.recoveryOpen() {
		t.Fatal("esc closes the dialog")
	}
}

func TestRecoveryDialogCompositedIntoRootView(t *testing.T) {
	// The shell is content-sized and the compositor drops a box that does not
	// fit the terminal — an over-wide key legend made the whole dialog vanish
	// instead of wrapping (#2160).
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		f := filepath.Join(dir, "work.txt")
		_ = os.WriteFile(f, []byte("on disk\n"), 0o644)
		_ = svc.Snapshot(backup.Doc{Key: f, Path: f, Text: "recovered\n"})
	})
	for _, size := range []tea.WindowSizeMsg{{Width: 100, Height: 30}, {Width: 60, Height: 24}} {
		tm, _ := m.Update(size)
		view := ansiSeq.ReplaceAllString(tm.(Model).View().Content, "")
		if !strings.Contains(view, "Recover unsaved changes") {
			t.Fatalf("dialog missing from the %dx%d root view:\n%s", size.Width, size.Height, view)
		}
		for _, line := range strings.Split(view, "\n") {
			if len([]rune(line)) > size.Width {
				t.Fatalf("%dx%d view line overflows the terminal: %q", size.Width, size.Height, line)
			}
		}
	}
}
