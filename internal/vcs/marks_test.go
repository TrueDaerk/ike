package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLineMarksCleanBufferHasNone(t *testing.T) {
	if got := LineMarks("a\nb\n", "a\nb\n"); got != nil {
		t.Fatalf("clean buffer marks = %v", got)
	}
}

func TestLineMarksAddedChangedDeleted(t *testing.T) {
	head := "one\ntwo\nthree\nfour\n"
	buffer := "one\nTWO\nnew\nthree\n" // two changed, new added, four deleted (EOF)
	got := LineMarks(head, buffer)
	want := map[int]LineMark{
		1: LineChanged, // TWO
		2: LineAdded,   // new
	}
	for line, mk := range want {
		if got[line] != mk {
			t.Errorf("line %d = %v, want %v", line, got[line], mk)
		}
	}
	// "four" was removed at EOF: the deletion folds onto the last buffer line.
	if got[3] != LineDeleted {
		t.Errorf("EOF deletion mark = %v, want deleted on last line", got[3])
	}
	if got[0] != 0 {
		t.Errorf("unchanged line 0 marked %v", got[0])
	}
}

func TestLineMarksDeletionBetweenLines(t *testing.T) {
	head := "a\nb\nc\n"
	buffer := "a\nc\n" // b removed between a and c
	got := LineMarks(head, buffer)
	if got[1] != LineDeleted {
		t.Fatalf("marks = %v, want deleted on line 1 (c)", got)
	}
	if got[0] != 0 {
		t.Errorf("line 0 marked %v", got[0])
	}
}

func TestHeadContentRealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	root, err := DetectRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Relative and absolute paths both resolve; content is the HEAD version.
	if got, err := HeadContent(root, "f.txt"); err != nil || got != "v1\n" {
		t.Fatalf("HeadContent rel = %q, %v", got, err)
	}
	if got, err := HeadContent(root, filepath.Join(dir, "f.txt")); err != nil || got != "v1\n" {
		t.Fatalf("HeadContent abs = %q, %v", got, err)
	}
	// Untracked file → error → RefreshMarks resolves to a clearing msg.
	if _, err := HeadContent(root, "nope.txt"); err == nil {
		t.Fatal("untracked HeadContent did not fail")
	}
	msg, ok := RefreshMarks(root, "nope.txt", "x\n")().(MarksMsg)
	if !ok || msg.Marks != nil || msg.Path != "nope.txt" {
		t.Fatalf("RefreshMarks untracked = %#v", msg)
	}
	// Tracked modified file → marks present.
	msg = RefreshMarks(root, "f.txt", "v2\n")().(MarksMsg)
	if msg.Marks[0] != LineChanged {
		t.Fatalf("RefreshMarks marks = %v", msg.Marks)
	}
}
