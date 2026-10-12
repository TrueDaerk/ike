//go:build cgo

package langpython

import (
	"strings"
	"testing"

	"ike/internal/highlight"
	"ike/internal/highlight/treetest"
)

// pythonComplexityProbe exercises every kind and operator the spec names: a
// method with if / elif / else, for, while, try / except, a conditional
// expression, a match statement and a lambda, plus a plain async function.
var pythonComplexityProbe = []string{
	"class C:",
	"    def m(self, a, b=1, *args, **kw):", // 1
	"        if a and b or not a:",
	"            pass",
	"        elif a:",
	"            pass",
	"        else:",
	"            pass",
	"        for x in a:",
	"            pass",
	"        while a:",
	"            pass",
	"        try:",
	"            pass",
	"        except ValueError:",
	"            pass",
	"        except (A, B) as e:",
	"            pass",
	"        y = 1 if a else 2",
	"        match a:",
	"            case 1:",
	"                pass",
	"            case _:",
	"                pass",
	"        f = lambda x: x > 1 or x < 0", // 24
	"        return f",
	"",
	"async def plain(x): pass", // 27
}

func TestPythonComplexitySpecKindsExist(t *testing.T) {
	root := highlight.SyntaxTree("python", pythonComplexityProbe)
	if root == nil {
		t.Fatal("no Python grammar")
	}
	kinds := treetest.Kinds(root)
	for _, k := range treetest.SpecKinds(pythonComplexity) {
		if !kinds[k] {
			t.Errorf("spec names kind %q, which the probe never parses to", k)
		}
	}
	ops := treetest.Operators(root)
	for _, op := range pythonComplexity.BoolOps {
		if !ops[op] {
			t.Errorf("spec names operator %q, which no probe node carries", op)
		}
	}
}

func TestPythonComplexityProbeFunctions(t *testing.T) {
	root := highlight.SyntaxTree("python", pythonComplexityProbe)
	if root == nil {
		t.Fatal("no Python grammar")
	}
	got := treetest.Functions(root, strings.Join(pythonComplexityProbe, "\n"), pythonComplexity)
	want := []treetest.Function{
		// 1 + if, elif, for, while, 2 except, conditional, 2 case arms
		// + and, or; the lambda's `or` is its own.
		{Name: "m", Kind: "function_definition", Line: 1, Params: 5, CCN: 12},
		{Name: "", Kind: "lambda", Line: 24, Params: 1, CCN: 2},
		{Name: "plain", Kind: "function_definition", Line: 27, Params: 1, CCN: 1},
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
