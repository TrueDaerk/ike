package langgo

import "ike/internal/lang"

// goComplexity is the Go complexity spec (Roadmap 0550, #2914): the
// tree-sitter-go node kinds the complexity engine measures by. Every kind is
// verified against a parse in complexity_test.go.
//
//   - Units: function_declaration and method_declaration carry their name in
//     the `name` field; a func_literal is an anonymous unit of its own.
//   - Branches: if, the one `for` kind (three-clause, range and
//     condition-only loops all parse as for_statement), the non-default arms
//     of switch (expression_case), type switch (type_case) and select
//     (communication_case). default_case is its own kind and not a branch;
//     Go has no conditional expression.
//   - Else: Go's else is a bare block in the if_statement's `alternative`
//     field, and else-if a nested if_statement there — no kind to list, the
//     engine reads the field.
//   - Boolean operators: binary_expression's `operator` field, && and ||.
//   - Jumps: goto, and break / continue when they name a label.
var goComplexity = &lang.ComplexitySpec{
	FunctionNodes: map[string]string{
		"function_declaration": "name",
		"method_declaration":   "name",
		"func_literal":         "",
	},
	ParamsField: "parameters",
	Branch: []string{
		"if_statement", "for_statement",
		"expression_case", "type_case", "communication_case",
	},
	Nesting: []string{"func_literal"},
	BoolOps: []string{"&&", "||"},
	Jumps:   []string{"goto_statement", "break_statement", "continue_statement"},
}
