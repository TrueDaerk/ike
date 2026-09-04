package app

// lsp_doctor_copy_test.go covers lsp.doctor.copy (#2487) at the app seam: the
// whole report reaches the clipboard through the shared pane-copy path, and
// the command is a harmless no-op before the first finished run.

import (
	"strings"
	"testing"

	ilsp "ike/internal/lsp"
	"ike/internal/lspdoctor"
)

func TestLSPDoctorCopyPutsReportOnClipboard(t *testing.T) {
	orig := clipboardWrite
	copied := ""
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = orig })

	m := sized(t, 120, 40)
	// Open the panel without probing the machine, then hand it a report.
	t.Setenv("PATH", t.TempDir())
	out, _ := m.Update(ilsp.DoctorMsg{Servers: []lspdoctor.Server{{Lang: "toml", Command: "taplo"}}})
	m = out.(Model)
	out, _ = m.Update(lspdoctor.ResultsMsg{Results: []lspdoctor.Result{{
		Lang:      "toml",
		Command:   "taplo",
		Checks:    []lspdoctor.Check{{Name: "binary", Status: lspdoctor.StatusFail, Detail: "not found"}},
		Class:     lspdoctor.ClassMissing,
		Diagnosis: "the binary is nowhere on PATH",
		Fix:       "cargo install taplo-cli",
	}}})
	m = out.(Model)

	// The command hands the report to the host as a CopyMsg, the same route
	// the pane's own 'c' takes.
	out, cmd := m.Update(LSPDoctorCopyMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("lsp.doctor.copy must return the copy command")
	}
	msg, ok := cmd().(lspdoctor.CopyMsg)
	if !ok {
		t.Fatalf("lsp.doctor.copy produced %T, want lspdoctor.CopyMsg", cmd())
	}
	m = dispatch(t, m, msg)
	for _, want := range []string{"LSP Doctor — 1 server · 1 failing", "toml — taplo", "fix: cargo install taplo-cli"} {
		if !strings.Contains(copied, want) {
			t.Errorf("clipboard misses %q\n---\n%s", want, copied)
		}
	}
	if strings.ContainsRune(copied, '\x1b') {
		t.Errorf("clipboard text carries ANSI escapes: %q", copied)
	}
	_ = m
}

func TestLSPDoctorCopyWithoutReportIsNoOp(t *testing.T) {
	orig := clipboardWrite
	copied := ""
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = orig })

	m := sized(t, 120, 40)
	m = dispatch(t, m, LSPDoctorCopyMsg{}) // panel not even open
	if copied != "" {
		t.Errorf("no report: clipboard written with %q", copied)
	}
	_ = m
}
