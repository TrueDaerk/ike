package treetest

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// node builds a SyntaxNode over src by kind, field and byte extent, with
// children — a hand-made tree, so the reader is checked without a grammar.
func node(kind, field string, start, end int, op string, children ...*highlight.SyntaxNode) *highlight.SyntaxNode {
	return &highlight.SyntaxNode{Kind: kind, Field: field, StartByte: start, EndByte: end, Operator: op, Children: children}
}

var spec = &lang.ComplexitySpec{
	FunctionNodes: map[string]string{"fn": "name", "lambda": ""},
	ParamsField:   "parameters",
	Branch:        []string{"if"},
	BoolOps:       []string{"&&"},
}

// TestFunctionsCountsPerUnit pins the reference count: 1 + branches +
// boolean operators per unit, a nested unit measured on its own and not
// counted toward the enclosing one, parameters per `name` field.
func TestFunctionsCountsPerUnit(t *testing.T) {
	src := "outer inner"
	root := node("root", "", 0, len(src), "",
		node("fn", "", 0, len(src), "",
			node("ident", "name", 0, 5, ""),
			node("params", "parameters", 0, 0, "",
				node("param", "", 0, 0, "", node("ident", "name", 0, 0, ""), node("ident", "name", 0, 0, "")),
				node("param", "", 0, 0, "", node("ident", "type", 0, 0, "")),
			),
			node("if", "", 0, 0, "", node("binary", "", 0, 0, "&&")),
			node("lambda", "", 6, 11, "",
				node("params", "parameters", 0, 0, "", node("param", "", 0, 0, "")),
				node("if", "", 0, 0, ""),
				node("if", "", 0, 0, ""),
			),
		),
	)
	got := Functions(root, src, spec)
	want := []Function{
		{Name: "outer", Kind: "fn", Params: 3, CCN: 3},
		{Name: "", Kind: "lambda", Params: 1, CCN: 3},
	}
	if len(got) != len(want) {
		t.Fatalf("Functions = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("function %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if !Kinds(root)["lambda"] || Kinds(root)["for"] {
		t.Errorf("Kinds = %v", Kinds(root))
	}
	if !Operators(root)["&&"] {
		t.Errorf("Operators = %v", Operators(root))
	}
	if Functions(nil, src, spec) != nil || Functions(root, src, nil) != nil {
		t.Error("nil root or spec must yield nil")
	}
}
