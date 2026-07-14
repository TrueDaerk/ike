package debug

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestToggleAndLines(t *testing.T) {
	b := NewBreakpoints()
	if !b.Toggle("a.py", 5) || !b.Has("a.py", 5) {
		t.Fatal("first toggle must set")
	}
	b.Toggle("a.py", 2)
	if got := b.Lines("a.py"); !reflect.DeepEqual(got, []int{2, 5}) {
		t.Fatalf("Lines = %v, want sorted [2 5]", got)
	}
	if b.Toggle("a.py", 5) || b.Has("a.py", 5) {
		t.Fatal("second toggle must clear")
	}
	if b.Count() != 1 {
		t.Fatalf("Count = %d", b.Count())
	}
	b.Toggle("a.py", 2)
	if len(b.All()) != 0 {
		t.Fatal("clearing the last line must drop the file entry")
	}
}

func TestAdjustEditInsert(t *testing.T) {
	b := NewBreakpoints()
	for _, l := range []int{2, 5, 9} {
		b.Toggle("a.py", l)
	}
	// Enter at the end of line 4: one line inserted, cursor lands on 5.
	b.AdjustEdit("a.py", 5, 1)
	if got := b.Lines("a.py"); !reflect.DeepEqual(got, []int{2, 6, 10}) {
		t.Fatalf("after insert: %v, want [2 6 10]", got)
	}
	// Open a line above line 2 (O): cursor on the new line 2, delta +1 —
	// the breakpoint that was on 2 moves down with its text.
	b2 := NewBreakpoints()
	b2.Toggle("a.py", 2)
	b2.AdjustEdit("a.py", 2, 1)
	if got := b2.Lines("a.py"); !reflect.DeepEqual(got, []int{3}) {
		t.Fatalf("after open-above: %v, want [3]", got)
	}
}

func TestAdjustEditDelete(t *testing.T) {
	b := NewBreakpoints()
	for _, l := range []int{2, 5, 9} {
		b.Toggle("a.py", l)
	}
	// dd on line 3 (cursor stays on 3): lines below pull up.
	b.AdjustEdit("a.py", 3, -1)
	if got := b.Lines("a.py"); !reflect.DeepEqual(got, []int{2, 4, 8}) {
		t.Fatalf("after delete: %v, want [2 4 8]", got)
	}
	// Deleting several lines collapses in-range breakpoints onto the cursor
	// row, deduplicated.
	b2 := NewBreakpoints()
	for _, l := range []int{4, 5, 6} {
		b2.Toggle("a.py", l)
	}
	b2.AdjustEdit("a.py", 3, -3)
	if got := b2.Lines("a.py"); !reflect.DeepEqual(got, []int{3}) {
		t.Fatalf("after range delete: %v, want [3]", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	b := NewBreakpoints()
	b.Toggle("pkg/a.py", 7)
	b.Toggle("b.go", 1)
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if !got.Has("pkg/a.py", 7) || !got.Has("b.go", 1) || got.Count() != 2 {
		t.Fatalf("round trip lost data: %v", got.All())
	}
}

func TestLoadTolerant(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	if Load().Count() != 0 {
		t.Fatal("missing file must load empty")
	}
	if err := os.WriteFile(filepath.Join(dir, "breakpoints.json"), []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Load().Count() != 0 {
		t.Fatal("malformed file must load empty")
	}
}
