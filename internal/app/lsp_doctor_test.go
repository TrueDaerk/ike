package app

// lsp_doctor_test.go guards the LSP Doctor wiring (#2164): the lsp.doctor
// open/focus/return state machine, the check run kicked off on open, results
// rendering with diagnosis + fix, and the app-owned report surviving a panel
// close so a re-run can verify fixes.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	ilsp "ike/internal/lsp"
	"ike/internal/lspdoctor"
	"ike/internal/pane"
)

// lspDoctorView renders the open LSP Doctor pane's content.
func lspDoctorView(t *testing.T, m Model) string {
	t.Helper()
	p := m.lspDoctorPanel()
	if p == nil {
		t.Fatal("LSP Doctor panel must be open")
	}
	return ansi.Strip(p.View())
}

// TestLSPDoctorOpensAndRuns guards the open path: a DoctorMsg opens the
// panel focused, stores the server set, and starts a check run whose results
// render with diagnosis and fix.
func TestLSPDoctorOpensAndRuns(t *testing.T) {
	// A bare PATH keeps the real probes off the machine's toolchains and
	// makes the fake server resolve to "missing" fast.
	t.Setenv("PATH", t.TempDir())
	m := sized(t, 120, 40)
	out, cmd := m.Update(ilsp.DoctorMsg{Servers: []lspdoctor.Server{
		{Lang: "fake", Command: "definitely-missing-server", Install: []string{"npm", "install", "-g", "fake-ls"}},
	}})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.LSPDoctorKey) {
		t.Fatal("lsp.doctor must open the panel")
	}
	if m.activeWS().Panes.Focused() != pane.LSPDoctorKey {
		t.Fatal("a fresh open must focus the panel")
	}
	if !m.lspDoctorReport.Running() {
		t.Fatal("opening must start a check run")
	}
	if cmd == nil {
		t.Fatal("opening must return the run command")
	}
	res, ok := cmd().(lspdoctor.ResultsMsg)
	if !ok {
		t.Fatal("the run command must deliver a ResultsMsg")
	}
	out, _ = m.Update(res)
	m = out.(Model)
	v := lspDoctorView(t, m)
	if !strings.Contains(v, "diagnosis [binary missing]") {
		t.Fatalf("missing-binary diagnosis must render: %q", v)
	}
	if !strings.Contains(v, "fix: npm install -g fake-ls") {
		t.Fatalf("install fix must render: %q", v)
	}

	// The report survives closing and reopening the panel (app-owned), and
	// the toggle machine returns focus when already focused.
	out, _ = m.Update(ilsp.DoctorMsg{}) // focused → return focus
	m = out.(Model)
	if m.activeWS().Panes.Focused() == pane.LSPDoctorKey {
		t.Fatal("a second lsp.doctor must return focus")
	}
	m.activeWS().Panes.Close(pane.LSPDoctorKey)
	out, _ = m.Update(ilsp.DoctorMsg{})
	m = out.(Model)
	if v := lspDoctorView(t, m); !strings.Contains(v, "diagnosis [binary missing]") {
		t.Fatalf("the report must survive a panel close: %q", v)
	}
}

// TestLSPDoctorRerunVerifies guards the verify loop: a re-run over a now-
// healthy setup marks the previous failure resolved instead of repeating the
// hint.
func TestLSPDoctorRerunVerifies(t *testing.T) {
	m := sized(t, 120, 40)
	m.lspDoctorReport.SetServers([]lspdoctor.Server{{Lang: "toml", Command: "taplo"}})
	out, _ := m.Update(lspdoctor.ResultsMsg{Results: []lspdoctor.Result{{
		Lang: "toml", Command: "taplo", Class: lspdoctor.ClassCrashInit, Diagnosis: "crash", Fix: "reinstall",
	}}})
	m = out.(Model)
	out, _ = m.Update(lspdoctor.ResultsMsg{Results: []lspdoctor.Result{{
		Lang: "toml", Command: "taplo", Class: lspdoctor.ClassOK,
	}}})
	m = out.(Model)
	res := m.lspDoctorReport.Results()
	if len(res) != 1 || res[0].Verdict != "resolved" {
		t.Fatalf("re-run must verify the fix: %+v", res)
	}

	// RerunMsg starts a fresh run over the stored servers.
	t.Setenv("PATH", t.TempDir())
	out, cmd := m.Update(lspdoctor.RerunMsg{})
	m = out.(Model)
	if !m.lspDoctorReport.Running() || cmd == nil {
		t.Fatal("RerunMsg must start a run")
	}
}
