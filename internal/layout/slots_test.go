package layout

import (
	"reflect"
	"testing"
)

// issueTemplate is the normative #1897 example: explorer-like X on the left,
// H on the right, a bottom strip shared by T and Z, editor in the middle.
func issueTemplate(t *testing.T) *Template {
	t.Helper()
	tpl, err := ParseTemplate([]string{"XEEH", "XEEH", "TTZZ"})
	if err != nil {
		t.Fatalf("ParseTemplate: %v", err)
	}
	return tpl
}

func TestParseTemplateRejects(t *testing.T) {
	cases := []struct {
		name string
		rows []string
	}{
		{"empty", nil},
		{"ragged rows", []string{"XE", "XEE"}},
		{"blank cell", []string{"X E"}},
		{"no editor region", []string{"XT"}},
		{"non-rectangular slot", []string{"XX", "XE"}},
		{"disconnected slot", []string{"XEX"}},
		{"pinwheel interlock", []string{"AAB", "CEB", "CDD"}},
	}
	for _, c := range cases {
		if _, err := ParseTemplate(c.rows); err == nil {
			t.Errorf("%s: ParseTemplate(%v) accepted, want error", c.name, c.rows)
		}
	}
}

func TestParseTemplateSlots(t *testing.T) {
	tpl := issueTemplate(t)
	if got := tpl.SlotNames(); !reflect.DeepEqual(got, []string{"H", "T", "X", "Z"}) {
		t.Fatalf("SlotNames = %v", got)
	}
	if !tpl.HasSlot(EditorSlot) || tpl.HasSlot("Q") {
		t.Fatalf("HasSlot: editor missing or phantom slot present")
	}
	if r, ok := tpl.SlotRect("T"); !ok || r != (Rect{X: 0, Y: 2, W: 2, H: 1}) {
		t.Fatalf("SlotRect(T) = %v, %v", r, ok)
	}
}

// TestIssueExampleSequence walks the three-step scenario from #1897 on a
// 40x12 viewport (10x the 4x3 template, so template cell fractions map to
// exact cell counts): X alone spans the full height at a quarter width,
// opening T takes the full bottom strip, opening Z splits the strip into
// the defined TT/ZZ halves — and closing works symmetrically via the plain
// Close collapse.
func TestIssueExampleSequence(t *testing.T) {
	tpl := issueTemplate(t)
	vp := Rect{W: 40, H: 12}
	editor := &Leaf{Pane: "editor"}

	// Step 1: only X open.
	tree := tpl.BuildTree(map[string]string{"X": "explorer"}, editor)
	r := Rects(tree, vp)
	if r["explorer"] != (Rect{X: 0, Y: 0, W: 10, H: 12}) {
		t.Fatalf("step 1 explorer = %v", r["explorer"])
	}
	if r["editor"] != (Rect{X: 10, Y: 0, W: 30, H: 12}) {
		t.Fatalf("step 1 editor = %v", r["editor"])
	}

	// Step 2: open T — full-width bottom strip, X and editor shrink.
	tree = tpl.BuildTree(map[string]string{"X": "explorer", "T": "tool-t"}, editor)
	r = Rects(tree, vp)
	if r["tool-t"] != (Rect{X: 0, Y: 8, W: 40, H: 4}) {
		t.Fatalf("step 2 tool-t = %v", r["tool-t"])
	}
	if r["explorer"] != (Rect{X: 0, Y: 0, W: 10, H: 8}) {
		t.Fatalf("step 2 explorer = %v", r["explorer"])
	}

	// Step 3: open Z — the strip subdivides into the defined TT/ZZ halves.
	tree = tpl.BuildTree(map[string]string{"X": "explorer", "T": "tool-t", "Z": "tool-z"}, editor)
	r = Rects(tree, vp)
	if r["tool-t"] != (Rect{X: 0, Y: 8, W: 20, H: 4}) {
		t.Fatalf("step 3 tool-t = %v", r["tool-t"])
	}
	if r["tool-z"] != (Rect{X: 20, Y: 8, W: 20, H: 4}) {
		t.Fatalf("step 3 tool-z = %v", r["tool-z"])
	}

	// Closing Z through the ordinary collapse hands the strip back to T.
	tree, ok := Close(tree, "tool-z")
	if !ok {
		t.Fatalf("Close(tool-z) failed")
	}
	r = Rects(tree, vp)
	if r["tool-t"] != (Rect{X: 0, Y: 8, W: 40, H: 4}) {
		t.Fatalf("after close tool-t = %v", r["tool-t"])
	}
}

