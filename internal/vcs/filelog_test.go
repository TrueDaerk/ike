package vcs

import "testing"

// filelog_test.go guards the git half of the per-file Timeline (#1916):
// windowed `git log --follow`, rename-aware per-commit paths, and the paging
// signal the Timeline's incremental loading rides on.

// renameRepo builds a repo whose file is committed twice, then renamed and
// committed once more under the new name.
func renameRepo(t *testing.T) string {
	t.Helper()
	dir := testRepo(t) // commit 1: "init" adds f.txt (v1)
	commit := func(msg string) {
		gitIn(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", msg)
	}
	writeIn(t, dir, "f.txt", "v1\nv2\n")
	gitIn(t, dir, "add", ".")
	commit("second: grow f")
	gitIn(t, dir, "mv", "f.txt", "g.txt")
	commit("third: rename f to g")
	writeIn(t, dir, "g.txt", "v1\nv2\nv3\n")
	gitIn(t, dir, "add", ".")
	commit("fourth: grow g")
	return dir
}

func TestFileLogCmdFollowsRenames(t *testing.T) {
	dir := renameRepo(t)
	msg := FileLogCmd(dir, "g.txt", 0, 10)().(FileLogMsg)
	if msg.Err != nil {
		t.Fatal(msg.Err)
	}
	if len(msg.Entries) != 4 {
		t.Fatalf("entries = %d, want 4 (--follow past the rename): %+v", len(msg.Entries), msg.Entries)
	}
	if msg.HasMore {
		t.Fatal("HasMore set on a fully returned history")
	}
	want := []string{"fourth: grow g", "third: rename f to g", "second: grow f", "init"}
	for i, subject := range want {
		if msg.Entries[i].Subject != subject {
			t.Fatalf("entry %d subject = %q, want %q", i, msg.Entries[i].Subject, subject)
		}
	}
	// The per-commit path is the one that commit's tree carried, so
	// `git show <hash>:<path>` resolves on both sides of the rename.
	if got := msg.Entries[0].Path; got != "g.txt" {
		t.Fatalf("newest entry path = %q, want g.txt", got)
	}
	if got := msg.Entries[2].Path; got != "f.txt" {
		t.Fatalf("pre-rename entry path = %q, want f.txt", got)
	}
	for _, e := range msg.Entries {
		if content, err := RevContent(dir, e.Hash, e.Path); err != nil || content == "" {
			t.Fatalf("blob at %s:%s unreadable: %v", e.ShortHash, e.Path, err)
		}
	}
}

func TestFileLogCmdPagesAndReportsMore(t *testing.T) {
	dir := renameRepo(t)
	first := FileLogCmd(dir, "g.txt", 0, 2)().(FileLogMsg)
	if first.Err != nil || len(first.Entries) != 2 || !first.HasMore {
		t.Fatalf("first window = %+v", first)
	}
	if first.Path != "g.txt" || first.Offset != 0 {
		t.Fatalf("window identity = %q @ %d", first.Path, first.Offset)
	}
	second := FileLogCmd(dir, "g.txt", 2, 2)().(FileLogMsg)
	if second.Err != nil || len(second.Entries) != 2 || second.HasMore || second.Offset != 2 {
		t.Fatalf("second window = %+v", second)
	}
	if second.Entries[1].Subject != "init" {
		t.Fatalf("tail subject = %q", second.Entries[1].Subject)
	}
}

func TestFileLogCmdUnknownPathAndNoRepo(t *testing.T) {
	dir := testRepo(t)
	// A path git never saw is an empty history, not an error (git exits 0).
	msg := FileLogCmd(dir, "nope.txt", 0, 10)().(FileLogMsg)
	if msg.Err != nil || len(msg.Entries) != 0 {
		t.Fatalf("unknown path = %+v", msg)
	}
	if msg := FileLogCmd(t.TempDir(), "f.txt", 0, 10)().(FileLogMsg); msg.Err == nil {
		t.Fatal("file log outside a repo must fail")
	}
}
