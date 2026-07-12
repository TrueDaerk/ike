package vcs

import (
	"strings"
	"testing"
)

// historyRepo builds a repo with three commits touching f.txt and adding
// b.txt, plus a rename in the last commit.
func historyRepo(t *testing.T) string {
	t.Helper()
	dir := testRepo(t) // commit 1: "init" adds f.txt (v1)
	commit := func(msg string) {
		gitIn(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", msg)
	}
	writeIn(t, dir, "f.txt", "v2\n")
	writeIn(t, dir, "b.txt", "b\n")
	gitIn(t, dir, "add", ".")
	commit("second: edit f, add b")
	gitIn(t, dir, "mv", "b.txt", "c.txt")
	commit("third: rename b to c")
	return dir
}

func TestLogCmdWindowsAndPaging(t *testing.T) {
	dir := historyRepo(t)
	root, _ := DetectRoot(dir)

	msg := LogCmd(root, 0, 2)().(LogMsg)
	if msg.Err != nil || len(msg.Entries) != 2 || !msg.HasMore {
		t.Fatalf("first window = %+v", msg)
	}
	if msg.Entries[0].Subject != "third: rename b to c" || msg.Entries[1].Subject != "second: edit f, add b" {
		t.Fatalf("order wrong: %+v", msg.Entries)
	}
	e := msg.Entries[0]
	if e.Author != "t" || e.ShortHash == "" || len(e.Hash) != 40 || e.Time.IsZero() {
		t.Fatalf("entry fields = %+v", e)
	}

	msg = LogCmd(root, 2, 2)().(LogMsg)
	if msg.Err != nil || len(msg.Entries) != 1 || msg.HasMore || msg.Offset != 2 {
		t.Fatalf("second window = %+v", msg)
	}
	if msg.Entries[0].Subject != "init" {
		t.Fatalf("tail subject = %q", msg.Entries[0].Subject)
	}

	// Not a repo: plain error.
	if msg := LogCmd(t.TempDir(), 0, 10)().(LogMsg); msg.Err == nil {
		t.Fatal("log outside a repo must fail")
	}
}

func TestShowCmdFilesAndRename(t *testing.T) {
	dir := historyRepo(t)
	root, _ := DetectRoot(dir)
	log := LogCmd(root, 0, 3)().(LogMsg)

	// Second commit: modified f.txt + added b.txt.
	show := ShowCmd(root, log.Entries[1].Hash)().(ShowMsg)
	if show.Err != nil || show.Entry.Subject != "second: edit f, add b" {
		t.Fatalf("show = %+v", show)
	}
	byPath := map[string]CommitFile{}
	for _, f := range show.Files {
		byPath[f.Path] = f
	}
	if byPath["f.txt"].Status != StatusModified || byPath["b.txt"].Status != StatusAdded {
		t.Fatalf("files = %+v", show.Files)
	}

	// Third commit: rename with OldPath.
	show = ShowCmd(root, log.Entries[0].Hash)().(ShowMsg)
	if len(show.Files) != 1 {
		t.Fatalf("rename files = %+v", show.Files)
	}
	if f := show.Files[0]; f.Status != StatusRenamed || f.Path != "c.txt" || f.OldPath != "b.txt" {
		t.Fatalf("rename entry = %+v", f)
	}

	if show := ShowCmd(root, "deadbeef")().(ShowMsg); show.Err == nil {
		t.Fatal("show of a bad hash must fail")
	}
}

func TestFileAtCmdSides(t *testing.T) {
	dir := historyRepo(t)
	root, _ := DetectRoot(dir)
	log := LogCmd(root, 0, 3)().(LogMsg)
	second, root0 := log.Entries[1], log.Entries[2]

	// Modified file: both sides present.
	at := FileAtCmd(root, second.Hash, "f.txt", "")().(FileAtMsg)
	if at.Err != nil || at.Parent != "v1\n" || at.Content != "v2\n" {
		t.Fatalf("modified sides = %+v", at)
	}
	// Added file: parent side empty.
	at = FileAtCmd(root, second.Hash, "b.txt", "")().(FileAtMsg)
	if at.Err != nil || at.Parent != "" || at.Content != "b\n" {
		t.Fatalf("added sides = %+v", at)
	}
	// Rename: parent read through OldPath.
	at = FileAtCmd(root, log.Entries[0].Hash, "c.txt", "b.txt")().(FileAtMsg)
	if at.Err != nil || at.Parent != "b\n" || at.Content != "b\n" {
		t.Fatalf("rename sides = %+v", at)
	}
	// Root commit: parent side empty, no error.
	at = FileAtCmd(root, root0.Hash, "f.txt", "")().(FileAtMsg)
	if at.Err != nil || at.Parent != "" || at.Content != "v1\n" {
		t.Fatalf("root sides = %+v", at)
	}
	// Bad revision: error.
	if at := FileAtCmd(root, strings.Repeat("d", 40), "f.txt", "")().(FileAtMsg); at.Err == nil {
		t.Fatal("bad revision must fail")
	}
}
