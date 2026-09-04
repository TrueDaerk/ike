package typehier

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// fetchRecorder captures the expansion requests the model issues.
type fetchRecorder struct {
	reqs []struct {
		reqID      int
		name       string
		supertypes bool
	}
}

func (f *fetchRecorder) fetch(reqID int, item protocol.TypeHierarchyItem, supertypes bool) tea.Cmd {
	f.reqs = append(f.reqs, struct {
		reqID      int
		name       string
		supertypes bool
	}{reqID, item.Name, supertypes})
	return nil
}

// ansiRE strips styling escapes so Contains assertions see plain text.
var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

func entry(name, path string, line int) ilsp.TypeHierarchyEntry {
	return ilsp.TypeHierarchyEntry{
		Item: protocol.TypeHierarchyItem{Name: name, URI: "file://" + path},
		Name: name,
		Path: path,
		Line: line,
	}
}

func openModel(t *testing.T) (*Model, *fetchRecorder) {
	t.Helper()
	m := New()
	m.SetSize(100, 40)
	rec := &fetchRecorder{}
	m.Open(ilsp.TypeHierarchyMsg{
		Path:  "/proj/a.go",
		Roots: []ilsp.TypeHierarchyEntry{entry("Circle", "/proj/a.go", 3)},
		Fetch: rec.fetch,
	})
	return m, rec
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func TestOpenExpandsFirstRootSupertypes(t *testing.T) {
	m, rec := openModel(t)
	if !m.IsOpen() {
		t.Fatal("overlay should be open")
	}
	if len(rec.reqs) != 1 || rec.reqs[0].name != "Circle" || !rec.reqs[0].supertypes {
		t.Fatalf("open should fetch the first root's supertypes, got %+v", rec.reqs)
	}
}

func TestApplyFillsChildrenAndEnterNavigates(t *testing.T) {
	m, rec := openModel(t)
	m.Apply(ilsp.TypeHierarchyItemsMsg{
		ReqID:      rec.reqs[0].reqID,
		Supertypes: true,
		Items:      []ilsp.TypeHierarchyEntry{entry("Shape", "/proj/shape.go", 8)},
	})
	view := plain(m.View())
	if !strings.Contains(view, "Circle") || !strings.Contains(view, "Shape") {
		t.Fatalf("view should show root and child, got:\n%s", view)
	}
	if !strings.Contains(view, "Supertypes") {
		t.Errorf("heading should name the direction, got:\n%s", view)
	}

	m.Update(key("down")) // onto the child
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter should navigate")
	}
	msg, ok := cmd().(ilsp.DefinitionMsg)
	if !ok || msg.Path != "/proj/shape.go" || msg.Line != 8 {
		t.Fatalf("navigation target wrong: %#v", msg)
	}
	if m.IsOpen() {
		t.Error("enter should close the overlay")
	}
}

// TestExpandChildFetchesInDirection: the tree's fetch carries the overlay's
// current direction (the tree mechanics themselves are hiertree's tests).
func TestExpandChildFetchesInDirection(t *testing.T) {
	m, rec := openModel(t)
	m.Apply(ilsp.TypeHierarchyItemsMsg{
		ReqID:      rec.reqs[0].reqID,
		Supertypes: true,
		Items:      []ilsp.TypeHierarchyEntry{entry("Shape", "/proj/shape.go", 8)},
	})
	m.Update(key("down"))
	m.Update(key("right"))
	if len(rec.reqs) != 2 || rec.reqs[1].name != "Shape" || !rec.reqs[1].supertypes {
		t.Fatalf("expanding the child should fetch its supertypes, got %+v", rec.reqs)
	}
}

func TestTabTogglesDirectionAndDropsStaleReplies(t *testing.T) {
	m, rec := openModel(t)
	staleID := rec.reqs[0].reqID
	m.Update(key("tab"))
	if len(rec.reqs) != 2 || rec.reqs[1].supertypes {
		t.Fatalf("tab should refetch the root as subtypes, got %+v", rec.reqs)
	}
	if !strings.Contains(plain(m.View()), "Subtypes") {
		t.Errorf("heading should flip to subtypes")
	}
	// The pre-toggle reply must not land in the fresh tree.
	m.Apply(ilsp.TypeHierarchyItemsMsg{
		ReqID:      staleID,
		Supertypes: true,
		Items:      []ilsp.TypeHierarchyEntry{entry("stale", "/proj/x.go", 0)},
	})
	if strings.Contains(plain(m.View()), "stale") {
		t.Error("stale reply should be dropped after a direction toggle")
	}
}

func TestEscCloses(t *testing.T) {
	m, _ := openModel(t)
	m.Update(key("esc"))
	if m.IsOpen() {
		t.Error("esc should close")
	}
}
