package editor

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// sharedPair loads a file into view a and makes b a second view of the same
// document.
func sharedPair(t *testing.T, content string) (a, b Model, path string) {
	t.Helper()
	a, path = loaded(t, content)
	b = New()
	b.SetSize(80, 20)
	b.ShareDocumentWith(&a)
	return a, b, path
}

func TestSharedDocumentMirrorsEdits(t *testing.T) {
	a, b, _ := sharedPair(t, "one\ntwo\n")
	a = send(a, key('i'), key('X'), special(tea.KeyEscape))
	if !strings.Contains(b.Text(), "Xone") {
		t.Fatalf("edit in one view must be visible in the other: %q", b.Text())
	}
	if !b.SharesBufferWith(&a) {
		t.Fatal("views must alias one buffer")
	}
}

func TestSharedDocumentSharesUndoStack(t *testing.T) {
	a, b, _ := sharedPair(t, "one\ntwo\n")
	a = send(a, key('d'), key('d')) // delete "one" in view a
	if strings.Contains(b.Text(), "one") {
		t.Fatalf("test setup: delete must be visible in b, got %q", b.Text())
	}
	b = send(b, key('u')) // undo in the OTHER view
	if !strings.Contains(a.Text(), "one") || !strings.Contains(b.Text(), "one") {
		t.Fatalf("undo from either view must revert the shared change: %q", b.Text())
	}
}

func TestSyncClampsCursorAndMirrorsFlags(t *testing.T) {
	a, b, path := sharedPair(t, strings.Repeat("line\n", 10))
	b.SetCursor(9, 2)
	a = send(a, keys("dddddddd")...) // shrink the shared buffer in view a
	b, _ = b.Update(SyncMsg{Path: path, FromKey: "editor:a", Dirty: true})
	if l, _ := b.CursorPos(); l > b.buf.LineCount()-1 {
		t.Fatalf("sync must clamp the cursor into the shrunk buffer, line=%d", l)
	}
	if !b.Dirty() {
		t.Fatal("sync must mirror the dirty flag")
	}
	b, _ = b.Update(SyncMsg{Path: path, FromKey: "editor:a", Dirty: false})
	if b.Dirty() {
		t.Fatal("a save sync must clear the dirty flag")
	}
}

func TestSyncIgnoresOtherPaths(t *testing.T) {
	_, b, _ := sharedPair(t, "one\n")
	before := b.docVersion
	b, _ = b.Update(SyncMsg{Path: "/elsewhere.txt", Dirty: true})
	if b.Dirty() || b.docVersion != before {
		t.Fatal("a sync for another file must be a no-op")
	}
}

func TestReloadKeepsSharingIntact(t *testing.T) {
	a, b, path := sharedPair(t, "one\ntwo\n")
	if err := writeFile(path, "fresh\n"); err != nil {
		t.Fatal(err)
	}
	a, _ = a.reloadFromDisk()
	if !strings.Contains(b.Text(), "fresh") {
		t.Fatalf("in-place reload must reach the other view: %q", b.Text())
	}
	if !b.SharesBufferWith(&a) {
		t.Fatal("reload must not break the buffer alias")
	}
	b = send(b, key('u'))
	if got := strings.TrimRight(b.Text(), "\n"); got != "fresh" {
		t.Fatalf("the shared undo stack must be reset in place, got %q", got)
	}
}
