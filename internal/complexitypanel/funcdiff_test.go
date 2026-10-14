package complexitypanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
)

// funcdiff_test.go covers the Changes tab's 'd' (#2956): the request names
// the function with both ranges (one empty on an added or removed row), and
// the other tabs neither send it nor advertise it.

// pressD presses 'd' and returns the message the pane sends, if any.
func pressD(m *Model) (FunctionDiffMsg, bool) {
	cmd := m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if cmd == nil {
		return FunctionDiffMsg{}, false
	}
	msg, ok := cmd().(FunctionDiffMsg)
	return msg, ok
}

func TestFunctionDiffOnChangesRows(t *testing.T) {
	m := changesPanel(t)
	// render (changed): both sides.
	msg, ok := pressD(m)
	if !ok || msg.Name != "render" || msg.Path != "/proj/internal/app/app.go" || msg.Ordinal != 0 {
		t.Fatalf("changed row: %+v ok=%v", msg, ok)
	}
	if msg.Change.Before.EndLine != 20 || msg.Change.After.EndLine != 24 {
		t.Fatalf("ranges = %+v", msg.Change)
	}
	// fresh (added): no base side.
	m.cursor = 2
	msg, ok = pressD(m)
	if !ok || msg.Name != "fresh" || msg.Change.Before.Name != "" || msg.Change.After.Line != 40 {
		t.Fatalf("added row: %+v", msg)
	}
	// gone (removed): no head side.
	m.cursor = 3
	msg, ok = pressD(m)
	if !ok || msg.Name != "gone" || msg.Change.After.Name != "" || msg.Change.Before.EndLine != 9 {
		t.Fatalf("removed row: %+v", msg)
	}
	if !strings.Contains(m.footer(), "d diff") {
		t.Fatalf("Changes footer must list d: %q", m.footer())
	}
}

func TestFunctionDiffOrdinals(t *testing.T) {
	m := newPanel(t)
	// Two same-named units in the head; the second one changed.
	base := map[string][]complexity.Function{"/proj/a.go": {fn("h", 0, 2, 1, 0), fn("h", 4, 6, 1, 0)}}
	head := map[string][]complexity.Function{"/proj/a.go": {fn("h", 0, 2, 1, 0), fn("h", 4, 8, 3, 2), fn("h", 10, 12, 1, 0)}}
	m.Set(complexity.View{Files: head}, complexity.Stats{Enabled: true})
	m.SetChanges(Changes{Rev: "HEAD", Sha: "abc", Diff: complexity.DiffProject(base, head, complexity.Uniform(complexity.Thresholds{CCN: 15, Cog: 15, Cmt: 40})), Ready: true})
	m.SetTab(TabChanges)
	got := map[int]int{}
	for i, r := range m.Rows() {
		m.cursor = i
		msg, ok := pressD(m)
		if !ok {
			t.Fatalf("row %d sent nothing", i)
		}
		got[r.Fn.Line] = msg.Ordinal
	}
	if got[4] != 1 || got[10] != 2 {
		t.Fatalf("ordinals = %v", got)
	}
}

func TestFunctionDiffOnlyOnChanges(t *testing.T) {
	m := newPanel(t)
	for _, tab := range []Tab{TabProject, TabFile} {
		m.SetTab(tab)
		if _, ok := pressD(m); ok {
			t.Fatalf("tab %v must not send a function diff", tab)
		}
		if strings.Contains(m.footer(), "d diff") {
			t.Fatalf("tab %v footer must not list d: %q", tab, m.footer())
		}
	}
}
