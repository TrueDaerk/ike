package settings

// intlist_test.go covers the IntList entry type (#1663): the List editor over a
// []int config field — editor.rulers is the first one. Elements must parse as
// numbers and the commit has to write a TOML integer array, because a string
// array would fail the typed decode.

import (
	"testing"

	"ike/internal/config"
)

func rulerPages() []Page {
	return []Page{
		{Title: "Editor", Entries: []Entry{
			{Key: "editor.rulers", Type: IntList, Title: "Rulers", Scope: config.UserScope},
		}},
	}
}

// openRulerEditor opens the rulers entry's editor in the detail column.
func openRulerEditor(t *testing.T) (*Model, *listEditor) {
	t.Helper()
	m := New(rulerPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*listEditor)
	if !ok {
		t.Fatalf("editor = %T, want *listEditor", m.editor)
	}
	if !ed.numeric {
		t.Fatal("an IntList entry must open the numeric list editor")
	}
	return m, ed
}

func TestIntListCommitsIntArray(t *testing.T) {
	restoreConfig(t)
	m, ed := openRulerEditor(t)
	for _, col := range []string{"80", "120"} {
		ed.idx = len(ed.items) // the "+ add value…" row
		m.Update(key("enter"))
		ed.tf.Set(col)
		m.Update(key("enter"))
	}
	apply(t, m.applyChanges())
	got := config.Get().Editor.Rulers
	want := []int{80, 120}
	if len(got) != len(want) {
		t.Fatalf("rulers = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rulers = %v, want %v", got, want)
		}
	}
}

// TestIntListRejectsNonNumber: a non-numeric element is refused in place — the
// row stays in edit mode with an error and nothing is staged, so the typed
// decode never sees a string where an int belongs.
func TestIntListRejectsNonNumber(t *testing.T) {
	restoreConfig(t)
	m, ed := openRulerEditor(t)
	ed.idx = len(ed.items)
	m.Update(key("enter"))
	ed.tf.Set("eighty")
	m.Update(key("enter"))
	if !ed.editing {
		t.Fatal("a rejected element must keep the row in edit mode")
	}
	if ed.err != "not a number" {
		t.Fatalf("err = %q, want %q", ed.err, "not a number")
	}
	if len(ed.items) != 0 || m.Dirty() {
		t.Fatalf("nothing may be staged: items=%v dirty=%v", ed.items, m.Dirty())
	}
	// Correcting the value clears the error and commits.
	ed.tf.Set("80")
	m.Update(key("enter"))
	if ed.err != "" {
		t.Fatalf("err = %q, want it cleared", ed.err)
	}
	apply(t, m.applyChanges())
	if got := config.Get().Editor.Rulers; len(got) != 1 || got[0] != 80 {
		t.Fatalf("rulers = %v, want [80]", got)
	}
}

// TestIntListRoundTripsLiveValue: the editor pre-fills from the live config's
// flat rendering, so reopening it shows the columns just written.
func TestIntListRoundTripsLiveValue(t *testing.T) {
	restoreConfig(t)
	m, ed := openRulerEditor(t)
	ed.idx = len(ed.items)
	m.Update(key("enter"))
	ed.tf.Set("100")
	m.Update(key("enter"))
	apply(t, m.applyChanges())

	m2, ed2 := openRulerEditor(t)
	_ = m2
	if len(ed2.items) != 1 || ed2.items[0] != "100" {
		t.Fatalf("items = %v, want [100]", ed2.items)
	}
	if ed2.Dirty() {
		t.Fatal("a freshly opened editor must not read as dirty")
	}
}
