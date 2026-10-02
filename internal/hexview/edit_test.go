package hexview

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// edit_test.go covers the overwrite edit mode (#2876): the two columns, hex
// nibble typing, text insertion, the overlay read path, undo/redo and the
// in-place save.

// seq is ten distinct bytes, so every edit is visible against the original.
func seq() []byte { return []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9} }

// view returns the bytes as the pane shows them, overlay included.
func view(m *Model) []byte { return append([]byte(nil), m.readAt(0, int(m.size))...) }

// TestEditTyping is the typing table of the issue: hex nibbles in the hex
// column, characters in text insertion, the cursor after each, and a size
// that never changes.
func TestEditTyping(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want []byte
		cur  int64
	}{
		{"two hex bytes", []string{"6", "b", "d", "4"},
			[]byte{0x6b, 0xd4, 2, 3, 4, 5, 6, 7, 8, 9}, 2},
		{"uppercase hex", []string{"A", "F"},
			[]byte{0xaf, 1, 2, 3, 4, 5, 6, 7, 8, 9}, 1},
		{"lone nibble replaces the high nibble", []string{"l", "l", "l", "7"},
			[]byte{0, 1, 2, 0x73, 4, 5, 6, 7, 8, 9}, 3},
		{"moving away commits the half edit", []string{"f", "l", "e", "e"},
			[]byte{0xf0, 0xee, 2, 3, 4, 5, 6, 7, 8, 9}, 2},
		{"non-hex printable ignored in hex column", []string{"z", "x", "1", "2"},
			[]byte{0x12, 1, 2, 3, 4, 5, 6, 7, 8, 9}, 1},
		{"text insertion", []string{"tab", "i", "z", ".", "0"},
			[]byte{'z', '.', '0', 3, 4, 5, 6, 7, 8, 9}, 3},
		{"i from the hex column inserts text", []string{"i", "A"},
			[]byte{'A', 1, 2, 3, 4, 5, 6, 7, 8, 9}, 1},
		{"text column without i navigates", []string{"tab", "l", "l", "z"},
			seq(), 2},
		{"esc leaves insertion", []string{"tab", "i", "a", "esc", "l", "b"},
			[]byte{'a', 1, 2, 3, 4, 5, 6, 7, 8, 9}, 2},
		{"arrows move while inserting", []string{"tab", "i", "right", "q"},
			[]byte{0, 'q', 2, 3, 4, 5, 6, 7, 8, 9}, 2},
		{"multi-byte UTF-8 writes every byte", []string{"tab", "i", "é"},
			[]byte{0xc3, 0xa9, 2, 3, 4, 5, 6, 7, 8, 9}, 2},
		{"multi-byte UTF-8 at the tail is refused", []string{"G", "tab", "i", "é"},
			seq(), 9},
		{"last byte keeps the cursor in range", []string{"G", "f", "f"},
			[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 0xff}, 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(t, seq())
			key(m, tc.keys...)
			if got := view(m); !bytes.Equal(got, tc.want) {
				t.Fatalf("bytes = % x, want % x", got, tc.want)
			}
			if m.Cursor() != tc.cur {
				t.Fatalf("cursor = %d, want %d", m.Cursor(), tc.cur)
			}
			if m.size != 10 {
				t.Fatalf("size changed to %d", m.size)
			}
			if st, _ := os.Stat(m.path); st.Size() != 10 {
				t.Fatalf("file size on disk changed to %d before any save", st.Size())
			}
		})
	}
}

// TestHalfNibbleShowsInRender: after one nibble the byte already shows the
// replaced high nibble with the old low nibble, and the cursor stays on it.
func TestHalfNibbleShowsInRender(t *testing.T) {
	m := newModel(t, []byte{0x12, 0x34})
	key(m, "a")
	if m.Cursor() != 0 {
		t.Fatalf("cursor moved after one nibble: %d", m.Cursor())
	}
	if !strings.Contains(stripANSI(m.View()), "a2 34") {
		t.Fatalf("half edit must render as a2, got %q", stripANSI(m.View()))
	}
}

// TestTabSwitchesColumnFooter: tab toggles the active column and the footer
// names it.
func TestTabSwitchesColumnFooter(t *testing.T) {
	m := newModel(t, seq())
	if m.ColumnLabel() != "HEX" || !strings.Contains(stripANSI(m.footer()), "HEX") {
		t.Fatalf("starts in the hex column, footer %q", stripANSI(m.footer()))
	}
	key(m, "tab")
	if m.ColumnLabel() != "TEXT" || !strings.Contains(stripANSI(m.footer()), "TEXT") {
		t.Fatalf("tab must switch to the text column, footer %q", stripANSI(m.footer()))
	}
	key(m, "i")
	if !strings.Contains(stripANSI(m.footer()), "TEXT INSERT") {
		t.Fatalf("i must show insertion, footer %q", stripANSI(m.footer()))
	}
	key(m, "tab")
	if m.ColumnLabel() != "HEX" {
		t.Fatalf("tab back must land in the hex column and leave insertion, got %s", m.ColumnLabel())
	}
}

