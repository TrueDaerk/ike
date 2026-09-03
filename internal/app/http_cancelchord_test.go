package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/httpclient"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/telemetry"
)

// http_cancelchord_test.go covers #2404: the keymap chord for http.cancel and
// the in-flight hint that advertises it.

// cancelChordApp is httpFileApp with the app command set actually registered:
// the chord route runs command ids through the registry, so an empty one would
// make the binding resolve to nothing regardless of the table.
func cancelChordApp(t *testing.T, url string) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	reg := registry.New()
	reg.Add(appCommands{})
	m := NewWith(reg, host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	// The first-start LSP dialog owns the keyboard while it is up (#301) and
	// would swallow the chord under test; esc is what a user presses too.
	for m.onboardingOpen() {
		tm, _ := m.updateOnboarding(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = tm.(Model)
	}
	path := filepath.Join(t.TempDir(), "req.http")
	if err := os.WriteFile(path, []byte("### one\nGET "+url+"/slow\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ = m.Update(explorer.OpenFileMsg{Path: path})
	return out.(Model)
}

// pressCancelChord sends ctrl+. and runs whatever command the keymap layer
// resolved it to, so the test exercises the whole route — chord to binding to
// registered command to HTTPCancelMsg — rather than only the lookup.
func pressCancelChord(t *testing.T, m Model) Model {
	t.Helper()
	out, cmd := m.Update(tea.KeyPressMsg{Code: '.', Text: ".", Mod: tea.ModCtrl})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("ctrl+. resolved to no command")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if sub := c(); sub != nil {
				if _, isCancel := sub.(HTTPCancelMsg); isCancel {
					msg = sub
					break
				}
			}
		}
	}
	if _, ok := msg.(HTTPCancelMsg); !ok {
		t.Fatalf("ctrl+. produced %T, want HTTPCancelMsg", msg)
	}
	out, _ = m.Update(msg)
	return out.(Model)
}

// TestHTTPCancelChordAbortsFromEditor: ctrl+. with the .http editor focused
// aborts the running dispatch. Before #2404 the only cancel gesture lived in
// the response pane, which is not where a slow request is usually watched.
func TestHTTPCancelChordAbortsFromEditor(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	srv := slowServer(t, release)
	m := cancelChordApp(t, srv.URL)

	out, _ := m.Update(HTTPRunMsg{})
	m = out.(Model)
	if len(m.httpFlight) != 1 {
		t.Fatalf("dispatch must be tracked, got %d", len(m.httpFlight))
	}

	m = pressCancelChord(t, m)
	for key, e := range m.httpFlight {
		if !e.canceled {
			t.Fatalf("the chord must mark %s canceled", key)
		}
	}
	if len(m.httpFlight) == 0 {
		t.Fatal("the flight entry should still be tracked until the response lands")
	}
}

// TestHTTPCancelChordFromResponsePane: the same chord works with the viewer
// focused, where "x" already did — the two gestures are alternatives, not a
// split of the action across panes.
func TestHTTPCancelChordFromResponsePane(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	srv := slowServer(t, release)
	m := cancelChordApp(t, srv.URL)

	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)

	out, _ = m.Update(HTTPRunMsg{})
	m = out.(Model)
	if len(m.httpFlight) != 1 {
		t.Fatalf("dispatch must be tracked, got %d", len(m.httpFlight))
	}
	m = pressCancelChord(t, m)
	for key, e := range m.httpFlight {
		if !e.canceled {
			t.Fatalf("the chord must mark %s canceled in the response pane too", key)
		}
	}
}

// TestHTTPCancelChordIsBound guards the default table entry itself: the
// command must resolve to a chord, and to a delivered one — the pane hint
// names it, so a chord this terminal swallows would be advice that fails.
func TestHTTPCancelChordIsBound(t *testing.T) {
	m := httpApp(t)
	chord := m.httpCancelChord()
	if chord == "" {
		t.Fatal("http.cancel carries no default keybinding")
	}
	if chord != "ctrl+." {
		t.Fatalf("cancel chord = %q, want the delivered ctrl+. over the fragile cmd+.", chord)
	}
}

// TestHTTPPaneLearnsCancelChord: the pane is told the chord whenever the
// flight state changes, so its hint names the user's own binding.
func TestHTTPPaneLearnsCancelChord(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	srv := slowServer(t, release)
	m := httpFileApp(t, srv.URL)

	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	out, _ = m.Update(HTTPRunMsg{})
	m = out.(Model)

	p := m.httpPanel()
	if p == nil {
		t.Fatal("no response pane")
	}
	// Age the flight past the hint threshold and read the composed view.
	for _, e := range m.httpFlight {
		p.SetPending(e.request, time.Now().Add(-2*time.Second))
	}
	p.SetSize(100, 20)
	if view := p.View(); !strings.Contains(view, "x / ctrl+. cancels") {
		t.Fatalf("the pane hint does not name the bound chord:\n%s", view)
	}
}

// TestTelemetryHTTPFlightCarriesTiming: the ok event carries the phase
// breakdown (#2404), so an export can say *where* a slow flight spent its
// time rather than only that it was slow — and still nothing about the
// request itself.
func TestTelemetryHTTPFlightCarriesTiming(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	send := func(ctx context.Context, source, key string, cb httpclient.StreamCallbacks) (*httpclient.Response, error) {
		return nil, nil // never executed: the returned tea.Cmd is not run
	}
	if cmd := m.dispatchHTTP("a.http", "GET /x", "GET /x", send); cmd == nil {
		t.Fatal("dispatch refused")
	}
	tm, _ := m.Update(HTTPResponseMsg{Source: "a.http", Request: "GET /x",
		Resp: &httpclient.Response{Status: "200 OK", StatusCode: 200, Timing: &httpclient.Timing{
			DNS: 2 * time.Millisecond, Connect: 11 * time.Millisecond,
			TLS: 34 * time.Millisecond, TTFB: 210 * time.Millisecond,
			Transfer: 4 * time.Millisecond,
		}}})
	m = tm.(Model)

	// Only this flight's ops: the launch also records its session.restore
	// span (#2403).
	ops := opsOf(usageEvents(t, m), telemetry.OpHTTPFlight)
	if len(ops) != 2 {
		t.Fatalf("want start + end ops, got %v", ops)
	}
	end := ops[1]
	for field, want := range map[string]string{
		"dns_ms": "2", "connect_ms": "11", "tls_ms": "34",
		"ttfb_ms": "210", "transfer_ms": "4", "reused": "false",
	} {
		if end.Data[field] != want {
			t.Errorf("%s = %q, want %q (event: %v)", field, end.Data[field], want, end)
		}
	}
}

// TestTelemetryHTTPFlightWithoutTiming: a response that measured nothing adds
// no breakdown fields at all, rather than six zeros that would read as a
// suspiciously instant exchange.
func TestTelemetryHTTPFlightWithoutTiming(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	send := func(ctx context.Context, source, key string, cb httpclient.StreamCallbacks) (*httpclient.Response, error) {
		return nil, nil
	}
	m.dispatchHTTP("a.http", "GET /x", "GET /x", send)
	tm, _ := m.Update(HTTPResponseMsg{Source: "a.http", Request: "GET /x",
		Resp: &httpclient.Response{Status: "200 OK", StatusCode: 200}})
	m = tm.(Model)

	ops := eventsOf(usageEvents(t, m), telemetry.TypeOp)
	end := ops[len(ops)-1]
	if _, ok := end.Data["ttfb_ms"]; ok {
		t.Fatalf("unmeasured flight reported a breakdown: %v", end)
	}
}
