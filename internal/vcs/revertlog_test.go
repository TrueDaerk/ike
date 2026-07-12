package vcs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Revert history (#556): pre-revert snapshots round-trip through the log,
// newest first, capped and age-pruned; RevertCmd records one automatically.

func TestRevertLogRoundTripNewestFirst(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "f.txt")

	if got := RevertSnapshots(path); got != nil {
		t.Fatalf("empty log = %#v", got)
	}
	SaveRevertSnapshot(path, "one\n", 1)
	SaveRevertSnapshot(path, "two\n", 2)

	snaps := RevertSnapshots(path)
	if len(snaps) != 2 {
		t.Fatalf("len = %d, want 2", len(snaps))
	}
	if snaps[0].Content != "two\n" || snaps[0].Changed != 2 || snaps[1].Content != "one\n" {
		t.Fatalf("order/content wrong: %+v", snaps)
	}
	if snaps[0].At.IsZero() {
		t.Fatal("timestamp not stamped")
	}
}

func TestRevertLogCapsEntries(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "f.txt")
	for i := 0; i < maxRevertEntries+3; i++ {
		SaveRevertSnapshot(path, "v", i)
	}
	snaps := RevertSnapshots(path)
	if len(snaps) != maxRevertEntries {
		t.Fatalf("len = %d, want %d", len(snaps), maxRevertEntries)
	}
	// Newest kept: the last save carried the highest changed count.
	if snaps[0].Changed != maxRevertEntries+2 {
		t.Fatalf("newest changed = %d", snaps[0].Changed)
	}
}

func TestRevertLogPrunesByAge(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "f.txt")
	SaveRevertSnapshot(path, "old\n", 1)

	// Backdate the stored entry past the age cap.
	file := revertFileFor(path)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var env revertEnvelope
	if err := json.Unmarshal(data, &env); err != nil || len(env.Entries) != 1 {
		t.Fatalf("log file unreadable: %v %+v", err, env)
	}
	env.Entries[0].At = time.Now().Add(-maxRevertAge - time.Hour)
	patched, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, patched, 0o644); err != nil {
		t.Fatal(err)
	}

	if got := RevertSnapshots(path); len(got) != 0 {
		t.Fatalf("expired entry survived: %+v", got)
	}
}

func TestRevertLogSkipsOversizedContent(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "f.txt")
	SaveRevertSnapshot(path, string(make([]byte, maxRevertBytes+1)), 1)
	if got := RevertSnapshots(path); len(got) != 0 {
		t.Fatalf("oversized content logged: %d entries", len(got))
	}
}

func TestRevertLogToleratesMalformedFile(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "f.txt")
	SaveRevertSnapshot(path, "v\n", 1)
	if err := os.WriteFile(revertFileFor(path), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := RevertSnapshots(path); got != nil {
		t.Fatalf("malformed file read as %#v", got)
	}
}

func TestRevertCmdRecordsSnapshot(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := testRepo(t)
	writeIn(t, dir, "f.txt", "dirty\n")
	root, _ := DetectRoot(dir)
	path := filepath.Join(dir, "f.txt")

	if done := RevertCmd(root, path)().(RevertDoneMsg); done.Err != nil {
		t.Fatalf("revert: %v", done.Err)
	}
	snaps := RevertSnapshots(path)
	if len(snaps) != 1 || snaps[0].Content != "dirty\n" || snaps[0].Changed != 1 {
		t.Fatalf("snapshot = %+v", snaps)
	}
}
