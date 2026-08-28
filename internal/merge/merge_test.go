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

// TestHeaderCounterTracksResolution (#2258): the header names the caret's
// place in the conflict cycle, and reads resolved once the block is gone.
func TestHeaderCounterTracksResolution(t *testing.T) {
	m := newView(t)
	m.SetContents("a\nx\nz\n", "a\nours\nz\n", "a\ntheirs\nz\n")
	if idx, remaining := m.ConflictIndex(); idx != 0 || remaining != 1 {
		t.Fatalf("caret outside the block: idx=%d remaining=%d, want 0/1", idx, remaining)
	}
	m.Update(editor.ActionMsg{Action: "merge_next_conflict"})
	if idx, remaining := m.ConflictIndex(); idx != 1 || remaining != 1 {
		t.Fatalf("caret in the block: idx=%d remaining=%d, want 1/1", idx, remaining)
	}
	if v := m.View(); !strings.Contains(v, "conflict 1/1") {
		t.Fatalf("header missing the conflict index:\n%s", v)
	}
	m.Update(editor.ActionMsg{Action: "merge_accept_ours"})
	if m.Unresolved() != 0 {
		t.Fatalf("unresolved=%d after accept, want 0", m.Unresolved())
	}
	if v := m.View(); !strings.Contains(v, "resolved") || strings.Contains(v, "unresolved") {
		t.Fatalf("header must read resolved:\n%s", v)
	}
}
