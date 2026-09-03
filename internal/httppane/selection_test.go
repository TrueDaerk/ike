package httppane

import (
	"net/http"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/httpclient"
)

// selViewer composes a small response whose rows are known by index:
// 0 status, 1 Content-Type, 2 X-Token, 3 blank, 4.. body.
func selViewer(t *testing.T) *Model {
	t.Helper()
	m := New(nil)
	m.SetSize(120, 20)
	m.Set("r", &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Headers: http.Header{
			"Content-Type": {"text/plain"},
			"X-Token":      {"token-abc"},
		},
		Body:     []byte("alpha beta\ngamma delta"),
		Duration: time.Millisecond,
	})
	return &m
}

// selViewerWithRequest is selViewer but with an as-sent request snapshot
// attached (#1832), so the pane header grows to two rows (#2424) — the
// mapping every mouse handler must follow (#2450).
func selViewerWithRequest(t *testing.T) *Model {
	t.Helper()
	m := New(nil)
	m.SetSize(120, 20)
	resp := &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Headers: http.Header{
			"Content-Type": {"text/plain"},
			"X-Token":      {"token-abc"},
		},
		Body:     []byte("alpha beta\ngamma delta"),
		Duration: time.Millisecond,
		Request: &httpclient.RequestSnapshot{
			Method: "GET",
			URL:    "https://example.test/r",
		},
	}
	m.Set("r", resp)
	return &m
}

// cell converts a row/column of the composed view into pane-local mouse
// coordinates: the fixed header rows take y < headerLineCount(), every row
// renders with one leading space.
func cell(m *Model, row, col int) (x, y int) {
	return col + 1, m.displayOf(row) - m.top + m.headerLineCount()
}

// clickAt presses (and releases) at a composed position.
func press(m *Model, row, col int) {
	x, y := cell(m, row, col)
	m.MousePress(x, y)
}

func drag(m *Model, row, col int) {
	x, y := cell(m, row, col)
	m.MouseDrag(x, y)
}

// fixedClock pins timeNow so click streaks are deterministic; step advances it.
func fixedClock(t *testing.T) func(time.Duration) {
	t.Helper()
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	prev := timeNow
	timeNow = func() time.Time { return now }
	t.Cleanup(func() { timeNow = prev })
	return func(d time.Duration) { now = now.Add(d) }
}

func bodyRow(m *Model, text string) int {
	for i, r := range m.rows {
		if r.kind == kindBody && r.text == text {
			return i
		}
	}
	return -1
}

func TestMouseDragSelectsAcrossRows(t *testing.T) {
	m := selViewer(t)
	fixedClock(t)
	first := bodyRow(m, "alpha beta")
	press(m, first, 6) // start at "beta"
	drag(m, first+1, 5)
	m.MouseRelease()
	if !m.HasSelection() {
		t.Fatal("drag must produce a selection")
	}
	if got, want := m.SelectionText(), "beta\ngamma"; got != want {
		t.Errorf("selection text: %q, want %q", got, want)
	}
}

func TestMouseDragBackwards(t *testing.T) {
	m := selViewer(t)
	fixedClock(t)
	first := bodyRow(m, "alpha beta")
	press(m, first+1, 5)
	drag(m, first, 6)
	if got, want := m.SelectionText(), "beta\ngamma"; got != want {
		t.Errorf("backwards selection: %q, want %q", got, want)
	}
}

func TestDoubleClickSelectsWord(t *testing.T) {
	m := selViewer(t)
	step := fixedClock(t)
	r := bodyRow(m, "alpha beta")
	press(m, r, 7)
	step(50 * time.Millisecond)
	press(m, r, 7)
	if got := m.SelectionText(); got != "beta" {
		t.Errorf("double click: %q, want beta", got)
	}
}

func TestTripleClickSelectsLine(t *testing.T) {
	m := selViewer(t)
	step := fixedClock(t)
	r := bodyRow(m, "alpha beta")
	for i := 0; i < 3; i++ {
		press(m, r, 3)
		step(50 * time.Millisecond)
	}
	if got := m.SelectionText(); got != "alpha beta" {
		t.Errorf("triple click: %q, want the whole line", got)
	}
}

// TestMouseDragSelectsAcrossRowsWithRequestLine mirrors
// TestMouseDragSelectsAcrossRows with the two-row header the request-line
// grows the pane to (#2424): the mapping from pane-local y to a body row must
// follow, or the click/drag lands one row below the pointer (#2450).
func TestMouseDragSelectsAcrossRowsWithRequestLine(t *testing.T) {
	m := selViewerWithRequest(t)
	if got, want := m.headerLineCount(), 2; got != want {
		t.Fatalf("headerLineCount = %d, want %d", got, want)
	}
	fixedClock(t)
	first := bodyRow(m, "alpha beta")
	press(m, first, 6) // start at "beta"
	drag(m, first+1, 5)
	m.MouseRelease()
	if !m.HasSelection() {
		t.Fatal("drag must produce a selection")
	}
	if got, want := m.SelectionText(), "beta\ngamma"; got != want {
		t.Errorf("selection text: %q, want %q", got, want)
	}
}

