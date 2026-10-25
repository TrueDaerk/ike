package complexity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hidden_test.go covers the hidden set (#3031): the baseline file's hidden
// section round trip, a version 1 file still loading, the ordinal matching
// of Mask / Visible and the malformed-entry errors.

func TestHiddenSetRoundTripInBaselineFile(t *testing.T) {
	dir := t.TempDir()
	path := BaselinePath(dir)
	b := NewBaseline(15, 15)
	b.Accept("a.go", Function{Name: "f", CCN: 20, Cognitive: 3}, 0)
	if !b.Hide(EntryKey{"lib/app.js", "(anonymous)", 2}) || b.Hide(EntryKey{"lib/app.js", "(anonymous)", 2}) {
		t.Fatal("Hide must report the first add only")
	}
	b.Hide(EntryKey{"a.go", "z", 0})
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `  "hidden": [
    {
      "file": "a.go",
      "function": "z",
      "ordinal": 0
    },
    {
      "file": "lib/app.js",
      "function": "(anonymous)",
      "ordinal": 2
    }
  ]
}
`
	if !strings.HasSuffix(string(data), want) || !strings.Contains(string(data), `"version": 2`) {
		t.Fatalf("written file:\n%s", data)
	}
	got, err := LoadBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.HiddenOf().Len() != 2 || !got.HiddenOf().Has(EntryKey{"lib/app.js", "(anonymous)", 2}) || got.HiddenOf().Has(EntryKey{"lib/app.js", "(anonymous)", 1}) {
		t.Fatalf("loaded hidden = %+v", got.HiddenOf().Entries())
	}
	if got.Len() != 1 {
		t.Fatalf("the accepted half must survive: %+v", got.Accepted)
	}
	// Un-hiding the last entry drops the section from the file again.
	if !got.Unhide(EntryKey{"a.go", "z", 0}) || got.Unhide(EntryKey{"a.go", "z", 0}) {
		t.Fatal("Unhide must report the first removal only")
	}
	got.Unhide(EntryKey{"lib/app.js", "(anonymous)", 2})
	if err := got.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "hidden") {
		t.Fatalf("an empty hidden set must not be written:\n%s", data)
	}
}

func TestLoadBaselineReadsVersionOneAndValidatesHidden(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v1.json")
	if err := os.WriteFile(path, []byte(`{"version": 1, "accepted": [{"file": "a.go", "function": "f", "ordinal": 0, "ccn": 3, "cognitive": 1}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := LoadBaseline(path)
	if err != nil || b == nil || b.Len() != 1 || b.HiddenOf().Len() != 0 {
		t.Fatalf("version 1 = %+v, %v", b, err)
	}
	// Hiding into a version 1 file writes version 2.
	b.Hide(EntryKey{"a.go", "g", 0})
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"version": 2`) {
		t.Fatalf("a hidden section needs version 2:\n%s", data)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"version": 2, "accepted": [], "hidden": [{"file": "a.go", "ordinal": 0}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := LoadBaseline(bad); err == nil || b != nil || !strings.Contains(err.Error(), "hidden[0]") {
		t.Fatalf("malformed hidden entry = %v, %v", b, err)
	}
}

func TestHiddenSetMaskMatchesByOrdinal(t *testing.T) {
	fns := []Function{{Name: "(anonymous)", CCN: 30}, {Name: "own", CCN: 2}, {Name: "(anonymous)", CCN: 40}, {Name: "(anonymous)", CCN: 1}}
	var h *HiddenSet
	if h.Mask("app.js", fns) != nil || h.Len() != 0 || h.Has(EntryKey{}) {
		t.Fatal("a nil set hides nothing")
	}
	if v, n := h.Visible("app.js", fns); len(v) != 4 || n != 0 {
		t.Fatalf("nil Visible = %d, %d", len(v), n)
	}
	h = &HiddenSet{}
	h.Add(EntryKey{"app.js", "(anonymous)", 1})
	mask := h.Mask("app.js", fns)
	if !mask[2] || mask[0] || mask[1] || mask[3] {
		t.Fatalf("mask = %v, want only the second anonymous unit", mask)
	}
	// The unit moved below another one: its ordinal among the same-named
	// units is unchanged, so it stays hidden.
	moved := []Function{{Name: "(anonymous)", CCN: 30}, {Name: "own", CCN: 2}, {Name: "extra", CCN: 2}, {Name: "(anonymous)", CCN: 40}, {Name: "(anonymous)", CCN: 1}}
	v, n := h.Visible("app.js", moved)
	if n != 1 || len(v) != 4 || v[3].CCN != 1 {
		t.Fatalf("Visible after a move = %+v, %d", v, n)
	}
	if h.Mask("other.js", fns) != nil && h.Mask("other.js", fns)[2] {
		t.Fatal("another file's unit is not hidden")
	}
	if !h.Remove(EntryKey{"app.js", "(anonymous)", 1}) || h.Len() != 0 {
		t.Fatal("Remove")
	}
}
