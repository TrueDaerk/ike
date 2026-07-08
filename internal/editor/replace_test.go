package editor

import (
	"strings"
	"testing"
)

func TestApplyReplacementsSingleUndoUnit(t *testing.T) {
	m, _ := loaded(t, "one needle\ntwo needle\nplain\n")
	n := m.ApplyReplacements([]Replacement{
		{Line: 1, StartCol: 4, EndCol: 10, Text: "thread", Expect: "one needle"},
		{Line: 2, StartCol: 4, EndCol: 10, Text: "thread", Expect: "two needle"},
	})
	if n != 2 {
		t.Fatalf("applied %d, want 2", n)
	}
	if got := m.buf.String(); !strings.Contains(got, "one thread") || !strings.Contains(got, "two thread") {
		t.Fatalf("replacements missing: %q", got)
	}
	if !m.Dirty() {
		t.Fatal("buffer edits must mark dirty")
	}
	m = send(m, key('u')) // ONE undo reverts the whole batch
	if got := m.buf.String(); !strings.Contains(got, "one needle") || !strings.Contains(got, "two needle") {
		t.Fatalf("one undo must revert every replacement: %q", got)
	}
}

func TestApplyReplacementsSkipsStaleLines(t *testing.T) {
	m, _ := loaded(t, "changed since scan\nstill needle here\n")
	n := m.ApplyReplacements([]Replacement{
		{Line: 1, StartCol: 0, EndCol: 6, Text: "X", Expect: "needle was here"}, // stale
		{Line: 2, StartCol: 6, EndCol: 12, Text: "thread", Expect: "still needle here"},
	})
	if n != 1 {
		t.Fatalf("applied %d, want 1 (stale line skipped)", n)
	}
	if got := line(m, 0); got != "changed since scan" {
		t.Fatalf("stale line must stay untouched: %q", got)
	}
	if got := line(m, 1); got != "still thread here" {
		t.Fatalf("valid line not replaced: %q", got)
	}
}

func TestApplyReplacementsMultipleMatchesOneLine(t *testing.T) {
	m, _ := loaded(t, "needle and needle\n")
	n := m.ApplyReplacements([]Replacement{
		{Line: 1, StartCol: 0, EndCol: 6, Text: "pin", Expect: "needle and needle"},
		{Line: 1, StartCol: 11, EndCol: 17, Text: "pin", Expect: "needle and needle"},
	})
	if n != 2 {
		t.Fatalf("applied %d, want 2", n)
	}
	if got := line(m, 0); got != "pin and pin" {
		t.Fatalf("got %q", got)
	}
	m = send(m, key('u'))
	if got := line(m, 0); got != "needle and needle" {
		t.Fatalf("undo failed: %q", got)
	}
}

func TestApplyReplacementsEmitsChange(t *testing.T) {
	m, _ := loaded(t, "needle\n")
	var kinds []EventKind
	m.SetEmitter(EmitterFunc(func(e Event) { kinds = append(kinds, e.Kind) }))
	m.ApplyReplacements([]Replacement{{Line: 1, StartCol: 0, EndCol: 6, Text: "x", Expect: "needle"}})
	for _, k := range kinds {
		if k == EventChange {
			return
		}
	}
	t.Fatal("replacements must emit EventChange (LSP/highlight/shared-doc sync)")
}
