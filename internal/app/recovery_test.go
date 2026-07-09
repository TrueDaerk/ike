package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/backup"
)

// recoverySeed builds a sized app whose config dir already holds the snapshots
// written by seed — a crash simulation: the previous session left them behind.
func recoverySeed(t *testing.T, seed func(svc *backup.Service, dir string)) Model {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	seed(backup.New(backup.Dir(dir), nil), dir)
	m := New()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return tm.(Model)
}

func remainingSnapshots(t *testing.T) int {
	t.Helper()
	snaps, err := backupService().List()
	if err != nil {
		t.Fatal(err)
	}
	return len(snaps)
}

func TestRecoveryPromptShownAtStartup(t *testing.T) {
	var file string
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		file = filepath.Join(dir, "work.txt")
		_ = os.WriteFile(file, []byte("on disk\n"), 0o644)
		_ = svc.Snapshot(backup.Doc{Key: file, Path: file, Text: "recovered\n"})
	})
	if !m.recoveryOpen() {
		t.Fatal("leftover snapshot must open the recovery prompt")
	}
	v := m.shell.View()
	if !strings.Contains(v, "work.txt") || !strings.Contains(v, "restore") {
		t.Fatalf("prompt missing file/choices: %q", v)
	}
}

func TestRecoveryNoPromptWhenClean(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {})
	if m.recoveryOpen() {
		t.Fatal("no snapshots ⇒ no prompt")
	}
}

func TestRecoveryRestoreOpensDirtyBuffer(t *testing.T) {
	var file string
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		file = filepath.Join(dir, "work.txt")
		_ = os.WriteFile(file, []byte("on disk\n"), 0o644)
		_ = svc.Snapshot(backup.Doc{Key: file, Path: file, Text: "recovered text"})
	})
	m = answer(m, tea.KeyPressMsg{Code: 'r', Text: "r"})

	ed := m.activeEditor()
	if ed == nil {
		t.Fatal("restore must focus an editor")
	}
	if got := ed.Text(); got != "recovered text" {
		t.Fatalf("editor text = %q, want the recovered text", got)
	}
	if !ed.Dirty() {
		t.Fatal("restored buffer must be marked dirty")
	}
	if ed.Path() != file {
		t.Fatalf("restored buffer path = %q, want %q", ed.Path(), file)
	}
	if remainingSnapshots(t) != 0 {
		t.Fatal("restoring must consume the snapshot")
	}
	if m.recoveryOpen() {
		t.Fatal("prompt must close after the last item is decided")
	}
}

func TestRecoveryDiscardRemovesSnapshot(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		a := filepath.Join(dir, "a.txt")
		b := filepath.Join(dir, "b.txt")
		_ = os.WriteFile(a, []byte("a\n"), 0o644)
		_ = os.WriteFile(b, []byte("b\n"), 0o644)
		_ = svc.Snapshot(backup.Doc{Key: a, Path: a, Text: "ra\n"})
		_ = svc.Snapshot(backup.Doc{Key: b, Path: b, Text: "rb\n"})
	})
	if remainingSnapshots(t) != 2 {
		t.Fatalf("setup should have 2 snapshots")
	}
	// Discard the highlighted one; the other stays and the prompt remains open.
	m = answer(m, tea.KeyPressMsg{Code: 'd', Text: "d"})
	if remainingSnapshots(t) != 1 {
		t.Fatalf("discard must remove exactly one snapshot, have %d", remainingSnapshots(t))
	}
	if !m.recoveryOpen() {
		t.Fatal("prompt should stay open while items remain")
	}
}

func TestRecoverySkipKeepsSnapshot(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		f := filepath.Join(dir, "keep.txt")
		_ = os.WriteFile(f, []byte("x\n"), 0o644)
		_ = svc.Snapshot(backup.Doc{Key: f, Path: f, Text: "r\n"})
	})
	m = answer(m, tea.KeyPressMsg{Code: 's', Text: "s"})
	if remainingSnapshots(t) != 1 {
		t.Fatal("skip must keep the snapshot for next launch")
	}
	if m.recoveryOpen() {
		t.Fatal("prompt closes once every item is decided")
	}
}

func TestRecoveryEscSkipsAll(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		for _, n := range []string{"a.txt", "b.txt"} {
			f := filepath.Join(dir, n)
			_ = os.WriteFile(f, []byte("x\n"), 0o644)
			_ = svc.Snapshot(backup.Doc{Key: f, Path: f, Text: "r\n"})
		}
	})
	m = answer(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if remainingSnapshots(t) != 2 {
		t.Fatal("esc must keep all remaining snapshots")
	}
	if m.recoveryOpen() {
		t.Fatal("esc closes the prompt")
	}
}

func TestRecoveryBaseChangedWarning(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		f := filepath.Join(dir, "moved.txt")
		_ = os.WriteFile(f, []byte("current on-disk content\n"), 0o644)
		// A snapshot taken against a different base version (wrong hash).
		_ = svc.Snapshot(backup.Doc{Key: f, Path: f, Text: "r\n", BaseHash: "stalehash", BaseMTime: time.Unix(1, 0)})
	})
	if v := m.shell.View(); !strings.Contains(v, "changed on disk") {
		t.Fatalf("base-changed file must be flagged: %q", v)
	}
}

func TestRecoveryUntitledRestoresUnsavedBuffer(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		_ = svc.Snapshot(backup.Doc{Key: "untitled:1", Path: "", Text: "scratch notes"})
	})
	if v := m.shell.View(); !strings.Contains(v, "untitled") {
		t.Fatalf("untitled buffer should be listed: %q", v)
	}
	m = answer(m, tea.KeyPressMsg{Code: 'r', Text: "r"})
	ed := m.activeEditor()
	if ed == nil || ed.Text() != "scratch notes" || !ed.Dirty() {
		t.Fatalf("untitled restore: text=%q dirty=%v", ed.Text(), ed.Dirty())
	}
	if ed.Path() != "" {
		t.Fatalf("untitled restore must have no path, got %q", ed.Path())
	}
}
