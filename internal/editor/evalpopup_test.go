package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// evalpopup_test.go covers the debugger's evaluate popup (#2174): open and
// render, the expand round trip through EvalExpandMsg/SetEvalChildren, the
// keys it owns, and the any-other-key close-and-fall-through rule.

func openedEval(t *testing.T, res EvalVar) Model {
	t.Helper()
	m, _ := loaded(t, "alpha\nbeta\ngamma\n")
	m.OpenEvalResult("cfg", res)
	if !m.EvalOpen() {
		t.Fatal("popup must be open after OpenEvalResult")
	}
	return m
}

func TestEvalViewShowsExpressionAndValue(t *testing.T) {
	m := openedEval(t, EvalVar{Value: "42", Type: "int"})
	v := m.EvalView()
	if !strings.Contains(v, "eval: cfg") {
		t.Fatalf("view must head with the expression:\n%s", v)
	}
	if !strings.Contains(v, "cfg = 42") || !strings.Contains(v, "(int)") {
		t.Fatalf("view must show value and type:\n%s", v)
	}
	if strings.Contains(v, "▸") {
		t.Fatalf("a leaf must carry no expansion marker:\n%s", v)
	}
}

// TestEvalExpandRoundTrip: a structured result asks the app for its children
// and renders them once they arrive.
func TestEvalExpandRoundTrip(t *testing.T) {
	m := openedEval(t, EvalVar{Value: "Config{…}", Ref: 900})
	if !strings.Contains(m.EvalView(), "▸") {
		t.Fatalf("a structured result must offer expansion:\n%s", m.EvalView())
	}
	handled, cmd := m.evalKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled || cmd == nil {
		t.Fatal("enter on a structured row must request its children")
	}
	msg, ok := cmd().(EvalExpandMsg)
	if !ok || msg.Ref != 900 {
		t.Fatalf("cmd produced %#v, want EvalExpandMsg{900}", cmd())
	}
	m.SetEvalChildren(900, []EvalVar{
		{Name: "host", Value: `"localhost"`, Type: "string"},
		{Name: "inner", Value: "Inner{…}", Ref: 901},
	})
	v := m.EvalView()
	if !strings.Contains(v, "host") || !strings.Contains(v, "inner") {
		t.Fatalf("children must render:\n%s", v)
	}
	if !strings.Contains(v, "▾") {
		t.Fatalf("the expanded row must flip its marker:\n%s", v)
	}
	// A re-expand after a collapse must not re-request: the children are
	// loaded.
	if _, cmd := m.evalKey(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("collapsing must not fire a request")
	}
	if _, cmd := m.evalKey(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("re-expanding loaded children must not fire a request")
	}
	// A nested reference still expands.
	m.evalKey(tea.KeyPressMsg{Code: 'j'})
	m.evalKey(tea.KeyPressMsg{Code: 'j'})
	_, cmd = m.evalKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("a nested structured child must expand too")
	}
	if msg, ok := cmd().(EvalExpandMsg); !ok || msg.Ref != 901 {
		t.Fatalf("nested expand produced %#v", cmd())
	}
}

// TestEvalKeysOwnedAndDismiss pins the key contract: navigation is consumed,
// esc closes, anything else closes and falls through.
func TestEvalKeysOwnedAndDismiss(t *testing.T) {
	m := openedEval(t, EvalVar{Value: "Config{…}", Ref: 900})
	m.SetEvalChildren(900, []EvalVar{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}})

	for _, k := range []tea.KeyPressMsg{
		{Code: 'j'}, {Code: tea.KeyDown}, {Code: 'k'}, {Code: tea.KeyUp}, {Code: 'h'},
	} {
		if handled, _ := m.evalKey(k); !handled {
			t.Fatalf("%v must be consumed by the popup", k)
		}
		if !m.EvalOpen() {
			t.Fatalf("%v must not close the popup", k)
		}
	}
	if handled, _ := m.evalKey(tea.KeyPressMsg{Code: 'x'}); handled {
		t.Fatal("an unrelated key must fall through")
	}
	if m.EvalOpen() {
		t.Fatal("an unrelated key must dismiss the popup")
	}

	m.OpenEvalResult("cfg", EvalVar{Value: "1"})
	if handled, _ := m.evalKey(tea.KeyPressMsg{Code: tea.KeyEscape}); !handled {
		t.Fatal("esc must be consumed")
	}
	if m.EvalOpen() {
		t.Fatal("esc must close the popup")
	}
}

