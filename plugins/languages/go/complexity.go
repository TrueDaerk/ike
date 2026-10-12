package langgo

import "ike/internal/lang"

// goComplexity is the Go complexity spec (Roadmap 0550, #2914, #2915): the
// tree-sitter-go node kinds the complexity engine measures by. Every kind is
// verified against a parse in complexity_test.go.
//
//   - Units: function_declaration and method_declaration carry their name in
//     the `name` field; a func_literal is an anonymous unit of its own. The
//     engine qualifies a method by its `receiver` type (S.handle).
//   - Branches: if and the one `for` kind (three-clause, range and
//     condition-only loops all parse as for_statement). Go has no
//     conditional expression.
//   - Selectors / arms: switch (expression_switch_statement), type switch
//     (type_switch_statement) and select (select_statement) count once for
//     cognitive; their non-default arms (expression_case, type_case,
//     communication_case) carry the CCN. default_case is its own kind and not
//     an arm.
//   - Else: Go's else is a bare block in the if_statement's `alternative`
//     field, and else-if a nested if_statement there — no kind to list, the
//     engine reads the field.
//   - Boolean operators: binary_expression's `operator` field, && and ||.
//   - Jumps: goto, and break / continue when they name a label.
//   - Calls: call_expression, callee in `function` (recursion check).
var goComplexity = &lang.ComplexitySpec{
	FunctionNodes: map[string]string{
		"function_declaration": "name",
		"method_declaration":   "name",
		"func_literal":         "",
	},
	ParamsField: "parameters",
	Branch:      []string{"if_statement", "for_statement"},
	Selector:    []string{"expression_switch_statement", "type_switch_statement", "select_statement"},
	Arms:        []string{"expression_case", "type_case", "communication_case"},
	Nesting:     []string{"func_literal"},
	BoolOps:     []string{"&&", "||"},
	Jumps:       []string{"goto_statement", "break_statement", "continue_statement"},
	Calls:       map[string]string{"call_expression": "function"},
}
