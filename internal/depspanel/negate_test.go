package depspanel

import "testing"

// Negated terms and words (#2951) drop what their positive spelling keeps.
func TestFilterNegatedTerms(t *testing.T) {
	for _, tc := range []struct {
		expr string
		want []string
	}{
		{"-state:vulnerable", []string{"github.com/foo/bar", "left-pad"}},
		{"-manifest:package.json", []string{"github.com/foo/bar", "github.com/baz/qux"}},
		{"-pad", []string{"github.com/foo/bar", "github.com/baz/qux"}},
		{"manifest:go.mod -state:outdated", []string{"github.com/baz/qux"}},
	} {
		m := newPanel()
		m.filter.SetText(tc.expr)
		m.Refresh()
		var got []string
		for _, r := range m.rows {
			if !r.header {
				got = append(got, r.d.Name)
			}
		}
		if len(got) != len(tc.want) {
			t.Fatalf("%q rows = %v, want %v", tc.expr, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%q rows = %v, want %v", tc.expr, got, tc.want)
			}
		}
	}
}
