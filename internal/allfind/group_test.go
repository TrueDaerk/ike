package allfind

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// groupProjects is a group's member list: all checked, one gone from disk.
func groupProjects() []Project {
	return []Project{
		{Root: "/api", Name: "api"},
		{Root: "/ui", Name: "ui"},
		{Root: "/gone", Name: "gone", Missing: true},
	}
}

func openedGroupForm() *Form {
	f := NewForm()
	f.SetSize(120, 40)
	f.OpenGroup(State{Query: "remembered", Include: "*.go"}, groupProjects(), "", "web")
	return f
}

// TestGroupFormNamesTheGroup: the group variant is the same form, titled and
// headed with the group it is restricted to (#2575).
func TestGroupFormNamesTheGroup(t *testing.T) {
	f := openedGroupForm()
	if !f.IsOpen() || f.Group() != "web" {
		t.Fatalf("group form not open for web: open=%v group=%q", f.IsOpen(), f.Group())
	}
	view := ansi.Strip(f.View())
	if !strings.Contains(view, "Find in Project Group") || !strings.Contains(view, "⦿ web") {
		t.Fatalf("title must name the group, got:\n%s", view)
	}
	if !strings.Contains(view, "Group web (2 of 3 searched") {
		t.Fatalf("heading must count the members, got:\n%s", view)
	}
	// The remembered project.find_all.* state is shared with the all-projects
	// form: query prefilled and preselected, globs carried over.
	if f.query.Text != "remembered" || f.include.Text != "*.go" || !f.preselect {
		t.Fatalf("remembered state not seeded: %+v", f.State())
	}
}

// TestGroupFormMembersAllChecked: every member starts checked, the missing one
// greyed out and skipped — the all-projects rule, on the group's list.
func TestGroupFormMembersAllChecked(t *testing.T) {
	f := openedGroupForm()
	kept := f.keptRoots()
	if len(kept) != 2 || kept[0].Root != "/api" || kept[1].Root != "/ui" {
		t.Fatalf("kept roots = %+v, want the two live members in group order", kept)
	}
	if st := f.State(); len(st.ExcludedRoots) != 0 {
		t.Fatalf("no member may start excluded, got %v", st.ExcludedRoots)
	}
}

// TestGroupFormConfirmCarriesGroup: confirming names the group on the msg, so
// the root model knows not to write project.find_all.excluded_roots back.
func TestGroupFormConfirmCarriesGroup(t *testing.T) {
	f := openedGroupForm()
	msg := confirmMsg(t, f.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	if msg.Group != "web" {
		t.Fatalf("ConfirmMsg.Group = %q, want web", msg.Group)
	}
	if len(msg.Roots) != 2 {
		t.Fatalf("scanned roots = %+v, want the live members only", msg.Roots)
	}
	if f.IsOpen() {
		t.Error("enter must close the form, like the all-projects one")
	}
}

// TestFormOpenClearsGroup: the all-projects form after a group one is the
// plain variant again.
func TestFormOpenClearsGroup(t *testing.T) {
	f := openedGroupForm()
	f.Open(State{Query: "q"}, testProjects(), "")
	if f.Group() != "" {
		t.Fatalf("Group() = %q, want empty for the all-projects form", f.Group())
	}
	if view := ansi.Strip(f.View()); !strings.Contains(view, "Find in All Projects") {
		t.Fatalf("title must be the all-projects one, got:\n%s", view)
	}
}

// TestGroupResultsHeaderNamesTheSet: the overlay says which set was searched —
// `group web · 3 projects` — and the progress segment counts the group.
func TestGroupResultsHeaderNamesTheSet(t *testing.T) {
	r := NewResults()
	r.SetSize(140, 44)
	roots := []Project{{Root: "/api", Name: "api"}, {Root: "/ui", Name: "ui"}, {Root: "/www", Name: "www"}}
	r.BeginGroup("needle", roots, "web")
	if r.Group() != "web" {
		t.Fatalf("Group() = %q, want web", r.Group())
	}
	if seg := r.ProgressLabel(); !strings.Contains(seg, "group web 0/3") {
		t.Fatalf("progress segment = %q, want the group counter", seg)
	}
	r.Finish(false, nil)
	r.Open()
	view := ansi.Strip(r.View())
	if !strings.Contains(view, "Find in Project Group") {
		t.Fatalf("title must name the group variant, got:\n%s", view)
	}
	if !strings.Contains(view, "group web · 3 projects") {
		t.Fatalf("header must name the searched set, got:\n%s", view)
	}
}

// TestAllProjectsResultsHeaderUnchanged: an all-projects run keeps its old
// header — the group wording is additive.
func TestAllProjectsResultsHeaderUnchanged(t *testing.T) {
	r := NewResults()
	r.SetSize(140, 44)
	r.Begin("needle", []Project{{Root: "/a", Name: "alpha"}})
	if seg := r.ProgressLabel(); !strings.Contains(seg, "all projects 0/1") {
		t.Fatalf("progress segment = %q, want the all-projects counter", seg)
	}
	r.Finish(false, nil)
	r.Open()
	view := ansi.Strip(r.View())
	if !strings.Contains(view, "Find in All Projects") || strings.Contains(view, "group ") {
		t.Fatalf("all-projects header changed:\n%s", view)
	}
}

// TestResultsBeginClearsGroup: an all-projects run after a group one drops the
// group from the retained set.
func TestResultsBeginClearsGroup(t *testing.T) {
	r := NewResults()
	r.SetSize(140, 44)
	r.BeginGroup("needle", []Project{{Root: "/api", Name: "api"}}, "web")
	r.Begin("other", []Project{{Root: "/a", Name: "alpha"}})
	if r.Group() != "" {
		t.Fatalf("Group() = %q, want empty after an all-projects Begin", r.Group())
	}
}
