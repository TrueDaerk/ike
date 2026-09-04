package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// tests_page_test.go guards the Tests page (#1911): the two Test Results
// toggles must be present as booleans, default to on, and edit end-to-end
// like any schema entry.

// testsEntry returns the shipped schema entry for key.
func testsEntry(t *testing.T, key string) Entry {
	t.Helper()
	for _, p := range BasePages([]string{"default"}, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == key {
				return e
			}
		}
	}
	t.Fatalf("the schema must expose %s", key)
	return Entry{}
}

// TestTestsEntriesShape: every key is a boolean on the Tests page.
func TestTestsEntriesShape(t *testing.T) {
	for _, key := range []string{"tests.results_window", "tests.auto_open", "tests.coverage_status"} {
		if e := testsEntry(t, key); e.Type != Bool {
			t.Fatalf("%s type = %v, want Bool", key, e.Type)
		}
	}
	if d := config.Defaults(); d["tests.results_window"] != "true" || d["tests.auto_open"] != "true" {
		t.Fatal("both Test Results toggles must default to on")
	}
	// The status-line coverage figure is opt-in (#2246).
	if config.Defaults()["tests.coverage_status"] != "false" {
		t.Fatal("tests.coverage_status must default to off")
	}
}

// TestTestsResultsWindowRoundTrip: toggling through the panel persists and
// the reloaded config reads it back.
func TestTestsResultsWindowRoundTrip(t *testing.T) {
	restoreConfig(t)
	e := testsEntry(t, "tests.results_window")
	m := New([]Page{{Title: "Tests", Entries: []Entry{e}}}, testOpts(t))
	m.SetSize(90, 24)
	m.Open()

	m.writeValue(e, false)
	commit(t, m)
	if config.Get().Tests.ResultsWindow {
		t.Fatal("persisted tests.results_window must be false")
	}
	if got := m.value("tests.results_window"); got != "false" {
		t.Fatalf("panel value = %q, want false", got)
	}
	if v := m.View(); !strings.Contains(v, " off") {
		t.Fatalf("the entry row must render the value:\n%s", v)
	}
}

// TestTestsAutoOpenRoundTrip: same for the auto-open toggle.
func TestTestsAutoOpenRoundTrip(t *testing.T) {
	restoreConfig(t)
	e := testsEntry(t, "tests.auto_open")
	m := New([]Page{{Title: "Tests", Entries: []Entry{e}}}, testOpts(t))
	m.SetSize(90, 24)
	m.Open()

	m.writeValue(e, false)
	commit(t, m)
	if config.Get().Tests.AutoOpen {
		t.Fatal("persisted tests.auto_open must be false")
	}
}

// TestTestsCoverageStatusRoundTrip: the status-line coverage segment's
// opt-in persists through the panel (#2246).
func TestTestsCoverageStatusRoundTrip(t *testing.T) {
	restoreConfig(t)
	e := testsEntry(t, "tests.coverage_status")
	m := New([]Page{{Title: "Tests", Entries: []Entry{e}}}, testOpts(t))
	m.SetSize(90, 24)
	m.Open()

	m.writeValue(e, true)
	commit(t, m)
	if !config.Get().Tests.CoverageStatus {
		t.Fatal("persisted tests.coverage_status must be true")
	}
	if got := m.value("tests.coverage_status"); got != "true" {
		t.Fatalf("panel value = %q, want true", got)
	}
}
