package diff

import (
	"strings"
	"testing"
)

// refine_bench_test.go measures intra-line refinement on line pairs at the
// 400-rune size of the old rune-level cap (#2849): a small edit inside a
// long code-like line, and two fully divergent lines — the worst case the
// #2505 bounds must keep cheap.

// benchLine400 is a ~400-rune code-like line with mid at its centre: 12
// units of 16 runes on either side (384) plus the 15/17-rune mid.
func benchLine400(mid string) string {
	unit := "foo(bar, baz) + "
	return strings.Repeat(unit, 12) + mid + strings.Repeat(unit, 12)
}

func BenchmarkRefineSmallEdit400(b *testing.B) {
	left := benchLine400("alpha(one, two)")
	right := benchLine400("alpha(one, three)")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		refine(left, right)
	}
}

func BenchmarkRefineDivergent400(b *testing.B) {
	left := strings.Repeat("abcd ", 80)
	right := strings.Repeat("wxyz-", 80)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		refine(left, right)
	}
}
