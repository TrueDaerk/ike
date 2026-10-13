package problems

import (
	"strings"
	"testing"
)

// Negated terms and words (#2951) drop what their positive spelling keeps.
func TestFilterNegatedTerms(t *testing.T) {
	for _, tt := range []struct {
		expr string
		want []string
	}{
		{"-source:vet", []string{"undefined: foo", "cannot find name"}},
		{"-sev:error", []string{"unused variable"}},
		{"-file:*.ts", []string{"undefined: foo", "unused variable"}},
		{"-code:TS", []string{"undefined: foo", "unused variable"}},
		{"sev:error -file:*.ts", []string{"undefined: foo"}},
		{"-undef", []string{"cannot find name", "unused variable"}},
	} {
		t.Run(tt.expr, func(t *testing.T) {
			m := filterModel(t)
			setFilter(t, m, tt.expr)
			got := messages(m)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("rows = %v, want %v", got, tt.want)
			}
		})
	}
}
