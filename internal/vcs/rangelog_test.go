package vcs

import (
	"strings"
	"testing"
)

// TestRangeLogCmd guards #1430: only commits touching the tracked range are
// returned, newest first, each with the patch for that range.
func TestRangeLogCmd(t *testing.T) {
	dir := testRepo(t) // f.txt = "v1\n" @ "init"
	writeIn(t, dir, "f.txt", "v1\nline2\nline3\n")
	gitIn(t, dir, "add", "f.txt")
	gitIn(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "grow")
	// Touch only line 3 — a query for line 1 must not include this commit.
	writeIn(t, dir, "f.txt", "v1\nline2\nline3 changed\n")
	gitIn(t, dir, "add", "f.txt")
	gitIn(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "tail edit")

	msg := RangeLogCmd(dir, "f.txt", 1, 1)().(RangeLogMsg)
	if msg.Err != nil {
		t.Fatal(msg.Err)
	}
	if len(msg.Entries) != 1 {
		t.Fatalf("entries = %d, want 1 (only the commit introducing line 1): %+v", len(msg.Entries), msg.Entries)
	}
	if msg.Entries[0].Subject != "init" {
		t.Fatalf("subject = %q", msg.Entries[0].Subject)
	}
	if !strings.Contains(msg.Entries[0].Patch, "+v1") {
		t.Fatalf("patch missing range hunk:\n%s", msg.Entries[0].Patch)
	}

	msg = RangeLogCmd(dir, "f.txt", 3, 3)().(RangeLogMsg)
	if msg.Err != nil {
		t.Fatal(msg.Err)
	}
	if len(msg.Entries) != 2 {
		t.Fatalf("line-3 entries = %d, want 2: %+v", len(msg.Entries), msg.Entries)
	}
	if msg.Entries[0].Subject != "tail edit" || msg.Entries[1].Subject != "grow" {
		t.Fatalf("order = %q, %q", msg.Entries[0].Subject, msg.Entries[1].Subject)
	}
	if msg.Truncated {
		t.Fatal("truncated set on a two-commit history")
	}
}

// TestRangeLogCmdBadRange guards #1430: a range past EOF surfaces git's error
// instead of an empty success.
func TestRangeLogCmdBadRange(t *testing.T) {
	dir := testRepo(t)
	msg := RangeLogCmd(dir, "f.txt", 100, 120)().(RangeLogMsg)
	if msg.Err == nil {
		t.Fatal("expected an error for a range past EOF")
	}
}
