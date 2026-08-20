package app

// xdebug_doctor_test.go guards the Xdebug Doctor wiring (#1991): the
// debug.doctor toggle state machine, the doctor log fed from the bridge's
// ike.listenState / ike.debugConn events (with or without a live session),
// the clear action, and the listener-stopped fallback when the listen
// session ends.

import (
	"encoding/json"
	"net"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/config"
	"ike/internal/dap"
	"ike/internal/debugdoctor"
	"ike/internal/lang"
	"ike/internal/pane"
)

// doctorView renders the open doctor pane's content.
func doctorView(t *testing.T, m Model) string {
	t.Helper()
	p := m.doctorPanel()
	if p == nil {
		t.Fatal("doctor panel must be open")
	}
	return ansi.Strip(p.View())
}

// TestDoctorToggle guards the debug.doctor state machine: open at the
// adaptive placement, focus when unfocused, return focus when focused.
func TestDoctorToggle(t *testing.T) {
	m := sized(t, 100, 40)
	out, _ := m.Update(DebugDoctorMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.DoctorKey) {
		t.Fatal("debug.doctor must open the panel")
	}
	if m.activeWS().Panes.Focused() != pane.DoctorKey {
		t.Fatal("a fresh open must focus the panel")
	}
	out, _ = m.Update(DebugDoctorMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() == pane.DoctorKey {
		t.Fatal("a second toggle must return focus")
	}
	if !m.activeWS().Panes.Has(pane.DoctorKey) {
		t.Fatal("the panel stays open on focus return")
	}
}

// TestDoctorTracksEventsWithoutSession guards the "works while no session is
// active" half: doctor events land in the log even when no debug session
// owns them (e.g. the trailing stopped event after a disconnect).
func TestDoctorTracksEventsWithoutSession(t *testing.T) {
	m := sized(t, 120, 40)
	out, _ := m.Update(DebugDoctorMsg{})
	m = out.(Model)
	if m.dbg != nil {
		t.Fatal("fixture: no session may be running")
	}

	body, _ := json.Marshal(map[string]any{"state": "listening", "port": 9003, "hostname": "onpage.local", "mappings": 1})
	out, _ = m.Update(debugEventMsg{ev: dap.Event{Name: "ike.listenState", Body: body}})
	m = out.(Model)
	v := doctorView(t, m)
	if !strings.Contains(v, "listening on port 9003") || !strings.Contains(v, "host filter onpage.local") {
		t.Fatalf("listener state must render: %q", v)
	}

	body, _ = json.Marshal(map[string]any{
		"outcome": "rejected", "reason": "filter", "detail": "request from \"other.local\" does not match",
		"remote": "127.0.0.1:5555", "ideKey": "web", "fileURI": "file:///srv/a.php", "host": "other.local",
	})
	out, _ = m.Update(debugEventMsg{ev: dap.Event{Name: "ike.debugConn", Body: body}})
	m = out.(Model)
	if v := doctorView(t, m); !strings.Contains(v, "hostname filter mismatch") ||
		!strings.Contains(v, "127.0.0.1:5555") {
		t.Fatalf("rejected attempt must render: %q", v)
	}

	body, _ = json.Marshal(map[string]any{
		"outcome": "accepted", "remote": "127.0.0.1:6666", "ideKey": "ike",
		"fileURI": "file:///srv/b.php", "local": "/proj/b.php", "mapped": true,
	})
	out, _ = m.Update(debugEventMsg{ev: dap.Event{Name: "ike.debugConn", Body: body}})
	m = out.(Model)
	if v := doctorView(t, m); !strings.Contains(v, "accepted → /proj/b.php") {
		t.Fatalf("accepted attempt must render: %q", v)
	}

	// The trace survives closing and reopening the panel (app-owned log).
	out, _ = m.Update(DebugDoctorMsg{}) // focused → return focus
	m = out.(Model)
	m.activeWS().Panes.Close(pane.DoctorKey)
	out, _ = m.Update(DebugDoctorMsg{})
	m = out.(Model)
	if v := doctorView(t, m); !strings.Contains(v, "accepted → /proj/b.php") {
		t.Fatalf("the trace must survive a panel close: %q", v)
	}
}

// TestDoctorClear guards the clear action: 'c' in the focused pane empties
// the trace but keeps the listener status.
func TestDoctorClear(t *testing.T) {
	m := sized(t, 120, 40)
	out, _ := m.Update(DebugDoctorMsg{})
	m = out.(Model)
	body, _ := json.Marshal(map[string]any{"state": "listening", "port": 9003})
	out, _ = m.Update(debugEventMsg{ev: dap.Event{Name: "ike.listenState", Body: body}})
	m = out.(Model)
	body, _ = json.Marshal(map[string]any{"outcome": "rejected", "reason": "busy", "remote": "r"})
	out, _ = m.Update(debugEventMsg{ev: dap.Event{Name: "ike.debugConn", Body: body}})
	m = out.(Model)

	out, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m = out.(Model)
	m = drainCmd(m, cmd)
	if len(m.doctorLog.Entries()) != 0 {
		t.Fatal("c must clear the trace")
	}
	if !m.doctorLog.Listener().Running {
		t.Fatal("clearing the trace must keep the listener status")
	}
}

// TestDoctorListenToggleMarksStopped guards the stopped fallback: starting
// the listen session and toggling it off marks the doctor's listener state
// stopped even when the bridge's own stopped event is lost in the teardown.
func TestDoctorListenToggleMarksStopped(t *testing.T) {
	// The registry is global and registerEnvTestPython (terminal_test.go)
	// strips toolchains; re-register the debug-capable php stub, as the
	// convention there prescribes.
	lang.Register(lang.Language{ID: "php", Toolchain: phpListenStub{}})
	// A free port keeps the bridge's bind off the real Xdebug default.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c := &config.Config{}
	c.Debug.PHP.Port = port
	config.Set(c)

	t.Chdir(t.TempDir())
	m := sized(t, 100, 40)
	out, _ := m.Update(DebugListenMsg{})
	m = out.(Model)
	if m.dbg == nil {
		var notes []string
		for _, h := range m.history {
			notes = append(notes, h.text)
		}
		t.Fatalf("fixture: listen session must be running; notifications: %q", notes)
	}
	m.doctorLog.SetListener(debugdoctor.Listener{Running: true, Port: 9003})

	out, _ = m.Update(DebugListenMsg{})
	m = out.(Model)
	if m.dbg != nil {
		t.Fatal("second toggle must stop the session")
	}
	if m.doctorLog.Listener().Running {
		t.Fatal("stopping the listen session must mark the listener stopped")
	}
}
