package merge

import (
	"strings"
	"testing"

	"ike/internal/editor"
)

func newView(t *testing.T) Model {
	t.Helper()
	ed := editor.New()
	ed.NewFile("conflicted.txt")
	m := New("merge", "conflicted.txt", &ed, nil)
	m.SetSize(150, 20)
	return m
}

// TestSetContentsSeedsMerge seeds the result with the auto-merged text and
// counts the conflict blocks.
func TestSetContentsSeedsMerge(t *testing.T) {
	m := newView(t)
	m.SetContents("a\nx\nz\n", "a\nours\nz\n", "a\ntheirs\nz\n")
	if m.Total() != 1 || m.Unresolved() != 1 {
		t.Fatalf("total=%d unresolved=%d, want 1/1", m.Total(), m.Unresolved())
	}
	text := m.Editor().Text()
	if !strings.Contains(text, "<<<<<<< ours") || !strings.Contains(text, "||||||| base") {
		t.Fatalf("result not seeded with diff3 markers: %q", text)
	}
}

// TestSetContentsAutoResolved seeds a clean merge with zero conflicts.
func TestSetContentsAutoResolved(t *testing.T) {
	m := newView(t)
	m.SetContents("a\nb\nc\n", "A\nb\nc\n", "a\nb\nC\n")
	if m.Total() != 0 || m.Unresolved() != 0 {
		t.Fatalf("total=%d unresolved=%d, want 0/0", m.Total(), m.Unresolved())
	}
	if got := m.Editor().Text(); got != "A\nb\nC" {
		t.Fatalf("merged = %q", got)
	}
}

// TestViewShowsColumnsAndCount renders the three columns and the unresolved
// count in the header.
func TestViewShowsColumnsAndCount(t *testing.T) {
	m := newView(t)
	m.SetContents("a\nx\nz\n", "a\nours-line\nz\n", "a\ntheirs-line\nz\n")
	v := m.View()
	if !strings.Contains(v, "Ours") || !strings.Contains(v, "Theirs") || !strings.Contains(v, "Result") {
		t.Fatalf("header missing columns:\n%s", v)
	}
	if !strings.Contains(v, "1/1 unresolved") {
		t.Fatalf("header missing unresolved count:\n%s", v)
	}
	if !strings.Contains(v, "ours-line") || !strings.Contains(v, "theirs-line") {
		t.Fatalf("side columns missing content:\n%s", v)
	}
}