// TestEditHighlightClasses: the active cursor, its mirror in the other
// column and a modified byte classify — and render — three different ways.
func TestEditHighlightClasses(t *testing.T) {
	m := newModel(t, seq())
	key(m, "f", "f") // byte 0 modified, cursor on byte 1
	if got := m.classify(1, colHex); got != classCursor {
		t.Fatalf("cursor in the active hex column = %v, want classCursor", got)
	}
	if got := m.classify(1, colText); got != classMirror {
		t.Fatalf("cursor in the other column = %v, want classMirror", got)
	}
	for _, col := range []column{colHex, colText} {
		if got := m.classify(0, col); got != classModified {
			t.Fatalf("edited byte in column %d = %v, want classModified", col, got)
		}
	}
	if got := m.classify(2, colHex); got != classPlain {
		t.Fatalf("untouched byte = %v, want classPlain", got)
	}
	key(m, "tab")
	if m.classify(1, colText) != classCursor || m.classify(1, colHex) != classMirror {
		t.Fatal("tab must move the strong cursor to the text column")
	}
	st := m.styles()
	a, b, c := st[classCursor].Render("x"), st[classMirror].Render("x"), st[classModified].Render("x")
	if a == b || b == c || a == c {
		t.Fatalf("cursor, mirror and modified must look different: %q %q %q", a, b, c)
	}
}

// TestUndoRedo: u undoes a completed hex byte or a text keystroke, ctrl+r
// redoes it, and undoing everything leaves the pane clean.
func TestUndoRedo(t *testing.T) {
	m := newModel(t, seq())
	key(m, "6", "b", "d", "4")
	key(m, "u")
	if got := view(m); !bytes.Equal(got[:2], []byte{0x6b, 1}) || m.Cursor() != 1 {
		t.Fatalf("undo must revert the last byte only, got % x cursor %d", got[:2], m.Cursor())
	}
	key(m, "u")
	if m.Dirty() {
		t.Fatal("undoing every edit must leave the pane clean")
	}
	key(m, "ctrl+r", "ctrl+r")
	if got := view(m); !bytes.Equal(got[:2], []byte{0x6b, 0xd4}) || m.Cursor() != 2 {
		t.Fatalf("redo must re-apply both bytes, got % x cursor %d", got[:2], m.Cursor())
	}
	// A half-typed nibble is one undo step once u commits it.
	key(m, "a", "u")
	if got := view(m); got[2] != 2 {
		t.Fatalf("undo of a half-typed byte must restore it, got %x", got[2])
	}
	// Text keystrokes undo one at a time.
	key(m, "tab", "i", "x", "y", "esc", "u")
	if got := view(m); got[2] != 'x' || got[3] != 3 {
		t.Fatalf("undo must revert the last keystroke, got % x", got[:4])
	}
}

// TestOverlayVisibleToSearchAndInspector: search and the inspector row read
// through the overlay.
func TestOverlayVisibleToSearchAndInspector(t *testing.T) {
	m := newModel(t, seq())
	key(m, "tab", "i", "H", "I", "esc")
	key(m, "g")
	if !strings.Contains(m.inspectorRow(), "u8 72") {
		t.Fatalf("inspector must decode the edited byte, got %q", m.inspectorRow())
	}
	key(m, "/", "H", "I", "enter")
	if got := m.Matches(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("search must find the edited bytes, got %v", got)
	}
}

// TestSaveWritesInPlace: save writes only the changed bytes, keeps the size,
// clears the overlay, and a fresh model sees the new bytes.
func TestSaveWritesInPlace(t *testing.T) {
	m := newModel(t, seq())
	key(m, "a", "a", "b", "b", "l", "l", "c") // 0, 1 changed, 4 half-typed
	if !m.Dirty() {
		t.Fatal("edits must mark the pane dirty")
	}
	if !strings.Contains(stripANSI(m.footer()), "●") {
		t.Fatalf("the footer must show the dirty marker, got %q", stripANSI(m.footer()))
	}
	n, err := m.Save()
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("wrote %d bytes, want 3", n)
	}
	if m.Dirty() || strings.Contains(stripANSI(m.footer()), "●") {
		t.Fatal("save must clear the overlay and the dirty marker")
	}
	disk, _ := os.ReadFile(m.path)
	want := []byte{0xaa, 0xbb, 2, 3, 0xc4, 5, 6, 7, 8, 9}
	if !bytes.Equal(disk, want) {
		t.Fatalf("disk = % x, want % x", disk, want)
	}
	if got := view(m); !bytes.Equal(got, want) {
		t.Fatalf("the pane must re-read the saved bytes, got % x", got)
	}
	re := New("hex2", m.path, testPal())
	defer re.Close()
	if got := re.readAt(0, 10); !bytes.Equal(got, want) {
		t.Fatalf("reopened pane shows % x", got)
	}
	// Undo after save dirties the pane against the new disk state.
	key(m, "u")
	if !m.Dirty() {
		t.Fatal("undo after save must dirty the pane again")
	}
}

// TestSaveRefusesResizedFile: offsets of a file that changed size on disk
// no longer mean what they meant.
func TestSaveRefusesResizedFile(t *testing.T) {
	m := newModel(t, seq())
	key(m, "f", "f")
	if err := os.WriteFile(m.path, []byte{1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Save(); err == nil {
		t.Fatal("save over a resized file must fail")
	}
	if !m.Dirty() {
		t.Fatal("a failed save keeps the edits")
	}
}

// TestPasteOverwritesInInsertion: a paste in text insertion overwrites like
// typing; outside insertion it is dropped.
func TestPasteOverwritesInInsertion(t *testing.T) {
	m := newModel(t, seq())
	m.PasteText("ab")
	if m.Dirty() {
		t.Fatal("a paste outside insertion must not edit")
	}
	key(m, "i")
	m.PasteText("ab")
	if got := view(m); !bytes.Equal(got[:3], []byte{'a', 'b', 2}) || m.Cursor() != 2 {
		t.Fatalf("paste must overwrite at the cursor, got % x cursor %d", got[:3], m.Cursor())
	}
}
