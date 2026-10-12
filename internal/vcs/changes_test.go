package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// changes_test.go covers the Changes-tab plumbing of #2918: the rename
// origin on status entries and ResolveRev / ChangedFiles against a real
// repository.

func TestParseStatusRenameRecordsOrigin(t *testing.T) {
	out := z(
		"# branch.head main",
		"2 R. N... 100644 100644 100644 aaaa aaaa R100 new/name.go",
		"old/name.go",
		"1 .M N... 100644 100644 100644 aaaa bbbb other.go",
	)
	s := parseStatus(out)
	var renamed *FileEntry
	for i := range s.Entries {
		if s.Entries[i].Path == "new/name.go" {
			renamed = &s.Entries[i]
		}
		if s.Entries[i].Path == "other.go" && s.Entries[i].From != "" {
			t.Errorf("ordinary entry carries From %q", s.Entries[i].From)
		}
	}
	if renamed == nil || renamed.From != "old/name.go" {
		t.Fatalf("renamed entry = %+v, want From old/name.go", renamed)
	}
}

func TestResolveRevAndChangedFilesRealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-b", "main")
	write("keep.go", "package p\n")
	write("mod.go", "package p\n\nfunc f() {}\n")
	write("gone.go", "package p\n\nfunc g() {}\n")
	write("old.go", "package p\n\nfunc old() {}\n// a\n// b\n// c\n// d\n")
	run("add", ".")
	run("commit", "-m", "init")
	main := run("rev-parse", "HEAD")
	run("checkout", "-q", "-b", "feature")
	write("mod.go", "package p\n\nfunc f() { if true {} }\n")
	write("new.go", "package p\n\nfunc n() {}\n")
	run("rm", "-q", "gone.go")
	run("mv", "old.go", "renamed.go")
	run("add", ".")
	run("commit", "-m", "work")
	write("keep.go", "package p\n\n// edited\n")

	root, err := DetectRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	sha, err := ResolveRev(root, "main")
	if err != nil || sha != main {
		t.Fatalf("ResolveRev(main) = %q, %v; want %q", sha, err, main)
	}
	if head, err := ResolveRev(root, ""); err != nil || head == main {
		t.Fatalf("ResolveRev('') must resolve HEAD: %q, %v", head, err)
	}
	if _, err := ResolveRev(root, "no-such-ref"); err == nil || !strings.HasPrefix(err.Error(), "git: ") {
		t.Fatalf("invalid ref error = %v", err)
	}

	files, err := ChangedFiles(root, "main")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]CommitFile{}
	for _, f := range files {
		got[f.Path] = f
	}
	for path, want := range map[string]CommitFile{
		"keep.go":    {Path: "keep.go", Status: StatusModified},
		"mod.go":     {Path: "mod.go", Status: StatusModified},
		"new.go":     {Path: "new.go", Status: StatusAdded},
		"gone.go":    {Path: "gone.go", Status: StatusDeleted},
		"renamed.go": {Path: "renamed.go", OldPath: "old.go", Status: StatusRenamed},
	} {
		if got[path] != want {
			t.Errorf("%s = %+v, want %+v", path, got[path], want)
		}
	}
	if len(files) != 5 {
		t.Errorf("files = %+v", files)
	}
	// Against HEAD only the unstaged edit differs.
	files, err = ChangedFiles(root, "HEAD")
	if err != nil || len(files) != 1 || files[0].Path != "keep.go" {
		t.Fatalf("ChangedFiles(HEAD) = %+v, %v", files, err)
	}
	// The base content of the renamed file is reachable under its old name.
	if content, err := RevContent(root, "main", "old.go"); err != nil || !strings.Contains(content, "func old()") {
		t.Fatalf("RevContent(main, old.go) = %q, %v", content, err)
	}
}
