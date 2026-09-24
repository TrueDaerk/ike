package langgo

import (
	"reflect"
	"testing"

	"ike/internal/lang"
)

func TestCompleteStatement(t *testing.T) {
	block := func(head, closer string) lang.StatementCompletion {
		return lang.StatementCompletion{Head: head, Body: true, Tail: []string{closer}}
	}
	for _, tc := range []struct {
		line string
		want lang.StatementCompletion
		ok   bool
	}{
		{"func main()", block("func main() {", "}"), true},
		{"func (m *Model) run(ctx context.Context) error", block("func (m *Model) run(ctx context.Context) error {", "}"), true},
		{"func abc(", block("func abc() {", "}"), true},
		{"\tif err != nil", block("\tif err != nil {", "}"), true},
		{"} else if x", block("} else if x {", "}"), true},
		{"} else", block("} else {", "}"), true},
		{"for i := range xs", block("for i := range xs {", "}"), true},
		{"for", block("for {", "}"), true},
		{"switch x", block("switch x {", "}"), true},
		{"select", block("select {", "}"), true},
		{"type Point struct", block("type Point struct {", "}"), true},
		{"type Shape interface", block("type Shape interface {", "}"), true},
		{"type Set[K comparable] struct", block("type Set[K comparable] struct {", "}"), true},
		{"go func()", block("go func() {", "}()"), true},
		{"defer func()", block("defer func() {", "}()"), true},
		{"f := func(a int) int", block("f := func(a int) int {", "}"), true},
		{"http.HandleFunc(\"/\", func(w http.ResponseWriter, r *http.Request)", block("http.HandleFunc(\"/\", func(w http.ResponseWriter, r *http.Request) {", "})"), true},
		// Already complete headers only open the body.
		{"func main() {", block("func main() {", "}"), true},
		{"go func() {", block("go func() {", "}()"), true},
		{"x := []int{", block("x := []int{", "}"), true},
		// No semicolon rule: other lines have nothing to complete.
		{"x := foo()", lang.StatementCompletion{}, false},
		{"return err", lang.StatementCompletion{}, false},
		{"fmt.Println(x)", lang.StatementCompletion{}, false},
		{"format := 1", lang.StatementCompletion{}, false},
		{"// if x", lang.StatementCompletion{}, false},
		{"", lang.StatementCompletion{}, false},
	} {
		got, ok := toolchain{}.CompleteStatement(tc.line)
		if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: got %+v, %v; want %+v, %v", tc.line, got, ok, tc.want, tc.ok)
		}
	}
}

func TestStatementCompleterRegistered(t *testing.T) {
	if !lang.SupportsStatementCompletion("go") {
		t.Fatal("go does not support statement completion")
	}
}
