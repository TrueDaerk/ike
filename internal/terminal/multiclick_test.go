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

// TestReflowShrinkRewraps guards #935: a width shrink rewraps overlong lines
// at the new width — as if the terminal had always been that small — instead
// of clipping them (#947's old failure mode). The rewrapped rows read as one
// logical line: triple-click selects it whole and copy joins it, while the
// hard newline before the marker line survives.
func TestReflowShrinkRewraps(t *testing.T) {
	c := &collector{}
	s, err := StartSession("terminal", "/bin/sh", t.TempDir(), 40, 24, nil, c.send)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	m := Model{sess: s, h: 24, w: 40}

	// Two independent lines at width 40 — neither wraps yet.
	for _, r := range "printf '%s\\n%s\\n' " + wrapped30 + " zzz-end-marker\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "unwrapped output", func() bool {
		return strings.Contains(plainView(s), "zzz-end-marker")
	})

	s.Resize(20, 24)
	waitFor(t, "shrink applied", func() bool { return s.Width() == 20 })
	m.w = 20

	// The 30-char line rewrapped: full head row, continuation row, then the
	// untouched marker line.
	sb := s.ScrollbackLen()
	v := -1
	for i := 0; i < sb+24; i++ {
		if s.LineText(i) == wrapped30[:20] && s.LineText(i+1) == wrapped30[20:] &&
			strings.HasPrefix(s.LineText(i+2), "zzz-end-marker") {
			v = i
			break
		}
	}
	if v < 0 {
		t.Fatalf("rewrapped rows not found; view:\n%s", plainView(s))
	}
	if !s.SoftWrapped(v) {
		t.Fatal("the rewrapped head row must read as soft-wrapped")
	}
	if s.SoftWrapped(v + 1) {
		t.Fatal("the hard newline before the marker line must survive the reflow")
	}

	// Triple-click on the continuation row selects the whole logical line.
	row := v + 1 - s.ScrollbackLen()
	for i := 0; i < 3; i++ {
		m.MousePress(3, row)
		m.MouseRelease(3, row)
	}
	if got := m.SelectionText(); got != wrapped30 {
		t.Fatalf("triple-click on rewrapped line = %q, want %q", got, wrapped30)
	}
}

// TestReflowGrowUnwraps guards #935 the other way: growing the terminal
// merges soft-wrapped segments back into one row that extends to the new
// edge.
func TestReflowGrowUnwraps(t *testing.T) {
	c := &collector{}
	s, err := StartSession("terminal", "/bin/sh", t.TempDir(), 20, 24, nil, c.send)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)

	for _, r := range "printf '%s\\n' " + wrapped30 + "\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "wrapped output", func() bool {
		sb := s.ScrollbackLen()
		for i := 0; i < sb+24; i++ {
			if s.LineText(i) == wrapped30[:20] && s.LineText(i+1) == wrapped30[20:] {
				return true
			}
		}
		return false
	})

	s.Resize(40, 24)
	waitFor(t, "grow applied", func() bool { return s.Width() == 40 })
	waitFor(t, "line unwrapped to one row", func() bool {
		sb := s.ScrollbackLen()
		for i := 0; i < sb+24; i++ {
			if s.LineText(i) == wrapped30 {
				return true
			}
		}
		return false
	})
}

// TestReflowScrollbackRewraps guards #935 for history: lines already in the
// scrollback rewrap at the new width too, and read as soft-wrapped.
func TestReflowScrollbackRewraps(t *testing.T) {
	c := &collector{}
	s, err := StartSession("terminal", "/bin/sh", t.TempDir(), 40, 24, nil, c.send)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)

	for _, r := range "printf '%s\\n' " + wrapped30 + " && seq 1 30\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "line scrolled into history", func() bool {
		if s.ScrollbackLen() == 0 {
			return false
		}
		for i := 0; i < s.ScrollbackLen(); i++ {
			if strings.HasPrefix(s.LineText(i), wrapped30[:20]) {
				return true
			}
		}
		return false
	})

	s.Resize(20, 24)
	waitFor(t, "shrink applied", func() bool { return s.Width() == 20 })

	v := -1
	for i := 0; i < s.ScrollbackLen(); i++ {
		if s.LineText(i) == wrapped30[:20] && s.LineText(i+1) == wrapped30[20:] {
			v = i
			break
		}
	}
	if v < 0 {
		t.Fatal("rewrapped line not found in scrollback")
	}
	if !s.SoftWrapped(v) {
		t.Fatal("the rewrapped scrollback head row must read as soft-wrapped")
	}
}

