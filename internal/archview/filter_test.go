package archview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// filter_test.go covers #2409: the archive viewer's filter row — opened with
// "/" or the shared find chord, narrowing the entry tree live.

// typeInto feeds each rune of s to the pane as a key press.
func typeInto(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// TestSlashOpensTheFilter: "/" puts the cursor in the filter row.
func TestSlashOpensTheFilter(t *testing.T) {
	m := newPane(t, writeArchive(t, "README.md", "src/main.go"))
	if m.Filtering() {
		t.Fatal("the filter starts unfocused")
	}
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !m.Filtering() {
		t.Fatal(`"/" must focus the filter row`)
	}
}

// TestFindChordOpensTheFilter: ctrl+f does what "/" does (#2409); the chord is
// answered in the pane because the keymap table deliberately leaves it unbound.
func TestFindChordOpensTheFilter(t *testing.T) {
	m := newPane(t, writeArchive(t, "README.md"))
	m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if !m.Filtering() {
		t.Fatal("ctrl+f must focus the filter row")
	}
}

// TestOpenSearchCapability: the pane.Searchable entry point opens the same row.
func TestOpenSearchCapability(t *testing.T) {
	m := newPane(t, writeArchive(t, "README.md"))
	if !m.OpenSearch() {
		t.Fatal("OpenSearch must report the filter opened")
	}
	if !m.Filtering() {
		t.Fatal("OpenSearch did not focus the filter row")
	}
}

// TestFilterNarrowsTheTree: free match text keeps the matching entries and the
// directories that hold them, and dropping the filter restores everything.
func TestFilterNarrowsTheTree(t *testing.T) {
	m := newPane(t, writeArchive(t, "README.md", "src/main.go", "docs/guide/intro.md"))
	full := rowNames(&m)
	m.OpenSearch()
	typeInto(&m, "main")
	got := rowNames(&m)
	if !equal(got, []string{"src", "src/main.go"}) {
		t.Fatalf("filtered rows = %v", got)
	}
	// esc clears the filter and leaves the input, the shared filterbar rule.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Filtering() {
		t.Fatal("esc must leave the filter input")
	}
	if got := rowNames(&m); !equal(got, full) {
		t.Fatalf("after esc rows = %v, want the unfiltered %v", got, full)
	}
}

// TestFilterByKind: type: gates the entries by file or directory, with the
// parents of a surviving file kept for context.
func TestFilterByKind(t *testing.T) {
	m := newPane(t, writeArchive(t, "README.md", "src/main.go"))
	m.OpenSearch()
	typeInto(&m, "type:dir")
	if got := rowNames(&m); !equal(got, []string{"src"}) {
		t.Fatalf("type:dir rows = %v", got)
	}
	if m.Filter() != "type:dir" {
		t.Fatalf("filter text = %q", m.Filter())
	}
}

// TestFilterByName: name: takes a path substring or glob.
func TestFilterByName(t *testing.T) {
	m := newPane(t, writeArchive(t, "README.md", "src/main.go", "src/util.go"))
	m.OpenSearch()
	typeInto(&m, "name:*.go")
	got := rowNames(&m)
	if !equal(got, []string{"src", "src/main.go", "src/util.go"}) {
		t.Fatalf("name:*.go rows = %v", got)
	}
}

// TestFilterRowCostsOneBodyLine: the permanent filter row takes a line from
// the entry list, like it does in every other list pane.
func TestFilterRowCostsOneBodyLine(t *testing.T) {
	m := newPane(t, writeArchive(t, "a.txt"))
	m.SetSize(80, 10)
	if got := m.bodyHeight(); got != 7 {
		t.Fatalf("bodyHeight = %d, want 7 (header + filter row + footer)", got)
	}
}
