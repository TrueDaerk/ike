package httppane

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestRerunKeyEmitsRerunMsg: "R" asks the host to re-run the shown entry's
// request from its .http file (#2247) — the pane reaches neither the file nor
// the environment, so it only names the moment.
func TestRerunKeyEmitsRerunMsg(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("create", withSnapshot())

	cmd := m.handleKey(tea.KeyPressMsg{Code: 'R', Text: "R", Mod: tea.ModShift})
	if cmd == nil {
		t.Fatal("R must emit a command")
	}
	if _, ok := cmd().(RerunMsg); !ok {
		t.Fatalf("message type: %T", cmd())
	}
}

// TestRerunFooterHintNeedsSource: re-running re-reads the request file, so the
// hint appears only once the pane knows which one it is (#2247).
func TestRerunFooterHintNeedsSource(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 20)
	m.Set("create", withSnapshot())
	if strings.Contains(m.View(), "R re-run") {
		t.Errorf("no source file must not advertise the re-run:\n%s", m.View())
	}
	m.SetSource("/p/req.http")
	if !strings.Contains(m.View(), "R re-run") {
		t.Errorf("footer must advertise the re-run:\n%s", m.View())
	}
}
