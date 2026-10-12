package lang

// complexity.go is the per-language complexity declaration (Roadmap 0550,
// #2913, #2914): the node kinds and operator tokens the complexity engine
// (internal/complexity) needs to measure cyclomatic (McCabe) and cognitive
// (Sonar) complexity on the Tree-sitter parse IKE already has. The spec names
// grammar kinds only — no Tree-sitter import, so lang stays a leaf package;
// both algorithms live once in the engine, and a language plugin only says
// which of its nodes are functions, branches, else arms, nesting scopes,
// boolean operators and jumps.
//
// The kinds must be the grammar's real node kinds (verified by a parse, see
// each plugin's complexity_test.go), and every registered language either
// declares a spec or carries a justified row in the audit ledger
// cmd/ike/complexity_audit_test.go, whose test fails otherwise.

// ComplexitySpec declares how the complexity engine reads a language's syntax
// tree. The engine walks every node of a function; the lists below say which
// kinds count and how, so the same two algorithms serve every language:
//
//   - Cyclomatic (CCN, as lizard/gocyclo): 1 + the Branch nodes + the boolean
//     operators inside the function, nested function units excluded.
//   - Cognitive (Sonar, as gocognit/complexipy): +1 and the current nesting
//     level for each Branch node, +1 for each Else, +1 for each sequence of
//     like boolean operators, +1 for each Jump, +1 for direct recursion;
//     Branch and Nesting kinds raise the nesting level for their bodies.
type ComplexitySpec struct {
	// FunctionNodes maps each node kind that starts a measured unit to the
	// field holding its name: "name" for a function_declaration. "" marks
	// an anonymous unit (func_literal, lambda, arrow_function), which the
	// engine names "(anonymous)" plus an ordinal. Nested units are their own
	// functions and do not count toward the enclosing one; the engine
	// qualifies methods (Receiver.Name, Class::method) from the surrounding
	// tree — the spec only names the fields.
	FunctionNodes map[string]string

	// ParamsField is the field holding the parameter list on a function
	// node ("parameters"). The engine counts the list's named children, a
	// child declaring several names (Go's `a, b int`) once per `name` field.
	ParamsField string

	// Branch lists the kinds counting +1 CCN and +1 cognitive plus nesting:
	// if / elif / else-if, for, foreach, while, do, a case / when / match arm,
	// catch / except, the conditional (ternary) expression. A default arm is
	// not a branch; a language whose default is its own kind (Go's
	// default_case, PHP's default_statement) simply leaves it out. An
	// else-if form is a Branch kind sitting in the else position (the
	// `alternative` field, or inside an Else kind): the engine counts it
	// without the nesting penalty, as Sonar does.
	Branch []string

	// Else lists the kinds counting +1 cognitive only (no CCN, no nesting
	// penalty; the body still nests): the else clause. A language whose
	// else is a bare block in the parent's `alternative` field (Go) leaves
	// it empty — the engine also counts a non-Branch node in that field as
	// an else.
	Else []string

	// Nesting lists the kinds that only raise the nesting level for their
	// bodies: a nested function or lambda (which is also a FunctionNodes
	// unit), a class body.
	Nesting []string

	// BoolOps lists the boolean operator tokens, as highlight.SyntaxNode's
	// Operator carries them: "&&", "||", "and", "or", "??". Each occurrence is
	// +1 CCN; cognitive counts +1 per sequence of like operators
	// (`a && b && c` = 1, `a && b || c` = 2). Optional chaining (`?.`) is not
	// a branch and never listed.
	BoolOps []string

	// Jumps lists the kinds counting +1 cognitive: goto, and break / continue
	// — which the engine counts only when they carry a target (a label, or
	// PHP's level), a plain break being free.
	Jumps []string
}
