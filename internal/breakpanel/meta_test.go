package breakpanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/debug"
)

func escKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEscape} }

// typeText feeds a string into the open inline editor rune by rune.
func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// TestConditionEditFlow drives `c` + typing + enter into a SetMetaMsg that
// keeps the row's other refinements.
func TestConditionEditFlow(t *testing.T) {
	b := store(t)
	b.SetMeta("a.py", 2, debug.Meta{HitCondition: "5"})
	m := New(nil)
	m.SetSize(120, 10)
	m.SetStore(b)
	m.Update(key("down")) // onto a.py:2
	m.Update(key("c"))
	if !m.Editing() {
		t.Fatal("c must open the condition editor")
	}
	typeText(&m, "i > 3")
	msg := run(t, m.Update(key("enter")))
	got, ok := msg.(SetMetaMsg)
	if !ok || got.Path != "a.py" || got.Line != 2 {
		t.Fatalf("commit = %+v", msg)
	}
	if got.Meta.Condition != "i > 3" || got.Meta.HitCondition != "5" {
		t.Fatalf("meta = %+v, want the hit count preserved", got.Meta)
	}
	if m.Editing() {
		t.Fatal("commit must close the editor")
	}
}

// TestLogMessageEditAndEscape covers `l`, prefill, esc cancel, and that an
// emptied field clears through the commit.
func TestLogMessageEditAndEscape(t *testing.T) {
	b := store(t)
	b.SetMeta("a.py", 2, debug.Meta{LogMessage: "old"})
	m := New(nil)
	m.SetSize(120, 10)
	m.SetStore(b)
	m.Update(key("down"))
	m.Update(key("l"))
	if string(m.editBuf) != "old" {
		t.Fatalf("editor must prefill the current value, got %q", string(m.editBuf))
	}
	m.Update(escKey())
	if m.Editing() {
		t.Fatal("esc must cancel")
	}

	m.Update(key("l"))
	for range "old" {
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	msg := run(t, m.Update(key("enter")))
	got, ok := msg.(SetMetaMsg)
	if !ok || got.Meta.LogMessage != "" {
		t.Fatalf("emptied commit = %+v, want a cleared log message", msg)
	}
}

// TestMetaRendersDistinctly checks the row suffixes and the logpoint glyph.
func TestMetaRendersDistinctly(t *testing.T) {
	b := store(t)
	b.SetMeta("a.py", 2, debug.Meta{Condition: "n == 1", HitCondition: "3"})
	b.SetMeta("a.py", 7, debug.Meta{LogMessage: "hit {n}"})
	m := New(nil)
	m.SetSize(120, 10)
	m.SetStore(b)
	v := m.View()
	if !strings.Contains(v, "if n == 1") || !strings.Contains(v, "hit 3") {
		t.Fatalf("condition/hit suffixes missing:\n%s", v)
	}
	if !strings.Contains(v, "◆") || !strings.Contains(v, "log \"hit {n}\"") {
		t.Fatalf("logpoint must render the ◆ glyph and its message:\n%s", v)
	}
	if !strings.Contains(v, "c condition") || !strings.Contains(v, "l log message") {
		t.Fatalf("footer must document the refinement keys:\n%s", v)
	}
}

// TestMetaEditIgnoredOnHeader verifies `c` on a file header is a no-op.
func TestMetaEditIgnoredOnHeader(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 10)
	m.SetStore(store(t))
	m.Update(key("c")) // cursor starts on the a.py header
	if m.Editing() {
		t.Fatal("headers carry no refinements")
	}
}
