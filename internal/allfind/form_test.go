package allfind

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func key(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

func typed(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Text: s, Code: rune(s[0])}
}

func testProjects() []Project {
	return []Project{
		{Root: "/a", Name: "alpha"},
		{Root: "/b", Name: "beta", Missing: true},
		{Root: "/c", Name: "gamma", Excluded: true},
	}
}

func openedForm(sel string) *Form {
	f := NewForm()
	f.SetSize(120, 40)
	f.Open(State{Query: "remembered", Include: "*.go", Exclude: "vendor/*"}, testProjects(), sel)
	return f
}

func confirmMsg(t *testing.T, cmd tea.Cmd) ConfirmMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a confirm cmd")
	}
	msg, ok := cmd().(ConfirmMsg)
	if !ok {
		t.Fatalf("cmd yielded %T, want ConfirmMsg", cmd())
	}
	return msg
}

func TestFormOpensWithRememberedState(t *testing.T) {
	f := openedForm("")
	if !f.IsOpen() || f.query.Text != "remembered" || f.include.Text != "*.go" || f.exclude.Text != "vendor/*" {
		t.Fatalf("state not seeded: %+v", f.State())
	}
	if !f.preselect {
		t.Fatal("remembered query must arrive preselected")
	}
	// The first typed character replaces the preselected query wholesale.
	f.Update(typed("x"))
	if f.query.Text != "x" {
		t.Fatalf("typed char must replace the prefill, got %q", f.query.Text)
	}
}

func TestFormSelectionPrefillOutranksRemembered(t *testing.T) {
	f := openedForm("needle")
	if f.query.Text != "needle" {
		t.Fatalf("selection must prefill the query, got %q", f.query.Text)
	}
	// Multi-line selections prefill nothing.
	f2 := openedForm("a\nb")
	if f2.query.Text != "remembered" {
		t.Fatalf("line-spanning selection must keep the remembered query, got %q", f2.query.Text)
	}
}

func TestFormConfirmDropsExcludedAndMissing(t *testing.T) {
	f := openedForm("")
	msg := confirmMsg(t, f.Update(key(tea.KeyEnter, 0)))
	if f.IsOpen() {
		t.Fatal("confirm must close the form")
	}
	if len(msg.Roots) != 1 || msg.Roots[0].Root != "/a" {
		t.Fatalf("kept roots = %+v, want only /a", msg.Roots)
	}
	if msg.State.Query != "remembered" {
		t.Fatalf("state query = %q", msg.State.Query)
	}
	if len(msg.State.ExcludedRoots) != 1 || msg.State.ExcludedRoots[0] != "/c" {
		t.Fatalf("excluded roots = %v, want [/c]", msg.State.ExcludedRoots)
	}
}

func TestFormEmptyQueryOrNoRootsDoesNotConfirm(t *testing.T) {
	f := NewForm()
	f.SetSize(120, 40)
	f.Open(State{}, testProjects(), "")
	if cmd := f.Update(key(tea.KeyEnter, 0)); cmd != nil {
		t.Fatal("an empty query must not confirm")
	}
	if !f.IsOpen() {
		t.Fatal("the form must stay open without a query")
	}
	f2 := NewForm()
	f2.Open(State{Query: "q"}, []Project{{Root: "/b", Name: "b", Missing: true}}, "")
	if cmd := f2.Update(key(tea.KeyEnter, 0)); cmd != nil {
		t.Fatal("all-missing project list must not confirm")
	}
}

func TestFormProjectToggling(t *testing.T) {
	f := openedForm("")
	// Tab to the project list (query → include → exclude → projects).
	for i := 0; i < 3; i++ {
		f.Update(key(tea.KeyTab, 0))
	}
	if f.focus != fieldProjects {
		t.Fatalf("focus = %v, want projects", f.focus)
	}
	// Space toggles the highlighted (first) project out.
	f.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if !f.projects[0].Excluded {
		t.Fatal("space must exclude the highlighted project")
	}
	// A missing root's toggle is inert.
	f.Update(key(tea.KeyDown, 0))
	f.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if f.projects[1].Excluded {
		t.Fatal("a missing root must not become excludable state")
	}
	// Enter on the project list toggles too (never confirms).
	f.Update(key(tea.KeyDown, 0))
	if cmd := f.Update(key(tea.KeyEnter, 0)); cmd != nil {
		t.Fatal("enter on the project list must toggle, not confirm")
	}
	if f.projects[2].Excluded {
		t.Fatal("enter must have re-included the third project")
	}
}

func TestFormTogglesAndEditKeys(t *testing.T) {
	f := openedForm("")
	f.Update(key('c', tea.ModCtrl))
	f.Update(key('w', tea.ModCtrl))
	f.Update(key('x', tea.ModCtrl))
	st := f.State()
	if !st.CaseSensitive || !st.WholeWord || !st.Regex {
		t.Fatalf("toggles not flipped: %+v", st)
	}
	// cmd+backspace clears to line start via ui.EditKey (#2394).
	f.preselect = false
	f.query.Cur = f.query.Len()
	f.Update(key(tea.KeyBackspace, tea.ModSuper))
	if f.query.Text != "" {
		t.Fatalf("super+backspace must clear the query, got %q", f.query.Text)
	}
}

func TestFormEscCloses(t *testing.T) {
	f := openedForm("")
	f.Update(key(tea.KeyEscape, 0))
	if f.IsOpen() {
		t.Fatal("esc must close the form")
	}
}

func TestFormViewShowsProjectsAndMissing(t *testing.T) {
	f := openedForm("")
	v := ansi.Strip(f.View())
	for _, want := range []string{"Find in All Projects", "alpha", "beta", "gamma", "(missing)"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view lacks %q:\n%s", want, v)
		}
	}
	if !strings.Contains(v, "1 of 3") {
		t.Fatalf("view lacks the kept/total count:\n%s", v)
	}
}

func TestFormPasteReplacesPrefill(t *testing.T) {
	f := openedForm("")
	if !f.Paste("pasted") {
		t.Fatal("paste not handled")
	}
	if f.query.Text != "pasted" {
		t.Fatalf("paste must replace the preselected query, got %q", f.query.Text)
	}
}
