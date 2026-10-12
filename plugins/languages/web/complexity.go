package langweb

import "ike/internal/lang"

// tsComplexity is the TypeScript / JavaScript complexity spec (Roadmap 0550,
// #2914): the node kinds of the TSX grammar — the one grammar serving every
// JS/TS dialect — that the complexity engine measures by. Every kind is
// verified against a parse in complexity_test.go. HTML and CSS have no
// functions and declare no spec.
//
//   - Units: function_declaration, generator_function_declaration and
//     method_definition carry their name in the `name` field, as do a named
//     function_expression / generator_function (an unnamed one has no name
//     child and is anonymous); an arrow_function is anonymous.
//   - Branches: if (`else if` is an if_statement inside an else_clause),
//     for, for-in / for-of (both for_in_statement), while, do, a switch
//     case (switch_case; switch_default is not a branch), catch and the
//     ternary (ternary_expression). Optional chaining `?.` is not a branch.
//   - Else: else_clause.
//   - Nesting: the anonymous function kinds and a class body.
//   - Boolean operators: binary_expression's `operator` field — &&, || and
//     null coalescing `??`.
//   - Jumps: break / continue when they name a label; there is no goto.
var tsComplexity = &lang.ComplexitySpec{
	FunctionNodes: map[string]string{
		"function_declaration":           "name",
		"generator_function_declaration": "name",
		"method_definition":              "name",
		"function_expression":            "name",
		"generator_function":             "name",
		"arrow_function":                 "",
	},
	ParamsField: "parameters",
	Branch: []string{
		"if_statement", "for_statement", "for_in_statement", "while_statement",
		"do_statement", "switch_case", "catch_clause", "ternary_expression",
	},
	Else:    []string{"else_clause"},
	Nesting: []string{"arrow_function", "function_expression", "generator_function", "class_declaration"},
	BoolOps: []string{"&&", "||", "??"},
	Jumps:   []string{"break_statement", "continue_statement"},
}
