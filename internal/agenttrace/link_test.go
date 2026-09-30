package agenttrace

import (
	"testing"
	"time"
)

// link_test.go covers change-feed matching (#2838): path, time window and
// source process must all agree, and an unattributed change never matches.

var t0 = time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)

// linkTree is one turn with two tool calls: an Edit of a.go (issued at t0,
// done 2s later) with a Read of b.go beside it, and a later Write of a.go
// at t0+60s that is still pending.
func linkTree() []Node {
	a := FileRef{Path: "/p/a.go", Op: OpEdit}
	b := FileRef{Path: "/p/b.go", Op: OpRead}
	w := FileRef{Path: "a.go", Op: OpWrite} // relative: resolved against cwd
	edit := Node{Kind: NodeTool, Key: "e2", At: t0, Until: t0.Add(2 * time.Second), Children: []Node{
		{Kind: NodeFile, Key: "e2/f0", Ref: &a, Path: a.Path, At: t0, Until: t0.Add(2 * time.Second)},
		{Kind: NodeFile, Key: "e2/f1", Ref: &b, Path: b.Path, At: t0, Until: t0.Add(2 * time.Second)},
	}}
	write := Node{Kind: NodeTool, Key: "e5", Ref: &w, Path: w.Path, At: t0.Add(time.Minute), Children: []Node{
		{Kind: NodeFile, Key: "e5/f0", Ref: &w, Path: w.Path, At: t0.Add(time.Minute)},
	}}
	return []Node{{Kind: NodeTurn, Key: "t1", Event: -1, Children: []Node{
		{Kind: NodeDecision, Key: "e1", Children: []Node{edit, write}},
	}}}
}

func TestLinkMatchesPathWindowAndSource(t *testing.T) {
	changes := []Change{
		{Path: "/p/a.go", First: t0.Add(time.Second), Last: t0.Add(time.Second), SourceKey: "term-1"},
		{Path: "/p/b.go", First: t0.Add(time.Second), Last: t0.Add(time.Second), SourceKey: "term-1"},
	}
	l := Link(linkTree(), changes, "term-1", "/p")
	if got := l.Node("e2/f0"); got != "/p/a.go" {
		t.Fatalf("edit file node links to %q", got)
	}
	if got := l.Node("e2/f1"); got != "" {
		t.Fatalf("a read must never link (got %q)", got)
	}
	if got := l.Node("e5/f0"); got != "" {
		t.Fatalf("write issued a minute after the change links to %q", got)
	}
	if got := l.Path("/p/a.go"); got != "e2/f0" {
		t.Fatalf("back-link = %q", got)
	}
	if l.Path("/p/b.go") != "" || l.Len() != 1 {
		t.Fatalf("links = %+v", l)
	}
}

func TestLinkAmbiguousOrForeignSourceStaysPlain(t *testing.T) {
	for name, key := range map[string]string{
		"unattributed":   "", // several processes were busy: nobody is named
		"other terminal": "term-2",
	} {
		changes := []Change{{Path: "/p/a.go", First: t0, Last: t0, SourceKey: key}}
		if l := Link(linkTree(), changes, "term-1", "/p"); l.Len() != 0 || len(l.ByNode) != 0 {
			t.Fatalf("%s: linked %+v", name, l)
		}
	}
	// A trace that follows no terminal cannot claim any process's write.
	changes := []Change{{Path: "/p/a.go", First: t0, Last: t0, SourceKey: "term-1"}}
	if l := Link(linkTree(), changes, "", "/p"); l.Len() != 0 {
		t.Fatalf("no follow key linked %+v", l)
	}
}

func TestLinkTimeWindow(t *testing.T) {
	at := func(d time.Duration) []Change {
		return []Change{{Path: "/p/a.go", First: t0.Add(d), Last: t0.Add(d), SourceKey: "k"}}
	}
	// Inside the slack around the call's [issued, done] window.
	for _, d := range []time.Duration{-LinkSlack, 0, 2*time.Second + LinkSlack} {
		if Link(linkTree(), at(d), "k", "/p").Node("e2/f0") == "" {
			t.Fatalf("change at %v not linked", d)
		}
	}
	// Outside it, and before the pending write: nothing.
	for _, d := range []time.Duration{-LinkSlack - time.Second, 2*time.Second + LinkSlack + time.Second} {
		if l := Link(linkTree(), at(d), "k", "/p"); l.Len() != 0 {
			t.Fatalf("change at %v linked %+v", d, l)
		}
	}
}

func TestLinkCoalescedChangeBacklinksNewestWrite(t *testing.T) {
	// One entry spanning both writes of a.go: both nodes link, the pending
	// write (open for LinkPending, relative path) included; the back-link
	// names the newest one, and its single-file tool row shares the link.
	changes := []Change{{Path: "/p/a.go", First: t0.Add(time.Second), Last: t0.Add(90 * time.Second), SourceKey: "k"}}
	l := Link(linkTree(), changes, "k", "/p")
	for _, key := range []string{"e2/f0", "e5/f0", "e5"} {
		if l.Node(key) != "/p/a.go" {
			t.Fatalf("%s not linked: %+v", key, l.ByNode)
		}
	}
	if l.Node("e2") != "" {
		t.Fatal("a multi-file tool row must not take one file's link")
	}
	if got := l.Path("/p/a.go"); got != "e5/f0" {
		t.Fatalf("back-link = %q, want the newest write", got)
	}
}
