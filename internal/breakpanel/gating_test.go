package breakpanel

import (
	"strings"
	"testing"

	"ike/internal/debug"
)

// TestUnsupportedFieldRefusesEditor gates the three inline editors on the
// adapter capabilities (#2245): the editor stays closed and the panel asks
// the root model for a notice instead.
func TestUnsupportedFieldRefusesEditor(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 10)
	m.SetStore(store(t))
	m.SetCaps(Caps{Condition: false, HitCondition: false, LogMessage: true})
	m.Update(key("down")) // onto a.py:2

	for _, c := range []struct{ key, want string }{
		{"c", "conditional breakpoints"},
		{"n", "hit counts"},
	} {
		msg := run(t, m.Update(key(c.key)))
		notice, ok := msg.(NoticeMsg)
		if !ok || !strings.Contains(notice.Text, c.want) {
			t.Fatalf("%q gave %+v, want a notice naming %q", c.key, msg, c.want)
		}
		if m.Editing() {
			t.Fatalf("%q opened the editor for an unsupported field", c.key)
		}
	}
	// The supported field still opens.
	if cmd := m.Update(key("l")); cmd != nil {
		t.Fatalf("l returned a command: %+v", cmd())
	}
	if !m.Editing() {
		t.Fatal("a supported field must still open its editor")
	}
}

// TestFullCapsIsTheDefault keeps offline editing unrestricted: without a
// session the store is edited freely and the session strips later (#2245).
func TestFullCapsIsTheDefault(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 10)
	m.SetStore(store(t))
	m.Update(key("down"))
	if cmd := m.Update(key("c")); cmd != nil {
		t.Fatalf("a fresh panel gated the condition field: %+v", cmd())
	}
	if !m.Editing() {
		t.Fatal("condition editor must open without a session")
	}
}

// TestInvalidHitCountKeepsEditorOpen rejects a bad value with the reason on
// the row instead of committing it (#2245).
func TestInvalidHitCountKeepsEditorOpen(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 10)
	m.SetStore(store(t))
	m.Update(key("down"))
	m.Update(key("n"))
	typeText(&m, "lots")
	if cmd := m.Update(key("enter")); cmd != nil {
		t.Fatalf("a rejected value committed: %+v", cmd())
	}
	if !m.Editing() {
		t.Fatal("a rejected value must keep the editor open")
	}
	if m.editErr == "" {
		t.Fatal("no reason recorded for the rejection")
	}
	if !strings.Contains(m.View(), "✗") {
		t.Fatalf("the complaint is not rendered:\n%s", m.View())
	}
	// The next keystroke retires the complaint, and a valid value commits.
	typeText(&m, "2")
	if m.editErr != "" {
		t.Fatal("the complaint must clear on the next keystroke")
	}
	m.edit.Text, m.edit.Cur = "%2", 2
	msg := run(t, m.Update(key("enter")))
	got, ok := msg.(SetMetaMsg)
	if !ok || got.Meta.HitCondition != "%2" {
		t.Fatalf("commit = %+v", msg)
	}
}

// TestGlyphsPerKind renders a distinct marker per breakpoint kind (#2245).
func TestGlyphsPerKind(t *testing.T) {
	b := debug.NewBreakpoints()
	b.Toggle("a.py", 1)
	b.Toggle("a.py", 2)
	b.Toggle("a.py", 3)
	b.Toggle("a.py", 4)
	b.SetMeta("a.py", 2, debug.Meta{Condition: "i > 3"})
	b.SetMeta("a.py", 3, debug.Meta{LogMessage: "i={i}"})
	b.SetEnabled("a.py", 4, false)
	m := New(nil)
	m.SetSize(120, 10)
	m.SetStore(b)
	view := m.View()
	for _, glyph := range []string{"●", "◉", "◆", "○"} {
		if !strings.Contains(view, glyph) {
			t.Fatalf("glyph %q missing from the list:\n%s", glyph, view)
		}
	}
}

// TestPropertiesActionEmitsEditMeta wires `p` to the root model's form
// (#2245).
func TestPropertiesActionEmitsEditMeta(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 10)
	m.SetStore(store(t))
	m.Update(key("down")) // onto a.py:2
	msg := run(t, m.Update(key("p")))
	got, ok := msg.(EditMetaMsg)
	if !ok || got.Path != "a.py" || got.Line != 2 {
		t.Fatalf("p gave %+v, want EditMetaMsg for a.py:2", msg)
	}
}

// TestPropertiesActionIgnoredOnHeader keeps `p` a row action.
func TestPropertiesActionIgnoredOnHeader(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 10)
	m.SetStore(store(t))
	if cmd := m.Update(key("p")); cmd != nil {
		t.Fatalf("p on a file header emitted %+v", cmd())
	}
}
