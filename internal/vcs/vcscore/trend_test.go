package vcscore

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// trend_test.go covers the trend plumbing (#2962): the raw-diff and
// ls-tree parsers on canned output, and the log / tree / diff / batch
// helpers against a real repository.

func TestParseRawDiff(t *testing.T) {
	out := []byte(":100644 100644 aaaa bbbb M\x00a.go\x00" +
		":000000 100644 " + nullBlob + " cccc A\x00new.go\x00" +
		":100644 000000 dddd " + nullBlob + " D\x00gone.go\x00" +
		":100644 100644 eeee eeee R100\x00old.go\x00renamed.go\x00" +
		":100644 120000 ffff 1111 T\x00link.go\x00" +
		":100644 100644 2222 3333 C75\x00src.go\x00copy.go\x00")
	got := parseRawDiff(out)
	want := []BlobChange{
		{Path: "a.go", Blob: "bbbb", Status: StatusModified},
		{Path: "new.go", Blob: "cccc", Status: StatusAdded},
		{Path: "gone.go", Status: StatusDeleted},
		{Path: "renamed.go", OldPath: "old.go", Blob: "eeee", Status: StatusRenamed},
		{Path: "link.go", Status: StatusDeleted},
		{Path: "copy.go", OldPath: "src.go", Blob: "3333", Status: StatusAdded},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d changes %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseTreeBlobs(t *testing.T) {
	out := []byte("100644 blob aaaa\tdir/a.go\x00160000 commit bbbb\tsub\x00120000 blob cccc\tlink\x00100755 blob dddd\tbin.sh\x00")
	got := parseTreeBlobs(out)
	if len(got) != 2 || got[0] != (TreeEntry{Path: "dir/a.go", Blob: "aaaa"}) || got[1] != (TreeEntry{Path: "bin.sh", Blob: "dddd"}) {
		t.Fatalf("entries = %+v", got)
	}
}

func TestParseFirstParentLog(t *testing.T) {
	out := []byte("abc\x00ab\x001700000000\x00first subject\n" + "def\x00de\x001700000100\x00second\n")
	got := parseFirstParentLog(out)
	if len(got) != 2 || got[0].Sha != "abc" || got[0].Short != "ab" || got[0].Subject != "first subject" || got[0].Time.Unix() != 1700000000 || got[1].Subject != "second" {
		t.Fatalf("commits = %+v", got)
	}
}

// trendRepo builds a three-commit repository: a.go on main, b.go added on
// a merged side branch, a.go renamed on top.
func trendRepo(t *testing.T) string {
	t.Helper()
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
	run("init", "-q", "-b", "main")
	write("a.go", "package p\n")
	run("add", ".")
	run("commit", "-qm", "one")
	run("checkout", "-qb", "side")
	write("b.go", "package p\n\nfunc b() {}\n")
	run("add", ".")
	run("commit", "-qm", "side work")
	run("checkout", "-q", "main")
	run("merge", "-q", "--no-ff", "-m", "merge side", "side")
	run("mv", "a.go", "c.go")
	run("commit", "-qm", "rename")
	return dir
}

func TestTrendHelpersRealRepo(t *testing.T) {
	dir := trendRepo(t)
	commits, err := FirstParentLog(dir, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	// First parent: rename, merge, one — the side commit is not a point.
	if len(commits) != 3 || commits[0].Subject != "rename" || commits[1].Subject != "merge side" || commits[2].Subject != "one" {
		t.Fatalf("commits = %+v", commits)
	}
	if commits[0].Short == "" || len(commits[0].Sha) != 40 || commits[0].Time.IsZero() {
		t.Fatalf("commit fields = %+v", commits[0])
	}
	if two, err := FirstParentLog(dir, "", 2); err != nil || len(two) != 2 {
		t.Fatalf("-n 2: %v %d", err, len(two))
	}
	tree, err := TreeBlobs(dir, commits[2].Sha)
	if err != nil || len(tree) != 1 || tree[0].Path != "a.go" || tree[0].Blob == "" {
		t.Fatalf("tree = %+v err %v", tree, err)
	}
	merge, err := ChangedBlobsBetween(dir, commits[2].Sha, commits[1].Sha)
	if err != nil || len(merge) != 1 || merge[0].Path != "b.go" || merge[0].Status != StatusAdded || merge[0].Blob == "" {
		t.Fatalf("merge diff = %+v err %v", merge, err)
	}
	ren, err := ChangedBlobsBetween(dir, commits[1].Sha, commits[0].Sha)
	if err != nil || len(ren) != 1 || ren[0].Status != StatusRenamed || ren[0].OldPath != "a.go" || ren[0].Path != "c.go" || ren[0].Blob != tree[0].Blob {
		t.Fatalf("rename diff = %+v err %v", ren, err)
	}
	got := map[string]string{}
	missing := 0
	err = BlobContents(dir, []string{tree[0].Blob, merge[0].Blob, nullBlob}, func(blob, content string, miss bool) {
		if miss {
			missing++
			return
		}
		got[blob] = content
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[tree[0].Blob] != "package p\n" || !strings.Contains(got[merge[0].Blob], "func b()") || missing != 1 {
		t.Fatalf("contents = %q missing %d", got, missing)
	}
	if err := BlobContents(dir, nil, func(string, string, bool) { t.Fatal("no blob asked") }); err != nil {
		t.Fatal(err)
	}
}
