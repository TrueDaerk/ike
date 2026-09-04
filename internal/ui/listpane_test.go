package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/theme"
)

func TestClampWindowPutsCursorInView(t *testing.T) {
	cases := []struct {
		name                string
		cursor, top, n, h   int
		wantCursor, wantTop int
	}{
		{"cursor past the end", 20, 0, 5, 3, 4, 2},
		{"negative cursor", -3, 4, 5, 3, 0, 0},
		{"window follows down", 7, 0, 10, 4, 7, 4},
		{"window follows up", 1, 5, 10, 4, 1, 1},
		{"already in view", 3, 2, 10, 4, 3, 2},
		{"empty list", 4, 6, 0, 4, 0, 0},
		// The fix the shared clamp brings: a window parked past the last
		// full page is pulled back up, so no blank rows trail the list.
		{"list shrank under the window", 2, 8, 5, 4, 2, 1},
	}
	for _, c := range cases {
		cursor, top := c.cursor, c.top
		ClampWindow(&cursor, &top, c.n, c.h)
		if cursor != c.wantCursor || top != c.wantTop {
			t.Errorf("%s: got cursor=%d top=%d, want cursor=%d top=%d",
				c.name, cursor, top, c.wantCursor, c.wantTop)
		}
	}
}

func TestClickRowSelectsThenActivates(t *testing.T) {
	var tr ClickTracker
	cursor := 0
	now := time.Now()
	activated := -1
	act := func(i int) tea.Cmd { activated = i; return func() tea.Msg { return nil } }

	// y 2 with two header rows and top 3 is list row 3.
	if cmd := tr.ClickRow(2, 3, 2, 5, 10, now, &cursor, act); cmd != nil {
		t.Fatal("the first click must only select")
	}
	if cursor != 3 || activated != -1 {
		t.Fatalf("first click: cursor=%d activated=%d", cursor, activated)
	}
	if cmd := tr.ClickRow(2, 3, 2, 5, 10, now.Add(50*time.Millisecond), &cursor, act); cmd == nil {
		t.Fatal("the second click on the same row must activate it")
	}
	if activated != 3 {
		t.Fatalf("activated %d, want 3", activated)
	}
	// A third click starts over: the tracker was reset by the activation.
	activated = -1
	if cmd := tr.ClickRow(2, 3, 2, 5, 10, now.Add(80*time.Millisecond), &cursor, act); cmd != nil || activated != -1 {
		t.Fatal("the click after an activation must count as a first click")
	}
}

func TestClickRowOffRowResetsAndKeepsCursor(t *testing.T) {
	var tr ClickTracker
	cursor := 4
	now := time.Now()
	if cmd := tr.ClickRow(0, 0, 2, 5, 10, now, &cursor, nil); cmd != nil {
		t.Fatal("a header click activates nothing")
	}
	if cursor != 4 {
		t.Fatalf("a header click must leave the cursor alone, got %d", cursor)
	}
	// The pending click was cleared, so the next row click is a first click.
	if cmd := tr.ClickRow(2, 0, 2, 5, 10, now, &cursor, func(int) tea.Cmd { return func() tea.Msg { return nil } }); cmd != nil {
		t.Fatal("the click after a miss must count as a first click")
	}
}

func TestClickRowWithoutActivateOnlySelects(t *testing.T) {
	var tr ClickTracker
	cursor := 0
	now := time.Now()
	tr.ClickRow(2, 0, 2, 5, 10, now, &cursor, nil)
	if cmd := tr.ClickRow(2, 0, 2, 5, 10, now, &cursor, nil); cmd != nil {
		t.Fatal("a list without an activate must never return a command")
	}
	if cursor != 0 {
		t.Fatalf("cursor = %d, want 0", cursor)
	}
}

func TestSelectClick(t *testing.T) {
	cursor := 1
	if ok := SelectClick(4, 2, 2, 5, 10, &cursor); !ok || cursor != 4 {
		t.Fatalf("row click: ok=%v cursor=%d, want true/4", ok, cursor)
	}
	if ok := SelectClick(1, 2, 2, 5, 10, &cursor); ok || cursor != 4 {
		t.Fatalf("header click: ok=%v cursor=%d, want false/4", ok, cursor)
	}
	// Past the end of a short list: the blank rows are not rows.
	if ok := SelectClick(6, 0, 2, 5, 3, &cursor); ok {
		t.Error("a blank row below a short list must not select")
	}
}

func TestRenderWindowPadsToHeight(t *testing.T) {
	out := RenderWindow(1, 4, 3, "", func(i int) string { return "row" + string(rune('0'+i)) })
	if got, want := out, "row1\nrow2\n\n\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n := strings.Count(out, "\n"); n != 4 {
		t.Errorf("the block must always be 4 lines, got %d", n)
	}
}

