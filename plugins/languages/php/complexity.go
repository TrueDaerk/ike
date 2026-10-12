package langphp

import "ike/internal/lang"

// phpComplexity is the PHP complexity spec (Roadmap 0550, #2914, #2915): the
// tree-sitter-php node kinds the complexity engine measures by. Every kind
// is verified against a parse in complexity_test.go.
//
//   - Units: function_definition and method_declaration carry their name in
//     the `name` field; an anonymous_function (`function () use () {}`) and
//     an arrow_function (`fn() =>`) are anonymous units of their own. A
//     method is qualified Class::method (MethodSep).
//   - Branches: if and elseif (else_if_clause, in the if_statement's
//     `alternative` field; `else if` is an if_statement inside an
//     else_clause), for, foreach, while, do, catch and the ternary
//     (conditional_expression, `?:` included).
//   - Selectors / arms: switch_statement and match_expression count once for
//     cognitive; their arms (case_statement, match_conditional_expression)
//     carry the CCN. default_statement / match_default_expression are not
//     arms.
//   - Else: else_clause.
//   - Nesting: the anonymous function kinds and the class-like bodies
//     (class, trait, interface, enum), which also qualify their methods.
//   - Boolean operators: binary_expression's `operator` field — &&, ||, the
//     keyword forms `and` / `or`, and null coalescing `??`.
//   - Jumps: goto, and break / continue when they carry a level (`break 2`).
//   - Calls: a plain call's callee is its `function`; a member, nullsafe or
//     scoped call names the method in `name`.
var phpComplexity = &lang.ComplexitySpec{
	FunctionNodes: map[string]string{
		"function_definition": "name",
		"method_declaration":  "name",
		"anonymous_function":  "",
		"arrow_function":      "",
	},
	ParamsField: "parameters",
	MethodSep:   "::",
	Branch: []string{
		"if_statement", "else_if_clause", "for_statement", "foreach_statement",
		"while_statement", "do_statement", "catch_clause", "conditional_expression",
	},
	Selector: []string{"switch_statement", "match_expression"},
	Arms:     []string{"case_statement", "match_conditional_expression"},
	Else:     []string{"else_clause"},
	Nesting: []string{
		"anonymous_function", "arrow_function",
		"class_declaration", "trait_declaration", "interface_declaration", "enum_declaration",
	},
	BoolOps: []string{"&&", "||", "and", "or", "??"},
	Jumps:   []string{"goto_statement", "break_statement", "continue_statement"},
	Calls: map[string]string{
		"function_call_expression":        "function",
		"member_call_expression":          "name",
		"nullsafe_member_call_expression": "name",
		"scoped_call_expression":          "name",
	},
}
