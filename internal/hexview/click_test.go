package hexview

import "testing"

// hexX / textX are the pane-local x of byte i's first cell in the hex and
// text columns of a row (8-digit offset gutter).
func hexX(i int) int       { return 10 + 3*i + i/8 }
func textX(per, i int) int { return 10 + 3*per + per/8 + i }

func clickData(n int) []byte {
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(i)
	}
	return data
}

// TestClickHexColumn: a click on either digit of a hex cell moves the cursor
// to that byte and activates the hex column, also across the mid gap.
func TestClickHexColumn(t *testing.T) {
	m := newModel(t, clickData(256))
	key(m, "tab") // start in the text column
	for _, c := range []struct {
		x, y int
		want int64
	}{
		{hexX(3), 0, 3},
		{hexX(3) + 1, 0, 3},
		{hexX(8), 1, 16 + 8},
		{hexX(15) + 1, 2, 32 + 15},
	} {
		if !m.Click(c.x, c.y) {
			t.Fatalf("click (%d,%d) missed", c.x, c.y)
		}
		if m.Cursor() != c.want || m.ColumnLabel() != "HEX" {
			t.Fatalf("click (%d,%d): cursor %d column %s, want %d HEX", c.x, c.y, m.Cursor(), m.ColumnLabel(), c.want)
		}
	}
}

// TestClickTextColumn: a click on a character moves the cursor to the same
// byte and activates the text column; insertion survives a text click and
// ends on a hex click.
func TestClickTextColumn(t *testing.T) {
	m := newModel(t, clickData(256))
	per := m.PerRow()
	if !m.Click(textX(per, 7), 1) {
		t.Fatal("text click missed")
	}
	if want := int64(per + 7); m.Cursor() != want || m.ColumnLabel() != "TEXT" {
		t.Fatalf("cursor %d column %s, want %d TEXT", m.Cursor(), m.ColumnLabel(), want)
	}
	key(m, "i")
	m.Click(textX(per, 0), 0)
	if m.ColumnLabel() != "TEXT INSERT" {
		t.Fatalf("text click ended insertion: %s", m.ColumnLabel())
	}
	m.Click(hexX(0), 0)
	if m.ColumnLabel() != "HEX" {
		t.Fatalf("hex click kept %s", m.ColumnLabel())
	}
}

// TestClickAllRowWidths: hit-testing follows the layout for 8, 16 and 32
// bytes per row.
func TestClickAllRowWidths(t *testing.T) {
	m := newModel(t, clickData(512))
	for _, w := range []int{60, 100, 160} {
		m.SetSize(w, 20)
		per := m.PerRow()
		if rowWidth(per, 8) > w {
			t.Fatalf("width %d: row of %d does not fit", w, per)
		}
		last := per - 1
		m.Click(hexX(last), 1)
		if want := int64(per + last); m.Cursor() != want {
			t.Fatalf("per %d hex: cursor %d, want %d", per, m.Cursor(), want)
		}
		m.Click(textX(per, last), 2)
		if want := int64(2*per + last); m.Cursor() != want {
			t.Fatalf("per %d text: cursor %d, want %d", per, m.Cursor(), want)
		}
	}
}

// TestClickScrolled: the row offset of a scrolled view is honoured.
func TestClickScrolled(t *testing.T) {
	m := newModel(t, clickData(4096))
	per := int64(m.PerRow())
	key(m, "G")
	top := m.top
	if top == 0 {
		t.Fatal("the view did not scroll")
	}
	m.Click(hexX(4), 2)
	if want := top + 2*per + 4; m.Cursor() != want {
		t.Fatalf("cursor %d, want %d", m.Cursor(), want)
	}
	if m.top != top {
		t.Fatalf("the click scrolled the view: top %d, was %d", m.top, top)
	}
}

// TestClickOutsideCells: the gutter, the gaps, the inspector row and the
// footer leave the cursor alone; past the data in the last row clamps to
// the last byte, below the data misses.
func TestClickOutsideCells(t *testing.T) {
	m := newModel(t, clickData(20))
	per := m.PerRow()
	key(m, "l", "l")
	rows := m.bodyRows()
	for _, c := range [][2]int{
		{0, 0},                 // offset gutter
		{9, 0},                 // gutter spacing
		{hexX(1) - 1, 0},       // between two hex cells
		{hexX(8) - 1, 0},       // mid gap
		{textX(per, 0) - 1, 0}, // hex/text separator
		{textX(per, per), 0},   // past the text column
		{hexX(0), rows},        // inspector row
		{hexX(0), rows + 1},    // footer
		{hexX(0), 2},           // row below the data
		{hexX(0), -1},
	} {
		if m.Click(c[0], c[1]) {
			t.Fatalf("click (%d,%d) hit a cell", c[0], c[1])
		}
		if m.Cursor() != 2 {
			t.Fatalf("click (%d,%d) moved the cursor to %d", c[0], c[1], m.Cursor())
		}
	}
	if !m.Click(hexX(per-1), 1) || m.Cursor() != 19 {
		t.Fatalf("past-the-end click: cursor %d, want 19", m.Cursor())
	}
}

// TestClickDropsSelectionAndCommitsNibble: a click ends a range selection
// and closes a half-typed byte into one undo step, like a keyboard move.
func TestClickDropsSelectionAndCommitsNibble(t *testing.T) {
	m := newModel(t, clickData(64))
	key(m, "v", "l", "l")
	m.Click(hexX(5), 0)
	if _, _, ok := m.Selection(); ok {
		t.Fatal("the click kept the selection")
	}
	key(m, "f")
	m.Click(hexX(9), 0)
	if m.nibble {
		t.Fatal("the click left the nibble pending")
	}
	if got := m.byteAt(5); got != 0xf5 {
		t.Fatalf("byte 5 = %#x, want 0xf5", got)
	}
	m.Undo()
	if got := m.byteAt(5); got != 0x05 {
		t.Fatalf("undo left byte 5 = %#x", got)
	}
}
