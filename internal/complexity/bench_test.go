//go:build cgo

package complexity

import (
	"fmt"
	"strings"
	"testing"

	_ "ike/plugins/languages/go"
)

// BenchmarkMeasure measures a synthetic 5 000-line Go file — the epic's budget
// is 50 ms per file on the dev machine (#2915). The file repeats a 25-line
// function with an else-if ladder, a switch, a nested loop with a boolean
// chain, a closure and a recursive call, so every rule is exercised.
func BenchmarkMeasure(b *testing.B) {
	lines := syntheticGo(5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fns, err := Measure("go", lines)
		if err != nil {
			b.Fatal(err)
		}
		if len(fns) < 100 {
			b.Fatalf("measured %d functions", len(fns))
		}
	}
}

// syntheticGo returns at least n lines of valid Go.
func syntheticGo(n int) []string {
	var sb strings.Builder
	sb.WriteString("package bench\n\n")
	for i := 0; sb.Len() == 0 || strings.Count(sb.String(), "\n") < n; i++ {
		fmt.Fprintf(&sb, `func f%d(a, b int, xs []int) int {
	if a > 0 && b > 0 || a < 0 {
		return 1
	} else if a == 2 {
		return 2
	} else {
		a++
	}
	switch b {
	case 1:
		return f%d(a-1, b, xs)
	case 2, 3:
		return 3
	default:
		b--
	}
	g := func(x int) bool { return x > a && x < b }
	n := 0
	for _, x := range xs {
		for j := 0; j < x; j++ {
			if g(j) || j == a && j != b {
				n++
			}
		}
	}
	return n
}

`, i, i)
	}
	return strings.Split(sb.String(), "\n")
}

func TestSyntheticGoMeasures(t *testing.T) {
	lines := syntheticGo(5000)
	fns, err := Measure("go", lines)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) < 5000 || len(fns) < 300 {
		t.Fatalf("synthetic file: %d lines, %d functions", len(lines), len(fns))
	}
	// Every repeated function measures the same: CCN 1 + if + || + && +
	// else-if + 2 arms + for + for + if + || + && = 12 (the closure's && is
	// its own); cognitive if(1) + 2 runs + else-if(1) + else(1) + switch(1)
	// + recursion(1) + closure && at level 1 (1) + for(1) + for(2) + if(3)
	// + 2 runs = 16.
	for i, f := range fns {
		if f.Kind == KindLambda {
			if f.CCN != 2 || f.Cognitive != 1 {
				t.Fatalf("closure %d = %+v", i, f)
			}
			continue
		}
		if f.CCN != 12 || f.Cognitive != 16 || f.Params != 3 {
			t.Fatalf("function %d = %+v", i, f)
		}
	}
}
