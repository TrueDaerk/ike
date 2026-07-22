package terminal

import (
	"strings"
	"testing"
)

// startNarrowSh spawns a 20-column /bin/sh so ordinary output soft-wraps.
func startNarrowSh(t *testing.T, c *collector) *Session {
	t.Helper()
	s, err := StartSession("terminal", "/bin/sh", t.TempDir(), 20, 24, nil, c.send)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// wrapped30 is a 30-char line: on a 20-column grid it renders as the full row
// "abcdefghij0123456789" wrapped into "ABCDEFGHIJ".
const wrapped30 = "abcdefghij0123456789ABCDEFGHIJ"

// printWrapped emits wrapped30 as one logical output line and returns the
// virtual line index of its first (full-width) row.
func printWrapped(t *testing.T, s *Session) int {
	t.Helper()
	for _, r := range "printf '%s\\n' " + wrapped30 + "\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "wrapped output", func() bool {
		return strings.Contains(plainView(s), "ABCDEFGHIJ")
	})
	// The echoed command wraps too; the output's first row is the one whose
	// full 20 columns are exactly the head of wrapped30 and that continues
	// into a row starting with the tail.
	sb := s.ScrollbackLen()
	for v := 0; v < sb+24; v++ {
		if s.LineText(v) == wrapped30[:20] && strings.HasPrefix(s.LineText(v+1), wrapped30[20:]) {
			return v
		}
	}
	t.Fatalf("wrapped output rows not found; view:\n%s", plainView(s))
	return -1
}

// screenRow converts a virtual line index to the pane-local row a mouse event
// uses (live view, no scroll offset).
func screenRow(s *Session, v int) int { return v - s.ScrollbackLen() }

// TestSelectionJoinsSoftWrappedRows guards #936: dragging across a
// soft-wrapped logical line copies it as one line — no `\n` at the visual
// wrap point.
func TestSelectionJoinsSoftWrappedRows(t *testing.T) {
	c := &collector{}
	s := startNarrowSh(t, c)
	m := Model{sess: s, h: 24, w: 20}

	v := printWrapped(t, s)
	if !s.SoftWrapped(v) {
		t.Fatal("the full first row must read as soft-wrapped")
	}
	if s.SoftWrapped(v + 1) {
		t.Fatal("the short second row ends the logical line")
	}

	row := screenRow(s, v)
	m.MousePress(0, row)
	m.MouseDrag(10, row+1)
	m.MouseRelease(10, row+1)
	if got := m.SelectionText(); got != wrapped30 {
		t.Fatalf("selection = %q, want %q (no embedded newline)", got, wrapped30)
	}
}

// TestTripleClickSelectsLogicalLine guards #936: a triple click on either row
// of a soft-wrapped line selects the whole logical line, and cmd+c-style
// extraction returns it newline-free. Hard newlines around it stay out.
func TestTripleClickSelectsLogicalLine(t *testing.T) {
	c := &collector{}
	s := startNarrowSh(t, c)
	m := Model{sess: s, h: 24, w: 20}

	v := printWrapped(t, s)
	// Triple click lands on the second (continuation) row: the selection
	// must still cover the rows above it.
	row := screenRow(s, v+1)
	m.MousePress(3, row)
	m.MouseRelease(3, row)
	m.MousePress(3, row)
	m.MouseRelease(3, row)
	m.MousePress(3, row)
	m.MouseRelease(3, row)
	if !m.HasSelection() {
		t.Fatal("triple click should select the logical line")
	}
	if got := m.SelectionText(); got != wrapped30 {
		t.Fatalf("triple-click selection = %q, want %q", got, wrapped30)
	}
}

// TestDoubleClickSelectsWord guards #936: a double click selects the word
// under the pointer with shell-friendly boundaries (path characters glue),
// and a word spanning the soft-wrap break stays whole.
func TestDoubleClickSelectsWord(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	m := Model{sess: s, h: 24, w: 80}

	for _, r := range "echo run /usr/local/bin now\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "echo output", func() bool {
		return strings.Count(plainView(s), "/usr/local/bin") >= 2
	})
	rows := strings.Split(plainView(s), "\n")
	rowIdx, colIdx := -1, -1
	for i, r := range rows {
		if idx := strings.Index(r, "/usr/local/bin"); idx >= 0 && !strings.Contains(r, "echo ") {
			rowIdx, colIdx = i, idx
			break
		}
	}
	if rowIdx < 0 {
		t.Fatalf("output row not found in:\n%s", plainView(s))
	}

	// Double click in the middle of the path: slashes glue it into one word.
	m.MousePress(colIdx+6, rowIdx)
	m.MouseRelease(colIdx+6, rowIdx)
	m.MousePress(colIdx+6, rowIdx)
	m.MouseRelease(colIdx+6, rowIdx)
	if got := m.SelectionText(); got != "/usr/local/bin" {
		t.Fatalf("double-click selection = %q, want %q", got, "/usr/local/bin")
	}
}

// TestDoubleClickWordAcrossWrap guards #936: the word under the pointer
// extends across the soft-wrap break in both directions.
func TestDoubleClickWordAcrossWrap(t *testing.T) {
	c := &collector{}
	s := startNarrowSh(t, c)
	m := Model{sess: s, h: 24, w: 20}

	v := printWrapped(t, s)
	// wrapped30 is one unbroken word (alphanumerics only): a double click on
	// the first row must select all 30 chars across both rows.
	row := screenRow(s, v)
	m.MousePress(5, row)
	m.MouseRelease(5, row)
	m.MousePress(5, row)
	m.MouseRelease(5, row)
	if got := m.SelectionText(); got != wrapped30 {
		t.Fatalf("cross-wrap word selection = %q, want %q", got, wrapped30)
	}
}
