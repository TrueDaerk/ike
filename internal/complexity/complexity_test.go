package complexity

import (
	"strings"
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// complexity_test.go tests the rules on hand-built trees, so they run without
// cgo and pin one decision each; golden_test.go covers the real grammars.

// node builds a SyntaxNode; the text of leaves is positioned by lineAt.
type node = highlight.SyntaxNode

func n(kind, field string, children ...*node) *node {
	return &node{Kind: kind, Field: field, Children: children}
}

func op(kind, operator string, left, right *node) *node {
	return &node{Kind: kind, Operator: operator, Children: []*node{left, right}}
}

// leaf is a named leaf whose Text is txt: it is placed at src byte offsets
// recorded in a tiny source builder.
type srcBuilder struct{ b strings.Builder }

func (s *srcBuilder) leaf(kind, field, txt string) *node {
	start := s.b.Len()
	s.b.WriteString(txt)
	s.b.WriteString(" ")
	return &node{Kind: kind, Field: field, StartByte: start, EndByte: start + len(txt)}
}

// goLike is a Go-shaped spec: no Else kinds, the else is a block in the
// alternative field.
var goLike = &lang.ComplexitySpec{
	FunctionNodes: map[string]string{"function_declaration": "name", "func_literal": ""},
	ParamsField:   "parameters",
	Branch:        []string{"if_statement", "for_statement"},
	Selector:      []string{"switch_statement"},
	Arms:          []string{"case"},
	Nesting:       []string{"func_literal"},
	BoolOps:       []string{"&&", "||"},
	Jumps:         []string{"goto_statement", "break_statement"},
	Calls:         map[string]string{"call_expression": "function"},
}

// tsLike is a TypeScript-shaped spec: an else_clause kind wrapping the
// else-if, a class body qualifying its methods.
var tsLike = &lang.ComplexitySpec{
	FunctionNodes: map[string]string{"function_declaration": "name", "method_definition": "name", "arrow_function": ""},
	ParamsField:   "parameters",
	Branch:        []string{"if_statement", "ternary_expression", "while_statement"},
	Else:          []string{"else_clause"},
	Nesting:       []string{"arrow_function", "class_declaration"},
	BoolOps:       []string{"&&", "||"},
}

func one(t *testing.T, fns []Function, name string) Function {
	t.Helper()
	for _, f := range fns {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no function %q in %+v", name, fns)
	return Function{}
}

// fn wraps body statements in a Go-shaped function declaration named name.
func fn(s *srcBuilder, name string, body ...*node) *node {
	return n("function_declaration", "", s.leaf("identifier", "name", name), n("parameter_list", "parameters"), n("block", "body", body...))
}

func TestElseIfChainCountsFlat(t *testing.T) {
	var s srcBuilder
	// if a {} else if b {} else { if c {} }
	root := n("source_file", "", fn(&s, "f",
		n("if_statement", "",
			s.leaf("identifier", "condition", "a"),
			n("block", "consequence"),
			n("if_statement", "alternative",
				s.leaf("identifier", "condition", "b"),
				n("block", "consequence"),
				n("block", "alternative",
					n("if_statement", "", s.leaf("identifier", "condition", "c"), n("block", "consequence")))))))
	fns := measure(root, strings.Split(s.b.String(), "\n"), goLike, comments{})
	f := one(t, fns, "f")
	// CCN 1 + 3 ifs; cognitive if(1) + else-if(1) + else(1) + nested if at level 1 (2) = 5;
	// depth 2: the else body is level 1, the nested if's body level 2.
	if f.CCN != 4 || f.Cognitive != 5 || f.Depth != 2 {
		t.Errorf("got CCN %d cog %d depth %d, want 4, 5, 2", f.CCN, f.Cognitive, f.Depth)
	}
}

func TestElseClauseWrappingIfIsElseIf(t *testing.T) {
	var s srcBuilder
	// TS: if (a) {} else if (b) {} else {}  — else_clause > if_statement > else_clause > block
	root := n("program", "", n("function_declaration", "",
		s.leaf("identifier", "name", "f"), n("formal_parameters", "parameters"),
		n("statement_block", "body",
			n("if_statement", "",
				s.leaf("identifier", "condition", "a"),
				n("statement_block", "consequence"),
				n("else_clause", "alternative",
					n("if_statement", "",
						s.leaf("identifier", "condition", "b"),
						n("statement_block", "consequence",
							n("while_statement", "", s.leaf("identifier", "condition", "w"), n("statement_block", "body"))),
						n("else_clause", "alternative", n("statement_block", ""))))))))
	f := one(t, measure(root, []string{s.b.String()}, tsLike, comments{}), "f")
	// if(1) + else-if(1) + while inside the else-if body at level 1 (2) + else(1) = 5; CCN 1 + 3.
	if f.CCN != 4 || f.Cognitive != 5 {
		t.Errorf("got CCN %d cog %d, want 4 and 5", f.CCN, f.Cognitive)
	}
}

func TestChainedTernaryNestsWhereElseKindsExist(t *testing.T) {
	var s srcBuilder
	// a ? b : c ? d : e — the nested ternary is in `alternative` but of the
	// parent's own kind, so it nests (+2) rather than reading as else-if.
	root := n("program", "", n("function_declaration", "",
		s.leaf("identifier", "name", "f"), n("formal_parameters", "parameters"),
		n("statement_block", "body",
			n("ternary_expression", "",
				s.leaf("identifier", "condition", "a"), s.leaf("identifier", "consequence", "b"),
				n("ternary_expression", "alternative",
					s.leaf("identifier", "condition", "c"), s.leaf("identifier", "consequence", "d"), s.leaf("identifier", "alternative", "e"))))))
	f := one(t, measure(root, []string{s.b.String()}, tsLike, comments{}), "f")
	if f.CCN != 3 || f.Cognitive != 3 {
		t.Errorf("got CCN %d cog %d, want 3 and 3", f.CCN, f.Cognitive)
	}
}

func TestSelectorCountsOnceArmsCarryCCN(t *testing.T) {
	var s srcBuilder
	// switch { case: if {} case: default: if {} }
	root := n("source_file", "", fn(&s, "f",
		n("switch_statement", "",
			s.leaf("identifier", "value", "x"),
			n("case", "", n("if_statement", "", s.leaf("identifier", "condition", "a"), n("block", "consequence"))),
			n("case", ""),
			n("default_case", "", n("if_statement", "", s.leaf("identifier", "condition", "b"), n("block", "consequence"))))))
	f := one(t, measure(root, []string{s.b.String()}, goLike, comments{}), "f")
	// CCN 1 + 2 arms + 2 ifs = 5; cognitive switch(1) + if at level 1 (2) + if in default at level 1 (2) = 5.
	if f.CCN != 5 || f.Cognitive != 5 || f.Depth != 2 {
		t.Errorf("got CCN %d cog %d depth %d, want 5, 5, 2", f.CCN, f.Cognitive, f.Depth)
	}
}

func TestNestedClosureExcludedFromCCNRaisesNesting(t *testing.T) {
	var s srcBuilder
	root := n("source_file", "", fn(&s, "outer",
		n("func_literal", "", n("parameter_list", "parameters"), n("block", "body",
			n("if_statement", "", s.leaf("identifier", "condition", "a"), n("block", "consequence")),
			n("for_statement", "", n("block", "body",
				n("if_statement", "", s.leaf("identifier", "condition", "b"), n("block", "consequence"))))))))
	fns := measure(root, []string{s.b.String()}, goLike, comments{})
	outer := one(t, fns, "outer")
	inner := one(t, fns, "(anonymous)#1")
	// The closure's if, for and nested if are its own CCN (1 + 3); for the
	// parent they count cognitive at levels 1, 1 and 2: 2 + 2 + 3 = 7.
	if outer.CCN != 1 || outer.Cognitive != 7 || outer.Depth != 3 {
		t.Errorf("outer CCN %d cog %d depth %d, want 1, 7, 3", outer.CCN, outer.Cognitive, outer.Depth)
	}
	if inner.CCN != 4 || inner.Cognitive != 4 || inner.Kind != KindLambda || inner.Depth != 2 {
		t.Errorf("inner = %+v, want CCN 4 cog 4 lambda depth 2", inner)
	}
}

func TestBooleanSequences(t *testing.T) {
	var s srcBuilder
	id := func(x string) *node { return s.leaf("identifier", "", x) }
	cases := []struct {
		name     string
		expr     *node
		ccn, cog int
	}{
		{"a && b && c", op("binary_expression", "&&", op("binary_expression", "&&", id("a"), id("b")), id("c")), 3, 1},
		{"a && b || c", op("binary_expression", "||", op("binary_expression", "&&", id("a"), id("b")), id("c")), 3, 2},
		{"a || b && c", op("binary_expression", "||", id("a"), op("binary_expression", "&&", id("b"), id("c"))), 3, 2},
		{"a && (b || c) && d", op("binary_expression", "&&",
			op("binary_expression", "&&", id("a"), n("parenthesized_expression", "", op("binary_expression", "||", id("b"), id("c")))), id("d")), 4, 3},
		{"a && g(b || c)", op("binary_expression", "&&", id("a"),
			n("call_expression", "", s.leaf("identifier", "function", "g"), n("argument_list", "arguments", op("binary_expression", "||", id("b"), id("c"))))), 3, 2},
		{"a + b", op("binary_expression", "+", id("a"), id("b")), 1, 0},
	}
	for _, c := range cases {
		root := n("source_file", "", fn(&s, "f", n("expression_statement", "", c.expr)))
		f := one(t, measure(root, []string{s.b.String()}, goLike, comments{}), "f")
		if f.CCN != c.ccn || f.Cognitive != c.cog {
			t.Errorf("%s: CCN %d cog %d, want %d and %d", c.name, f.CCN, f.Cognitive, c.ccn, c.cog)
		}
	}
}

func TestLongBooleanChainIsBounded(t *testing.T) {
	// A 5000-term left-leaning && chain: the walk and the flattening are
	// iterative, so this neither overflows nor recurses.
	var s srcBuilder
	var expr *node = s.leaf("identifier", "", "x")
	for i := 0; i < 5000; i++ {
		expr = op("binary_expression", "&&", expr, s.leaf("identifier", "", "y"))
	}
	root := n("source_file", "", fn(&s, "f", n("expression_statement", "", expr)))
	f := one(t, measure(root, []string{s.b.String()}, goLike, comments{}), "f")
	if f.CCN != 5001 || f.Cognitive != 1 {
		t.Errorf("CCN %d cog %d, want 5001 and 1", f.CCN, f.Cognitive)
	}
}

func TestJumpsAndRecursion(t *testing.T) {
	var s srcBuilder
	root := n("source_file", "", fn(&s, "f",
		n("break_statement", ""),                                                     // plain break: free
		n("break_statement", "", s.leaf("label_name", "", "L")),                      // labelled: +1
		n("goto_statement", "", s.leaf("label_name", "", "L")),                       // +1
		n("call_expression", "", s.leaf("identifier", "function", "f")),              // recursion +1
		n("call_expression", "", s.leaf("selector_expression", "function", "pkg.f")), // last segment: +1
		n("call_expression", "", s.leaf("identifier", "function", "g")),
		n("func_literal", "", n("parameter_list", "parameters"), n("block", "body",
			n("call_expression", "", s.leaf("identifier", "function", "f")))))) // closure calling f: +1 for f
	fns := measure(root, []string{s.b.String()}, goLike, comments{})
	if f := one(t, fns, "f"); f.Cognitive != 5 || f.CCN != 1 {
		t.Errorf("f cog %d CCN %d, want 5 and 1", f.Cognitive, f.CCN)
	}
	if c := one(t, fns, "(anonymous)#1"); c.Cognitive != 0 {
		t.Errorf("closure cog %d, want 0 (f is not its name)", c.Cognitive)
	}
}

func TestMethodNamingAndKinds(t *testing.T) {
	var s srcBuilder
	// Go receiver.
	recv := n("method_declaration", "",
		n("parameter_list", "receiver", n("parameter_declaration", "", s.leaf("identifier", "name", "s"), s.leaf("pointer_type", "type", "*Server"))),
		s.leaf("field_identifier", "name", "Handle"), n("parameter_list", "parameters"), n("block", "body"))
	goSpec := *goLike
	goSpec.FunctionNodes = map[string]string{"function_declaration": "name", "method_declaration": "name", "func_literal": ""}
	fns := measure(n("source_file", "", recv), []string{s.b.String()}, &goSpec, comments{})
	if f := fns[0]; f.Name != "Server.Handle" || f.Kind != KindMethod {
		t.Errorf("Go method = %+v, want Server.Handle method", f)
	}
	// Class body qualifies with the spec's separator; a nested function does not.
	var s2 srcBuilder
	class := n("class_declaration", "", s2.leaf("type_identifier", "name", "C"), n("class_body", "body",
		n("method_definition", "", s2.leaf("property_identifier", "name", "m"), n("formal_parameters", "parameters",
			n("required_parameter", ""), n("optional_parameter", "")),
			n("statement_block", "body",
				n("function_declaration", "", s2.leaf("identifier", "name", "inner"), n("formal_parameters", "parameters"), n("statement_block", "body")),
				n("arrow_function", "", n("formal_parameters", "parameters"), n("statement_block", "body"))))))
	php := *tsLike
	php.MethodSep = "::"
	fns = measure(n("program", "", class), []string{s2.b.String()}, &php, comments{})
	want := []struct{ name, kind string }{{"C::m", KindMethod}, {"inner", KindFunction}, {"(anonymous)#1", KindLambda}}
	if len(fns) != len(want) {
		t.Fatalf("got %+v", fns)
	}
	for i, w := range want {
		if fns[i].Name != w.name || fns[i].Kind != w.kind {
			t.Errorf("unit %d = %s %s, want %s %s", i, fns[i].Name, fns[i].Kind, w.name, w.kind)
		}
	}
	if fns[0].Params != 2 {
		t.Errorf("C::m params %d, want 2", fns[0].Params)
	}
}

func TestElseOutsideBranchDoesNotCount(t *testing.T) {
	var s srcBuilder
	// Python try / except / else: the else is not a Branch's alternative.
	spec := &lang.ComplexitySpec{
		FunctionNodes: map[string]string{"function_definition": "name"},
		Branch:        []string{"if_statement", "except_clause", "for_statement"},
		Else:          []string{"else_clause"},
	}
	root := n("module", "", n("function_definition", "", s.leaf("identifier", "name", "f"), n("block", "body",
		n("try_statement", "", n("block", "body"), n("except_clause", "", n("block", "")), n("else_clause", "", n("block", "body"))),
		n("for_statement", "", s.leaf("identifier", "left", "x"), s.leaf("identifier", "right", "xs"), n("block", "body"),
			n("else_clause", "alternative", n("block", "body"))))))
	f := one(t, measure(root, []string{s.b.String()}, spec, comments{}), "f")
	// except(1) + for(1) + the loop's else(1) = 3; the try's else is free. CCN 1 + 2.
	if f.CCN != 3 || f.Cognitive != 3 {
		t.Errorf("CCN %d cog %d, want 3 and 3", f.CCN, f.Cognitive)
	}
}

func TestNLOC(t *testing.T) {
	lines := []string{
		"func f() {",
		"",
		"	// a line comment",
		"	x := 1 // trailing comment still code",
		"	/* block",
		"	   comment */",
		"	/* one-line block */ y := 2",
		"	s := \"//not a comment\"",
		"}",
	}
	got, cmt := nloc(lines, 0, len(lines)-1, comments{line: "//", block: [2]string{"/*", "*/"}})
	if got != 5 || cmt != 3 {
		t.Errorf("nloc = %d/%d, want 5 code / 3 comment", got, cmt)
	}
	// Hash comments, no block syntax; the range is clamped.
	got, cmt = nloc([]string{"a=1", "# only", "", "b=2 # tail"}, 0, 99, comments{line: "#"})
	if got != 2 || cmt != 1 {
		t.Errorf("nloc(#) = %d/%d, want 2/1", got, cmt)
	}
	// No comment syntax at all: only blank lines are dropped.
	if got, cmt := nloc([]string{"x", "", " "}, 0, 2, comments{}); got != 1 || cmt != 0 {
		t.Errorf("nloc(none) = %d/%d, want 1/0", got, cmt)
	}
	// A block comment running into the closing-brace line: the covered
	// lines are comment, the brace line is code.
	got, cmt = nloc([]string{"f() {", "  x()", "  /* a", "  b", "  c */ }"}, 0, 4, comments{line: "//", block: [2]string{"/*", "*/"}})
	if got != 3 || cmt != 2 {
		t.Errorf("nloc(block to end) = %d/%d, want 3/2", got, cmt)
	}
}

func TestDensity(t *testing.T) {
	if d := density(0, 0); d != 0 {
		t.Errorf("density(0,0) = %v", d)
	}
	if d := density(9, 11); d != 0.55 {
		t.Errorf("density(9,11) = %v, want 0.55", d)
	}
	f := Function{CommentDensity: 0.395}
	if DensityPercent(f.CommentDensity) != 40 || !f.CommentsOver(40) || f.CommentsOver(41) || f.CommentsOver(0) {
		t.Errorf("CommentsOver mis-rounds %v", f.CommentDensity)
	}
}

func TestSummarize(t *testing.T) {
	fns := []Function{
		{NLOC: 10, CCN: 3, Cognitive: 2},
		{NLOC: 20, CCN: 15, Cognitive: 4},
		{NLOC: 5, CCN: 1, Cognitive: 16},
	}
	s := Summarize(fns, 15, 15, 0)
	want := Summary{Functions: 3, NLOC: 35, CCNSum: 19, CCNAvg: 19.0 / 3, CCNMax: 15, CogSum: 22, CogAvg: 22.0 / 3, CogMax: 16, OverThreshold: 2, Excess: 1}
	if s != want {
		t.Errorf("Summarize = %+v, want %+v", s, want)
	}
	if s := Summarize(nil, 15, 15, 0); s != (Summary{}) {
		t.Errorf("Summarize(nil) = %+v, want zero", s)
	}
	// A non-positive threshold disables that check.
	if s := Summarize(fns, 0, 15, 0); s.OverThreshold != 1 {
		t.Errorf("OverThreshold with CCN check off = %d, want 1", s.OverThreshold)
	}
	// Comment density: summed lines, aggregate ratio, its own threshold.
	cm := []Function{
		{NLOC: 6, CommentLines: 4, CommentDensity: 0.4},
		{NLOC: 9, CommentLines: 1, CommentDensity: 0.1},
	}
	s = Summarize(cm, 0, 0, 40)
	if s.CommentLines != 5 || s.CommentDensity != 0.25 || s.OverThreshold != 1 {
		t.Errorf("Summarize(comments) = %+v, want 5 lines, 0.25, 1 over", s)
	}
	if s := Summarize(cm, 0, 0, 0); s.OverThreshold != 0 {
		t.Errorf("cmtWarn 0 must disable the check, got %d over", s.OverThreshold)
	}
}

func TestExcess(t *testing.T) {
	// Straddling both thresholds: 20/30 is 5 + 15 over, 15/15 sits on them
	// (over, no excess), 3/18 only above the cognitive one.
	fns := []Function{
		{CCN: 20, Cognitive: 30},
		{CCN: 15, Cognitive: 15},
		{CCN: 3, Cognitive: 18},
	}
	if e := fns[0].Excess(15, 15); e != 20 {
		t.Errorf("Excess(20/30) = %d, want 20", e)
	}
	if s := Summarize(fns, 15, 15, 0); s.Excess != 23 || s.OverThreshold != 3 {
		t.Errorf("Summarize straddling = excess %d over %d, want 23 / 3", s.Excess, s.OverThreshold)
	}
	below := []Function{{CCN: 14, Cognitive: 14}, {CCN: 1, Cognitive: 0}}
	if s := Summarize(below, 15, 15, 0); s.Excess != 0 {
		t.Errorf("Summarize below = excess %d, want 0", s.Excess)
	}
	// A non-positive threshold disables its term.
	if e := fns[0].Excess(0, 15); e != 15 {
		t.Errorf("Excess with the CCN check off = %d, want 15", e)
	}
}

func TestLastSegment(t *testing.T) {
	cases := map[string]string{
		"f": "f", "pkg.F": "F", "s.handle": "handle", "$this->m": "m", "self::m": "m",
		"$obj?->m": "m", " spaced ": "spaced", "$fn": "fn", "": "",
	}
	for in, want := range cases {
		if got := lastSegment(in); got != want {
			t.Errorf("lastSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCountParams(t *testing.T) {
	list := n("parameter_list", "parameters",
		n("parameter_declaration", "", n("identifier", "name"), n("identifier", "name"), n("type_identifier", "type")), // a, b int
		n("parameter_declaration", "", n("type_identifier", "type")),                                                   // unnamed
		n("variadic_parameter_declaration", "", n("identifier", "name"), n("type_identifier", "type")))
	if got := countParams(list); got != 4 {
		t.Errorf("countParams = %d, want 4", got)
	}
	if got := countParams(nil); got != 0 {
		t.Errorf("countParams(nil) = %d, want 0", got)
	}
}

func TestMeasureEmptyTreeAndNoUnits(t *testing.T) {
	fns := measure(n("source_file", ""), nil, goLike, comments{})
	if fns == nil || len(fns) != 0 {
		t.Errorf("measure of an empty tree = %#v, want an empty, non-nil slice", fns)
	}
	// Structures outside any unit count nothing and do not panic.
	var s srcBuilder
	root := n("source_file", "", n("if_statement", "", s.leaf("identifier", "condition", "a"), n("block", "consequence",
		op("binary_expression", "&&", s.leaf("identifier", "", "x"), s.leaf("identifier", "", "y")))))
	if fns := measure(root, []string{s.b.String()}, goLike, comments{}); len(fns) != 0 {
		t.Errorf("top-level code measured %+v", fns)
	}
}