func TestRenderWindowEmptyList(t *testing.T) {
	if got, want := RenderWindow(0, 3, 0, "(nothing)", nil), "(nothing)\n\n\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := RenderWindow(0, 2, 0, "", nil), "\n\n"; got != want {
		t.Errorf("no notice: got %q, want %q", got, want)
	}
}

func TestListPaneView(t *testing.T) {
	got := ListPaneView("head", "filter", "a\nb\n", " hints")
	if want := "head\nfilter\na\nb\n hints"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFileHeader(t *testing.T) {
	pal := theme.DefaultPalette()
	if got := FileHeader(pal, "", 0, false); !strings.Contains(got, "(no file)") {
		t.Errorf("no path: %q", got)
	}
	got := FileHeader(pal, "/src/pkg/main.go", 12, true)
	if !strings.Contains(got, "main.go") || !strings.Contains(got, "(12)") {
		t.Errorf("got %q, want the base name and the count", got)
	}
	if strings.Contains(got, "/src/pkg") {
		t.Errorf("the header shows the base name only, got %q", got)
	}
}

func TestBaseName(t *testing.T) {
	cases := map[string]string{
		"/a/b/c.go":       "c.go",
		`C:\a\b\c.go`:     "c.go",
		"c.go":            "c.go",
		"":                "",
		"/a/b/":           "",
		"/a/b\\weird.txt": "weird.txt",
	}
	for in, want := range cases {
		if got := BaseName(in); got != want {
			t.Errorf("BaseName(%q) = %q, want %q", in, got, want)
		}
	}
}

type trow struct {
	header bool
	name   string
}

func TestTargetRow(t *testing.T) {
	rows := []trow{{header: true, name: "h1"}, {name: "a"}, {name: "b"}, {header: true, name: "h2"}}
	isHeader := func(r trow) bool { return r.header }
	if r, ok := TargetRow(rows, 0, isHeader); !ok || r.name != "a" {
		t.Errorf("a header stands for its first entry: %v %v", r, ok)
	}
	if r, ok := TargetRow(rows, 2, isHeader); !ok || r.name != "b" {
		t.Errorf("an entry stands for itself: %v %v", r, ok)
	}
	if _, ok := TargetRow(rows, 3, isHeader); ok {
		t.Error("a trailing header has nothing under it")
	}
	if _, ok := TargetRow(rows, -1, isHeader); ok {
		t.Error("below the list")
	}
	if _, ok := TargetRow(rows, 9, isHeader); ok {
		t.Error("past the list")
	}
	if _, ok := TargetRow([]trow(nil), 0, isHeader); ok {
		t.Error("empty list")
	}
}

// fakeFilter is a FilterStepper stand-in: internal/filterbar builds on this
// package, so the interface is what StepFiltered can be tested against.
type fakeFilter struct {
	active bool
	shown  MatchStep
}

func (f *fakeFilter) Active() bool { return f.active }
func (f *fakeFilter) ShowStep(st MatchStep) MatchStep {
	f.shown = st
	return st
}

func TestStepFilteredSkipsHeaders(t *testing.T) {
	// rows: header, a, b, header, c — indices 0 and 3 are chrome.
	header := map[int]bool{0: true, 3: true}
	keep := func(i int) bool { return !header[i] }
	f := &fakeFilter{active: true}
	cursor, top := 1, 0
	if st := StepFiltered(f, &cursor, &top, 5, 2, 1, keep); !st.Handled {
		t.Fatalf("an open filter must answer the chord: %+v", st)
	}
	if cursor != 2 {
		t.Fatalf("cursor = %d, want 2", cursor)
	}
	if st := StepFiltered(f, &cursor, &top, 5, 2, 1, keep); !st.Handled {
		t.Fatalf("second step: %+v", st)
	}
	if cursor != 4 {
		t.Fatalf("the step must skip the header row, cursor = %d, want 4", cursor)
	}
	if top != 3 {
		t.Fatalf("the window must follow the cursor, top = %d, want 3", top)
	}
}

func TestStepFilteredClosedFilterDoesNotAnswer(t *testing.T) {
	f := &fakeFilter{}
	cursor, top := 1, 0
	if st := StepFiltered(f, &cursor, &top, 5, 2, 1, func(int) bool { return true }); st.Handled {
		t.Errorf("a closed filter must not answer the chord: %+v", st)
	}
	if cursor != 1 || top != 0 {
		t.Errorf("a closed filter must not move anything: cursor=%d top=%d", cursor, top)
	}
}
