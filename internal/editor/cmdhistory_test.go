package editor

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/histories"
)

// cmdhistory_test.go covers query-history recall on the command line (#1171):
// up/down cycle recent "/" queries (and ":" lines in their own bucket),
// recalling replaces the input with the cursor at the end, typing after a
// recall edits normally, and committed queries persist through the store.

// histEditor is loaded() plus a query-history store at a temp file.
func histEditor(t *testing.T, content string) (Model, *histories.Store) {
	t.Helper()
	m, _ := loaded(t, content)
	h := histories.NewAt(filepath.Join(t.TempDir(), "histories.json"))
	m.SetHistories(h)
	return m, h
}

func up() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyUp} }
func down() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyDown} }

// commitSearchLine types /q<enter>.
func commitSearchLine(m Model, q string) Model {
	m = typeKeys(m, "/"+q)
	return send(m, special(tea.KeyEnter))
}

// TestSearchHistoryRecallOrder: up walks committed queries newest first (vim:
// up = older), down walks back and past the newest restores the live line.
func TestSearchHistoryRecallOrder(t *testing.T) {
	m, _ := histEditor(t, "alpha\nbeta\ngamma\n")
	m = commitSearchLine(m, "beta")
	m = commitSearchLine(m, "gamma")

	m = typeKeys(m, "/")
	m = send(m, up())
	if m.cmdline != "gamma" {
		t.Fatalf("first up = %q, want the most recent query", m.cmdline)
	}
	if m.cmdCur != len("gamma") {
		t.Fatalf("recall cursor = %d, want end of line", m.cmdCur)
	}
	m = send(m, up())
	if m.cmdline != "beta" {
		t.Fatalf("second up = %q, want the older query", m.cmdline)
	}
	m = send(m, up()) // past the oldest: stays
	if m.cmdline != "beta" {
		t.Fatalf("up past the oldest = %q, want beta", m.cmdline)
	}
	m = send(m, down())
	if m.cmdline != "gamma" {
		t.Fatalf("down = %q, want the newer query", m.cmdline)
	}
	m = send(m, down()) // back to the live (empty) line
	if m.cmdline != "" {
		t.Fatalf("down past the newest = %q, want the live line", m.cmdline)
	}
}

// TestSearchRecallKeepsLiveLine: a half-typed query survives the walk and
// comes back on down.
func TestSearchRecallKeepsLiveLine(t *testing.T) {
	m, _ := histEditor(t, "alpha\nbeta\n")
	m = commitSearchLine(m, "beta")
	m = typeKeys(m, "/alp")
	m = send(m, up())
	if m.cmdline != "beta" {
		t.Fatalf("up = %q, want beta", m.cmdline)
	}
	m = send(m, down())
	if m.cmdline != "alp" {
		t.Fatalf("down must restore the live line, got %q", m.cmdline)
	}
}

// TestSearchRecallThenEdit: typing after a recall edits the recalled text
// normally (#1110 composes) and leaves recall mode — the next up starts at
// the newest entry again.
func TestSearchRecallThenEdit(t *testing.T) {
	m, _ := histEditor(t, "alpha\nbeta\n")
	m = commitSearchLine(m, "bet")
	m = commitSearchLine(m, "alp")

	m = typeKeys(m, "/")
	m = send(m, up(), up()) // "bet"
	if m.cmdline != "bet" {
		t.Fatalf("recall = %q, want bet", m.cmdline)
	}
	m = typeKeys(m, "a")
	if m.cmdline != "beta" {
		t.Fatalf("typing after recall = %q, want beta", m.cmdline)
	}
	m = send(m, up()) // edited text is live again: up starts at the newest
	if m.cmdline != "alp" {
		t.Fatalf("up after editing = %q, want the newest entry", m.cmdline)
	}
}

// TestSearchHistoryDedupes: recommitting a query moves it to the front
// instead of duplicating it.
func TestSearchHistoryDedupes(t *testing.T) {
	m, h := histEditor(t, "alpha\nbeta\n")
	m = commitSearchLine(m, "alpha")
	m = commitSearchLine(m, "beta")
	_ = commitSearchLine(m, "alpha")
	got := h.All(histories.Search)
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("bucket = %v, want [alpha beta]", got)
	}
}

// TestExHistorySeparateBucket: ":" lines recall from their own bucket — the
// shared input code path (#1110) keeps the histories apart.
func TestExHistorySeparateBucket(t *testing.T) {
	m, h := histEditor(t, "alpha\nbeta\n")
	m = commitSearchLine(m, "beta")
	m = typeKeys(m, ":nosuchcmd")
	m = send(m, special(tea.KeyEnter)) // pushed even though it errors, vim-style

	if got := h.All(histories.Ex); len(got) != 1 || got[0] != "nosuchcmd" {
		t.Fatalf("ex bucket = %v, want [nosuchcmd]", got)
	}
	m = typeKeys(m, ":")
	m = send(m, up())
	if m.cmdline != "nosuchcmd" {
		t.Fatalf(": up = %q, want the ex entry, not the search one", m.cmdline)
	}
	m = send(m, special(tea.KeyEscape))
	m = typeKeys(m, "/")
	m = send(m, up())
	if m.cmdline != "beta" {
		t.Fatalf("/ up = %q, want the search entry, not the ex one", m.cmdline)
	}
}

// TestEmptyCommitNotPushed: Enter on an empty line records nothing.
func TestEmptyCommitNotPushed(t *testing.T) {
	m, h := histEditor(t, "alpha\n")
	m = typeKeys(m, "/")
	_ = send(m, special(tea.KeyEnter))
	if got := h.All(histories.Search); len(got) != 0 {
		t.Fatalf("bucket = %v, want empty", got)
	}
}

// TestHistoryPersistsAcrossEditors: a committed query is recalled by a fresh
// editor reading the same store file — the marks.json pattern (#1151).
func TestHistoryPersistsAcrossEditors(t *testing.T) {
	file := filepath.Join(t.TempDir(), "histories.json")
	m, _ := loaded(t, "alpha\nbeta\n")
	m.SetHistories(histories.NewAt(file))
	_ = commitSearchLine(m, "beta")

	fresh, _ := loaded(t, "alpha\nbeta\n")
	fresh.SetHistories(histories.NewAt(file))
	fresh = typeKeys(fresh, "/")
	fresh = send(fresh, up())
	if fresh.cmdline != "beta" {
		t.Fatalf("recall in a fresh editor = %q, want beta", fresh.cmdline)
	}
}

// TestRecallWithoutStoreIsNoop: nil store (the default) leaves up/down inert
// on the command line.
func TestRecallWithoutStoreIsNoop(t *testing.T) {
	m, _ := loaded(t, "alpha\n")
	m = typeKeys(m, "/alp")
	m = send(m, up())
	if m.cmdline != "alp" {
		t.Fatalf("up without a store changed the line to %q", m.cmdline)
	}
}
