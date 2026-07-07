package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
)

// notify raises a notification and runs one Update pass so the root model
// drains it, returning the model and the drain's cmd (the expiry ticks).
func notify(m Model, sev host.Severity, text string) (Model, tea.Cmd) {
	m.host.Notify(sev, text)
	tm, cmd := m.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	return tm.(Model), cmd
}

func TestToastExpiry(t *testing.T) {
	m := newSized()
	m, cmd := notify(m, host.Info, "saved")
	if len(m.toasts) != 1 {
		t.Fatalf("toasts = %d want 1", len(m.toasts))
	}
	if cmd == nil {
		t.Fatal("info toast must schedule an expiry tick")
	}
	tm, _ := m.Update(toastExpireMsg{id: m.toasts[0].id})
	if m = tm.(Model); len(m.toasts) != 0 {
		t.Fatalf("toast should be gone after expiry, have %d", len(m.toasts))
	}
}

func TestErrorToastPersistsUntilEsc(t *testing.T) {
	m := newSized()
	m, _ = notify(m, host.Error, "server crashed")
	// A foreign expiry must not remove it, and no tick targets it anyway.
	tm, _ := m.Update(toastExpireMsg{id: -1})
	m = tm.(Model)
	if len(m.toasts) != 1 {
		t.Fatal("error toast must persist")
	}
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m = tm.(Model); len(m.toasts) != 0 {
		t.Fatal("Esc should dismiss error toasts")
	}
}

func TestToastStackingNewestOnTopCappedAtThree(t *testing.T) {
	m := newSized()
	for _, s := range []string{"one", "two", "three", "four"} {
		m, _ = notify(m, host.Info, s)
	}
	if len(m.toasts) != 4 {
		t.Fatalf("all toasts kept in the store, got %d", len(m.toasts))
	}
	if m.toasts[0].text != "four" {
		t.Fatalf("newest first, got %q", m.toasts[0].text)
	}
	frame := m.render()
	if !strings.Contains(frame, "four") || !strings.Contains(frame, "two") {
		t.Fatal("visible toasts missing from the frame")
	}
	if strings.Contains(frame, "● one ") {
		t.Fatal("fourth-oldest toast must not render (cap 3)")
	}
}

func TestToastNeverCoversStatusLine(t *testing.T) {
	m := newSized()
	m, _ = notify(m, host.Info, "hello toast")
	lines := strings.Split(m.render(), "\n")
	last := lines[len(lines)-1]
	if strings.Contains(last, "hello toast") {
		t.Fatal("toast rendered on the status line row")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "hello toast") {
		t.Fatal("toast missing from frame")
	}
}
