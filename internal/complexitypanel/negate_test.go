package complexitypanel

import (
	"sort"
	"strings"
	"testing"

	"ike/internal/complexity"
)

// negate_test.go pins the negated filter terms (#2951): -field:value drops
// what the positive term would keep, -word drops the fuzzy matches.

func sortedNames(rows []Row) string {
	got := names(rows)
	sort.Strings(got)
	return strings.Join(got, ",")
}

func TestFilterNegatedTerms(t *testing.T) {
	m := newPanel(t)
	v := sample()
	v.Files["/proj/internal/app/app_test.go"] = []complexity.Function{fn("TestRender", 0, 10, 2, 1)}
	m.Set(v, complexity.Stats{Enabled: true})
	for _, tc := range []struct{ expr, want string }{
		{"file:internal/app", "TestRender,render,tiny"},
		{"file:internal/app -file:*_test.go", "render,tiny"},
		{"-fn:render", "ListNav,Page::show,inner,tiny"},
		{"-lang:php", "ListNav,TestRender,inner,render,tiny"},
		{"-rndr", "ListNav,Page::show,inner,tiny"},
		{"file:internal -inner", "ListNav,TestRender,render,tiny"},
	} {
		m.filter.SetText(tc.expr)
		m.Refresh()
		if got := sortedNames(m.Rows()); got != tc.want {
			t.Errorf("%q rows = %s, want %s", tc.expr, got, tc.want)
		}
	}
}
