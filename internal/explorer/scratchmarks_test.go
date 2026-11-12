package explorer

// scratchmarks_test.go covers the Scratches section's multi-select (#3138):
// space marks and shift+j/k ranges work in the section like in the tree, the
// delete key removes every marked / ranged scratch behind one confirmation
// listing them all, and the section's selection never leaks into a tree
// operation (nor the tree's into a scratch delete).

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/scratch"
)

// removeSeam wires a store-removing delete seam onto m and returns the
// removed base names in call order.
func removeSeam(m *Model, entries *[]scratch.Entry) *[]string {
	removed := &[]string{}
	m.SetScratchOps(func(p string) error {
		*removed = append(*removed, filepath.Base(p))
		kept := (*entries)[:0]
		for _, e := range *entries {
			if e.Path != p {
				kept = append(kept, e)
			}
		}
		*entries = kept
		return nil
	}, nil)
	return removed
}

// sectionAt puts the unified cursor on the scratch with the given base name.
func sectionAt(t *testing.T, m Model, name string) Model {
	t.Helper()
	m.FocusScratches()
	for i, e := range m.ScratchEntries() {
		if filepath.Base(e.Path) == name {
			m.scrCursor = i
			m.followScratchCursor()
			return m
		}
	}
	t.Fatalf("no scratch %q", name)
	return m
}

func yesKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'y', Text: "y"} }

// deletedNames runs a batch command and collects the announced deletions.
func deletedNames(t *testing.T, cmd tea.Cmd) []string {
	t.Helper()
	var out []string
	var walk func(msg tea.Msg)
	walk = func(msg tea.Msg) {
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, c := range v {
				if c != nil {
					walk(c())
				}
			}
		case FileDeletedMsg:
			out = append(out, filepath.Base(v.Path))
		}
	}
	if cmd != nil {
		walk(cmd())
	}
	return out
}

func TestScratchSpaceMarksAndEscClears(t *testing.T) {
	m, _ := scratchModel(t, 1, "a.txt", "b.txt", "c.txt")
	m = sectionAt(t, m, "a.txt")
	m, _ = send(m, spaceKey())
	if !m.scratchMarked("/scratches/a.txt") {
		t.Fatal("space must mark the cursor scratch")
	}
	if m.markCount() != 0 {
		t.Fatal("a scratch mark must not land in the tree's mark set")
	}
	for _, l := range sectionLines(m) {
		if strings.Contains(l, "a.txt") && !strings.HasPrefix(strings.TrimSpace(l), markGlyph) {
			t.Fatalf("marked scratch row must show the mark glyph: %q", l)
		}
		if strings.Contains(l, "b.txt") && strings.Contains(l, markGlyph) {
			t.Fatalf("unmarked scratch row must not show the glyph: %q", l)
		}
	}
	// The mark survives motion; a second space on the row unmarks it.
	m, _ = send(m, key("j"), spaceKey(), key("k"))
	if !m.scratchMarked("/scratches/a.txt") || !m.scratchMarked("/scratches/b.txt") {
		t.Fatalf("marks must survive cursor motion: %v", m.scrMarks)
	}
	m, _ = send(m, spaceKey())
	if m.scratchMarked("/scratches/a.txt") {
		t.Fatal("space on a marked scratch must unmark it")
	}
	m, _ = send(m, escKey())
	if len(m.scrMarks) != 0 {
		t.Fatalf("esc must clear scratch marks: %v", m.scrMarks)
	}
}

func TestScratchShiftRangeStaysInSection(t *testing.T) {
	m, _ := scratchModel(t, 1, "a.txt", "b.txt", "c.txt")
	m = sectionAt(t, m, "b.txt")
	m, _ = send(m, key("J"), key("J"))
	lo, hi, ok := m.scratchSelRange()
	if !ok || lo != 1 || hi != 2 {
		t.Fatalf("range = %d..%d ok=%v want 1..2", lo, hi, ok)
	}
	if _, _, treeOK := m.selRange(); treeOK {
		t.Fatal("a section range must not start a tree range")
	}
	// Extending upward past the first scratch stops at the section's top.
	m, _ = send(m, key("K"), key("K"), key("K"), key("K"))
	if !m.inScratch() || m.scrCursor != 0 {
		t.Fatalf("range must not cross the divider: inScratch=%v cur=%d", m.inScratch(), m.scrCursor)
	}
	// A plain motion collapses it.
	m, _ = send(m, key("j"))
	if _, _, ok := m.scratchSelRange(); ok {
		t.Fatal("a plain motion must collapse the section range")
	}
}

