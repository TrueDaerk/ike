package allfind

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/search"
)

// filledResults is a finished two-project scan: three matches, two files in
// the first project, one in the second.
func filledResults() *Results {
	r := NewResults()
	r.SetSize(120, 40)
	r.Begin("needle", []Project{{Root: "/a", Name: "alpha"}, {Root: "/b", Name: "beta"}})
	r.Append("/a", []search.Match{
		{Path: "/a/x.go", Line: 3, Text: "needle here", StartCol: 0, EndCol: 6},
		{Path: "/a/y.go", Line: 7, Text: "a needle", StartCol: 2, EndCol: 8},
	})
	r.Append("/b", []search.Match{
		{Path: "/b/z.go", Line: 1, Text: "needle too", StartCol: 0, EndCol: 6},
	})
	r.Finish(false, nil)
	return r
}

func rkey(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "cmd+g":
		return tea.KeyPressMsg{Code: 'g', Mod: tea.ModMeta}
	case "cmd+shift+g":
		return tea.KeyPressMsg{Code: 'g', Mod: tea.ModMeta | tea.ModShift}
	}
	panic("unmapped key " + s)
}

// openTarget presses enter and reports the match the pane dispatched.
func openTarget(t *testing.T, r *Results) OpenMatchMsg {
	t.Helper()
	cmd := r.Update(rkey("enter"))
	if cmd == nil {
		t.Fatal("enter must emit a cmd")
	}
	msg, ok := cmd().(OpenMatchMsg)
	if !ok {
		t.Fatalf("got %T, want OpenMatchMsg", cmd())
	}
	return msg
}

// TestResultsGroupProjectThenFileThenMatches pins the grouping order the pane
// renders: project header, its files, their matches — in scan order (#2413).
func TestResultsGroupProjectThenFileThenMatches(t *testing.T) {
	r := filledResults()
	if r.Total() != 3 {
		t.Fatalf("Total() = %d, want 3", r.Total())
	}
	v := r.View()
	order := []string{"alpha", "x.go", "y.go", "beta", "z.go"}
	at := -1
	for _, want := range order {
		i := strings.Index(v, want)
		if i < 0 {
			t.Fatalf("view lacks %q:\n%s", want, v)
		}
		if i < at {
			t.Fatalf("%q renders out of order (want %v):\n%s", want, order, v)
		}
		at = i
	}
	if !strings.Contains(v, "3 matches in 2 projects") {
		t.Fatalf("view lacks the summary line:\n%s", v)
	}
	// File headers are project-relative, like find-in-path's.
	if strings.Contains(v, "/a/x.go") {
		t.Fatalf("file headers must be project-relative:\n%s", v)
	}
}

// TestResultsOpenOnFinishFocused: the completed scan opens the overlay with
// the keyboard — the find-in-path presentation, not a blurred corner box.
func TestResultsOpenOnFinishFocused(t *testing.T) {
	r := filledResults()
	if !r.IsOpen() {
		t.Fatal("a finished scan with hits must open the results overlay")
	}
	r.Update(rkey("esc"))
	if r.IsOpen() {
		t.Fatal("esc must close the overlay")
	}
	if !r.HasResults() || r.Total() != 3 {
		t.Fatal("the result set must survive closing")
	}
	r.Open()
	if !r.IsOpen() {
		t.Fatal("the show-results command must re-open the overlay")
	}
}

// TestResultsEmptyScanStaysClosed: nothing to walk, nothing to steal the
// keyboard for — the pane opens only on demand then.
func TestResultsEmptyScanStaysClosed(t *testing.T) {
	r := NewResults()
	r.SetSize(120, 40)
	r.Begin("nothing", []Project{{Root: "/a", Name: "alpha"}})
	r.Finish(false, nil)
	if r.IsOpen() {
		t.Fatal("an empty result set must not open the overlay")
	}
	if !r.HasResults() {
		t.Fatal("the empty result must still be re-openable")
	}
	r.Open()
	if !strings.Contains(r.View(), "no matches") {
		t.Fatalf("the empty overlay must say so:\n%s", r.View())
	}
}

// TestResultsEnterOpensMatchWithItsProject: the dispatched target carries the
// project root, so a hit in another project can switch first.
func TestResultsEnterOpensMatchWithItsProject(t *testing.T) {
	r := filledResults()
	r.Update(rkey("down")) // second match, still project /a
	r.Update(rkey("down")) // third match, project /b
	msg := openTarget(t, r)
	if msg.Root != "/b" || msg.Path != "/b/z.go" || msg.Line != 1 || msg.Col != 0 {
		t.Fatalf("wrong target: %+v", msg)
	}
	if r.IsOpen() {
		t.Fatal("opening a match closes the overlay, like find-in-path's")
	}
	if r.Total() != 3 {
		t.Fatal("the result set must stay available after the jump")
	}
}

