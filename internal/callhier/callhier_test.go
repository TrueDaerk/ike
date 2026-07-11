package callhier

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
		reqID    int
		name     string
		incoming bool
	}
}

func (f *fetchRecorder) fetch(reqID int, item protocol.CallHierarchyItem, incoming bool) tea.Cmd {
	f.reqs = append(f.reqs, struct {
		reqID    int
		name     string
		incoming bool
	}{reqID, item.Name, incoming})
	return nil
}

// ansiRE strips styling escapes so Contains assertions see plain text (the
// title style renders per-grapheme escapes).
var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

func entry(name, path string, line int) ilsp.CallHierarchyEntry {
	return ilsp.CallHierarchyEntry{
		Item: protocol.CallHierarchyItem{Name: name, URI: "file://" + path},
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
	m.Open(ilsp.CallHierarchyMsg{
		Path:  "/proj/a.go",
		Roots: []ilsp.CallHierarchyEntry{entry("Greet", "/proj/a.go", 3)},
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

func TestOpenExpandsFirstRootIncoming(t *testing.T) {
	m, rec := openModel(t)
	if !m.IsOpen() {
		t.Fatal("overlay should be open")
	}
	if len(rec.reqs) != 1 || rec.reqs[0].name != "Greet" || !rec.reqs[0].incoming {
		t.Fatalf("open should fetch the first root's callers, got %+v", rec.reqs)
	}
}

func TestApplyFillsChildrenAndEnterNavigates(t *testing.T) {
	m, rec := openModel(t)
	m.Apply(ilsp.CallHierarchyCallsMsg{
		ReqID:    rec.reqs[0].reqID,
		Incoming: true,
		Calls:    []ilsp.CallHierarchyEntry{entry("main", "/proj/main.go", 8)},
	})
	view := plain(m.View())
	if !strings.Contains(view, "Greet") || !strings.Contains(view, "main") {
		t.Fatalf("view should show root and child, got:\n%s", view)
	}
	if !strings.Contains(view, "Callers") {
		t.Errorf("heading should name the direction, got:\n%s", view)
	}

	m.Update(key("down")) // onto the child
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter should navigate")
	}
	msg, ok := cmd().(ilsp.DefinitionMsg)
	if !ok || msg.Path != "/proj/main.go" || msg.Line != 8 {
		t.Fatalf("navigation target wrong: %#v", msg)
	}
	if m.IsOpen() {
		t.Error("enter should close the overlay")
	}
}

func TestExpandChildFetchesLazily(t *testing.T) {
	m, rec := openModel(t)
	m.Apply(ilsp.CallHierarchyCallsMsg{
		ReqID:    rec.reqs[0].reqID,
		Incoming: true,
		Calls:    []ilsp.CallHierarchyEntry{entry("main", "/proj/main.go", 8)},
	})
	m.Update(key("down"))
	m.Update(key("right"))
	if len(rec.reqs) != 2 || rec.reqs[1].name != "main" {
		t.Fatalf("expanding the child should fetch its callers, got %+v", rec.reqs)
	}
	// A second expand while loading must not refetch.
	m.Update(key("right"))
	if len(rec.reqs) != 2 {
		t.Fatalf("in-flight node refetched: %+v", rec.reqs)
	}
}

func TestTabTogglesDirectionAndDropsStaleReplies(t *testing.T) {
	m, rec := openModel(t)
	staleID := rec.reqs[0].reqID
	m.Update(key("tab"))
	if len(rec.reqs) != 2 || rec.reqs[1].incoming {
		t.Fatalf("tab should refetch the root as outgoing, got %+v", rec.reqs)
	}
	if !strings.Contains(plain(m.View()), "Callees") {
		t.Errorf("heading should flip to callees")
	}
	// The pre-toggle reply must not land in the fresh tree.
	m.Apply(ilsp.CallHierarchyCallsMsg{
		ReqID:    staleID,
		Incoming: true,
		Calls:    []ilsp.CallHierarchyEntry{entry("stale", "/proj/x.go", 0)},
	})
	if strings.Contains(plain(m.View()), "stale") {
		t.Error("stale reply should be dropped after a direction toggle")
	}
}

func TestCollapseAndParentJump(t *testing.T) {
	m, rec := openModel(t)
	m.Apply(ilsp.CallHierarchyCallsMsg{
		ReqID:    rec.reqs[0].reqID,
		Incoming: true,
		Calls: []ilsp.CallHierarchyEntry{
			entry("main", "/proj/main.go", 8),
			entry("other", "/proj/other.go", 2),
		},
	})
	m.Update(key("down"))
	m.Update(key("down")) // on "other"
	m.Update(key("left")) // collapsed leaf: jump to parent (the root)
	if m.cursor != 0 {
		t.Fatalf("left on a collapsed node should land on the parent, cursor = %d", m.cursor)
	}
	m.Update(key("left")) // collapse the root
	if strings.Contains(plain(m.View()), "main") {
		t.Error("collapsing the root should hide its children")
	}
	// Re-expanding a loaded node must not refetch.
	m.Update(key("right"))
	if len(rec.reqs) != 1 {
		t.Fatalf("re-expanding a loaded node refetched: %+v", rec.reqs)
	}
	if !strings.Contains(plain(m.View()), "main") {
		t.Error("re-expand should show the cached children")
	}
}

func TestEscCloses(t *testing.T) {
	m, _ := openModel(t)
	m.Update(key("esc"))
	if m.IsOpen() {
		t.Error("esc should close")
	}
}
