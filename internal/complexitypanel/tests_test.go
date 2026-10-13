package complexitypanel

import (
	"strings"
	"testing"

	"ike/internal/complexity"
	"ike/internal/theme"

	_ "ike/plugins/languages/go"
)

// tests_test.go covers the hide-tests toggle and the test: filter key
// (#2950).

// testsPanel holds production code, a Go test file and a JS spec.
func testsPanel(t *testing.T) *Model {
	t.Helper()
	m := New(theme.DefaultPalette())
	m.pathLang = langByExt
	m.SetSize(240, 12)
	m.SetRoot("/proj")
	m.Set(complexity.View{Gen: 1, Files: map[string][]complexity.Function{
		"/proj/srv/server.go":      {fn("Handle", 0, 20, 10, 12), fn("helper", 30, 35, 3, 2)},
		"/proj/srv/server_test.go": {fn("runFakeServer", 0, 90, 52, 60)},
		"/proj/web/foo.spec.ts":    {fn("specFoo", 0, 9, 4, 3)},
	}}, complexity.Stats{Enabled: true})
	return &m
}

func TestToggleTestsHidesOnEveryTab(t *testing.T) {
	m := testsPanel(t)
	if got := strings.Join(names(m.Rows()), ","); got != "runFakeServer,Handle,specFoo,helper" {
		t.Fatalf("rows = %s", got)
	}
	if !strings.Contains(m.View(), "t tests") || strings.Contains(m.View(), "tests: hidden") {
		t.Fatalf("footer:\n%s", m.View())
	}
	m.Update(key("t"))
	if !m.HideTests() || strings.Join(names(m.Rows()), ",") != "Handle,helper" {
		t.Fatalf("hidden: %v rows %v", m.HideTests(), names(m.Rows()))
	}
	// The totals follow the visible population.
	if s := m.Summary(); s.Functions != 2 || s.CCNMax != 10 || m.Total() != 2 {
		t.Errorf("summary %+v total %d", s, m.Total())
	}
	if v := m.View(); !strings.Contains(v, "t tests: hidden") || !strings.Contains(v, "2 fns") {
		t.Errorf("view:\n%s", v)
	}
	// Grouped rows drop the test files as well.
	m.Update(key("b"))
	m.Update(key("b"))
	if got := labels(m.Rows()); got != "srv/server.go" {
		t.Errorf("file groups = %q", got)
	}
	// The File tab of a test file is empty while hidden.
	m.Follow("/proj/srv/server_test.go", 3)
	m.SetTab(TabFile)
	if len(m.Rows()) != 0 {
		t.Errorf("File tab rows = %v", names(m.Rows()))
	}
	m.Update(key("t"))
	if names(m.Rows())[0] != "runFakeServer" {
		t.Errorf("shown again: %v", names(m.Rows()))
	}
}

func TestToggleTestsChangesTab(t *testing.T) {
	m := testsPanel(t)
	base := map[string][]complexity.Function{
		"/proj/srv/server.go":      {fn("Handle", 0, 20, 8, 12)},
		"/proj/srv/server_test.go": {fn("runFakeServer", 0, 90, 40, 60)},
	}
	head := map[string][]complexity.Function{
		"/proj/srv/server.go":      {fn("Handle", 0, 20, 10, 12)},
		"/proj/srv/server_test.go": {fn("runFakeServer", 0, 90, 52, 60)},
	}
	m.SetChanges(Changes{Rev: "HEAD", Diff: complexity.DiffProject(base, head, 15, 15, 40), Ready: true})
	m.SetTab(TabChanges)
	if len(m.Rows()) != 2 || !strings.Contains(m.totalsText(), "ΣCCN 48→62") {
		t.Fatalf("rows %v totals %q", names(m.Rows()), m.totalsText())
	}
	m.Update(key("t"))
	if got := names(m.Rows()); len(got) != 1 || got[0] != "Handle" {
		t.Fatalf("hidden rows = %v", got)
	}
	if !strings.Contains(m.totalsText(), "ΣCCN 8→10 (+2)") {
		t.Errorf("totals must follow the visible files: %q", m.totalsText())
	}
}

func TestTestFilterKey(t *testing.T) {
	m := testsPanel(t)
	for _, tc := range []struct{ q, want string }{
		{"test:no", "Handle,helper"},
		{"test:yes", "runFakeServer,specFoo"},
		{"test:yes file:web", "specFoo"},
		{"test:no min:5", "Handle"},
		{"test:yes min:5", "runFakeServer"},
	} {
		m.filter.SetText(tc.q)
		m.Refresh()
		if got := strings.Join(names(m.Rows()), ","); got != tc.want {
			t.Errorf("%q rows = %q, want %q", tc.q, got, tc.want)
		}
	}
	// The toggle and the key combine: both narrow.
	m.filter.SetText("test:yes")
	m.Update(key("t"))
	if len(m.Rows()) != 0 {
		t.Errorf("hidden + test:yes rows = %v", names(m.Rows()))
	}
}

func TestModeRoundTrip(t *testing.T) {
	m := testsPanel(t)
	m.Update(key("b"))
	m.Update(key("t"))
	if got := m.Mode(); got != "project:class,tests:hidden" {
		t.Fatalf("mode = %q", got)
	}
	r := testsPanel(t)
	r.SetMode("project:class,tests:hidden,future:thing")
	if !r.HideTests() || r.Grouping() != complexity.ByClass || r.Mode() != "project:class,tests:hidden" {
		t.Errorf("restored %v %v %q", r.HideTests(), r.Grouping(), r.Mode())
	}
	r.SetMode("tests:hidden")
	if !r.HideTests() || r.Grouping() != complexity.ByNone || r.Mode() != "tests:hidden" {
		t.Errorf("tests only: %q", r.Mode())
	}
	r.SetMode("")
	if r.HideTests() || r.Mode() != "" {
		t.Errorf("reset: %q", r.Mode())
	}
}
