package langpython

// postfix.go is Python's postfix-completion table (#1913) — the counterpart of
// the Go one: templates offered after a dot, plus the Tree-sitter node kinds
// that count as the expression before it. EXPR marks the detected expression;
// the rest is LSP snippet syntax.

import "ike/internal/lang"

// postfixTemplates are the Python transformations. The block bodies use a
// literal tab, which the editor re-indents to the buffer's indent settings
// (spaces, EditorConfig width) on accept.
var postfixTemplates = []lang.PostfixTemplate{
	{Trigger: "if", Body: "if EXPR:\n\t$0", Detail: "if EXPR: …"},
	{Trigger: "for", Body: "for ${1:item} in EXPR:\n\t$0", Detail: "for item in EXPR: …"},
	{Trigger: "ret", Body: "return EXPR", Detail: "return EXPR"},
	{Trigger: "print", Body: "print(EXPR)", Detail: "print(EXPR)"},
	{Trigger: "not", Body: "not EXPR", Detail: "not EXPR"},
	{Trigger: "len", Body: "len(EXPR)", Detail: "len(EXPR)"},
}

// postfixExprNodes are the Tree-sitter kinds a Python postfix template may
// wrap: the attribute/call/subscript chain, parenthesized expressions and
// literals. Operator expressions are left out for the same reason as in Go —
// `a + b.if` must not wrap the sum; `(a + b).if` does.
var postfixExprNodes = []string{
	"identifier", "attribute", "call", "subscript",
	"parenthesized_expression", "list", "dictionary", "set", "tuple",
	"list_comprehension", "dictionary_comprehension", "set_comprehension",
	"string", "integer", "float", "true", "false", "none",
}