// TestDoubleClickSelectsWordWithRequestLine mirrors TestDoubleClickSelectsWord
// with the two-row header (#2450).
func TestDoubleClickSelectsWordWithRequestLine(t *testing.T) {
	m := selViewerWithRequest(t)
	step := fixedClock(t)
	r := bodyRow(m, "alpha beta")
	press(m, r, 7)
	step(50 * time.Millisecond)
	press(m, r, 7)
	if got := m.SelectionText(); got != "beta" {
		t.Errorf("double click: %q, want beta", got)
	}
}

// TestTripleClickSelectsLineWithRequestLine mirrors TestTripleClickSelectsLine
// with the two-row header (#2450).
func TestTripleClickSelectsLineWithRequestLine(t *testing.T) {
	m := selViewerWithRequest(t)
	step := fixedClock(t)
	r := bodyRow(m, "alpha beta")
	for i := 0; i < 3; i++ {
		press(m, r, 3)
		step(50 * time.Millisecond)
	}
	if got := m.SelectionText(); got != "alpha beta" {
		t.Errorf("triple click: %q, want the whole line", got)
	}
}

// TestPressOnRequestLineDoesNotSelectBodyRow guards the boundary the bug
// report hinges on: a click on the request-line row itself (still header,
// with the two-row header) must not be mistaken for the first body row.
func TestPressOnRequestLineDoesNotSelectBodyRow(t *testing.T) {
	m := selViewerWithRequest(t)
	fixedClock(t)
	first := bodyRow(m, "alpha beta")
	x, y := cell(m, first, 0)
	m.MousePress(x, y-1) // one row up: the request-line row, not the body
	if m.sel.anchor.row == first {
		t.Errorf("press on the request-line row landed on body row %d", first)
	}
}

// TestClickStreakExpires: a slow second click starts a fresh char selection
// instead of growing into a word selection.
func TestClickStreakExpires(t *testing.T) {
	m := selViewer(t)
	step := fixedClock(t)
	r := bodyRow(m, "alpha beta")
	press(m, r, 7)
	step(multiClickWindow + time.Millisecond)
	press(m, r, 7)
	if m.HasSelection() {
		t.Errorf("expired streak must not select a word, got %q", m.SelectionText())
	}
}

func TestDoubleClickSelectsWholeToken(t *testing.T) {
	m := selViewer(t)
	step := fixedClock(t)
	// The X-Token header value is one token: "token-abc" — hyphens included,
	// because response ids are what users double-click.
	r := 0
	for i, row := range m.rows {
		if row.kind == kindHeader && strings.HasPrefix(row.text, "X-Token") {
			r = i
		}
	}
	press(m, r, 12)
	step(50 * time.Millisecond)
	press(m, r, 12)
	if got := m.SelectionText(); got != "token-abc" {
		t.Errorf("header value double click: %q", got)
	}
}

func TestCopySelectionEmitsCopyMsg(t *testing.T) {
	m := selViewer(t)
	fixedClock(t)
	r := bodyRow(m, "alpha beta")
	press(m, r, 0)
	drag(m, r, 5)
	cmd := m.handleKey(keyPress("y"))
	if cmd == nil {
		t.Fatal("y with a selection must emit a copy command")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("message type: %T", cmd())
	}
	if msg.Text != "alpha" || msg.What != "selection" {
		t.Errorf("copy message: %+v", msg)
	}
	if m.HasSelection() {
		t.Error("copying must clear the selection")
	}
}

func TestCopyBodyAndHeadersWithoutSelection(t *testing.T) {
	m := selViewer(t)
	cmd := m.handleKey(keyPress("y"))
	if cmd == nil {
		t.Fatal("y without a selection must copy the body")
	}
	msg := cmd().(CopyMsg)
	if msg.Text != "alpha beta\ngamma delta" || msg.What != "response body" {
		t.Errorf("body copy: %+v", msg)
	}

	cmd = m.handleKey(keyPress("Y"))
	if cmd == nil {
		t.Fatal("Y must copy the headers")
	}
	msg = cmd().(CopyMsg)
	if !strings.HasPrefix(msg.Text, "HTTP/1.1 200 OK") ||
		!strings.Contains(msg.Text, "X-Token: token-abc") ||
		strings.Contains(msg.Text, "alpha beta") {
		t.Errorf("headers copy: %q", msg.Text)
	}
}

func TestCopyEmptyBodyEmitsNothing(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.Set("r", &httpclient.Response{Status: "204 No Content", Proto: "HTTP/1.1"})
	if cmd := m.handleKey(keyPress("y")); cmd != nil {
		t.Errorf("an empty body must not clobber the clipboard: %+v", cmd())
	}
}

func TestSelectionRendersAndEscClears(t *testing.T) {
	m := selViewer(t)
	fixedClock(t)
	plain := m.View()
	r := bodyRow(m, "alpha beta")
	press(m, r, 0)
	drag(m, r, 5)
	if m.View() == plain {
		t.Error("a selection must change the rendered output")
	}
	m.handleKey(keyPress("esc"))
	if m.HasSelection() {
		t.Error("esc must clear the selection")
	}
}

