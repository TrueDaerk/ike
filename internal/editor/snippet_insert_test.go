package editor

import (
	"testing"

	"ike/internal/config"
)

// TestInsertSnippetFromNormalModeExpandsAndEntersInsert (#2694): the picker's
// expansion works from normal mode — the caret lands on the first tab stop in
// insert mode, exactly where trigger+tab would leave it.
func TestInsertSnippetFromNormalModeExpandsAndEntersInsert(t *testing.T) {
	withSnippets(t, []config.SnippetEntry{{Trigger: "pair", Body: "left($1) right(${2:val})"}})
	m, _ := loaded(t, "\n")
	if !m.InsertSnippet("left($1) right(${2:val})") {
		t.Fatal("InsertSnippet must fire in normal mode")
	}
	if m.mode != Insert {
		t.Fatalf("mode = %v, want Insert", m.mode)
	}
	if got := line(m, 0); got != "left() right(val)" {
		t.Fatalf("expansion = %q", got)
	}
	if m.cursor.Col != 5 {
		t.Fatalf("cursor at col %d, want 5 ($1)", m.cursor.Col)
	}
	if m.snippet == nil {
		t.Fatal("the tabstop session must be live after the expansion")
	}
	// The stops cycle like the Tab-trigger path's do.
	m = typeKeys(m, "ab")
	m = send(m, tab())
	if got := line(m, 0); got != "left(ab) right(val)" {
		t.Fatalf("after filling $1 = %q", got)
	}
	if m.cursor.Col != 18 {
		t.Fatalf("second stop col = %d, want 18", m.cursor.Col)
	}
}

// TestInsertSnippetInInsertModeMatchesTabExpansion (#2694): picked in insert
// mode, the body lands at the caret with the surrounding indentation the Tab
// expansion applies — the picker is the trigger-less door to the same path.
func TestInsertSnippetInInsertModeMatchesTabExpansion(t *testing.T) {
	withSnippets(t, nil)
	m, _ := loaded(t, "    \n")
	m.useSpaces = true
	m.tabWidth = 2
	m.SetCursor(0, 4)
	m = send(m, key('a')) // insert mode after the indent
	if !m.InsertSnippet("if x {\n\t$1\n}") {
		t.Fatal("InsertSnippet must fire in insert mode")
	}
	want := []string{"    if x {", "      ", "    }"}
	for i, w := range want {
		if got := line(m, i); got != w {
			t.Fatalf("line %d = %q, want %q", i, got, w)
		}
	}
	if m.cursor.Line != 1 || m.cursor.Col != 6 {
		t.Fatalf("cursor at %v, want line 1 col 6 ($1)", m.cursor)
	}
}

// TestInsertSnippetUndoRemovesTheBody (#2694): the expansion is its own undo
// unit, like the Tab one — a single undo takes the whole body back out.
func TestInsertSnippetUndoRemovesTheBody(t *testing.T) {
	withSnippets(t, nil)
	m, _ := loaded(t, "x\n")
	m.InsertSnippet("Y$1Z")
	if got := line(m, 0); got != "YZx" {
		t.Fatalf("expansion = %q", got)
	}
	m = send(m, esc())
	m = send(m, key('u'))
	if got := line(m, 0); got != "x" {
		t.Fatalf("after undo = %q, want %q", got, "x")
	}
}

// TestInsertSnippetRefusedReadOnly (#2694): a read-only buffer is never
// mutated by the picker either.
func TestInsertSnippetRefusedReadOnly(t *testing.T) {
	withSnippets(t, nil)
	m, _ := loaded(t, "\n")
	m.SetReadOnly(true)
	if m.InsertSnippet("body") {
		t.Fatal("a read-only buffer must refuse the expansion")
	}
	if got := line(m, 0); got != "" {
		t.Fatalf("buffer changed: %q", got)
	}
}

// TestSnippetEntriesFollowsBufferLanguage (#2694): the picker's list is the
// set Tab resolves against — a txt buffer never sees the go-scoped templates.
func TestSnippetEntriesFollowsBufferLanguage(t *testing.T) {
	withSnippets(t, []config.SnippetEntry{
		{Trigger: "gonly", Language: "go", Body: "GO"},
		{Trigger: "every", Body: "ALL"},
	})
	m, _ := loaded(t, "\n") // f.txt
	var triggers []string
	for _, e := range m.SnippetEntries() {
		triggers = append(triggers, e.Trigger)
	}
	for _, tr := range triggers {
		if tr == "gonly" {
			t.Fatalf("a txt buffer must not list the go-scoped entry: %v", triggers)
		}
	}
	found := false
	for _, tr := range triggers {
		if tr == "every" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the global entry must be listed: %v", triggers)
	}
}
