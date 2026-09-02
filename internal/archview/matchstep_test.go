package archview

// matchstep_test.go covers #2410 for the archive viewer. Its search is the
// shared filter row, so its "matches" are the entries the expression matched
// — not the parent directories the tree keeps around them for context.

import (
	"strings"
	"testing"
)

func TestArchiveMatchStepWalksTheHits(t *testing.T) {
	m := newPane(t, writeArchive(t, "README.md", "src/main.go", "src/util.go"))
	m.OpenSearch()
	typeInto(&m, "name:*.go")
	st := m.NextMatch()
	if !st.Handled || st.Total != 2 {
		t.Fatalf("NextMatch = %+v, want the two .go entries", st)
	}
	if name, ok := m.SelectedEntry(); !ok || !strings.HasSuffix(name, ".go") {
		t.Fatalf("the walk must land on a matching entry, got %q", name)
	}
	if !m.Filtering() || m.Filter() != "name:*.go" {
		t.Fatalf("the filter row must survive the step: open=%v text=%q",
			m.Filtering(), m.Filter())
	}
}

func TestArchiveMatchStepWrapsAndSaysSo(t *testing.T) {
	m := newPane(t, writeArchive(t, "README.md", "src/main.go", "src/util.go"))
	m.OpenSearch()
	typeInto(&m, "name:*.go")
	m.NextMatch()
	st := m.NextMatch() // off the end of the two hits
	if !st.Wrapped {
		t.Fatalf("the walk must wrap, got %+v", st)
	}
	if got := m.filter.View(80, nil); !strings.Contains(got, "(wrapped)") {
		t.Fatalf("the filter row must mark the wrap: %q", got)
	}
}

func TestArchiveMatchStepWithoutTheFilterRow(t *testing.T) {
	m := newPane(t, writeArchive(t, "README.md"))
	if st := m.NextMatch(); st.Handled {
		t.Fatalf("NextMatch with the row unfocused = %+v, want not handled", st)
	}
}

func TestArchiveMatchStepWithNoMatches(t *testing.T) {
	m := newPane(t, writeArchive(t, "README.md"))
	m.OpenSearch()
	typeInto(&m, "name:*.zzz")
	if st := m.NextMatch(); !st.Handled || st.Total != 0 {
		t.Fatalf("NextMatch over nothing = %+v", st)
	}
}