// TestBuildTreeGraftsEditorSubtree ensures the editor region keeps its own
// internal splits: BuildTree replaces the E leaf with the passed subtree
// as-is.
func TestBuildTreeGraftsEditorSubtree(t *testing.T) {
	tpl := issueTemplate(t)
	editor := &Split{Orient: Horizontal, Ratio: 0.5, A: &Leaf{"editor"}, B: &Leaf{"editor:2"}}
	tree := tpl.BuildTree(map[string]string{"H": "structure"}, editor)
	got := leaves(tree)
	if len(got) != 3 || !got["editor"] || !got["editor:2"] || !got["structure"] {
		t.Fatalf("leaves = %v", got)
	}
	// With X and the bottom strip closed, H spans the full height and its
	// ratio renormalizes within the surviving E|H group: H held 2 of the 3
	// columns left of it in the template's E region, i.e. 1/3 of the width.
	r := Rects(tree, Rect{W: 40, H: 12})
	if r["structure"] != (Rect{X: 27, Y: 0, W: 13, H: 12}) {
		t.Fatalf("structure = %v (want full-height 1/3-width column)", r["structure"])
	}
}

// TestBuildTreeNoResidents degrades to the editor subtree untouched.
func TestBuildTreeNoResidents(t *testing.T) {
	tpl := issueTemplate(t)
	editor := &Leaf{Pane: "editor"}
	if tree := tpl.BuildTree(nil, editor); tree != Node(editor) {
		t.Fatalf("BuildTree(nil) = %#v, want the editor subtree itself", tree)
	}
}

// TestBuildTreeRoundTrips guards persistence: a materialized slot tree
// encodes and decodes back to identical leaves and geometry, so a slotted
// arrangement survives layout.json untouched.
func TestBuildTreeRoundTrips(t *testing.T) {
	tpl := issueTemplate(t)
	tree := tpl.BuildTree(map[string]string{"X": "explorer", "T": "tool-t", "Z": "tool-z"}, &Leaf{Pane: "editor"})
	data, err := Encode(tree)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, ok := Decode(data, leaves(tree))
	if !ok {
		t.Fatalf("Decode rejected the slot tree")
	}
	vp := Rect{W: 40, H: 12}
	if !reflect.DeepEqual(Rects(tree, vp), Rects(back, vp)) {
		t.Fatalf("geometry drifted across encode/decode")
	}
	if !reflect.DeepEqual(Leaves(tree), Leaves(back)) {
		t.Fatalf("leaf order drifted across encode/decode")
	}
}

func TestRemoveLeaves(t *testing.T) {
	tpl := issueTemplate(t)
	editor := &Split{Orient: Vertical, Ratio: 0.5, A: &Leaf{"editor"}, B: &Leaf{"editor:2"}}
	tree := tpl.BuildTree(map[string]string{"X": "explorer", "T": "tool-t"}, editor)

	rest, ok := RemoveLeaves(tree, map[string]bool{"explorer": true, "tool-t": true})
	if !ok {
		t.Fatalf("RemoveLeaves failed")
	}
	if got := leaves(rest); len(got) != 2 || !got["editor"] || !got["editor:2"] {
		t.Fatalf("remainder leaves = %v", got)
	}

	// Dropping everything must refuse instead of emptying the tree.
	if _, ok := RemoveLeaves(Clone(editor), map[string]bool{"editor": true, "editor:2": true}); ok {
		t.Fatalf("RemoveLeaves emptied the tree")
	}

	// Ids absent from the tree are ignored.
	rest, ok = RemoveLeaves(&Leaf{Pane: "editor"}, map[string]bool{"ghost": true})
	if !ok || len(leaves(rest)) != 1 {
		t.Fatalf("RemoveLeaves with absent ids: %v %v", rest, ok)
	}
}
