package vcs

import (
	"strings"
	"testing"
	"time"
)

func TestParseBlame(t *testing.T) {
	sha := strings.Repeat("a", 40)
	zero := strings.Repeat("0", 40)
	out := []byte(strings.Join([]string{
		sha + " 1 1 2",
		"author Alice",
		"author-time 1700000000",
		"summary feat: first",
		"\tline one",
		sha + " 2 2",
		"\tline two",
		zero + " 3 3 1",
		"author Not Committed Yet",
		"author-time 1700000100",
		"summary Version of f.txt from f.txt",
		"\tline three",
	}, "\n"))
	lines := parseBlame(out)
	if len(lines) != 3 {
		t.Fatalf("lines = %v", lines)
	}
	for _, i := range []int{0, 1} {
		if l := lines[i]; l.Author != "Alice" || l.Summary != "feat: first" || l.Uncommitted {
			t.Errorf("line %d = %+v", i, l)
		}
	}
	if !lines[2].Uncommitted {
		t.Errorf("line 2 = %+v, want uncommitted", lines[2])
	}
}

func TestBlameAnnotationText(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	b := BlameLine{Author: "Alice", Time: now.Add(-49 * time.Hour), Summary: "fix: y"}
	if got := b.Annotation(now); got != "Alice, 2 days ago · fix: y" {
		t.Fatalf("annotation = %q", got)
	}
	if got := (BlameLine{Uncommitted: true}).Annotation(now); got != "not committed yet" {
		t.Fatalf("uncommitted annotation = %q", got)
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	cases := map[time.Duration]string{
		30 * time.Second:         "just now",
		5 * time.Minute:          "5 minutes ago",
		3 * time.Hour:            "3 hours ago",
		26 * time.Hour:           "1 day ago",
		40 * 24 * time.Hour:      "1 month ago",
		3 * 365 * 24 * time.Hour: "3 years ago",
	}
	for d, want := range cases {
		if got := RelativeTime(now.Add(-d), now); got != want {
			t.Errorf("RelativeTime(-%v) = %q, want %q", d, got, want)
		}
	}
}

func TestBlameCmdRealRepo(t *testing.T) {
	dir := testRepo(t)
	writeIn(t, dir, "f.txt", "v1\nnew line\n")
	root, _ := DetectRoot(dir)

	msg := BlameCmd(root, "f.txt")().(BlameMsg)
	if msg.Err != nil {
		t.Fatalf("blame: %v", msg.Err)
	}
	if l := msg.Lines[0]; l.Uncommitted || l.Summary != "init" {
		t.Fatalf("line 0 = %+v", l)
	}
	if l := msg.Lines[1]; !l.Uncommitted {
		t.Fatalf("line 1 = %+v, want uncommitted", l)
	}

	// Untracked file: plain error.
	writeIn(t, dir, "new.txt", "x\n")
	if msg := BlameCmd(root, "new.txt")().(BlameMsg); msg.Err == nil {
		t.Fatal("blame of an untracked file must fail")
	}
}
