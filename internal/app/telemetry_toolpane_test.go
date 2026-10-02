package app

// telemetry_toolpane_test.go covers #2889: the deferred "unbound" verdict of
// #2303 extended from editors to the focused tool panes. A chord the pane's
// own one-line input answers — alt+backspace in the HTTP response search, the
// issues filter, the explorer speed search — must not be logged as a missing
// keybind; the same chord with the input closed, and a chord nothing handles
// even with it open, still are.

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
)

// altBackspace() (searchkill_test.go) is the word-kill chord the 2026-10-02
// review found logged 23 times in the http context while the search prompt
// took it.

// unhandledChord is bound nowhere and answered by no pane (TestTelemetry-
// UnboundChordRecorded relies on the same chord).
var unhandledChord = tea.KeyPressMsg{Code: '0', Mod: tea.ModCtrl | tea.ModAlt}

// toolTelemetryModel is the full app — every command registered, so the
// keymap resolves exactly as it does for a user — on an isolated config dir,
// with the first-start LSP dialog out of the way so scripted keys reach the
// panes.
func toolTelemetryModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	tm, _ := New().Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m := tm.(Model)
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	return m
}

// httpTelemetryModel opens and focuses the HTTP response viewer.
func httpTelemetryModel(t *testing.T) Model {
	t.Helper()
	m := toolTelemetryModel(t)
	m.openHTTPPanel()
	if m.activeWS().Panes.Get(pane.HTTPKey) == nil {
		t.Skip("the HTTP pane did not open in this environment")
	}
	m.setFocus(pane.HTTPKey)
	return m
}

func TestTelemetryHTTPSearchOwnsAltBackspace(t *testing.T) {
	m := httpTelemetryModel(t)
	hp := func(m Model) interface{ SearchQuery() (string, bool) } {
		return m.activeWS().Panes.Get(pane.HTTPKey).HTTP()
	}
	m.activeWS().Panes.Get(pane.HTTPKey).HTTP().BeginSearch()
	m = typeInto(m, "one two")
	m = drainKey(m, altBackspace())
	if q, open := hp(m).SearchQuery(); q != "one " || !open {
		t.Fatalf("alt+backspace must kill a word in the open prompt, got %q (open %v)", q, open)
	}
	if u := unboundChords(t, m); len(u) != 0 {
		t.Fatalf("a chord the search prompt took must not be recorded unbound, got %v", u)
	}
}

func TestTelemetryHTTPClosedSearchStillRecordsAltBackspace(t *testing.T) {
	m := httpTelemetryModel(t)
	m = drainKey(m, altBackspace())
	if u := unboundChords(t, m); !slices.Contains(u, "alt+backspace") {
		t.Fatalf("with the prompt closed nothing handles alt+backspace: want it unbound, got %v", u)
	}
}

// TestTelemetryHTTPSearchRecordsUnhandledChord is the regression guard: the
// open prompt only answers for the keys it edits with, so a chord it has no
// use for is still the missing-keybind signal.
func TestTelemetryHTTPSearchRecordsUnhandledChord(t *testing.T) {
	m := httpTelemetryModel(t)
	m.activeWS().Panes.Get(pane.HTTPKey).HTTP().BeginSearch()
	m = typeInto(m, "one")
	m = drainKey(m, unhandledChord)
	if u := unboundChords(t, m); len(u) != 1 || u[0] != "ctrl+alt+0" {
		t.Fatalf("an unhandled chord in the open prompt must stay unbound, got %v", u)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyF13})
	if u := unboundChords(t, m); !slices.Contains(u, "f13") {
		t.Fatalf("an unhandled function key must stay unbound, got %v", u)
	}
}

// issuesTelemetryModel opens and focuses the issues tool window.
func issuesTelemetryModel(t *testing.T) Model {
	t.Helper()
	m := toolTelemetryModel(t)
	tm, cmd := m.Update(IssuesToggleMsg{})
	m = drainCmd(tm.(Model), cmd)
	if m.activeWS().Panes.Get(pane.IssuesKey) == nil {
		t.Skip("the issues pane did not open in this environment")
	}
	m.setFocus(pane.IssuesKey)
	return m
}

func TestTelemetryIssuesFilterOwnsAltBackspace(t *testing.T) {
	m := issuesTelemetryModel(t)
	m = drainKey(m, findChord())
	gi := func(m Model) string { return m.activeWS().Panes.Get(pane.IssuesKey).Issues().Filter() }
	if !m.activeWS().Panes.Get(pane.IssuesKey).Issues().Filtering() {
		t.Fatal("setup: the find chord must open the match row")
	}
	m = typeInto(m, "one two")
	m = drainKey(m, altBackspace())
	if got := gi(m); got != "one " {
		t.Fatalf("alt+backspace must kill a word in the filter, got %q", got)
	}
	if u := unboundChords(t, m); len(u) != 0 {
		t.Fatalf("a chord the filter took must not be recorded unbound, got %v", u)
	}
}

func TestTelemetryIssuesListStillRecordsAltBackspace(t *testing.T) {
	m := issuesTelemetryModel(t)
	if m.activeWS().Panes.Get(pane.IssuesKey).Issues().Filtering() {
		t.Fatal("setup: the filter starts closed")
	}
	m = drainKey(m, altBackspace())
	if u := unboundChords(t, m); !slices.Contains(u, "alt+backspace") {
		t.Fatalf("the issue list has no use for alt+backspace: want it unbound, got %v", u)
	}
}

func TestTelemetryExplorerSpeedSearchOwnsAltBackspace(t *testing.T) {
	m := toolTelemetryModel(t)
	m.setFocus(pane.ExplorerKey)
	m = drainKey(m, findChord())
	if !m.explorer().Searching() {
		t.Fatal("setup: the find chord must open the speed search")
	}
	m = typeInto(m, "abc")
	m = drainKey(m, altBackspace())
	if !m.explorer().Searching() {
		t.Fatal("alt+backspace must leave the speed search open")
	}
	if u := unboundChords(t, m); len(u) != 0 {
		t.Fatalf("a chord the speed search took must not be recorded unbound, got %v", u)
	}
}

func TestTelemetryExplorerTreeStillRecordsAltBackspace(t *testing.T) {
	m := toolTelemetryModel(t)
	m.setFocus(pane.ExplorerKey)
	if m.explorer().Searching() {
		t.Fatal("setup: the speed search starts closed")
	}
	m = drainKey(m, altBackspace())
	if u := unboundChords(t, m); !slices.Contains(u, "alt+backspace") {
		t.Fatalf("the tree has no use for alt+backspace: want it unbound, got %v", u)
	}
}

// TestTelemetryToolPaneConsumedChordNotUnbound: a chord a tool pane binds
// itself rather than through the keymap table (the explorer's ctrl+d half
// page) is no missing keybind either.
func TestTelemetryToolPaneConsumedChordNotUnbound(t *testing.T) {
	m := toolTelemetryModel(t)
	m.setFocus(pane.ExplorerKey)
	m = drainKey(m, tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if u := unboundChords(t, m); slices.Contains(u, "ctrl+d") {
		t.Fatalf("a chord the explorer answers must not be recorded unbound, got %v", u)
	}
}
