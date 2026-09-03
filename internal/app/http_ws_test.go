package app

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/httpclient"
	"ike/internal/httppane"
	"ike/internal/telemetry"
)

// wsFlightModel arms one websocket flight the way dispatchHTTP does, without
// running the returned command (no network involved).
func wsFlightModel(t *testing.T, m Model) Model {
	t.Helper()
	send := func(ctx context.Context, source, key string, cb httpclient.WSCallbacks) (*httpclient.Response, error) {
		return nil, nil // never executed: the returned tea.Cmd is not run
	}
	if cmd := m.dispatchHTTP("a.http", "ws", "WEBSOCKET /socket", true, send); cmd == nil {
		t.Fatal("dispatch refused")
	}
	return m
}

// TestHTTPWSStreamUnlocksInput: the stream start of a websocket flight marks
// the pane ws-live (#2422), a plain flight's does not.
func TestHTTPWSStreamUnlocksInput(t *testing.T) {
	m := wsFlightModel(t, httpApp(t))
	events := make(chan tea.Msg, 1)
	tm, _ := m.Update(HTTPStreamStartMsg{Source: "a.http", Request: "ws",
		Status: "101 Switching Protocols", Proto: "HTTP/1.1", events: events})
	m = tm.(Model)
	p := m.httpPanel()
	if p == nil || !p.WSLive() {
		t.Fatal("pane not ws-live after the websocket stream start")
	}
	if p.Source() != "a.http" {
		t.Fatalf("source not set at stream start: %q", p.Source())
	}
}

// TestHTTPWSSessionStoredAndSendRouted: the session handle lands on the
// flight entry and the pane's WSSendMsg resolves it (#2422).
func TestHTTPWSSessionStoredAndSendRouted(t *testing.T) {
	m := wsFlightModel(t, httpApp(t))
	events := make(chan tea.Msg, 1)
	tm, _ := m.Update(HTTPStreamStartMsg{Source: "a.http", Request: "ws",
		Status: "101 Switching Protocols", Proto: "HTTP/1.1", events: events})
	m = tm.(Model)

	session := &httpclient.WSSession{}
	tm, cmd := m.Update(HTTPWSSessionMsg{Source: "a.http", Request: "ws",
		Session: session, events: events})
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("session message did not re-arm the event pump")
	}
	if e := m.httpFlight[httpFlightKey("a.http", "ws")]; e == nil || e.wsSession != session {
		t.Fatal("session handle not stored on the flight entry")
	}
	if got := m.wsSessionOf(); got != session {
		t.Fatalf("wsSessionOf: %v", got)
	}

	// A closed session (or none) refuses the send with a notice instead of a
	// command.
	tm, cmd = m.Update(httppane.WSSendMsg{Text: "hello"})
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("send with an open session produced no command")
	}
}

// TestHTTPWSSendWithoutSession: WSSendMsg without an open session notifies
// instead of panicking (#2422).
func TestHTTPWSSendWithoutSession(t *testing.T) {
	m := httpApp(t)
	if cmd := m.sendWSMessage("hello"); cmd != nil {
		t.Fatal("send without a session produced a command")
	}
}

// TestTelemetryHTTPFlightWSKindAndFrames: a websocket flight's end op carries
// kind=ws and the frame count (#2422) — structural only.
func TestTelemetryHTTPFlightWSKindAndFrames(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	m = wsFlightModel(t, m)
	tm, _ := m.Update(HTTPResponseMsg{Source: "a.http", Request: "ws",
		Resp: &httpclient.Response{Status: "101 Switching Protocols", StatusCode: 101, Frames: 7}})
	m = tm.(Model)
	ops := opsOf(usageEvents(t, m), telemetry.OpHTTPFlight)
	if len(ops) != 2 {
		t.Fatalf("want start + end ops, got %v", ops)
	}
	end := ops[1]
	if end.Data["kind"] != "ws" || end.Data["frames"] != "7" {
		t.Fatalf("end op wrong: %v", end.Data)
	}
}