// TestDoubleClickDragExtendsWordWise guards #951: after a double click,
// dragging extends the selection word by word — forward and backward — with
// the origin word always fully covered.
func TestDoubleClickDragExtendsWordWise(t *testing.T) {
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

	// Double click on "run", then drag forward through the path and on to
	// "now": each step grows the selection by whole words.
	m.MousePress(colIdx-4, rowIdx)
	m.MouseRelease(colIdx-4, rowIdx)
	m.MousePress(colIdx-4, rowIdx)
	if got := m.SelectionText(); got != "run" {
		t.Fatalf("double-click selection = %q, want %q", got, "run")
	}
	m.MouseDrag(colIdx+6, rowIdx)
	if got := m.SelectionText(); got != "run /usr/local/bin" {
		t.Fatalf("forward word drag = %q, want %q", got, "run /usr/local/bin")
	}
	m.MouseDrag(colIdx+16, rowIdx)
	if got := m.SelectionText(); got != "run /usr/local/bin now" {
		t.Fatalf("forward word drag 2 = %q, want %q", got, "run /usr/local/bin now")
	}
	// Dragging back onto the origin shrinks to the origin word again.
	m.MouseDrag(colIdx-4, rowIdx)
	if got := m.SelectionText(); got != "run" {
		t.Fatalf("drag back to origin = %q, want %q", got, "run")
	}
	m.MouseRelease(colIdx-4, rowIdx)

	// Backward: double click "now", drag back to "run".
	m.ClearSelection()
	m.MousePress(colIdx+16, rowIdx)
	m.MouseRelease(colIdx+16, rowIdx)
	m.MousePress(colIdx+16, rowIdx)
	m.MouseDrag(colIdx-3, rowIdx)
	if got := m.SelectionText(); got != "run /usr/local/bin now" {
		t.Fatalf("backward word drag = %q, want %q", got, "run /usr/local/bin now")
	}
	m.MouseRelease(colIdx-3, rowIdx)
}

// TestTripleClickDragExtendsLineWise guards #951: after a triple click,
// dragging extends by whole logical lines — a soft-wrapped chain joins as
// one line in the extended copy.
func TestTripleClickDragExtendsLineWise(t *testing.T) {
	c := &collector{}
	s := startNarrowSh(t, c)
	m := Model{sess: s, h: 24, w: 20}

	for _, r := range "printf '%s\\nzzz-mark\\n' " + wrapped30 + "\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "output", func() bool { return strings.Contains(plainView(s), "zzz-mark") })
	sb := s.ScrollbackLen()
	v := -1
	for i := 0; i < sb+24; i++ {
		if s.LineText(i) == wrapped30[:20] && s.LineText(i+1) == wrapped30[20:] &&
			strings.HasPrefix(s.LineText(i+2), "zzz-mark") {
			v = i
			break
		}
	}
	if v < 0 {
		t.Fatalf("rows not found; view:\n%s", plainView(s))
	}

	// Triple click on the marker line (third press held), then drag up into
	// the wrapped chain: the whole logical line above joins the selection.
	row := v + 2 - s.ScrollbackLen()
	m.MousePress(2, row)
	m.MouseRelease(2, row)
	m.MousePress(2, row)
	m.MouseRelease(2, row)
	m.MousePress(2, row)
	if got := m.SelectionText(); got != "zzz-mark" {
		t.Fatalf("triple-click selection = %q, want %q", got, "zzz-mark")
	}
	m.MouseDrag(3, row-1) // onto the wrapped continuation row above
	if got := m.SelectionText(); got != wrapped30+"\nzzz-mark" {
		t.Fatalf("line-wise drag up = %q, want %q", got, wrapped30+"\nzzz-mark")
	}
	m.MouseRelease(3, row-1)
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
