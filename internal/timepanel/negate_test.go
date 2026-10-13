package timepanel

import (
	"testing"
	"time"
)

// Negated terms and words (#2951) drop what their positive spelling keeps.
func TestFilterNegatedTerms(t *testing.T) {
	rep := report(t, map[string]string{"aaa": "ike", "bbb": "site"},
		entry{token: "aaa", day: dayKey(0), active: time.Hour, sessions: 1},
		entry{token: "bbb", day: dayKey(0), active: 3 * time.Hour, sessions: 1},
	)
	for _, expr := range []string{"-project:sit", "-site"} {
		m := panel(t, rep)
		m.filter.SetText(expr)
		m.Refresh()
		if got := m.Rows(); got != 1 || m.Summaries()[0].Name != "ike" {
			t.Errorf("%q rows = %d (%v), want just ike", expr, got, m.Summaries())
		}
	}
}
