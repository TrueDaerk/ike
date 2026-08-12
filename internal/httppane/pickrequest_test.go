package httppane

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestPickRequestKeyEmitsMsg: "r" asks the host for the request picker
// (#1829) — the pane knows neither the .http file's other requests nor the
// history store.
func TestPickRequestKeyEmitsMsg(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("create", sample())

	cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("r must emit a command")
	}
	if _, ok := cmd().(PickRequestMsg); !ok {
		t.Fatalf("message type: %T", cmd())
	}
}

// TestFooterAdvertisesRequestSwitch: the hint appears once the pane knows
// which .http file the response came from, and only then (#1829).
func TestFooterAdvertisesRequestSwitch(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 20)
	m.Set("create", sample())
	if view := m.View(); strings.Contains(view, "r request") {
		t.Errorf("without a source file there is nothing to pick from:\n%s", view)
	}
	m.SetSource("/p/req.http")
	if got := m.Source(); got != "/p/req.http" {
		t.Errorf("source: %q", got)
	}
	if view := m.View(); !strings.Contains(view, "r request") {
		t.Errorf("footer must advertise the request switch:\n%s", view)
	}
}

// TestEmptyPaneNamesStoredResponses: an untouched pane points at
// http.showResponse instead of looking like a dispatch-only view (#1829).
func TestEmptyPaneNamesStoredResponses(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 20)
	if view := m.View(); !strings.Contains(view, "http.showResponse") {
		t.Errorf("the empty state must name the stored-response route:\n%s", view)
	}
}
