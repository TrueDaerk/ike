//go:build cgo

package langgo

// postfix_test.go checks the shipped Go template set end-to-end (#1913):
// through the real registration (the package's init()), the real grammar and
// the real postfix source, so a broken template body or node-kind list fails
// here rather than in the editor.

import (
	"context"
	"testing"

	"ike/internal/complete"
	"ike/internal/complete/postfix"
	"ike/internal/host"
)

// offered returns the postfix items for the caret at the end of the given
// function-body line.
func offered(t *testing.T, body string) map[string]string {
	t.Helper()
	text := "package main\n\nfunc f() {\n\t" + body + "\n}\n"
	s := postfix.New(nil)
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: "main.go", Text: text})
	items, err := s.Complete(context.Background(), complete.Request{
		Path: "main.go", Line: 3, Col: len([]rune("\t" + body)), Char: ".",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, it := range items {
		out[it.Label] = it.InsertText
	}
	return out
}

func TestGoPostfixTemplates(t *testing.T) {
	got := offered(t, "err.")
	if got["nil"] != "if err == nil {\n\t$0\n}" {
		t.Errorf("err.nil = %q", got["nil"])
	}
	if got["if"] != "if err {\n\t$0\n}" {
		t.Errorf("err.if = %q", got["if"])
	}
	if got["err"] != "if err != nil {\n\t$0\n}" {
		t.Errorf("err.err = %q", got["err"])
	}
	if got["ret"] != "return err" {
		t.Errorf("err.ret = %q", got["ret"])
	}
	if got["print"] != "fmt.Println(err)" {
		t.Errorf("err.print = %q", got["print"])
	}
}

func TestGoPostfixWrapsCallExpression(t *testing.T) {
	got := offered(t, "foo(bar).")
	if got["if"] != "if foo(bar) {\n\t$0\n}" {
		t.Errorf("foo(bar).if = %q", got["if"])
	}
	if got["range"] != "for ${1:_}, ${2:v} := range foo(bar) {\n\t$0\n}" {
		t.Errorf("foo(bar).range = %q", got["range"])
	}
}

// TestGoPostfixErrGuardIsErrorOnly pins the ErrorLike gate: the `if x != nil`
// guard is noise on an ordinary value.
func TestGoPostfixErrGuardIsErrorOnly(t *testing.T) {
	if _, ok := offered(t, "items.")["err"]; ok {
		t.Error("items.err must not be offered")
	}
	if _, ok := offered(t, "readErr.")["err"]; !ok {
		t.Error("readErr.err must be offered")
	}
}

// TestGoPostfixIgnoresTheAssignment guards the node-kind list: on
// `x := foo(bar).` the widest node ending at the dot is the short_var_declaration,
// which must not qualify as an expression.
func TestGoPostfixIgnoresTheAssignment(t *testing.T) {
	if got := offered(t, "x := foo(bar).")["ret"]; got != "return foo(bar)" {
		t.Errorf("x := foo(bar).ret = %q, want only the call wrapped", got)
	}
}