// TestEvalCollapseStepsToParent: left on an expanded row folds it, left on a
// leaf walks up to its parent — the usual tree-left semantics.
func TestEvalCollapseStepsToParent(t *testing.T) {
	m := openedEval(t, EvalVar{Value: "Config{…}", Ref: 900})
	m.SetEvalChildren(900, []EvalVar{{Name: "a", Value: "1"}})
	m.evalKey(tea.KeyPressMsg{Code: 'j'}) // onto the child
	if m.eval.sel != 1 {
		t.Fatalf("selection = %d, want the child row", m.eval.sel)
	}
	m.evalKey(tea.KeyPressMsg{Code: 'h'})
	if m.eval.sel != 0 {
		t.Fatalf("left on a leaf must step to the parent, sel = %d", m.eval.sel)
	}
	m.evalKey(tea.KeyPressMsg{Code: 'h'})
	if strings.Contains(m.EvalView(), " a = 1") {
		t.Fatalf("left on the parent must fold it:\n%s", m.EvalView())
	}
}

// TestEvalScrollsLongResults: a tree taller than the window scrolls with the
// selection and marks the clipped rows.
func TestEvalScrollsLongResults(t *testing.T) {
	m := openedEval(t, EvalVar{Value: "Big{…}", Ref: 900})
	kids := make([]EvalVar, 0, 30)
	for i := 0; i < 30; i++ {
		kids = append(kids, EvalVar{Name: "f" + string(rune('a'+i%26)) + strings.Repeat("!", i/26), Value: "v"})
	}
	m.SetEvalChildren(900, kids)
	for i := 0; i < 25; i++ {
		m.evalKey(tea.KeyPressMsg{Code: 'j'})
	}
	v := m.EvalView()
	if !strings.Contains(v, "…") {
		t.Fatalf("a clipped tree must mark the scroll:\n%s", v)
	}
	if strings.Count(v, "\n") > evalVisibleRows+6 {
		t.Fatalf("popup must stay bounded:\n%s", v)
	}
	if m.eval.scroll == 0 {
		t.Fatal("the window must have scrolled with the selection")
	}
}

// TestEvalDismissClosesEverywhere: the app clears the popup when the debuggee
// resumes.
func TestEvalDismissClosesEverywhere(t *testing.T) {
	m := openedEval(t, EvalVar{Value: "1"})
	m.DismissEval()
	if m.EvalOpen() || m.EvalView() != "" {
		t.Fatal("DismissEval must close and blank the popup")
	}
	// A late children push for a closed popup is dropped, not a panic.
	m.SetEvalChildren(900, []EvalVar{{Name: "a"}})
}

// TestEvalKeyRoutedBeforeDispatch: the editor gives the popup the key before
// the mode machine sees it, so navigating the result never edits the buffer.
func TestEvalKeyRoutedBeforeDispatch(t *testing.T) {
	m := openedEval(t, EvalVar{Value: "Config{…}", Ref: 900})
	m.SetEvalChildren(900, []EvalVar{{Name: "a", Value: "1"}})
	before := m.Text()
	m, _ = m.Update(tea.KeyPressMsg{Code: 'j'})
	if m.Text() != before {
		t.Fatal("a popup key must not reach the buffer")
	}
	if !m.EvalOpen() || m.eval.sel != 1 {
		t.Fatal("the popup must have consumed the motion")
	}
}
