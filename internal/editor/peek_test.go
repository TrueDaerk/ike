package editor

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
)

// peek_test.go covers the peek-definition popup (#1154): open/render, the
// keys it owns (esc close, enter jump, scroll), and the any-other-key
// close-and-fall-through rule — plus the inline candidate list of a
// multi-target peek (#2168): counter, tab cycling, per-candidate excerpt.

func peekLines(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "line" + strings.Repeat("x", i%3)
	}
	return out
}

func openedPeek(t *testing.T, n int) Model {
	t.Helper()
	m, _ := loaded(t, "alpha\nbeta\ngamma\n")
	m.OpenPeek("pkg/target.go:42", peekLines(n), "/tmp/target.go", 41, 2)
	if !m.PeekOpen() {
		t.Fatal("peek must be open after OpenPeek")
	}
	return m
}

func TestPeekViewShowsTitleAndLines(t *testing.T) {
	m := openedPeek(t, 3)
	v := m.PeekView()
	if !strings.Contains(v, "pkg/target.go:42") {
		t.Fatalf("view must carry the path:line title, got:\n%s", v)
	}
	for _, l := range peekLines(3) {
		if !strings.Contains(v, l) {
			t.Fatalf("view must contain excerpt line %q, got:\n%s", l, v)
		}
	}
	if strings.Contains(v, "…") {
		t.Fatalf("short excerpt must not show scroll ellipses:\n%s", v)
	}
}

func TestPeekEscCloses(t *testing.T) {
	m := openedPeek(t, 3)
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.PeekOpen() {
		t.Fatal("esc must close the peek")
	}
	if cmd != nil {
		t.Fatal("esc close must not emit a command")
	}
}

func TestPeekEnterJumpsViaDefinitionMsg(t *testing.T) {
	m := openedPeek(t, 3)
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.PeekOpen() {
		t.Fatal("enter must close the peek")
	}
	if cmd == nil {
		t.Fatal("enter must emit the jump command")
	}
	msg, ok := cmd().(ilsp.DefinitionMsg)
	if !ok {
		t.Fatalf("enter must jump through the DefinitionMsg funnel, got %#v", cmd())
	}
	if msg.Path != "/tmp/target.go" || msg.Line != 41 || msg.Col != 2 {
		t.Fatalf("jump target = %+v, want /tmp/target.go:41:2", msg)
	}
}

func TestPeekOtherKeyClosesAndFallsThrough(t *testing.T) {
	m := openedPeek(t, 3)
	line0 := m.buf.Line(0)
	m, _ = m.Update(key('x'))
	if m.PeekOpen() {
		t.Fatal("an unowned key must close the peek")
	}
	if got := m.buf.Line(0); got == line0 {
		t.Fatalf("the key must still be handled normally (x deletes), line stayed %q", got)
	}
}

func TestPeekScrollAndEllipses(t *testing.T) {
	m := openedPeek(t, peekVisibleRows+8)
	v := m.PeekView()
	if !strings.Contains(v, "…") {
		t.Fatalf("long excerpt must mark clipped rows below:\n%s", v)
	}
	// Down moves one row, ctrl+d half a window.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.peek.scroll != 1 {
		t.Fatalf("down must scroll one row, scroll=%d", m.peek.scroll)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if m.peek.scroll != 1+peekVisibleRows/2 {
		t.Fatalf("ctrl+d must scroll half a window, scroll=%d", m.peek.scroll)
	}
	// Far past the end clamps to the last full window.
	for range 20 {
		m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if want := 8; m.peek.scroll != want {
		t.Fatalf("scroll must clamp at %d, got %d", want, m.peek.scroll)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.peek.scroll != 8-peekVisibleRows/2-1 {
		t.Fatalf("ctrl+u/up must scroll back, scroll=%d", m.peek.scroll)
	}
	if !m.PeekOpen() {
		t.Fatal("scrolling must keep the peek open")
	}
}

// openedPeekTargets opens a peek over n candidates, each with its own
// one-line excerpt naming it.
func openedPeekTargets(t *testing.T, n int) Model {
	t.Helper()
	m, _ := loaded(t, "alpha\nbeta\ngamma\n")
	targets := make([]PeekTarget, n)
	for i := range targets {
		name := "t" + strconv.Itoa(i)
		targets[i] = PeekTarget{
			Title: name + ".go:" + strconv.Itoa(i+1),
			Lines: []string{"body of " + name},
			Path:  "/tmp/" + name + ".go",
			Line:  i,
			Col:   0,
		}
	}
	m.OpenPeekTargets(targets)
	if !m.PeekOpen() {
		t.Fatal("peek must be open after OpenPeekTargets")
	}
	return m
}

func TestPeekCandidateListAndCounter(t *testing.T) {
	m := openedPeekTargets(t, 3)
	v := m.PeekView()
	for _, want := range []string{"t0.go:1", "(1/3)", "body of t0", "t1.go:2", "t2.go:3", "tab: next candidate"} {
		if !strings.Contains(v, want) {
			t.Fatalf("multi-candidate view must show %q, got:\n%s", want, v)
		}
	}
	// A single candidate keeps the plain popup: no counter, no list, no hint.
	single := openedPeekTargets(t, 1)
	v = single.PeekView()
	for _, unwanted := range []string{"(1/1)", "tab: next candidate"} {
		if strings.Contains(v, unwanted) {
			t.Fatalf("single-candidate view must not show %q, got:\n%s", unwanted, v)
		}
	}
}

func TestPeekTabCyclesCandidates(t *testing.T) {
	m := openedPeekTargets(t, 3)
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if cmd != nil {
		t.Fatal("tab must not emit a command")
	}
	if !m.PeekOpen() {
		t.Fatal("tab must keep the peek open")
	}
	if v := m.PeekView(); !strings.Contains(v, "(2/3)") || !strings.Contains(v, "body of t1") {
		t.Fatalf("tab must show the next candidate's excerpt, got:\n%s", v)
	}
	// shift+tab goes back, and wraps around the front of the list.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if v := m.PeekView(); !strings.Contains(v, "(3/3)") {
		t.Fatalf("shift+tab must wrap to the last candidate, got:\n%s", v)
	}
	// Enter jumps to the selected candidate, not the first one.
	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter must emit the jump command")
	}
	msg, ok := cmd().(ilsp.DefinitionMsg)
	if !ok || msg.Path != "/tmp/t2.go" || msg.Line != 2 {
		t.Fatalf("enter must jump to the selected candidate, got %#v", cmd())
	}
}

