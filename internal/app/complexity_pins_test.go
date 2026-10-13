package app

// complexity_pins_test.go covers the app half of the pins (#2959):
// complexity.pin toggles the selected function into the state store's
// complexity-pins.json and back, the window shows it, and the session base
// is recorded once per project root and handed to the window.

import (
	"strings"
	"testing"

	"ike/internal/complexity"
)

func TestComplexityPinToggleSavesAndApplies(t *testing.T) {
	m, proj := baselineApp(t)
	path := complexity.PinsPath(proj)
	m = baselineGen(t, m)
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	p := m.complexityPanel()
	if r, ok := p.Selected(); !ok || r.Fn.Name != "A" {
		t.Fatalf("selected row = %+v", r)
	}

	out, cmd := m.Update(ComplexityPinMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("complexity.pin must yield the toggle message")
	}
	out, _ = m.Update(cmd())
	m = out.(Model)
	pins, err := complexity.LoadPins(path)
	if err != nil || pins.Len() != 1 {
		t.Fatalf("saved pins = %+v, %v", pins, err)
	}
	if e := pins.Pinned[0]; e.File != "src/a.go" || e.Function != "A" || e.Ordinal != 0 || e.CCN != 2 || e.PinnedAt.IsZero() {
		t.Fatalf("pin = %+v", e)
	}
	if !strings.Contains(cxNotices(m), "pinned A") {
		t.Fatalf("the pin must be reported: %q", cxNotices(m))
	}
	if r, _ := p.Selected(); r.Pin == nil || !r.PinRow {
		t.Fatalf("the panel row must be pinned now: %+v", r)
	}

	// The second toggle unpins and removes the emptied file.
	out, cmd = m.Update(ComplexityPinMsg{})
	m = out.(Model)
	out, _ = m.Update(cmd())
	m = out.(Model)
	if pins, _ := complexity.LoadPins(path); pins.Len() != 0 {
		t.Fatalf("the second toggle must unpin: %+v", pins)
	}
	if !strings.Contains(cxNotices(m), "unpinned A") {
		t.Fatalf("the unpin must be reported: %q", cxNotices(m))
	}
}

func TestComplexitySessionBaseRecordedOncePerRoot(t *testing.T) {
	m, _ := baselineApp(t)
	m = baselineGen(t, m)
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	p := m.complexityPanel()
	if d, ok := p.SessionDelta(); !ok || d != 0 {
		t.Fatalf("session delta = %d %v, want a base and 0", d, ok)
	}
	first, ok := m.complexitySessionBase()
	if !ok {
		t.Fatal("the settled index must have recorded a session base")
	}
	m = baselineGen(t, m)
	again, _ := m.complexitySessionBase()
	if again.Gen != first.Gen {
		t.Fatalf("the session base must stay the first snapshot: gen %d → %d", first.Gen, again.Gen)
	}
}
