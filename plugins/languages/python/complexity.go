package langpython

import "ike/internal/lang"

// pythonComplexity is the Python complexity spec (Roadmap 0550, #2914,
// #2915): the tree-sitter-python node kinds the complexity engine measures
// by. Every kind is verified against a parse in complexity_test.go.
//
//   - Units: function_definition (def and async def alike) carries its name
//     in the `name` field; a lambda is an anonymous unit of its own. A def
//     directly inside a class body is qualified Class.method.
//   - Branches: if and elif (elif_clause is its own kind, in the
//     if_statement's `alternative` field), for, while, except and the
//     conditional expression (`a if c else b`). finally_clause is not a
//     branch.
//   - Selectors / arms: match_statement counts once for cognitive; its
//     case_clause arms carry the CCN (`case _:` is a case_clause too — Python
//     has no default kind).
//   - Else: else_clause (of if, for and while; a try's else is not a Branch's
//     alternative and does not count).
//   - Nesting: a lambda and a class body.
//   - Boolean operators: boolean_operator's `operator` field, `and` / `or`.
//   - Jumps: none — Python has no goto and no labelled break.
//   - Calls: call, callee in `function` (recursion check).
var pythonComplexity = &lang.ComplexitySpec{
	FunctionNodes: map[string]string{
		"function_definition": "name",
		"lambda":              "",
	},
	ParamsField: "parameters",
	Branch: []string{
		"if_statement", "elif_clause", "for_statement", "while_statement",
		"except_clause", "conditional_expression",
	},
	Selector: []string{"match_statement"},
	Arms:     []string{"case_clause"},
	Else:     []string{"else_clause"},
	Nesting:  []string{"lambda", "class_definition"},
	BoolOps:  []string{"and", "or"},
	Calls:    map[string]string{"call": "function"},
}
