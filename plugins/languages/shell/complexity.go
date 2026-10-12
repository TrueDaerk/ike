package langshell

import "ike/internal/lang"

// shellComplexity is the shell complexity spec (Roadmap 0550, #2914): the
// tree-sitter-bash node kinds the complexity engine measures by. Every kind
// is verified against a parse in complexity_test.go.
//
//   - Units: function_definition carries its name in the `name` field, for
//     `f() {}`, `function f {}` and `function f() {}` alike. Shell functions
//     declare no parameters, so ParamsField is empty.
//   - Branches: if and elif (elif_clause), for / select (both
//     for_statement), the C-style for ((;;)) loop, while / until (both
//     while_statement) and a case arm (case_item).
//   - Else: else_clause.
//   - Boolean operators: a field-less list — `a && b`, `a || b` — whose
//     infix token highlight.SyntaxNode.Operator carries.
//   - Jumps: none — break / continue are plain commands in the grammar.
var shellComplexity = &lang.ComplexitySpec{
	FunctionNodes: map[string]string{"function_definition": "name"},
	Branch: []string{
		"if_statement", "elif_clause", "for_statement", "c_style_for_statement",
		"while_statement", "case_item",
	},
	Else:    []string{"else_clause"},
	BoolOps: []string{"&&", "||"},
}
