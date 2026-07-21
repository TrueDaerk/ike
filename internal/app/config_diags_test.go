package app

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// config_diags_test.go covers #793: config-load diagnostics surface as
// warning notifications, deduped per session.

func reloadWithDiags(m Model, diags []config.Diagnostic) Model {
	out, _ := m.Update(config.ConfigReloadedMsg{Config: config.Get(), Diags: diags})
	return out.(Model)
}

func TestConfigDiagnosticsSurfaceAsNotifications(t *testing.T) {
	m := sized(t, 100, 40)
	base := len(m.history)
	m = reloadWithDiags(m, []config.Diagnostic{
		{Source: "/p/.ike/settings.toml", Field: "(file)", Message: "parse error"},
		{Field: "editor.tab_wdth", Message: "unknown setting (ignored)"},
	})
	if got := len(m.history) - base; got != 2 {
		t.Fatalf("notifications = %d, want 2: %+v", got, m.history)
	}
	// History is newest-first; both diagnostics must be present.
	joined := m.history[0].text + " | " + m.history[1].text
	if !strings.Contains(joined, "parse error") || !strings.Contains(joined, "unknown setting") {
		t.Fatalf("notification texts = %+v", m.history[:2])
	}

	// The same diagnostic on a later reload does not re-toast.
	m = reloadWithDiags(m, []config.Diagnostic{
		{Source: "/p/.ike/settings.toml", Field: "(file)", Message: "parse error"},
	})
	if got := len(m.history) - base; got != 2 {
		t.Fatalf("repeated diagnostics must dedupe, history grew to %d", got)
	}

	// A new message still surfaces.
	m = reloadWithDiags(m, []config.Diagnostic{
		{Field: "theme.name", Message: "unknown theme"},
	})
	if got := len(m.history) - base; got != 3 {
		t.Fatalf("fresh diagnostic must surface, history = %d", got)
	}
}