func TestPeekTabOnSingleCandidateIsSwallowed(t *testing.T) {
	m := openedPeekTargets(t, 1)
	line0 := m.buf.Line(0)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.PeekOpen() {
		t.Fatal("tab must not close a single-candidate peek")
	}
	if got := m.buf.Line(0); got != line0 {
		t.Fatalf("tab must not reach the buffer, line0 = %q", got)
	}
}

func TestPeekCandidateSwitchResetsScroll(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\ngamma\n")
	m.OpenPeekTargets([]PeekTarget{
		{Title: "long.go:1", Lines: peekLines(peekVisibleRows + 8), Path: "/tmp/long.go", Line: 0},
		{Title: "short.go:1", Lines: []string{"only"}, Path: "/tmp/short.go", Line: 0},
	})
	m, _ = m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if m.peek.scroll == 0 {
		t.Fatal("setup: ctrl+d must scroll the long excerpt")
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.peek.scroll != 0 {
		t.Fatalf("switching candidates must start at the top, scroll=%d", m.peek.scroll)
	}
	// The short excerpt has nothing to scroll: the offset stays clamped.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.peek.scroll != 0 {
		t.Fatalf("a one-line excerpt must not scroll, scroll=%d", m.peek.scroll)
	}
}

func TestPeekDropsEmptyCandidates(t *testing.T) {
	m, _ := loaded(t, "alpha\n")
	m.OpenPeekTargets([]PeekTarget{
		{Title: "empty.go:1", Path: "/tmp/empty.go"},
		{Title: "real.go:2", Lines: []string{"code"}, Path: "/tmp/real.go", Line: 1},
	})
	if !m.PeekOpen() {
		t.Fatal("a candidate with an excerpt must open the peek")
	}
	if v := m.PeekView(); strings.Contains(v, "empty.go") {
		t.Fatalf("excerpt-less candidates must be dropped, got:\n%s", v)
	}
	// Nothing left to show opens nothing at all.
	var none Model
	none, _ = loaded(t, "alpha\n")
	none.OpenPeekTargets([]PeekTarget{{Title: "empty.go:1", Path: "/tmp/empty.go"}})
	if none.PeekOpen() {
		t.Fatal("candidates without excerpts must not open a popup")
	}
}

func TestPeekLineRange(t *testing.T) {
	m, _ := loaded(t, "l0\nl1\nl2\nl3\nl4\n")
	got := m.LineRange(1, 3)
	if len(got) != 3 || got[0] != "l1" || got[2] != "l3" {
		t.Fatalf("LineRange(1,3) = %v", got)
	}
	if got := m.LineRange(4, 10); len(got) > 2 {
		t.Fatalf("LineRange past EOF must clamp, got %v", got)
	}
	if got := m.LineRange(99, 3); got != nil {
		t.Fatalf("LineRange beyond the buffer = %v, want nil", got)
	}
}
