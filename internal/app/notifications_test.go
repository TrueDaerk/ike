package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
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

// TestServerStatusRouting guards the 0130 call-site migration: persistent
// server state lands on the status line, transient events become toasts of the
// matching severity.
func TestServerStatusRouting(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(ilsp.ServerStatusMsg{Text: "go language server ready", Kind: ilsp.ServerState})
	m = tm.(Model)
	if got := m.host.Status(); got != "go language server ready" {
		t.Fatalf("persistent state should hit the status line, got %q", got)
	}
	if len(m.toasts) != 0 {
		t.Fatalf("persistent state must not toast, have %d toasts", len(m.toasts))
	}

	events := []struct {
		kind ilsp.ServerStatusKind
		sev  host.Severity
	}{
		{ilsp.ServerEventInfo, host.Info},
		{ilsp.ServerEventWarn, host.Warn},
		{ilsp.ServerEventError, host.Error},
	}
	for _, ev := range events {
		tm, _ = m.Update(ilsp.ServerStatusMsg{Text: "event", Kind: ev.kind})
		m = tm.(Model)
		if len(m.toasts) == 0 || m.toasts[0].sev != ev.sev {
			t.Fatalf("kind %d should raise a severity-%d toast", ev.kind, ev.sev)
		}
	}
	if got := m.host.Status(); got != "go language server ready" {
		t.Fatalf("events must not overwrite the persistent segment, got %q", got)
	}
}

// TestStatusLineKeepsSegmentsWithHostStatus guards the 0130 defect fix: a
// persistent host status renders as one more segment, never replacing the
// mode/cursor segments.
func TestStatusLineKeepsSegmentsWithHostStatus(t *testing.T) {
	m := newSized()
	m.host.SetStatus("go language server ready")
	line := m.statusLine()
	if !strings.Contains(line, "NORMAL") || !strings.Contains(line, "Ln 1") {
		t.Fatalf("host status must not cover the mode/cursor segments: %q", line)
	}
	if !strings.Contains(line, "go language server ready") {
		t.Fatalf("persistent host status missing from the status line: %q", line)
	}
}

// TestSaveAllNotifiesSavedCount guards the save-all migration: SaveAllMsg
// reports 'saved N files' as an info toast, and stays silent with nothing to
// save.
func TestSaveAllNotifiesSavedCount(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(SaveAllMsg{})
	if m = tm.(Model); len(m.toasts) != 0 {
		t.Fatal("save-all with no dirty editors must not toast")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ = m.openPath(path, false)
	m = tm.(Model)
	for _, k := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"},
		{Code: 'X', Text: "X"},
		{Code: tea.KeyEscape},
	} {
		m = drainKey(m, k)
	}
	tm, _ = m.Update(SaveAllMsg{})
	m = tm.(Model)
	if len(m.toasts) != 1 || m.toasts[0].text != "saved 1 file" || m.toasts[0].sev != host.Info {
		t.Fatalf("expected a 'saved 1 file' info toast, toasts=%+v", m.toasts)
	}
}

// TestThemeSelectNotifies guards the theme migration: selecting a theme
// confirms via toast (warn on unknown names) and leaves the persistent status
// segment untouched.
func TestThemeSelectNotifies(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(SelectThemeMsg{Name: "no-such-theme"})
	m = tm.(Model)
	if len(m.toasts) != 1 || m.toasts[0].sev != host.Warn {
		t.Fatalf("unknown theme should raise a warn toast, toasts=%+v", m.toasts)
	}
	if m.host.Status() != "" {
		t.Fatalf("theme selection must not write the status line, got %q", m.host.Status())
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
