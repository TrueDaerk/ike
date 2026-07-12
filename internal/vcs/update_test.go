package vcs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateCmdPullsIncomingCommits(t *testing.T) {
	upstream := testRepo(t)
	clone := t.TempDir()
	gitIn(t, clone, "clone", upstream, ".")

	// No incoming commits: up to date.
	msg := UpdateCmd(clone, "merge")().(UpdateDoneMsg)
	if msg.Err != nil || !msg.UpToDate {
		t.Fatalf("clean update = %+v", msg)
	}

	// One upstream commit: merged with a summary.
	writeIn(t, upstream, "f.txt", "v2\n")
	gitIn(t, upstream, "add", "f.txt")
	gitIn(t, upstream, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "up")
	msg = UpdateCmd(clone, "merge")().(UpdateDoneMsg)
	if msg.Err != nil || msg.UpToDate || msg.Commits != 1 || msg.Files != 1 {
		t.Fatalf("update = %+v", msg)
	}
	got, _ := os.ReadFile(filepath.Join(clone, "f.txt"))
	if string(got) != "v2\n" {
		t.Fatalf("clone content = %q", got)
	}

	// Rebase strategy also runs; no remote at all errors plainly.
	if msg := UpdateCmd(clone, "rebase")().(UpdateDoneMsg); msg.Err != nil || !msg.UpToDate {
		t.Fatalf("rebase update = %+v", msg)
	}
	if msg := UpdateCmd(testRepo(t), "merge")().(UpdateDoneMsg); msg.Err == nil {
		t.Fatal("update without a remote must fail")
	}
}

func TestRevertCmdRestoresHead(t *testing.T) {
	dir := testRepo(t)
	writeIn(t, dir, "f.txt", "dirty\n")
	root, _ := DetectRoot(dir)

	info := RevertInfoCmd(root, filepath.Join(dir, "f.txt"))().(RevertInfoMsg)
	if info.Err != nil || info.Changed != 1 {
		t.Fatalf("revert info = %+v", info)
	}

	done := RevertCmd(root, filepath.Join(dir, "f.txt"))().(RevertDoneMsg)
	if done.Err != nil {
		t.Fatalf("revert: %v", done.Err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(got) != "v1\n" {
		t.Fatalf("content after revert = %q", got)
	}

	// Untracked file: no HEAD version, plain error.
	writeIn(t, dir, "new.txt", "x\n")
	if info := RevertInfoCmd(root, filepath.Join(dir, "new.txt"))().(RevertInfoMsg); info.Err == nil {
		t.Fatal("untracked revert info must fail")
	}
}
