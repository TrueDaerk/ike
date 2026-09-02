package allfind

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/search"
)

func filledPopup() *Popup {
	p := NewPopup()
	p.SetSize(120, 40)
	p.Begin("needle", []Project{{Root: "/a", Name: "alpha"}, {Root: "/b", Name: "beta"}})
	p.Append("/a", []search.Match{
		{Path: "/a/x.go", Line: 3, Text: "needle here", StartCol: 0, EndCol: 6},
		{Path: "/a/y.go", Line: 7, Text: "a needle", StartCol: 2, EndCol: 8},
	})
	p.Append("/b", []search.Match{
		{Path: "/b/z.go", Line: 1, Text: "needle too", StartCol: 0, EndCol: 6},
	})
	p.Finish(false, nil)
	return p
}

func TestPopupGroupsByProjectThenFile(t *testing.T) {
	p := filledPopup()
	if len(p.groups) != 2 || p.groups[0].name != "alpha" || p.groups[1].name != "beta" {
		t.Fatalf("groups = %+v", p.groups)
	}
	if len(p.groups[0].files) != 2 || p.groups[0].count != 2 || p.total != 3 {
		t.Fatalf("grouping wrong: files=%d count=%d total=%d",
			len(p.groups[0].files), p.groups[0].count, p.total)
	}
	v := p.View()
	for _, want := range []string{"alpha", "beta", "x.go", "z.go", "3 matches in 2 projects"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view lacks %q:\n%s", want, v)
		}
	}
}

func TestPopupFinishShowsWithoutFocus(t *testing.T) {
	p := filledPopup()
	if !p.Visible() {
		t.Fatal("finish must show the popup")
	}
	if p.Focused() {
		t.Fatal("finish must NOT focus the popup — the editor keeps the keyboard")
	}
	p.Focus()
	if !p.Focused() {
		t.Fatal("focus must hand the popup the keyboard")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.Visible() || p.Focused() {
		t.Fatal("esc must hide the popup")
	}
	if !p.HasResults() {
		t.Fatal("results must survive hiding for the show-results command")
	}
}

func TestPopupEnterEmitsOpenMatch(t *testing.T) {
	p := filledPopup()
	p.Focus()
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // second match, still project /a
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // third match, project /b
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter must emit a cmd")
	}
	msg, ok := cmd().(OpenMatchMsg)
	if !ok {
		t.Fatalf("got %T, want OpenMatchMsg", cmd())
	}
	if msg.Root != "/b" || msg.Path != "/b/z.go" || msg.Line != 1 || msg.Col != 0 {
		t.Fatalf("wrong target: %+v", msg)
	}
	if p.Visible() {
		t.Fatal("opening a match hides the popup")
	}
}

func TestPopupTruncationAndErrors(t *testing.T) {
	p := NewPopup()
	p.SetSize(120, 40)
	p.Begin("q", []Project{{Root: "/a", Name: "alpha"}, {Root: "/gone", Name: "gone"}})
	p.Append("/a", []search.Match{{Path: "/a/x.go", Line: 1, Text: "q", StartCol: 0, EndCol: 1}})
	p.Finish(true, map[string]error{"/gone": errors.New("no such directory")})
	v := p.View()
	for _, want := range []string{"(truncated)", "1 root failed", "2 projects"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view lacks %q:\n%s", want, v)
		}
	}
}

func TestPopupBeginResetsForRerun(t *testing.T) {
	p := filledPopup()
	p.Begin("other", []Project{{Root: "/a", Name: "alpha"}})
	if p.Visible() || p.total != 0 || len(p.groups) != 0 || !p.Scanning() {
		t.Fatal("begin must reset results and hide the box until the scan finishes")
	}
}

func TestPopupClickSelectsThenOpens(t *testing.T) {
	p := filledPopup()
	x, y, _, _ := p.Rect()
	// Body rows: 0 = alpha header, 1 = x.go, 2 = match#0, 3 = y.go,
	// 4 = match#1. The cursor starts on match#0, so clicking match#1 selects.
	rowY := y + 1 + p.headerRows() + 4
	if cmd := p.Click(x+2, rowY); cmd != nil {
		t.Fatal("first click selects, must not open")
	}
	if p.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", p.cursor)
	}
	cmd := p.Click(x+2, rowY)
	if cmd == nil {
		t.Fatal("second click on the selected row must open")
	}
	if _, ok := cmd().(OpenMatchMsg); !ok {
		t.Fatalf("got %T, want OpenMatchMsg", cmd())
	}
}
