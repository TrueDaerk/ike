package todoindex

import "testing"

// Negated terms and words (#2951) drop what their positive spelling keeps.
func TestFilterNegatedTerms(t *testing.T) {
	for _, tt := range []struct {
		expr string
		want int
	}{
		{"-tag:TODO", 2},
		{"-file:*.ts", 3},
		{"-scope:file", 3},
		{"tag:TODO -scope:file", 1},
		{"-broken", 3},
		{"file:internal/app -tag:FIXME -wire", 1},
	} {
		t.Run(tt.expr, func(t *testing.T) {
			m, _ := filterModel(t)
			setFilter(t, m, tt.expr)
			if got := m.Total(); got != tt.want {
				t.Fatalf("total = %d, want %d", got, tt.want)
			}
		})
	}
}
