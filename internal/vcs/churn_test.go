package vcs

import (
	"testing"
	"time"
)

// TestChurnCmdCountsCommits: the history repo touches f.txt twice and b.txt
// once before its rename to c.txt; the message carries the counts, the
// root and the window start, and outside a repository the error.
func TestChurnCmdCountsCommits(t *testing.T) {
	dir := historyRepo(t)
	root, _ := DetectRoot(dir)
	since := time.Now().AddDate(0, 0, -1)
	msg := ChurnCmd(root, since)().(ChurnMsg)
	if msg.Err != nil || msg.Root != root || !msg.Since.Equal(since) {
		t.Fatalf("churn msg = %+v", msg)
	}
	if msg.Counts["f.txt"] != 2 || msg.Counts["b.txt"] != 1 || msg.Counts["c.txt"] != 1 {
		t.Fatalf("counts = %v, want f.txt:2 b.txt:1 c.txt:1 (renames split)", msg.Counts)
	}
	if msg := ChurnCmd(t.TempDir(), since)().(ChurnMsg); msg.Err == nil {
		t.Fatal("churn outside a repo must fail")
	}
}
