package editor

// Tests for the search match counter and the all-matches highlighting (#2145).

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/editor/search"
	"ike/internal/theme"
)

func TestSearchCounterIncrementalAndNavigation(t *testing.T) {
	m, _ := loaded(t, "foo one\nbar\nfoo two\nfoo three\n")
	m = send(m, key('/'))
	m = typeKeys(m, "foo")
	// Incremental: the counter is live on the open search line, and the
	// preview already parked the cursor on the first match past the origin.
	if got := m.SearchCounter(); got != "2/3" {
		t.Fatalf("incremental counter = %q, want %q", got, "2/3")
	}
	m = send(m, special(tea.KeyEnter))
	if got := m.SearchCounter(); got != "2/3" {
		t.Fatalf("committed counter = %q, want %q", got, "2/3")
	}
	m = send(m, key('n'))
	if got := m.SearchCounter(); got != "3/3" {
		t.Fatalf("after n: counter = %q, want %q", got, "3/3")
	}
	m = send(m, key('n')) // wraps to the first match
	if got := m.SearchCounter(); got != "1/3" {
		t.Fatalf("after wrap: counter = %q, want %q", got, "1/3")
	}
	m = send(m, key('N'))
	if got := m.SearchCounter(); got != "3/3" {
		t.Fatalf("after N: counter = %q, want %q", got, "3/3")
	}
	// A normal-mode Esc clears the highlights, and with them the counter.
	m = send(m, special(tea.KeyEscape))
	if got := m.SearchCounter(); got != "" {
		t.Fatalf("after Esc: counter = %q, want empty", got)
	}
}

func TestSearchCounterNoMatchesAndIdle(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\n")
	if got := m.SearchCounter(); got != "" {
		t.Fatalf("idle counter = %q, want empty", got)
	}
	m = send(m, key('/'))
	m = typeKeys(m, "gamma")
	if got := m.SearchCounter(); got != "no matches" {
		t.Fatalf("counter = %q, want %q", got, "no matches")
	}
	// The command-line row carries the same tally, dimmed.
	if row := m.commandLineRow(); !strings.Contains(row, "no matches") {
		t.Fatalf("command line %q lacks the tally", row)
	}
}

func TestSearchCounterCapsOnLargeBuffer(t *testing.T) {
	var b strings.Builder
	for i := 0; i < search.MaxMatches+50; i++ {
		b.WriteString("hit\n")
	}
	m, _ := loaded(t, b.String())
	m = send(m, key('/'))
	m = typeKeys(m, "hit")
	want := "2/" + strconv.Itoa(search.MaxMatches) + "+"
	if got := m.SearchCounter(); got != want {
		t.Fatalf("capped counter = %q, want %q", got, want)
	}
}

// TestSearchTallyCachedPerQuery pins the per-keystroke cost cap: within one
// document version and query the match list is scanned once, so cursor motion
// (n/N and plain j/k alike) re-derives only the index.
func TestSearchTallyCachedPerQuery(t *testing.T) {
	m, _ := loaded(t, "foo\nbar\nfoo\n")
	m = send(m, key('/'))
	m = typeKeys(m, "foo")
	m = send(m, special(tea.KeyEnter))
	m.SearchCounter() // the first render fills the cache
	if m.tally == nil || !m.tally.valid {
		t.Fatal("tally cache not populated by the search")
	}
	spans := m.tally.spans
	if len(spans) != 2 {
		t.Fatalf("cached spans = %v, want 2", spans)
	}
	m = send(m, key('n'))
	if got := m.SearchCounter(); got != "1/2" {
		t.Fatalf("after n: counter = %q, want %q", got, "1/2")
	}
	if &m.tally.spans[0] != &spans[0] {
		t.Fatal("n rescanned the buffer instead of reusing the cached spans")
	}
	// An edit bumps the document version, which must invalidate the cache:
	// deleting the character under the cursor destroys the match it sits on.
	m = send(m, key('x'))
	if got := m.SearchCounter(); !strings.HasSuffix(got, "/1") {
		t.Fatalf("after edit: counter = %q, want a total of 1", got)
	}
}

// TestSearchHighlightsAllVisibleMatches checks the rendered spans: every match
// on a visible line carries the muted match background, and the match the
// cursor sits on is styled apart from the rest.
func TestSearchHighlightsAllVisibleMatches(t *testing.T) {
	m, _ := loaded(t, "foo x foo\nbar\nfoo\n")
	m.SetSize(40, 6)
	m = send(m, key('/'))
	m = typeKeys(m, "foo")
	m = send(m, special(tea.KeyEnter))

	// The query the renderer highlights covers every match, not just the
	// current one — three on the three visible lines.
	q, ok := m.searchHLQuery()
	if !ok {
		t.Fatal("no highlight query armed after the search")
	}
	var visible int
	for line := m.view.Top; line < m.view.Top+m.view.Height() && line < m.buf.LineCount(); line++ {
		visible += len(q.LineMatches(m.buf, line))
	}
	if visible != 3 {
		t.Fatalf("highlighted spans in the viewport = %d, want 3", visible)
	}

	// The two non-current matches render on the muted background; the
	// current one takes the accent tint instead.
	view := m.View()
	muted := sgrPrefix(lipgloss.NewStyle().Background(m.theme().SelectionMuted).Render("x"))
	current := sgrPrefix(lipgloss.NewStyle().
		Background(theme.Mix(m.theme().Accent, m.theme().Surface, 0.45)).
		Underline(true).Render("x"))
	if muted == current {
		t.Fatal("current match is not styled apart from the other matches")
	}
	if n := strings.Count(view, muted); n == 0 {
		t.Fatalf("no muted match cells in the view (%d current cells)", strings.Count(view, current))
	}
	if n := strings.Count(view, current); n == 0 {
		t.Fatal("no current-match cells in the view")
	}
}

// sgrPrefix returns the leading escape sequence of a styled cell, which
// identifies the style in rendered output.
func sgrPrefix(s string) string {
	if i := strings.Index(s, "m"); i > 0 {
		return s[:i+1]
	}
	return s
}
