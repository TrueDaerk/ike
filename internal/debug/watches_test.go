package debug

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestWatchesMutations covers the add/replace/remove vocabulary the panel
// drives, including the "an emptied expression removes the row" rule (#2174).
func TestWatchesMutations(t *testing.T) {
	w := NewWatches()
	if w.Add("  ") {
		t.Fatal("a blank expression must not become a watch")
	}
	if !w.Add(" x + 1 ") || !w.Add("obj.field") {
		t.Fatal("Add must report the change")
	}
	if got := w.List(); !reflect.DeepEqual(got, []string{"x + 1", "obj.field"}) {
		t.Fatalf("List = %v", got)
	}
	if !w.Replace(0, "x + 2") {
		t.Fatal("Replace must report the change")
	}
	if w.Replace(0, "x + 2") {
		t.Fatal("Replace with the same text is not a change")
	}
	if w.Replace(5, "nope") || w.Remove(-1) {
		t.Fatal("out-of-range indices must be no-ops")
	}
	if !w.Replace(1, "   ") {
		t.Fatal("an emptied expression must remove its row")
	}
	if got := w.List(); !reflect.DeepEqual(got, []string{"x + 2"}) {
		t.Fatalf("after the emptied edit: %v", got)
	}
	if !w.Remove(0) || w.Len() != 0 {
		t.Fatalf("Remove left %d expressions", w.Len())
	}
}

// TestWatchesListIsACopy: the caller may hold the slice without aliasing the
// store — the app hands it to an evaluation goroutine.
func TestWatchesListIsACopy(t *testing.T) {
	w := NewWatches()
	w.Add("a")
	got := w.List()
	w.Add("b")
	if len(got) != 1 {
		t.Fatalf("List aliased the store: %v", got)
	}
}

// TestWatchesPersist round-trips the list through .ike/watches.json.
func TestWatchesPersist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	w := NewWatches()
	w.Add("x")
	w.Add("obj.field")
	if err := w.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "watches.json")); err != nil {
		t.Fatalf("watches.json not written: %v", err)
	}
	if got := LoadWatches().List(); !reflect.DeepEqual(got, []string{"x", "obj.field"}) {
		t.Fatalf("reloaded %v", got)
	}
}

// TestWatchesLoadTolerant: a missing or malformed file loads empty — watches
// are convenience state, never a startup error.
func TestWatchesLoadTolerant(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	if LoadWatches().Len() != 0 {
		t.Fatal("a missing file must load empty")
	}
	if err := os.WriteFile(filepath.Join(dir, "watches.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if LoadWatches().Len() != 0 {
		t.Fatal("a malformed file must load empty")
	}
	if err := os.WriteFile(filepath.Join(dir, "watches.json"),
		[]byte(`{"expressions":["x","  ","y"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadWatches().List(); !reflect.DeepEqual(got, []string{"x", "y"}) {
		t.Fatalf("blank entries survived the load: %v", got)
	}
}
