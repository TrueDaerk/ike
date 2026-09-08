package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/backup"
	"ike/internal/editor"
)

// editorsync_test.go covers the settled-pass sync of #2541: an edit's
// document-change sync applies inside the keystroke's own Update pass
// instead of travelling through the loop as an editor.SyncMsg of its own.

func TestEditorSyncQueueFoldsAndDrainsInOrder(t *testing.T) {
	var q editorSyncQueue
	q.push(editor.SyncMsg{Path: "/a", FromKey: "1"})
	q.push(editor.SyncMsg{Path: "/b", FromKey: "1"})
	q.push(editor.SyncMsg{Path: "/a", FromKey: "1"}) // same document, same origin: folds
	q.push(editor.SyncMsg{Path: "/a", FromKey: "2"}) // another view of it: its own sync
	got := q.drain()
	if len(got) != 3 || got[0].Path != "/a" || got[1].Path != "/b" || got[2].FromKey != "2" {
		t.Fatalf("drain = %+v, want a/1, b/1, a/2", got)
	}
	if rest := q.drain(); len(rest) != 0 {
		t.Fatalf("a drained queue must be empty, got %+v", rest)
	}
}

// TestKeystrokeSyncAppliesInTheSamePass: typing into a dirty buffer arms the
// crash-recovery debounce during the key's Update — no SyncMsg is fed back
// through the loop, and nothing is left queued.
func TestKeystrokeSyncAppliesInTheSamePass(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {})
	file := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "work.txt")
	if err := os.WriteFile(file, []byte("on disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(file, false)
	m = dismissOnboarding(tm.(Model)) // the first-start dialog would swallow the keys
	if m.activeEditorKey() == "" {
		t.Fatal("open must focus an editor")
	}
	if m.backupDeb.Pending() != 0 {
		t.Fatalf("setup: nothing pending before the edit, got %d", m.backupDeb.Pending())
	}
	// Enter insert mode and type one character: the two Updates alone, no
	// command drained, no message fed back.
	tm, _ = m.Update(tea.KeyPressMsg{Text: "i", Code: 'i'})
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyPressMsg{Text: "x", Code: 'x'})
	m = tm.(Model)
	if m.backupDeb.Pending() != 1 {
		t.Fatalf("the keystroke's own pass must apply the sync (backup armed), pending = %d", m.backupDeb.Pending())
	}
	if left := m.editorSyncs.drain(); len(left) != 0 {
		t.Fatalf("the settled pass must drain the queue, left %+v", left)
	}
}

// TestSyncMsgThroughTheLoopStillApplies: a sync fed as a message (a bare
// emitter, a test) takes the same handler.
func TestSyncMsgThroughTheLoopStillApplies(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {})
	file := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "work.txt")
	if err := os.WriteFile(file, []byte("on disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, key := openDirtyKey(t, m, file)
	tm, _ := m.Update(editor.SyncMsg{Path: file, FromKey: key})
	m = tm.(Model)
	if m.backupDeb.Pending() != 1 {
		t.Fatalf("a looped SyncMsg must still arm the debounce, pending = %d", m.backupDeb.Pending())
	}
}
