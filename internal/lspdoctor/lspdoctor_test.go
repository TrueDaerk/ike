package lspdoctor

// lspdoctor_test.go guards the report's fix-verification loop and the panel
// rendering (#2164): a re-run marks previously failing servers resolved or
// still failing instead of repeating the same hint, and 'r' asks for a
// re-run.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func view(m *Model) string { return ansi.Strip(m.View()) }

func failing(lang string, class Class) Result {
	return Result{
		Lang:      lang,
		Command:   lang + "-ls",
		Class:     class,
		Diagnosis: "it is broken",
		Fix:       "install it",
		Checks:    []Check{{Name: "binary", Status: StatusFail, Detail: "not found"}},
	}
}

func healthy(lang string) Result {
	return Result{
		Lang:    lang,
		Command: lang + "-ls",
		Class:   ClassOK,
		Checks:  []Check{{Name: "initialize", Status: StatusOK, Detail: "spawned and answered initialize"}},
	}
}

// TestReportVerdicts guards Finish's verification: resolved when a failure
// clears, unresolved when the same class repeats, changed when the failure
// morphs, and no verdict on first runs or already-healthy servers.
func TestReportVerdicts(t *testing.T) {
	r := NewReport()
	r.Finish([]Result{failing("toml", ClassCrashInit), failing("go", ClassMissing), failing("python", ClassMissing), healthy("shell")})
	for _, res := range r.Results() {
		if res.Verdict != "" {
			t.Fatalf("first run must carry no verdict, got %q for %s", res.Verdict, res.Lang)
		}
	}
	r.Finish([]Result{healthy("toml"), failing("go", ClassMissing), failing("python", ClassCrashInit), healthy("shell")})
	want := map[string]string{"toml": "resolved", "go": "unresolved", "python": "changed", "shell": ""}
	for _, res := range r.Results() {
		if res.Verdict != want[res.Lang] {
			t.Fatalf("%s: verdict = %q want %q", res.Lang, res.Verdict, want[res.Lang])
		}
	}
}

// TestPanelRendersDiagnosisAndVerdict: the panel shows the check rows, the
// classified diagnosis with its fix, and the re-run verdict wording.
func TestPanelRendersDiagnosisAndVerdict(t *testing.T) {
	r := NewReport()
	r.SetServers([]Server{{Lang: "toml", Command: "taplo"}})
	r.Finish([]Result{failing("toml", ClassCrashInit)})
	r.Finish([]Result{failing("toml", ClassCrashInit)})

	m := New(nil)
	m.SetReport(r)
	m.SetSize(240, 14)
	v := view(&m)
	if !strings.Contains(v, "LSP Doctor") || !strings.Contains(v, "1 failing") {
		t.Fatalf("header must count failures: %q", v)
	}
	if !strings.Contains(v, "diagnosis [crash on initialize]: it is broken") {
		t.Fatalf("diagnosis row missing: %q", v)
	}
	if !strings.Contains(v, "fix: install it") {
		t.Fatalf("fix row missing: %q", v)
	}
	if !strings.Contains(v, "still failing after re-run") {
		t.Fatalf("unresolved verdict missing: %q", v)
	}
	if !strings.Contains(v, "press r to verify") {
		t.Fatalf("status line must point at the verify loop: %q", v)
	}

	r.Finish([]Result{healthy("toml")})
	if v := view(&m); !strings.Contains(v, "resolved — the previous failure is gone") {
		t.Fatalf("resolved verdict missing: %q", v)
	}
}

// TestPanelRerunKey: 'r' emits RerunMsg while idle with servers, and stays
// silent while a run is in flight.
func TestPanelRerunKey(t *testing.T) {
	r := NewReport()
	r.SetServers([]Server{{Lang: "go", Command: "gopls"}})
	m := New(nil)
	m.SetReport(r)
	m.SetSize(120, 10)

	cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("r must request a re-run")
	}
	if _, ok := cmd().(RerunMsg); !ok {
		t.Fatal("r must emit RerunMsg")
	}
	r.Begin()
	if cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"}); cmd != nil {
		t.Fatal("r must be inert while a run is in flight")
	}
}
