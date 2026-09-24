package langshell

import (
	"reflect"
	"testing"

	"ike/internal/lang"
)

func TestCompleteStatement(t *testing.T) {
	block := func(head string, tail ...string) lang.StatementCompletion {
		return lang.StatementCompletion{Head: head, Body: true, Tail: tail}
	}
	for _, tc := range []struct {
		line string
		want lang.StatementCompletion
		ok   bool
	}{
		{"if [ -f x ]", block("if [ -f x ]; then", "fi"), true},
		{"if [ -f x ];", block("if [ -f x ]; then", "fi"), true},
		{"  if grep -q x f", block("  if grep -q x f; then", "fi"), true},
		{"elif [ -d x ]", block("elif [ -d x ]; then"), true},
		{"else", block("else"), true},
		{"for f in *.go", block("for f in *.go; do", "done"), true},
		{"while read -r line", block("while read -r line; do", "done"), true},
		{"until ping -c1 host", block("until ping -c1 host; do", "done"), true},
		{"case $x", block("case $x in", "esac"), true},
		{"case \"$1\"", block("case \"$1\" in", "esac"), true},
		{"main()", block("main() {", "}"), true},
		{"function main", block("function main {", "}"), true},
		{"function main()", block("function main() {", "}"), true},
		{"my-func ()", block("my-func () {", "}"), true},
		// Already complete headers only open the body.
		{"if [ -f x ]; then", block("if [ -f x ]; then", "fi"), true},
		{"if [ -f x ];then", block("if [ -f x ];then", "fi"), true},
		{"for f in *.go; do", block("for f in *.go; do", "done"), true},
		{"case $x in", block("case $x in", "esac"), true},
		{"main() {", block("main() {", "}"), true},
		{"main(){", block("main(){", "}"), true},
		// Word boundaries: a loop over $undo is not finished by "do".
		{"for f in $undo", block("for f in $undo; do", "done"), true},
		{"if $then", block("if $then; then", "fi"), true},
		// Nothing to complete.
		{"echo hi", lang.StatementCompletion{}, false},
		{"x=1", lang.StatementCompletion{}, false},
		{"format=1", lang.StatementCompletion{}, false},
		{"# if x", lang.StatementCompletion{}, false},
		{"", lang.StatementCompletion{}, false},
	} {
		got, ok := toolchain{}.CompleteStatement(tc.line)
		if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: got %+v, %v; want %+v, %v", tc.line, got, ok, tc.want, tc.ok)
		}
	}
}

func TestStatementCompleterRegistered(t *testing.T) {
	if !lang.SupportsStatementCompletion("shell") {
		t.Fatal("shell does not support statement completion")
	}
}
