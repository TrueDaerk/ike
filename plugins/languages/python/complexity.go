package langpython

import "ike/internal/lang"

// pythonComplexity is the Python complexity spec (Roadmap 0550, #2914): the
// tree-sitter-python node kinds the complexity engine measures by. Every
// kind is verified against a parse in complexity_test.go.
//
//   - Units: function_definition (def and async def alike) carries its name
//     in the `name` field; a lambda is an anonymous unit of its own.
//   - Branches: if and elif (elif_clause is its own kind, in the
//     if_statement's `alternative` field), for, while, except, the
//     conditional expression (`a if c else b`) and a match arm
//     (case_clause). else_clause / finally_clause are not branches.
//   - Else: else_clause (of if, for, while and try alike).
//   - Nesting: a lambda and a class body.
//   - Boolean operators: boolean_operator's `operator` field, `and` / `or`.
//   - Jumps: none — Python has no goto and no labelled break.
var pythonComplexity = &lang.ComplexitySpec{
	FunctionNodes: map[string]string{
		"function_definition": "name",
		"lambda":              "",
	},
	ParamsField: "parameters",
	Branch: []string{
		"if_statement", "elif_clause", "for_statement", "while_statement",
		"except_clause", "conditional_expression", "case_clause",
	},
	Else:    []string{"else_clause"},
	Nesting: []string{"lambda", "class_definition"},
	BoolOps: []string{"and", "or"},
}
