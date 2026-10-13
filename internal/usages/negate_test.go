package usages

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
		{"-file:internal/app", []string{"const f = Foo", "call Foo here"}},
		{"-text:return", []string{"x := Foo{}", "const f = Foo", "call Foo here"}},
		{"file:internal/app -text:return", []string{"x := Foo{}"}},
		{"-readme", []string{"return Foo()", "x := Foo{}", "const f = Foo"}},
	} {
		t.Run(tt.expr, func(t *testing.T) {
			m := filterModel(t)
			setFilter(t, m, tt.expr)
			got := previews(m)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("rows = %v, want %v", got, tt.want)
			}
		})
	}
}
