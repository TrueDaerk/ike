package langgo

// postfix.go is Go's postfix-completion table (#1913): the templates
// `internal/complete/postfix` offers after a dot, and the Tree-sitter node
// kinds that count as the expression before it. `lang.ExprPlaceholder` (EXPR)
// marks where the detected expression goes; the rest is LSP snippet syntax, so
// `$1`/`$0` place the caret and a literal tab is re-indented to the buffer's
// indent settings on accept.

import "ike/internal/lang"

// postfixTemplates are the Go transformations. `err` (and camel/snake-case
// names ending in it) additionally gets `.err`, the `if err != nil` guard that
// would be nonsense on any other expression.
var postfixTemplates = []lang.PostfixTemplate{
	{Trigger: "if", Body: "if EXPR {\n\t$0\n}", Detail: "if EXPR { … }"},
	{Trigger: "nil", Body: "if EXPR == nil {\n\t$0\n}", Detail: "if EXPR == nil { … }"},
	{Trigger: "err", Body: "if EXPR != nil {\n\t$0\n}", Detail: "if EXPR != nil { … }", ErrorLike: true},
	{Trigger: "for", Body: "for ${1:i} := 0; ${1:i} < len(EXPR); ${1:i}++ {\n\t$0\n}", Detail: "for i := 0; i < len(EXPR); i++ { … }"},
	{Trigger: "range", Body: "for ${1:_}, ${2:v} := range EXPR {\n\t$0\n}", Detail: "for _, v := range EXPR { … }"},
	{Trigger: "ret", Body: "return EXPR", Detail: "return EXPR"},
	{Trigger: "var", Body: "${1:x} := EXPR", Detail: "x := EXPR"},
	{Trigger: "print", Body: "fmt.Println(EXPR)", Detail: "fmt.Println(EXPR)"},
}

// postfixExprNodes are the Tree-sitter kinds a Go postfix template may wrap.
// Deliberately the member-access chain plus literals — not binary/unary
// expressions: on `a + b.if` the widest node ending at the dot would be the
// whole sum, which is not what the dot reads as. A user who wants the sum
// writes `(a + b).if`, and parenthesized_expression covers it.
var postfixExprNodes = []string{
	"identifier", "field_identifier", "package_identifier",
	"selector_expression", "call_expression", "index_expression",
	"slice_expression", "parenthesized_expression", "type_assertion_expression",
	"type_conversion_expression", "composite_literal", "func_literal",
	"int_literal", "float_literal", "interpreted_string_literal",
	"raw_string_literal", "rune_literal", "true", "false", "nil",
}
