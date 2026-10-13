package archview

import "testing"

// TestFilterNegatedTerms: a negated term or word (#2951) drops the entries its
// positive spelling would keep.
func TestFilterNegatedTerms(t *testing.T) {
	for _, tc := range []struct {
		expr string
		want []string
	}{
		{"-name:*.md", []string{"src", "src/main.go"}},
		{"-type:dir", []string{"src", "src/main.go", "README.md"}}, // src stays as main.go's parent
		{"-readme", []string{"src", "src/main.go"}},
	} {
		m := newPane(t, writeArchive(t, "README.md", "src/main.go"))
		m.OpenSearch()
		typeInto(&m, tc.expr)
		if got := rowNames(&m); !equal(got, tc.want) {
			t.Errorf("%q rows = %v, want %v", tc.expr, got, tc.want)
		}
	}
}
