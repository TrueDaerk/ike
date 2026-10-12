package langshell

import "ike/internal/lang"

// shellComplexity is the shell complexity spec (Roadmap 0550, #2914, #2915):
// the tree-sitter-bash node kinds the complexity engine measures by. Every
// kind is verified against a parse in complexity_test.go.
//
//   - Units: function_definition carries its name in the `name` field, for
//     `f() {}`, `function f {}` and `function f() {}` alike. Shell functions
//     declare no parameters, so ParamsField is empty.
//   - Branches: if and elif (elif_clause, an unfielded child after the body),
//     for / select (both for_statement), the C-style for ((;;)) loop and
//     while / until (both while_statement).
//   - Selectors / arms: case_statement counts once for cognitive; its
//     case_item arms carry the CCN (the `*)` arm is a case_item too).
//   - Else: else_clause.
//   - Boolean operators: a field-less list — `a && b`, `a || b` — whose
//     infix token highlight.SyntaxNode.Operator carries.
//   - Jumps: none — break / continue are plain commands in the grammar.
//   - Calls: command, callee in `name` (recursion check).
var shellComplexity = &lang.ComplexitySpec{
	FunctionNodes: map[string]string{"function_definition": "name"},
	Branch: []string{
		"if_statement", "elif_clause", "for_statement", "c_style_for_statement",
		"while_statement",
	},
	Selector: []string{"case_statement"},
	Arms:     []string{"case_item"},
	Else:     []string{"else_clause"},
	BoolOps:  []string{"&&", "||"},
	Calls:    map[string]string{"command": "name"},
}
