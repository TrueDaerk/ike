package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// testRepo builds a throwaway repository with one committed file.
func testRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-b", "main")
	writeIn(t, dir, "f.txt", "v1\n")
	gitIn(t, dir, "add", "f.txt")
	gitIn(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")
	return dir
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeIn(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStageUnstageCommitRoundtrip(t *testing.T) {
	dir := testRepo(t)
	writeIn(t, dir, "f.txt", "v2\n")

	snap, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Entries) != 1 || snap.Entries[0].Staged() {
		t.Fatalf("setup entries = %+v", snap.Entries)
	}

	if msg := StageCmd(snap.Root, "f.txt")().(OpDoneMsg); msg.Err != nil {
		t.Fatalf("stage: %v", msg.Err)
	}
	snap, _ = Load(dir)
	if !snap.Entries[0].Staged() {
		t.Fatalf("after stage: %+v", snap.Entries)
	}

	if msg := UnstageCmd(snap.Root, "f.txt")().(OpDoneMsg); msg.Err != nil {
		t.Fatalf("unstage: %v", msg.Err)
	}
	snap, _ = Load(dir)
	if snap.Entries[0].Staged() {
		t.Fatalf("after unstage: %+v", snap.Entries)
	}

	// Commit with nothing staged fails loudly.
	if msg := CommitCmd(snap.Root, "empty")().(CommitDoneMsg); msg.Err == nil {
		t.Fatal("empty-index commit did not fail")
	}

	StageCmd(snap.Root, "f.txt")()
	done := CommitCmd(snap.Root, "feat: v2")().(CommitDoneMsg)
	if done.Err != nil || done.Hash == "" || done.Summary != "feat: v2" {
		t.Fatalf("commit = %+v", done)
	}
	snap, _ = Load(dir)
	if len(snap.Entries) != 0 {
		t.Fatalf("tree not clean after commit: %+v", snap.Entries)
	}
}

func TestEntriesPartialStaging(t *testing.T) {
	dir := testRepo(t)
	writeIn(t, dir, "f.txt", "v2\n")
	gitIn(t, dir, "add", "f.txt")
	writeIn(t, dir, "f.txt", "v3\n") // staged v2 + worktree edit on top

	snap, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := snap.Entries[0]
	if !e.Staged() || !e.PartiallyStaged() {
		t.Fatalf("entry = %+v", e)
	}
}