func TestScratchBulkDeleteMarked(t *testing.T) {
	m, entries := scratchModel(t, 1, "a.txt", "b.txt", "c.txt")
	removed := removeSeam(&m, entries)
	m = sectionAt(t, m, "a.txt")
	m, _ = send(m, spaceKey(), key("j"), key("j"), spaceKey()) // mark a, c; cursor c
	m, _ = m.Update(DeleteMsg{})
	if !m.Prompting() || m.prompt.kind != promptConfirm {
		t.Fatal("delete with marks must open the confirmation")
	}
	if !strings.Contains(m.prompt.title, "2 scratches") {
		t.Fatalf("title = %q", m.prompt.title)
	}
	got := strings.Join(m.prompt.lines, "\n")
	if !strings.Contains(got, "a.txt") || !strings.Contains(got, "c.txt") || strings.Contains(got, "b.txt") {
		t.Fatalf("confirmation must list every target: %q", got)
	}
	m, cmd := m.Update(yesKey())
	if strings.Join(*removed, ",") != "a.txt,c.txt" {
		t.Fatalf("removed = %v", *removed)
	}
	if d := strings.Join(deletedNames(t, cmd), ","); d != "a.txt,c.txt" {
		t.Fatalf("announced = %q, want every removed path", d)
	}
	if len(m.ScratchEntries()) != 1 || len(m.scrMarks) != 0 {
		t.Fatalf("rows=%d marks=%v after bulk delete", len(m.ScratchEntries()), m.scrMarks)
	}
}

func TestScratchBulkDeleteRange(t *testing.T) {
	m, entries := scratchModel(t, 1, "a.txt", "b.txt", "c.txt")
	removed := removeSeam(&m, entries)
	m = sectionAt(t, m, "a.txt")
	m, _ = send(m, key("J"))
	m, _ = m.Update(DeleteMsg{})
	if !m.Prompting() || len(m.prompt.lines) != 2 {
		t.Fatalf("range delete must list both targets, prompt=%+v", m.prompt)
	}
	m, _ = m.Update(yesKey())
	if strings.Join(*removed, ",") != "a.txt,b.txt" {
		t.Fatalf("removed = %v", *removed)
	}
}

func TestScratchBulkDeleteCancel(t *testing.T) {
	m, entries := scratchModel(t, 1, "a.txt", "b.txt")
	removed := removeSeam(&m, entries)
	m = sectionAt(t, m, "a.txt")
	m, _ = send(m, spaceKey(), key("j"), spaceKey())
	m, _ = m.Update(DeleteMsg{})
	m, cmd := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if len(*removed) != 0 || cmd != nil || m.Prompting() {
		t.Fatalf("cancel must delete nothing, removed=%v", *removed)
	}
	if len(m.scrMarks) != 2 {
		t.Fatal("a cancelled delete keeps the marks")
	}
}

// TestScratchAndTreeMarksDoNotLeak: tree marks never reach a scratch delete,
// scratch marks never reach a tree operation, and each section's bulk
// operation leaves the other section's marks alone.
func TestScratchAndTreeMarksDoNotLeak(t *testing.T) {
	m, entries := scratchModel(t, 2, "a.txt", "b.txt")
	removed := removeSeam(&m, entries)
	// Mark a tree file, then a scratch.
	m, _ = send(m, key("j"), spaceKey())
	treeMarked := m.MarkedPaths()
	if len(treeMarked) != 1 {
		t.Fatalf("tree marks = %v", treeMarked)
	}
	m = sectionAt(t, m, "b.txt")
	m, _ = send(m, spaceKey())

	// A tree operation sees only the tree mark.
	m.exitScratch()
	if paths, _ := m.OpTargetPaths(); len(paths) != 1 || paths[0] != treeMarked[0] {
		t.Fatalf("tree targets = %v, want only the tree mark", paths)
	}
	m, _ = m.Update(DeleteMsg{})
	if strings.Contains(strings.Join(m.prompt.lines, "\n"), "b.txt") {
		t.Fatal("a tree delete must not list a scratch mark")
	}
	m, _ = send(m, tea.KeyPressMsg{Code: 'n', Text: "n"})

	// A scratch delete sees only the scratch mark and leaves the tree mark.
	m = sectionAt(t, m, "a.txt")
	m, _ = m.Update(DeleteMsg{})
	if !m.Prompting() || m.prompt.title != "Delete 1 scratch?" || len(m.prompt.lines) != 1 {
		t.Fatalf("scratch delete prompt = %+v", m.prompt)
	}
	m, _ = m.Update(yesKey())
	if strings.Join(*removed, ",") != "b.txt" {
		t.Fatalf("removed = %v, want only the marked scratch", *removed)
	}
	if got := m.MarkedPaths(); len(got) != 1 || got[0] != treeMarked[0] {
		t.Fatalf("a scratch delete must leave tree marks alone: %v", got)
	}
}

// TestScratchSingleDeleteUnchanged: without marks or a range the delete acts
// on the cursor scratch only, with the single-entry confirmation.
func TestScratchSingleDeleteUnchanged(t *testing.T) {
	m, entries := scratchModel(t, 1, "a.txt", "b.txt")
	removed := removeSeam(&m, entries)
	m = sectionAt(t, m, "b.txt")
	m, _ = m.Update(DeleteMsg{})
	if !strings.Contains(m.prompt.title, `"b.txt"`) || len(m.prompt.lines) != 0 {
		t.Fatalf("single prompt = %+v", m.prompt)
	}
	m, _ = m.Update(yesKey())
	if strings.Join(*removed, ",") != "b.txt" {
		t.Fatalf("removed = %v", *removed)
	}
}
