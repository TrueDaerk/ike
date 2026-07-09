package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

var escKey = tea.KeyPressMsg{Code: tea.KeyEscape}

func TestSubConfirmEntersAndPrompts(t *testing.T) {
	m, _ := loaded(t, "a b a\n")
	m = runEx(m, "s/a/X/gc")
	if m.subConfirm == nil {
		t.Fatal("c flag should enter confirmation mode")
	}
	if m.mode != Command || m.cmdMsg != "replace (y/n/a/q/l)?" {
		t.Fatalf("prompt not shown: mode=%v msg=%q", m.mode, m.cmdMsg)
	}
	// First match highlighted at its span (0..0 inclusive for "a").
	if m.subConfirm.curLine != 0 || m.subConfirm.curStart != 0 || m.subConfirm.curEnd != 1 {
		t.Fatalf("highlight span = (%d,%d,%d)", m.subConfirm.curLine, m.subConfirm.curStart, m.subConfirm.curEnd)
	}
	// The prompt renders on the command-line row.
	if !strings.Contains(m.View(), "replace (y/n/a/q/l)?") {
		t.Fatal("prompt should render in the view")
	}
}

func TestSubConfirmYesReplacesAll(t *testing.T) {
	m, _ := loaded(t, "a a a\n")
	m = runEx(m, "s/a/X/gc")
	m = send(m, key('y'), key('y'), key('y'))
	if got := line(m, 0); got != "X X X" {
		t.Fatalf("all-yes: %q", got)
	}
	if m.subConfirm != nil || m.mode != Normal {
		t.Fatal("confirmation should end after the last match")
	}
}

func TestSubConfirmSkip(t *testing.T) {
	m, _ := loaded(t, "a a a\n")
	m = runEx(m, "s/a/X/gc")
	// y, n, y → replace first and third, skip the middle.
	m = send(m, key('y'), key('n'), key('y'))
	if got := line(m, 0); got != "X a X" {
		t.Fatalf("skip middle: %q", got)
	}
}

func TestSubConfirmAllRemaining(t *testing.T) {
	m, _ := loaded(t, "a a a a\n")
	m = runEx(m, "s/a/X/gc")
	// Skip the first, then 'a' replaces every remaining match.
	m = send(m, key('n'), key('a'))
	if got := line(m, 0); got != "a X X X" {
		t.Fatalf("all-remaining: %q", got)
	}
	if m.subConfirm != nil {
		t.Fatal("'a' should finish the confirmation")
	}
}

func TestSubConfirmQuit(t *testing.T) {
	m, _ := loaded(t, "a a a\n")
	m = runEx(m, "s/a/X/gc")
	// Replace first, then quit before the rest.
	m = send(m, key('y'), key('q'))
	if got := line(m, 0); got != "X a a" {
		t.Fatalf("quit: %q", got)
	}
	if m.subConfirm != nil {
		t.Fatal("'q' should finish the confirmation")
	}
}

func TestSubConfirmLastReplaceAndQuit(t *testing.T) {
	m, _ := loaded(t, "a a a\n")
	m = runEx(m, "s/a/X/gc")
	// 'l' replaces the current match then stops.
	m = send(m, key('l'))
	if got := line(m, 0); got != "X a a" {
		t.Fatalf("l: %q", got)
	}
	if m.subConfirm != nil {
		t.Fatal("'l' should finish the confirmation")
	}
}

func TestSubConfirmEscapeCancels(t *testing.T) {
	m, _ := loaded(t, "a a a\n")
	m = runEx(m, "s/a/X/gc")
	m = send(m, key('y')) // one replacement applied
	m = send(m, escKey)   // cancel the rest
	if got := line(m, 0); got != "X a a" {
		t.Fatalf("escape keeps applied replacements: %q", got)
	}
	if m.subConfirm != nil || m.mode != Normal {
		t.Fatal("escape should leave confirmation mode")
	}
}

func TestSubConfirmSingleUndoUnit(t *testing.T) {
	m, _ := loaded(t, "a\na\na\n")
	m = runEx(m, "%s/a/b/gc")
	m = send(m, key('y'), key('y'), key('y'))
	for i := 0; i < 3; i++ {
		if line(m, i) != "b" {
			t.Fatalf("setup line %d: %q", i, line(m, i))
		}
	}
	// One undo reverts every confirmed replacement.
	m = send(m, key('u'))
	for i := 0; i < 3; i++ {
		if got := line(m, i); got != "a" {
			t.Fatalf("after undo line %d = %q want a", i, got)
		}
	}
}

func TestSubConfirmDeltaMultiMatchPerLine(t *testing.T) {
	// A longer replacement shifts later matches on the same line.
	m, _ := loaded(t, "aaa\n")
	m = runEx(m, "s/a/XX/gc")
	m = send(m, key('y'), key('y'), key('y'))
	if got := line(m, 0); got != "XXXXXX" {
		t.Fatalf("delta handling: %q", got)
	}
}
