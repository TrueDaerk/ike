package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
	"ike/internal/palette"
)

func actionsOffer() (ilsp.CodeActionsMsg, *int) {
	var picked int = -1
	return ilsp.CodeActionsMsg{
		Path: "/proj/a.go",
		Actions: []ilsp.CodeActionChoice{
			{Title: "Organize imports", Kind: "source.organizeImports"},
			{Title: "Fix undeclared name", Kind: "quickfix", Preferred: true},
		},
		Apply: func(i int) tea.Cmd {
			picked = i
			return nil
		},
	}, &picked
}

func TestActionsModePreferredFirstAndIndices(t *testing.T) {
	a := &actionsMode{}
	msg, picked := actionsOffer()
	a.Set(msg)
	items := a.Results("", palette.Context{})
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Title != "★ Fix undeclared name" || items[0].Detail != "quickfix" {
		t.Fatalf("preferred action should list first, got %+v", items[0])
	}
	// The picked index must reference the ORIGINAL offer order despite the sort.
	pm, ok := items[0].Msg.(actionPickedMsg)
	if !ok {
		t.Fatalf("msg = %#v", items[0].Msg)
	}
	a.Run(pm)
	if *picked != 1 {
		t.Fatalf("picked = %d, want original index 1", *picked)
	}
}

func TestCodeActionsMsgOpensLockedPicker(t *testing.T) {
	m := sized(t, 100, 40)
	msg, picked := actionsOffer()
	out, _ := m.Update(msg)
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("offer should open the palette")
	}
	// Activating routes through actionPickedMsg to the continuation.
	out, cmd := m.Update(actionPickedMsg{index: 0})
	m = out.(Model)
	_ = cmd
	if *picked != 0 {
		t.Fatalf("activation should run the continuation, picked = %d", *picked)
	}
}
