//go:build cgo

package postfix

// tree_cgo_test.go exercises the Tree-sitter side of expression detection
// against the real Go and Python grammars — specifically the case the whole
// feature hinges on: the buffer is syntactically *broken* while the user types
// (`err.` is not a statement), so the node must come out of Tree-sitter's error
// recovery. The language plugins are not linked into this package's tests, so
// the grammars are registered here under private extensions, the way the
// highlight package's own tests do it.

import (
	"context"
	"testing"

	ts "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tspy "github.com/tree-sitter/tree-sitter-python/bindings/go"

	"ike/internal/complete"
	"ike/internal/highlight"
	"ike/internal/lang"
)

// The kind sets mirror the ones the Go and Python plugins register.
var (
	goExprNodes = []string{
		"identifier", "field_identifier", "package_identifier",
		"selector_expression", "call_expression", "index_expression",
		"slice_expression", "parenthesized_expression", "type_assertion_expression",
		"type_conversion_expression", "composite_literal", "func_literal",
		"int_literal", "float_literal", "interpreted_string_literal",
		"raw_string_literal", "rune_literal", "true", "false", "nil",
	}
	pyExprNodes = []string{
		"identifier", "attribute", "call", "subscript",
		"parenthesized_expression", "list", "dictionary", "set", "tuple",
		"list_comprehension", "dictionary_comprehension", "set_comprehension",
		"string", "integer", "float", "true", "false", "none",
	}
	pyTemplates = []lang.PostfixTemplate{
		{Trigger: "if", Body: "if EXPR:\n\t$0", Detail: "if EXPR: …"},
		{Trigger: "len", Body: "len(EXPR)", Detail: "len(EXPR)"},
	}
)

func regTreeGo(t *testing.T) string {
	t.Helper()
	lang.Register(lang.Language{
		ID:               "pftreego",
		Extensions:       []string{"pftreego"},
		Grammar:          highlight.NewGrammar(ts.NewLanguage(tsgo.Language()), "(package_clause) @keyword"),
		Postfix:          goTemplates,
		PostfixExprNodes: goExprNodes,
	})
	return "/x/main.pftreego"
}

func regTreePy(t *testing.T) string {
	t.Helper()
	lang.Register(lang.Language{
		ID:               "pftreepy",
		Extensions:       []string{"pftreepy"},
		Grammar:          highlight.NewGrammar(ts.NewLanguage(tspy.Language()), "(module) @none"),
		Postfix:          pyTemplates,
		PostfixExprNodes: pyExprNodes,
	})
	return "/x/main.pftreepy"
}

// TestExpressionBeforeBrokenGoTree is the core case from #1913: every input
// below is invalid Go at the caret, and the expression still has to come out.
func TestExpressionBeforeBrokenGoTree(t *testing.T) {
	path := regTreeGo(t)
	cases := []struct {
		name string
		body string // the single statement line, without its leading tab
		col  int    // rune column of the caret on that line (1 = after the tab)
		want string
	}{
		{name: "identifier", body: "err.", col: 5, want: "err"},
		{name: "partial trigger", body: "err.ni", col: 7, want: "err"},
		{name: "call", body: "foo(bar).", col: 10, want: "foo(bar)"},
		{name: "call with partial trigger", body: "foo(bar).if", col: 12, want: "foo(bar)"},
		{name: "assignment right-hand side", body: "x := foo(bar).", col: 15, want: "foo(bar)"},
		{name: "index chain", body: "a.b[0].", col: 8, want: "a.b[0]"},
		{name: "not the whole return statement", body: "return foo(bar).", col: 17, want: "foo(bar)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := []string{"package main", "", "func f() {", "\t" + tc.body, "}"}
			expr, dot, ok := ExpressionBefore(path, lines, 3, tc.col, goExprNodes)
			if !ok {
				t.Fatalf("no expression found in %q", tc.body)
			}
			if expr != tc.want {
				t.Errorf("expr = %q, want %q", expr, tc.want)
			}
			if got := []rune(lines[3])[dot]; got != '.' {
				t.Errorf("dot col %d points at %q", dot, string(got))
			}
		})
	}
}

func TestExpressionBeforeGoUnicodeColumns(t *testing.T) {
	path := regTreeGo(t)
	// The ä is 2 bytes but 1 rune: byte↔rune conversion must hold in both
	// directions or the expression comes out shifted.
	lines := []string{"package main", "", "func f() {", "\ts := \"ä\"; s.", "}"}
	expr, _, ok := ExpressionBefore(path, lines, 3, 13, goExprNodes)
	if !ok || expr != "s" {
		t.Fatalf("expr = %q, ok = %v, want \"s\"", expr, ok)
	}
}

func TestExpressionBeforePythonBrokenTree(t *testing.T) {
	path := regTreePy(t)
	cases := []struct{ body, want string }{
		{body: "x.", want: "x"},
		{body: "foo(bar).", want: "foo(bar)"},
		{body: "y = foo(bar).", want: "foo(bar)"},
		{body: "items[0].", want: "items[0]"},
	}
	for _, tc := range cases {
		lines := []string{"def f():", "    " + tc.body}
		expr, _, ok := ExpressionBefore(path, lines, 1, len([]rune("    "+tc.body)), pyExprNodes)
		if !ok {
			t.Errorf("no expression found in %q", tc.body)
			continue
		}
		if expr != tc.want {
			t.Errorf("%q: expr = %q, want %q", tc.body, expr, tc.want)
		}
	}
}

func TestPythonTemplatesExpand(t *testing.T) {
	path := regTreePy(t)
	s := feed(path, "def f():\n    items.\n")
	items, err := s.Complete(context.Background(), complete.Request{Path: path, Line: 1, Col: 10, Char: "."})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, it := range items {
		got[it.Label] = it.InsertText
	}
	if got["if"] != "if items:\n\t$0" {
		t.Errorf("if expansion = %q", got["if"])
	}
	if got["len"] != "len(items)" {
		t.Errorf("len expansion = %q", got["len"])
	}
}

// TestExpressionEndingAtNoKinds guards the neutral cases of the highlight
// helper itself: no kinds, no grammar, a line outside the buffer.
func TestExpressionEndingAtNeutral(t *testing.T) {
	path := regTreeGo(t)
	lines := []string{"package main", "", "func f() {", "\terr.", "}"}
	if _, ok := highlight.ExpressionEndingAt(path, lines, 3, 4, nil); ok {
		t.Error("no kinds must yield no node")
	}
	if _, ok := highlight.ExpressionEndingAt(path, lines, 9, 0, goExprNodes); ok {
		t.Error("a line past the buffer must yield no node")
	}
	if _, ok := highlight.ExpressionEndingAt("/x/main.unknownpfext", lines, 3, 4, goExprNodes); ok {
		t.Error("a path without a grammar must yield no node")
	}
}