// TestResultsMatchStepChordSteps: cmd+g / cmd+shift+g walk the matches while
// the overlay owns the keyboard (#2410), wrapping at both ends and reporting
// the shared counter.
func TestResultsMatchStepChordSteps(t *testing.T) {
	r := filledResults()
	r.Update(rkey("cmd+g"))
	if msg := openTarget(t, r); msg.Path != "/a/y.go" {
		t.Fatalf("cmd+g landed on %s, want the second match", msg.Path)
	}
	r.Open()
	st := r.StepMatch(-1)
	if !st.Handled || st.Index != 1 || st.Total != 3 || st.Wrapped {
		t.Fatalf("StepMatch(-1) = %+v, want 1/3 unwrapped", st)
	}
	st = r.StepMatch(-1)
	if !st.Wrapped || st.Index != 3 {
		t.Fatalf("StepMatch(-1) = %+v, want the wrap to the last match", st)
	}
	if !strings.Contains(r.View(), "3/3 (wrapped)") {
		t.Fatalf("the status row must show the shared counter:\n%s", r.View())
	}
}

// TestResultsProgressLabelCountsProjects: while the scan runs the pane shows
// nothing — the status line carries the progress instead (#2413).
func TestResultsProgressLabelCountsProjects(t *testing.T) {
	r := NewResults()
	r.SetSize(120, 40)
	r.Begin("needle", []Project{{Root: "/a", Name: "alpha"}, {Root: "/b", Name: "beta"}})
	if r.IsOpen() {
		t.Fatal("a running scan must not open the overlay")
	}
	if got := r.ProgressLabel(); !strings.Contains(got, "0/2") {
		t.Fatalf("ProgressLabel() = %q, want the project counter", got)
	}
	r.Append("/a", []search.Match{{Path: "/a/x.go", Line: 1, Text: "needle", StartCol: 0, EndCol: 6}})
	r.Progress(1)
	got := r.ProgressLabel()
	if !strings.Contains(got, "1/2") || !strings.Contains(got, "1 hit") {
		t.Fatalf("ProgressLabel() = %q, want projects done and hits so far", got)
	}
	r.Finish(false, nil)
	if r.ProgressLabel() != "" {
		t.Fatalf("ProgressLabel() = %q, want it gone once the scan is done", r.ProgressLabel())
	}
}

// TestResultsTruncationAndErrors: the summary carries the truncation marker
// and the failed-root count, the project header the error itself.
func TestResultsTruncationAndErrors(t *testing.T) {
	r := NewResults()
	r.SetSize(120, 40)
	r.Begin("q", []Project{{Root: "/a", Name: "alpha"}, {Root: "/gone", Name: "gone"}})
	r.Append("/a", []search.Match{{Path: "/a/x.go", Line: 1, Text: "q", StartCol: 0, EndCol: 1}})
	r.Finish(true, map[string]error{"/gone": errors.New("no such directory")})
	v := r.View()
	for _, want := range []string{"(truncated)", "1 root failed", "2 projects"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view lacks %q:\n%s", want, v)
		}
	}
}

// TestResultsBeginResetsForRerun: a second search drops the previous set and
// closes the overlay until the new scan finishes.
func TestResultsBeginResetsForRerun(t *testing.T) {
	r := filledResults()
	r.Begin("other", []Project{{Root: "/a", Name: "alpha"}})
	if r.IsOpen() || r.Total() != 0 || !r.Scanning() {
		t.Fatal("begin must reset the results and keep the overlay closed while scanning")
	}
	if r.HasResults() {
		t.Fatal("a running scan has no results to show yet")
	}
}

// TestResultsClickSelectsThenOpens mirrors the finder's press-again-to-open
// contract, with the section rows counted in.
func TestResultsClickSelectsThenOpens(t *testing.T) {
	r := filledResults()
	r.View() // fills the click layout
	// List rows: 0 = alpha section, 1 = x.go, 2 = match#0, 3 = y.go,
	// 4 = match#1. The cursor starts on match#0, so clicking match#1 selects.
	y := 1 + r.lay.listTop + 4
	if cmd := r.Click(2, y); cmd != nil {
		t.Fatal("the first click selects, it must not open")
	}
	if _, it, _ := r.Current(); it.Path != "/a/y.go" {
		t.Fatalf("click selected %s, want /a/y.go", it.Path)
	}
	cmd := r.Click(2, y)
	if cmd == nil {
		t.Fatal("a second click on the selected row must open it")
	}
	if _, ok := cmd().(OpenMatchMsg); !ok {
		t.Fatalf("got %T, want OpenMatchMsg", cmd())
	}
}

// TestResultsAdvanceWalksAcrossProjects: the retained-results seam cmd+g uses
// with the overlay closed reports each match with its project root.
func TestResultsAdvanceWalksAcrossProjects(t *testing.T) {
	r := filledResults()
	r.Update(rkey("esc"))
	roots := []string{}
	for i := 0; i < 3; i++ {
		root, _, ok := r.Advance(1)
		if !ok {
			t.Fatal("Advance must report a match")
		}
		roots = append(roots, root)
	}
	if want := []string{"/a", "/b", "/a"}; strings.Join(roots, ",") != strings.Join(want, ",") {
		t.Fatalf("roots = %v, want %v (wrapping back into the first project)", roots, want)
	}
}
