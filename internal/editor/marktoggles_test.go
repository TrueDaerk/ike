package editor

import (
	"testing"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/vcs"
)

// TestSeverityTogglesFilterDecorations checks that editor.marks.lsp_* switches
// hide the matching severities from the scrollbar stripe, the gutter severity
// and the inline underline — while the diagnostic set itself stays complete
// (#1259).
func TestSeverityTogglesFilterDecorations(t *testing.T) {
	m := sbEditor(t, 40, 20, 10)
	m.setDiagnostics([]ilsp.Diagnostic{diag(0, 1), diag(4, 2), diag(8, 3), diag(12, 4)})

	stripe := m.scrollbarStripe(40, 40)
	if len(stripe) != 4 {
		t.Fatalf("all severities on: stripe has %d marks, want 4", len(stripe))
	}

	m.Configure(host.MapConfig{
		"editor.marks.lsp_warnings": "false",
		"editor.marks.lsp_info":     "false",
		"editor.marks.lsp_hints":    "false",
	})
	stripe = m.scrollbarStripe(40, 40)
	if len(stripe) != 1 || stripe[0] != 1 {
		t.Fatalf("only errors on: stripe = %v, want just the error at row 0", stripe)
	}
	if _, ok := m.worstSeverityOnLine(4); ok {
		t.Fatal("gutter still reports the toggled-off warning line")
	}
	if sev, ok := m.worstSeverityOnLine(0); !ok || sev != 1 {
		t.Fatalf("gutter error line = %d/%v, want 1/true", sev, ok)
	}
	if _, ok := m.diagSeverityAt(4, 0); ok {
		t.Fatal("underline still covers the toggled-off warning")
	}
	// The data stays complete: popup and jump keep seeing everything.
	if len(m.diags) != 4 || len(m.diagByLine) != 4 {
		t.Fatalf("diagnostic set was mutated: %d diags, %d lines", len(m.diags), len(m.diagByLine))
	}

	// An unspecified severity counts as an error and follows the error switch.
	m.Configure(host.MapConfig{"editor.marks.lsp_errors": "false"})
	m.setDiagnostics([]ilsp.Diagnostic{diag(0, 0)})
	if stripe := m.scrollbarStripe(40, 40); len(stripe) != 0 {
		t.Fatalf("severity-0 mark survived the error toggle: %v", stripe)
	}
}

// TestGitMarkTogglesFilterDecorations checks the editor.marks.git_* switches on
// the scrollbar git marks and the gutter colouring (#1259).
func TestGitMarkTogglesFilterDecorations(t *testing.T) {
	m := sbEditor(t, 40, 20, 10)
	m.gitMarks = map[int]vcs.LineMark{0: vcs.LineAdded, 4: vcs.LineChanged, 8: vcs.LineDeleted}
	m.marksEpoch++

	git, _ := m.scrollbarGitMarks(40, 40)
	if len(git) != 3 {
		t.Fatalf("all kinds on: %d git marks, want 3", len(git))
	}

	m.Configure(host.MapConfig{
		"editor.marks.git_added":   "false",
		"editor.marks.git_deleted": "false",
	})
	git, _ = m.scrollbarGitMarks(40, 40)
	if len(git) != 1 || git[4] != vcs.LineChanged {
		t.Fatalf("changed only: git marks = %v", git)
	}
	if !m.gitVisible(vcs.LineChanged) || m.gitVisible(vcs.LineAdded) || m.gitVisible(vcs.LineDeleted) {
		t.Fatal("gitVisible does not reflect the toggles")
	}
}

// TestMarkTogglesInvalidateScrollbarCache ensures a toggle flip bumps the
// epochs so the memoized stripe rebuilds (#1097).
func TestMarkTogglesInvalidateScrollbarCache(t *testing.T) {
	m := sbEditor(t, 40, 20, 10)
	m.setDiagnostics([]ilsp.Diagnostic{diag(0, 2)})
	stripe, _, _, _ := m.stripesFor(10, 40)
	if len(stripe) != 1 {
		t.Fatalf("warm-up stripe = %v, want one mark", stripe)
	}
	m.Configure(host.MapConfig{"editor.marks.lsp_warnings": "false"})
	stripe, _, _, _ = m.stripesFor(10, 40)
	if len(stripe) != 0 {
		t.Fatalf("stale memo survived the toggle: %v", stripe)
	}
}
