package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// marks_changed_test.go covers the pass-saving recompute of #2541: a marks
// refresh that lands on what the editor already shows resolves to nil.

func TestMarksEqual(t *testing.T) {
	a := map[int]LineMark{1: LineChanged, 3: LineAdded}
	if !MarksEqual(a, map[int]LineMark{3: LineAdded, 1: LineChanged}) {
		t.Fatal("same marks in any order are equal")
	}
	if MarksEqual(a, map[int]LineMark{1: LineChanged}) {
		t.Fatal("a missing mark is a difference")
	}
	if MarksEqual(a, map[int]LineMark{1: LineChanged, 3: LineDeleted}) {
		t.Fatal("a different kind is a difference")
	}
	if !MarksEqual(nil, map[int]LineMark{}) || !MarksEqual(nil, nil) {
		t.Fatal("nil and empty are the same clean gutter")
	}
}

func TestRefreshMarksIfChangedRealRepo(t *testing.T) {
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
	root, err := DetectRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The gutter shows nothing yet: the changed line is a message.
	msg, ok := RefreshMarksIfChanged(root, "f.txt", "v2\n", nil)().(MarksMsg)
	if !ok || msg.Marks[0] != LineChanged {
		t.Fatalf("first recompute = %#v, want a changed mark on line 0", msg)
	}
	// The same buffer against the marks it produced: nothing to deliver.
	if got := RefreshMarksIfChanged(root, "f.txt", "v2\n", msg.Marks)(); got != nil {
		t.Fatalf("unchanged marks must resolve to nil, got %#v", got)
	}
	// Reverted to HEAD while marks show: the clearing message goes out.
	msg, ok = RefreshMarksIfChanged(root, "f.txt", "v1\n", msg.Marks)().(MarksMsg)
	if !ok || msg.Marks != nil || msg.Path != "f.txt" {
		t.Fatalf("clean buffer over marks = %#v, want a clearing message", msg)
	}
	// Untracked and clean gutter: nil, no message for a gutter already clean.
	if got := RefreshMarksIfChanged(root, "nope.txt", "x\n", nil)(); got != nil {
		t.Fatalf("untracked over a clean gutter = %#v, want nil", got)
	}
}
