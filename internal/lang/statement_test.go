package lang

import (
	"reflect"
	"testing"
)

func TestCloseBrackets(t *testing.T) {
	for _, tc := range []struct{ in, openers, want string }{
		{"def abc(", "([{", "def abc()"},
		{"def abc()", "([{", "def abc()"},
		{"f([1, (2", "([{", "f([1, (2)])"},
		{"x = {'a': [1", "([{", "x = {'a': [1]}"},
		{"foo(\"(\"", "([{", "foo(\"(\")"},
		{"foo('\\'(', [", "([{", "foo('\\'(', [])"},
		{"f(x) {", "([", "f(x) {"},
		{"x = [1, {", "([", "x = [1, {]"},
		{"x)", "([{", "x)"},
	} {
		if got := CloseBrackets(tc.in, tc.openers); got != tc.want {
			t.Errorf("CloseBrackets(%q, %q) = %q, want %q", tc.in, tc.openers, got, tc.want)
		}
	}
}

func TestTopLevelIndex(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"def f(x: int) -> dict[str, int]:", 31},
		{"if x:", 4},
		{"x = {'a': 1}", -1},
		{"s = ':'", -1},
		{"case {'a': 1}: pass", 13},
	} {
		if got := TopLevelIndex(tc.in, ':'); got != tc.want {
			t.Errorf("TopLevelIndex(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestLeadingKeyword(t *testing.T) {
	words := []string{"if", "else", "function", "match"}
	skip := []string{"public", "static", "async"}
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"if x", true},
		{"if(x)", true},
		{"if", true},
		{"else:", true},
		{"else{", true},
		{"} else", true},
		{"public static function foo()", true},
		{"async function foo()", true},
		{"iffy = 1", false},
		{"match = re.match(x)", false},
		{"match x", true},
		{"match == 1", true},
		{"public", false},
		{"$x = 1", false},
	} {
		if got := LeadingKeyword(tc.in, skip, words); got != tc.want {
			t.Errorf("LeadingKeyword(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestAssignedExpression(t *testing.T) {
	words := []string{"function", "match"}
	for _, tc := range []struct {
		in      string
		wantRHS string
		want    bool
	}{
		{"$x = match($y)", "match($y)", true},
		{"const f = function ()", "function ()", true},
		{"return function ()", "function ()", true},
		{"return $x", "", false},
		{"if ($x == function)", "", false},
		{"$x = foo()", "", false},
	} {
		rhs, ok := AssignedExpression(tc.in, words)
		if ok != tc.want || rhs != tc.wantRHS {
			t.Errorf("AssignedExpression(%q) = %q, %v; want %q, %v", tc.in, rhs, ok, tc.wantRHS, tc.want)
		}
	}
}

func TestBraceCompletion(t *testing.T) {
	header := func(stmt string) (string, bool) {
		if LeadingKeyword(stmt, nil, []string{"if", "function"}) {
			return "}", true
		}
		return "", false
	}
	for _, tc := range []struct {
		in         string
		semicolons bool
		want       StatementCompletion
		ok         bool
	}{
		{"  function query()", true, StatementCompletion{Head: "  function query() {", Body: true, Tail: []string{"}"}}, true},
		{"if (x", true, StatementCompletion{Head: "if (x) {", Body: true, Tail: []string{"}"}}, true},
		{"if (x) {  ", true, StatementCompletion{Head: "if (x) {", Body: true, Tail: []string{"}"}}, true},
		{"$x = foo()", true, StatementCompletion{Head: "$x = foo();"}, true},
		{"$x = foo(", true, StatementCompletion{Head: "$x = foo();"}, true},
		{"$x = foo();", true, StatementCompletion{Head: "$x = foo();"}, true},
		{"}", true, StatementCompletion{Head: "}"}, true},
		{"case 1:", true, StatementCompletion{Head: "case 1:"}, true},
		{"// note", true, StatementCompletion{}, false},
		{"   ", true, StatementCompletion{}, false},
		{"x := foo()", false, StatementCompletion{}, false},
		{"x := []int{", false, StatementCompletion{Head: "x := []int{", Body: true, Tail: []string{"}"}}, true},
		// A header nested in an unclosed call keeps the call open and closes
		// it on the closing line.
		{"foo(function ($x)", true, StatementCompletion{Head: "foo(function ($x) {", Body: true, Tail: []string{"});"}}, true},
		{"foo([function ($x", true, StatementCompletion{Head: "foo([function ($x) {", Body: true, Tail: []string{"}]);"}}, true},
		{"foo(function ($x) {", true, StatementCompletion{Head: "foo(function ($x) {", Body: true, Tail: []string{"});"}}, true},
		{"handle(\"/\", function (w)", false, StatementCompletion{Head: "handle(\"/\", function (w) {", Body: true, Tail: []string{"})"}}, true},
	} {
		got, ok := BraceCompletion(tc.in, header, tc.semicolons)
		if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("BraceCompletion(%q) = %+v, %v; want %+v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

type stmtToolchain struct{}

func (stmtToolchain) Detect(string) (map[string]any, bool) { return nil, false }
func (stmtToolchain) CompleteStatement(line string) (StatementCompletion, bool) {
	if line == "nothing" {
		return StatementCompletion{}, false
	}
	return StatementCompletion{Head: line + ":", Body: true}, true
}

type stmtPlainToolchain struct{}

func (stmtPlainToolchain) Detect(string) (map[string]any, bool) { return nil, false }

func TestCompleteStatementDispatch(t *testing.T) {
	Register(Language{ID: "stmt-yes", Extensions: []string{"stmtyes"}, Toolchain: stmtToolchain{}})
	Register(Language{ID: "stmt-plain", Extensions: []string{"stmtplain"}, Toolchain: stmtPlainToolchain{}})
	Register(Language{ID: "stmt-none", Extensions: []string{"stmtnone"}})

	c, supported, ok := CompleteStatement("stmt-yes", "if x")
	if !supported || !ok || c.Head != "if x:" || !c.Body {
		t.Fatalf("stmt-yes: %+v supported=%v ok=%v", c, supported, ok)
	}
	if _, supported, ok := CompleteStatement("stmt-yes", "nothing"); !supported || ok {
		t.Fatalf("nothing to complete: supported=%v ok=%v", supported, ok)
	}
	for _, id := range []string{"stmt-plain", "stmt-none", "", "no-such-language"} {
		if _, supported, ok := CompleteStatement(id, "if x"); supported || ok {
			t.Errorf("%q: supported=%v ok=%v, want neither", id, supported, ok)
		}
		if SupportsStatementCompletion(id) {
			t.Errorf("%q: SupportsStatementCompletion = true", id)
		}
	}
	if !SupportsStatementCompletion("stmt-yes") {
		t.Error("stmt-yes: SupportsStatementCompletion = false")
	}
}
