//go:build cgo

package langpython

// postfix_test.go is the Python counterpart of the Go template check (#1913):
// the shipped set through the real registration, grammar and source.

import (
	"context"
	"testing"

	"ike/internal/complete"
	"ike/internal/complete/postfix"
	"ike/internal/host"
)

func offered(t *testing.T, body string) map[string]string {
	t.Helper()
	text := "def f():\n    " + body + "\n"
	s := postfix.New(nil)
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: "main.py", Text: text})
	items, err := s.Complete(context.Background(), complete.Request{
		Path: "main.py", Line: 1, Col: len([]rune("    " + body)), Char: ".",
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

func TestPythonPostfixTemplates(t *testing.T) {
	got := offered(t, "items.")
	want := map[string]string{
		"if":    "if items:\n\t$0",
		"for":   "for ${1:item} in items:\n\t$0",
		"ret":   "return items",
		"print": "print(items)",
		"not":   "not items",
		"len":   "len(items)",
	}
	for label, body := range want {
		if got[label] != body {
			t.Errorf("items.%s = %q, want %q", label, got[label], body)
		}
	}
}

func TestPythonPostfixWrapsCall(t *testing.T) {
	if got := offered(t, "foo(bar).")["len"]; got != "len(foo(bar))" {
		t.Errorf("foo(bar).len = %q", got)
	}
	if got := offered(t, "y = foo(bar).")["not"]; got != "not foo(bar)" {
		t.Errorf("y = foo(bar).not = %q, want only the call wrapped", got)
	}
}
