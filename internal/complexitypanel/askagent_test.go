package complexitypanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
)

// askagent_test.go covers the pane's 'a' (#2964): a function row sends the
// request with its range and metrics, a site row stands for its parent, a
// group or removed row sends nothing and says so, and the footer
// advertises the key.

// pressA presses 'a' and returns the message the pane sends, if any.
func pressA(m *Model) (AskAgentMsg, bool) {
	cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if cmd == nil {
		return AskAgentMsg{}, false
	}
	msg, ok := cmd().(AskAgentMsg)
	return msg, ok
}

func TestAskAgentOnFunctionRow(t *testing.T) {
	m := newPanel(t)
	r := m.Rows()[0]
	msg, ok := pressA(m)
	if !ok {
		t.Fatal("a on a function row must send the request")
	}
	want := AskAgentMsg{Path: r.Path, Line: r.Fn.Line, EndLine: r.Fn.EndLine, Name: r.Fn.Name, CCN: r.Fn.CCN, Cognitive: r.Fn.Cognitive, Lang: r.Lang, Fn: r.Fn}
	if msg.Path != want.Path || msg.Line != want.Line || msg.EndLine != want.EndLine || msg.Name != want.Name || msg.CCN != want.CCN || msg.Cognitive != want.Cognitive || msg.Lang != want.Lang || msg.Fn.Name != want.Fn.Name {
		t.Fatalf("request = %+v, want %+v", msg, want)
	}
	if !strings.Contains(m.View(), "a agent") {
		t.Fatal("the footer must advertise a")
	}
}

func TestAskAgentSiteRowStandsForParent(t *testing.T) {
	m := newPanel(t)
	fn := complexity.Function{Name: "render", Kind: complexity.KindFunction, Line: 0, EndLine: 9, NLOC: 10, CCN: 3, Cognitive: 4,
		Sites: []complexity.Site{{Line: 2, EndLine: 4, Kind: "if", CCN: 1, Cognitive: 1}}}
	m.Set(complexity.View{Gen: 2, Files: map[string][]complexity.Function{"/proj/a.go": {fn}}}, complexity.Stats{Enabled: true})
	if !m.ToggleSites() {
		t.Fatal("expand")
	}
	m.cursor = 1
	if r, _ := m.Selected(); r.Site == nil {
		t.Fatalf("row 1 = %+v, want the site row", r)
	}
	msg, ok := pressA(m)
	if !ok || msg.Name != "render" || msg.EndLine != 9 || len(msg.Fn.Sites) != 1 {
		t.Fatalf("site row: %+v ok=%v", msg, ok)
	}
}

func TestAskAgentRefusesGroupAndRemovedRows(t *testing.T) {
	m := newPanel(t)
	m.CycleGroup() // class
	m.CycleGroup() // file
	m.cursor = 0
	if r, _ := m.Selected(); r.Group == nil {
		t.Fatalf("row 0 = %+v, want a group row", r)
	}
	if _, ok := pressA(m); ok || m.note != "a asks the agent about a function row" {
		t.Fatalf("group row must refuse with a note: ok=%v note=%q", ok, m.note)
	}
	c := changesPanel(t)
	c.cursor = 3 // gone (removed)
	if r, _ := c.Selected(); r.Change != complexity.Removed {
		t.Fatalf("row 3 = %+v, want the removed row", r)
	}
	if _, ok := pressA(c); ok {
		t.Fatal("a removed function has no source to simplify")
	}
}
