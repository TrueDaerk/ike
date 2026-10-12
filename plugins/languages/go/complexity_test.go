//go:build cgo

package langgo

import (
	"strings"
	"testing"

	"ike/internal/highlight"
	"ike/internal/highlight/treetest"
)

// goComplexityProbe exercises every kind and operator the spec names: a
// method with if / else-if / else, the three for shapes, an expression
// switch, a type switch, a select, a func literal, a labelled break and a
// goto, plus a plain function.
var goComplexityProbe = []string{
	"package p",
	"",
	"type S struct{}",
	"",
	"func (s *S) Handle(a, b int, c string) error {", // 4
	"	if a > 0 && b > 0 || c != \"\" {",
	"		return nil",
	"	} else if a < 0 {",
	"		return nil",
	"	} else {",
	"		for i := 0; i < 3; i++ {",
	"		}",
	"		for range c {",
	"		}",
	"		for a > 0 {",
	"		}",
	"	}",
	"	switch a {",
	"	case 1, 2:",
	"	case 3:",
	"	default:",
	"	}",
	"	switch any(a).(type) {",
	"	case int:",
	"	default:",
	"	}",
	"	select {",
	"	case <-ch:",
	"	default:",
	"	}",
	"	f := func(x int) bool { return x > 1 || x < 0 }", // 30
	"	_ = f",
	"L:",
	"	for {",
	"		if a > 1 {",
	"			continue L",
	"		}",
	"		break L",
	"	}",
	"	goto L",
	"}",
	"",
	"func plain() {}", // 42
}

func TestGoComplexitySpecKindsExist(t *testing.T) {
	root := highlight.SyntaxTree("go", goComplexityProbe)
	if root == nil {
		t.Fatal("no Go grammar")
	}
	kinds := treetest.Kinds(root)
	for _, k := range treetest.SpecKinds(goComplexity) {
		if !kinds[k] {
			t.Errorf("spec names kind %q, which the probe never parses to", k)
		}
	}
	ops := treetest.Operators(root)
	for _, op := range goComplexity.BoolOps {
		if !ops[op] {
			t.Errorf("spec names operator %q, which no probe node carries", op)
		}
	}
}

func TestGoComplexityProbeFunctions(t *testing.T) {
	root := highlight.SyntaxTree("go", goComplexityProbe)
	if root == nil {
		t.Fatal("no Go grammar")
	}
	got := treetest.Functions(root, strings.Join(goComplexityProbe, "\n"), goComplexity)
	want := []treetest.Function{
		// 1 + if, else-if, 3 for, 2 expression cases, 1 type case, 1
		// communication case, the labelled for and its if, + 2 boolean
		// operators; the literal's || is its own.
		{Name: "Handle", Kind: "method_declaration", Line: 4, Params: 3, CCN: 14},
		{Name: "", Kind: "func_literal", Line: 30, Params: 1, CCN: 2},
		{Name: "plain", Kind: "function_declaration", Line: 42, Params: 0, CCN: 1},
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
