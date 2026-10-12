//go:build cgo

package langweb

import (
	"strings"
	"testing"

	"ike/internal/highlight"
	"ike/internal/highlight/treetest"
	"ike/internal/lang"
)

// tsComplexityProbe exercises every kind and operator the spec names: a
// method with if / else if / else, the loops, switch, try / catch, the
// ternary, null coalescing, optional chaining (not a branch), an arrow
// function, a named and an unnamed function expression, generators and a
// labelled break, plus a plain function.
var tsComplexityProbe = []string{
	"class C {",
	"  m(a: number, b = 1, ...rest: string[]): number {", // 1
	"    if (a && b || !a) {",
	"    } else if (a) {",
	"    } else {",
	"    }",
	"    for (let i = 0; i < 3; i++) {}",
	"    for (const x of rest) {}",
	"    for (const k in rest) {}",
	"    while (a) {}",
	"    do {} while (a);",
	"    switch (a) { case 1: break; case 2: break; default: break; }",
	"    try {} catch (e) {} finally {}",
	"    const x = a ? 1 : 2;",
	"    const z = a ?? 2;",
	"    const q = rest?.length;",
	"    const f = (x: number) => x > 1 || x < 0;",   // 16
	"    const g = function named(x) { return x; };", // 17
	"    const h = function (x) { return x; };",      // 18
	"    const i = function* () { yield 1; };",       // 19
	"    outer: for (;;) { if (a) { continue outer; } break outer; }",
	"    return 1;",
	"  }",
	"}",
	"function plain(a, b) { return 1; }", // 24
	"function* gen() { yield 1; }",       // 25
}

func TestTSComplexitySpecKindsExist(t *testing.T) {
	root := highlight.SyntaxTree("typescript", tsComplexityProbe)
	if root == nil {
		t.Fatal("no TypeScript grammar")
	}
	kinds := treetest.Kinds(root)
	for _, k := range treetest.SpecKinds(tsComplexity) {
		if !kinds[k] {
			t.Errorf("spec names kind %q, which the probe never parses to", k)
		}
	}
	ops := treetest.Operators(root)
	for _, op := range tsComplexity.BoolOps {
		if !ops[op] {
			t.Errorf("spec names operator %q, which no probe node carries", op)
		}
	}
}

func TestTSComplexityProbeFunctions(t *testing.T) {
	root := highlight.SyntaxTree("typescript", tsComplexityProbe)
	if root == nil {
		t.Fatal("no TypeScript grammar")
	}
	got := treetest.Functions(root, strings.Join(tsComplexityProbe, "\n"), tsComplexity)
	want := []treetest.Function{
		// 1 + if, else-if, for, for-of, for-in, while, do, 2 cases, catch,
		// ternary, the labelled for and its if + &&, ||, ??; the arrow's ||
		// is its own.
		{Name: "m", Kind: "method_definition", Line: 1, Params: 3, CCN: 17},
		{Name: "", Kind: "arrow_function", Line: 16, Params: 1, CCN: 2},
		{Name: "named", Kind: "function_expression", Line: 17, Params: 1, CCN: 1},
		{Name: "", Kind: "function_expression", Line: 18, Params: 1, CCN: 1},
		{Name: "", Kind: "generator_function", Line: 19, Params: 0, CCN: 1},
		{Name: "plain", Kind: "function_declaration", Line: 24, Params: 2, CCN: 1},
		{Name: "gen", Kind: "generator_function_declaration", Line: 25, Params: 0, CCN: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("Functions = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("function %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestHTMLAndCSSDeclareNoComplexity pins the ledger side: markup and style
// sheets have no functions, so the audit records them as reasonNoFunctions.
func TestHTMLAndCSSDeclareNoComplexity(t *testing.T) {
	for _, id := range []string{"html", "css"} {
		l, ok := lang.ByID(id)
		if !ok {
			t.Fatalf("%s not registered", id)
		}
		if l.Complexity != nil {
			t.Errorf("%s declares a complexity spec; it has no functions", id)
		}
	}
}
