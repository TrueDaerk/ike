package langphp

import "ike/internal/lang"

// phpComplexity is the PHP complexity spec (Roadmap 0550, #2914): the
// tree-sitter-php node kinds the complexity engine measures by. Every kind
// is verified against a parse in complexity_test.go.
//
//   - Units: function_definition and method_declaration carry their name in
//     the `name` field; an anonymous_function (`function () use () {}`) and
//     an arrow_function (`fn() =>`) are anonymous units of their own.
//   - Branches: if and elseif (else_if_clause, in the if_statement's
//     `alternative` field; `else if` is an if_statement inside an
//     else_clause), for, foreach, while, do, a switch case
//     (case_statement; default_statement is not a branch), catch, the
//     ternary (conditional_expression, `?:` included) and a match arm
//     (match_conditional_expression; match_default_expression is not).
//   - Else: else_clause.
//   - Nesting: the anonymous function kinds and a class body.
//   - Boolean operators: binary_expression's `operator` field — &&, ||, the
//     keyword forms `and` / `or`, and null coalescing `??`.
//   - Jumps: goto, and break / continue when they carry a level (`break 2`).
var phpComplexity = &lang.ComplexitySpec{
	FunctionNodes: map[string]string{
		"function_definition": "name",
		"method_declaration":  "name",
		"anonymous_function":  "",
		"arrow_function":      "",
	},
	ParamsField: "parameters",
	Branch: []string{
		"if_statement", "else_if_clause", "for_statement", "foreach_statement",
		"while_statement", "do_statement", "case_statement", "catch_clause",
		"conditional_expression", "match_conditional_expression",
	},
	Else:    []string{"else_clause"},
	Nesting: []string{"anonymous_function", "arrow_function", "class_declaration"},
	BoolOps: []string{"&&", "||", "and", "or", "??"},
	Jumps:   []string{"goto_statement", "break_statement", "continue_statement"},
}
