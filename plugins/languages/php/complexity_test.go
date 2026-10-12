//go:build cgo

package langphp

import (
	"strings"
	"testing"

	"ike/internal/highlight"
	"ike/internal/highlight/treetest"
)

// phpComplexityProbe exercises every kind and operator the spec names: a
// method with if / elseif / else, the four loops, switch, try / catch, the
// ternary, null coalescing, match, a closure, an arrow function, a levelled
// break, a goto and the four call shapes, plus a plain function and the
// class-like declarations.
var phpComplexityProbe = []string{
	"<?php",
	"class C {",
	"    public function m(int $a, $b = 1, ...$rest): int {", // 2
	"        if ($a && $b || !$a) {",
	"        } elseif ($a) {",
	"        } else {",
	"        }",
	"        for ($i = 0; $i < 3; $i++) {}",
	"        foreach ($a as $k => $v) {}",
	"        while ($a) {}",
	"        do {} while ($a);",
	"        switch ($a) { case 1: break; case 2: break; default: break; }",
	"        try {} catch (E $e) {} finally {}",
	"        $x = $a ? 1 : 2;",
	"        $z = $a ?? 2;",
	"        $w = match($a) { 1, 2 => 'a', default => 'b' };",
	"        $f = function ($x) use ($a) { return $x > 1 || $x < 0; };", // 16
	"        $g = fn($x) => $x and $y;",                                 // 17
	"        $h = ($a or $b);",
	"        foo($a); $this->m($a); $a?->m($a); self::m($a);",
	"        while ($a) { continue 2; }",
	"        goto end;",
	"        end:",
	"        return 1;",
	"    }",
	"}",
	"function plain($a) { return 1; }", // 26
	"trait T {}",
	"interface I {}",
	"enum E {}",
}

func TestPHPComplexitySpecKindsExist(t *testing.T) {
	root := highlight.SyntaxTree("php", phpComplexityProbe)
	if root == nil {
		t.Fatal("no PHP grammar")
	}
	kinds := treetest.Kinds(root)
	for _, k := range treetest.SpecKinds(phpComplexity) {
		if !kinds[k] {
			t.Errorf("spec names kind %q, which the probe never parses to", k)
		}
	}
	ops := treetest.Operators(root)
	for _, op := range phpComplexity.BoolOps {
		if !ops[op] {
			t.Errorf("spec names operator %q, which no probe node carries", op)
		}
	}
}

func TestPHPComplexityProbeFunctions(t *testing.T) {
	root := highlight.SyntaxTree("php", phpComplexityProbe)
	if root == nil {
		t.Fatal("no PHP grammar")
	}
	got := treetest.Functions(root, strings.Join(phpComplexityProbe, "\n"), phpComplexity)
	want := []treetest.Function{
		// 1 + if, elseif, for, foreach, while, do, 2 cases, catch, ternary,
		// 1 match arm, while + &&, ||, ??, or; the closures' operators are
		// their own.
		{Name: "m", Kind: "method_declaration", Line: 2, Params: 3, CCN: 17},
		{Name: "", Kind: "anonymous_function", Line: 16, Params: 1, CCN: 2},
		{Name: "", Kind: "arrow_function", Line: 17, Params: 1, CCN: 2},
		{Name: "plain", Kind: "function_definition", Line: 26, Params: 1, CCN: 1},
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
