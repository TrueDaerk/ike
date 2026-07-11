package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

func TestCallHierarchyMsgRouting(t *testing.T) {
	m := sized(t, 100, 40)

	fetched := 0
	out, _ := m.Update(ilsp.CallHierarchyMsg{
		Path: "/proj/a.go",
		Roots: []ilsp.CallHierarchyEntry{{
			Item: protocol.CallHierarchyItem{Name: "Greet"},
			Name: "Greet", Path: "/proj/a.go", Line: 3,
		}},
		Fetch: func(reqID int, item protocol.CallHierarchyItem, incoming bool) tea.Cmd {
			fetched++
			return nil
		},
	})
	m = out.(Model)
	if !m.callhier.IsOpen() {
		t.Fatal("CallHierarchyMsg should open the overlay")
	}
	if fetched != 1 {
		t.Fatalf("opening should expand the first root once, got %d", fetched)
	}

	// The open overlay owns the keyboard: esc closes it.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.callhier.IsOpen() {
		t.Fatal("esc should close the overlay")
	}
}

func TestCallHierarchyCallsMsgApplied(t *testing.T) {
	m := sized(t, 100, 40)

	var gotReq int
	out, _ := m.Update(ilsp.CallHierarchyMsg{
		Path: "/proj/a.go",
		Roots: []ilsp.CallHierarchyEntry{{
			Item: protocol.CallHierarchyItem{Name: "Greet"},
			Name: "Greet", Path: "/proj/a.go", Line: 3,
		}},
		Fetch: func(reqID int, item protocol.CallHierarchyItem, incoming bool) tea.Cmd {
			gotReq = reqID
			return nil
		},
	})
	m = out.(Model)
	out, _ = m.Update(ilsp.CallHierarchyCallsMsg{
		ReqID:    gotReq,
		Incoming: true,
		Calls: []ilsp.CallHierarchyEntry{{
			Item: protocol.CallHierarchyItem{Name: "main"},
			Name: "main", Path: "/proj/main.go", Line: 8,
		}},
	})
	m = out.(Model)

	// Down onto the child, enter navigates through the DefinitionMsg path.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = out.(Model)
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a row should produce a navigation command")
	}
	msg := cmd()
	def, ok := msg.(ilsp.DefinitionMsg)
	if !ok || def.Path != "/proj/main.go" || def.Line != 8 {
		t.Fatalf("expected DefinitionMsg to /proj/main.go:8, got %#v", msg)
	}
}