func TestSelectionClearedOnNewResponse(t *testing.T) {
	m := selViewer(t)
	fixedClock(t)
	r := bodyRow(m, "alpha beta")
	press(m, r, 0)
	drag(m, r, 5)
	m.Set("r", &httpclient.Response{Status: "200 OK", Proto: "HTTP/1.1", Body: []byte("new")})
	if m.HasSelection() {
		t.Error("a new response must drop the stale selection")
	}
}

// TestCtrlCCopies mirrors the platform copy chords onto the same path.
func TestCtrlCCopies(t *testing.T) {
	m := selViewer(t)
	cmd := m.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c must copy")
	}
	if msg := cmd().(CopyMsg); msg.What != "response body" {
		t.Errorf("ctrl+c copy: %+v", msg)
	}
}

// TestSearchOpenPrefillsFromSelection covers #2122: opening search with a
// single-row selection active seeds the query with the selected text, moves
// the cursor to its end and computes matches immediately, mirroring the
// editor's visual-selection prefill.
func TestSearchOpenPrefillsFromSelection(t *testing.T) {
	m := selViewer(t)
	fixedClock(t)
	r := bodyRow(m, "alpha beta")
	press(m, r, 0)
	drag(m, r, 5) // selects "alpha"
	m.handleKey(keyPress("/"))

	q, open := m.SearchQuery()
	if !open {
		t.Fatal("/ must open the search prompt")
	}
	if q != "alpha" {
		t.Fatalf("prefilled query: %q, want %q", q, "alpha")
	}
	if m.search.Field.Cur != len([]rune("alpha")) {
		t.Fatalf("cursor after prefill: %d, want end of query", m.search.Field.Cur)
	}
	if _, total := m.MatchPosition(); total == 0 {
		t.Error("matches must be computed immediately for the prefilled query")
	}
}

// TestSearchOpenWithoutSelectionStaysEmpty covers the no-selection branch of
// #2122: opening search without an active selection keeps prior behavior —
// an empty query.
func TestSearchOpenWithoutSelectionStaysEmpty(t *testing.T) {
	m := selViewer(t)
	m.handleKey(keyPress("/"))
	q, open := m.SearchQuery()
	if !open {
		t.Fatal("/ must open the search prompt")
	}
	if q != "" {
		t.Fatalf("query without a selection: %q, want empty", q)
	}
	if _, total := m.MatchPosition(); total != 0 {
		t.Errorf("matches without a query: %d, want 0", total)
	}
}

// TestSearchOpenMultilineSelectionStaysEmpty covers the AC that a selection
// spanning multiple rows must not break search: it simply isn't prefilled,
// same reasoning as the editor's visualSearchPrefill for multiline visual
// selections.
func TestSearchOpenMultilineSelectionStaysEmpty(t *testing.T) {
	m := selViewer(t)
	fixedClock(t)
	first := bodyRow(m, "alpha beta")
	second := bodyRow(m, "gamma delta")
	press(m, first, 0)
	drag(m, second, 5)
	if !m.HasSelection() {
		t.Fatal("drag must produce a selection")
	}
	m.handleKey(keyPress("/"))
	q, open := m.SearchQuery()
	if !open {
		t.Fatal("/ must open the search prompt")
	}
	if q != "" {
		t.Fatalf("query for a multiline selection: %q, want empty", q)
	}
}

// TestCopySelectionWhileSearching covers #2051: a selection made while the
// "/" prompt is open must still copy on ctrl+c, and the search itself must
// keep working around it.
func TestCopySelectionWhileSearching(t *testing.T) {
	m := selViewer(t)
	fixedClock(t)
	r := bodyRow(m, "alpha beta")
	press(m, r, 0)
	drag(m, r, 5)
	m.handleKey(keyPress("/"))
	if !m.search.Open {
		t.Fatal("/ must open the search prompt")
	}

	cmd := m.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c with a selection must copy while searching")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("message type: %T", cmd())
	}
	if msg.Text != "alpha" || msg.What != "selection" {
		t.Errorf("copy message: %+v", msg)
	}
	if m.HasSelection() {
		t.Error("copying must clear the selection")
	}
	if !m.search.Open {
		t.Error("copying must not close the search prompt")
	}

	// The prompt opened prefilled with the selection text (#2122). Typing
	// still reaches the query, and "y" — a valid copy chord outside search —
	// types normally instead of being stolen.
	if q, _ := m.SearchQuery(); q != "alpha" {
		t.Errorf("prefilled query: %q", q)
	}
	m.handleKey(keyPress("y"))
	m.handleKey(keyPress("e"))
	if q, _ := m.SearchQuery(); q != "alphaye" {
		t.Errorf("query after typing: %q", q)
	}

	// ctrl+c without a selection is a no-op, not a crash or a stray notice.
	if cmd := m.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd != nil {
		t.Errorf("ctrl+c without a selection must not copy: %+v", cmd())
	}
	if !m.search.Open {
		t.Error("ctrl+c without a selection must leave the prompt open")
	}
}
