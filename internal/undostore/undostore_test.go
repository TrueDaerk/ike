package undostore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
)

// snapshotFor builds a one-change snapshot inserting text at the end of base.
func snapshotFor(base, text string) history.Snapshot {
	b := buffer.FromString(base)
	h := history.New()
	e := buffer.Insert(buffer.Position{Line: 0, Col: len(base)}, text)
	inv, end := b.Apply(e)
	h.Push(history.Change{
		Forwards:    []buffer.Edit{e},
		Inverses:    []buffer.Edit{inv},
		CursorAfter: end,
	})
	return h.Snapshot()
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	hash := Hash([]byte("hello world"))
	Save("/tmp/some/file.txt", hash, snapshotFor("hello", " world"))

	snap, ok := Load("/tmp/some/file.txt", hash)
	if !ok {
		t.Fatal("load failed for matching hash")
	}
	if len(snap.Past) != 1 {
		t.Fatalf("past = %d changes, want 1", len(snap.Past))
	}
}

func TestLoadRejectsHashMismatch(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	Save("/tmp/f.txt", Hash([]byte("old content")), snapshotFor("old", "x"))
	if _, ok := Load("/tmp/f.txt", Hash([]byte("new content"))); ok {
		t.Error("load must reject a mismatched content hash")
	}
}

func TestLoadRejectsMissingAndMalformed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	if _, ok := Load("/tmp/nothing.txt", Hash([]byte("x"))); ok {
		t.Error("load must fail on a missing undo file")
	}
	if err := os.MkdirAll(filepath.Join(dir, "undo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileFor("/tmp/bad.txt"), []byte("{garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Load("/tmp/bad.txt", Hash([]byte("x"))); ok {
		t.Error("load must fail on a malformed undo file")
	}
}

func TestSaveEmptySnapshotRemovesFile(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	hash := Hash([]byte("content"))
	Save("/tmp/f.txt", hash, snapshotFor("c", "x"))
	if _, err := os.Stat(fileFor("/tmp/f.txt")); err != nil {
		t.Fatal("undo file should exist after save")
	}
	Save("/tmp/f.txt", hash, history.Snapshot{})
	if _, err := os.Stat(fileFor("/tmp/f.txt")); !os.IsNotExist(err) {
		t.Error("empty snapshot must remove the undo file")
	}
}

func TestSaveSkipsOversizedSnapshot(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	hash := Hash([]byte("content"))
	Save("/tmp/f.txt", hash, snapshotFor("c", "x"))
	big := snapshotFor("c", strings.Repeat("y", maxFileBytes+1))
	Save("/tmp/f.txt", hash, big)
	if _, err := os.Stat(fileFor("/tmp/f.txt")); !os.IsNotExist(err) {
		t.Error("oversized snapshot must remove the stale undo file, not keep it")
	}
	if _, ok := Load("/tmp/f.txt", hash); ok {
		t.Error("oversized snapshot must not be persisted")
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	snap := snapshotFor("a", "b")
	for i := 0; i < maxStoreFiles+10; i++ {
		Save(filepath.Join("/tmp/prune", fileName(i)), Hash([]byte("c")), snap)
	}
	entries, err := os.ReadDir(dir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > maxStoreFiles {
		t.Errorf("store holds %d files, cap is %d", len(entries), maxStoreFiles)
	}
}

func fileName(i int) string {
	return "f" + string(rune('0'+i/100)) + string(rune('0'+(i/10)%10)) + string(rune('0'+i%10)) + ".txt"
}
