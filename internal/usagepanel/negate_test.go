package usagepanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/telemetry"
)

// A negated word (#2951) drops the rows it fuzzy-matches.
func TestFilterNegatedWord(t *testing.T) {
	m := panel(t, report(map[string]func(*telemetry.UsageDay){dayKey(0): sample}))
	press(m, "/")
	for _, r := range "-file" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	rows := m.Rows()
	if len(rows) != 1 || rows[0].Name != "editor.save" {
		t.Errorf("filtered rows = %+v, want editor.save only", rows)
	}
}
