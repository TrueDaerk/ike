package app

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/changefeed"
	"ike/internal/watch"
)

// TestWatchBatchRoutesEveryEvent (#2176): one EventBatchMsg applies all its
// events — the change feed records each file and an open clean buffer's
// pre-change content is captured inline before routing reloads it.
func TestWatchBatchRoutesEveryEvent(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.openPath(a, false)
	m = tm.(Model)

	if err := os.WriteFile(a, []byte("AGENT A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("AGENT B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.Update(watch.EventBatchMsg{Events: []watch.EventMsg{
		{Kind: watch.FileChanged, Path: a},
		{Kind: watch.FileCreated, Path: b},
	}})
	m = drainCmd(tm.(Model), cmd)

	if m.feed.Len() != 2 {
		t.Fatalf("feed = %d entries after a 2-event batch, want 2", m.feed.Len())
	}
	ea, ok := m.feed.Get(absTestPath(a))
	if !ok || ea.Origin != changefeed.FromBuffer || ea.Before != "one" {
		t.Fatalf("a.txt entry = %+v, want the pre-change buffer text captured inline", ea)
	}
	if eb, ok := m.feed.Get(absTestPath(b)); !ok || eb.Kind != changefeed.Created {
		t.Fatalf("b.txt entry = %+v, want a Created entry", eb)
	}
	if ed := m.editorForPath(a); ed == nil || ed.Text() != "AGENT A" {
		t.Fatal("the batch did not route the reload to the open buffer")
	}
}

// TestWatchBatchDefersSnapshotCapture (#2176): a batched event for a file
// with no open buffer resolves its pre-change content off-loop — the returned
// command reads the local-history snapshot and lands it via
// changeFeedCapturedMsg.
func TestWatchBatchDefersSnapshotCapture(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("saved by ike\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m.lhStore.Record(path, []byte("saved by ike\n"))
	if err := os.WriteFile(path, []byte("AGENT\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tm, cmd := m.Update(watch.EventBatchMsg{Events: []watch.EventMsg{
		{Kind: watch.FileChanged, Path: path},
	}})
	m = tm.(Model)
	if m.feed.Len() != 0 {
		t.Fatalf("snapshot-backed entry recorded inline: %+v", m.feed.Entries())
	}
	m = drainCmd(m, cmd)
	e, ok := m.feed.Get(absTestPath(path))
	if !ok {
		t.Fatalf("no entry after the deferred capture; feed holds %+v", m.feed.Entries())
	}
	if e.Origin != changefeed.FromSnapshot || e.Before != "saved by ike" {
		t.Fatalf("entry = %+v, want the local-history snapshot as Before", e)
	}
}
