package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// z joins porcelain v2 records with NUL, the way `git status -z` emits them.
func z(records ...string) []byte {
	return []byte(strings.Join(records, "\x00") + "\x00")
}

func TestParseStatusBranchAndFiles(t *testing.T) {
	out := z(
		"# branch.oid 1234567890abcdef1234567890abcdef12345678",
		"# branch.head main",
		"# branch.ab +2 -1",
		"1 .M N... 100644 100644 100644 aaaa bbbb internal/app/app.go",
		"1 A. N... 000000 100644 100644 0000 cccc internal/vcs/vcs.go",
		"1 .D N... 100644 100644 000000 dddd 0000 docs/old.md",
		"? notes.txt",
	)
	s := parseStatus(out)
	if s.Branch != "main" || s.Detached {
		t.Fatalf("branch = %q detached=%v", s.Branch, s.Detached)
	}
	if s.Ahead != 2 || s.Behind != 1 {
		t.Fatalf("ahead/behind = %d/%d", s.Ahead, s.Behind)
	}
	want := map[string]FileStatus{
		"internal/app/app.go": StatusModified,
		"internal/vcs/vcs.go": StatusAdded,
		"docs/old.md":         StatusDeleted,
		"notes.txt":           StatusUntracked,
	}
	for p, st := range want {
		if got := s.Files[p]; got != st {
			t.Errorf("Files[%q] = %v, want %v", p, got, st)
		}
	}
	if len(s.Files) != len(want) {
		t.Errorf("got %d files, want %d", len(s.Files), len(want))
	}
}

func TestParseStatusRenameConsumesOrigPath(t *testing.T) {
	out := z(
		"# branch.head main",
		"2 R. N... 100644 100644 100644 aaaa aaaa R100 new/name.go",
		"old/name.go",
		"1 .M N... 100644 100644 100644 aaaa bbbb other.go",
	)
	s := parseStatus(out)
	if got := s.Files["new/name.go"]; got != StatusRenamed {
		t.Errorf("renamed file = %v", got)
	}
	if _, ok := s.Files["old/name.go"]; ok {
		t.Errorf("orig path leaked into Files")
	}
	if got := s.Files["other.go"]; got != StatusModified {
		t.Errorf("entry after rename = %v", got)
	}
}

func TestParseStatusUnmergedAndDetached(t *testing.T) {
	out := z(
		"# branch.oid 1234567890abcdef1234567890abcdef12345678",
		"# branch.head (detached)",
		"u UU N... 100644 100644 100644 100644 a b c conflicted.go",
	)
	s := parseStatus(out)
	if !s.Detached || s.Branch != "1234567" {
		t.Fatalf("detached=%v branch=%q", s.Detached, s.Branch)
	}
	if got := s.Files["conflicted.go"]; got != StatusConflicted {
		t.Errorf("conflicted = %v", got)
	}
}

func TestParseStatusPathWithSpaces(t *testing.T) {
	out := z("1 .M N... 100644 100644 100644 aaaa bbbb dir with space/my file.go")
	s := parseStatus(out)
	if got := s.Files["dir with space/my file.go"]; got != StatusModified {
		t.Errorf("spaced path = %v", got)
	}
}

func TestSnapshotStatusAndDirDirty(t *testing.T) {
	s := parseStatus(z("1 .M N... 100644 100644 100644 aaaa bbbb internal/app/app.go"))
	s.Root = filepath.FromSlash("/work/repo")

	if got := s.Status("internal/app/app.go"); got != StatusModified {
		t.Errorf("rel status = %v", got)
	}
	if got := s.Status(filepath.FromSlash("/work/repo/internal/app/app.go")); got != StatusModified {
		t.Errorf("abs status = %v", got)
	}
	if got := s.Status(filepath.FromSlash("/elsewhere/app.go")); got != StatusNone {
		t.Errorf("outside repo = %v", got)
	}
	if got := s.Status("internal/app/clean.go"); got != StatusNone {
		t.Errorf("clean file = %v", got)
	}

	for _, dir := range []string{"internal", "internal/app", ""} {
		if !s.DirDirty(dir) {
			t.Errorf("DirDirty(%q) = false", dir)
		}
	}
	if !s.DirDirty(filepath.FromSlash("/work/repo/internal")) {
		t.Errorf("abs DirDirty = false")
	}
	if s.DirDirty("docs") {
		t.Errorf("clean dir reported dirty")
	}
}

func TestNilSnapshotIsClean(t *testing.T) {
	var s *Snapshot
	if s.Status("x.go") != StatusNone || s.DirDirty("x") {
		t.Fatal("nil snapshot must be clean")
	}
}

func TestStatusBadges(t *testing.T) {
	want := map[FileStatus]string{
		StatusNone: "", StatusModified: "M", StatusAdded: "A", StatusDeleted: "D",
		StatusRenamed: "R", StatusUntracked: "?", StatusConflicted: "U",
	}
	for st, badge := range want {
		if st.String() != badge {
			t.Errorf("%d.String() = %q, want %q", st, st.String(), badge)
		}
	}
}

// TestLoadRealRepo exercises DetectRoot/Load against a throwaway repository;
// skipped when git is not on PATH.
func TestLoadRealRepo(t *testing.T) {
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
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	root, err := DetectRoot(dir)
	if err != nil {
		t.Fatalf("DetectRoot: %v", err)
	}
	snap, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap.Root != root {
		t.Errorf("root %q != %q", snap.Root, root)
	}
	if snap.Branch != "main" {
		t.Errorf("branch = %q", snap.Branch)
	}
	if got := snap.Status("new.txt"); got != StatusUntracked {
		t.Errorf("new.txt = %v", got)
	}

	// Not a repo: Load must fail, Refresh must resolve to a nil snapshot.
	outside := t.TempDir()
	if _, err := Load(outside); err == nil {
		t.Error("Load outside a repo did not fail")
	}
	if msg, ok := Refresh(outside)().(SnapshotMsg); !ok || msg.Snap != nil {
		t.Errorf("Refresh outside a repo = %#v", msg)
	}
}
