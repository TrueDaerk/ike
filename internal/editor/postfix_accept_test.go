package editor

// postfix_accept_test.go covers the editor half of postfix completion (#1913):
// an accepted item whose ReplacePrefix carries the `<expr>.` text rewrites that
// whole span, re-indents its block body to the cursor's line and starts the
// tabstop session — the JetBrains `err.nil` → `if err == nil { | }` shape.

import (
	"strings"
	"testing"

	ilsp "ike/internal/lsp"
)

// postfixBatch opens the popup with one postfix item, the way the local
// engine's batch does.
func postfixBatch(label, insert, prefix string, col int) ilsp.CompletionMsg {
	return ilsp.CompletionMsg{
		Line: 0, Col: col,
		Items: []ilsp.CompletionItem{{
			Label: label, FilterText: label, InsertText: insert,
			ReplacePrefix: prefix, IsSnippet: true, Source: ilsp.SourcePostfix,
			Detail: "postfix",
		}},
		Source: ilsp.SourcePostfix, SourcePriority: ilsp.PriorityPostfix,
	}
}

func TestPostfixAcceptRewritesExpression(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = send(m, key('i'))
	m = typeKeys(m, "err.")
	msg := postfixBatch("nil", "if err == nil {\n\t$0\n}", "err.", 4)
	msg.Path = m.path
	m, _ = m.Update(msg)
	if !m.CompletionOpen() {
		t.Fatal("popup must open from the postfix batch alone (no LSP)")
	}
	m = typeKeys(m, "ni") // narrows the popup; "err.ni" is in the buffer now
	m = send(m, tab())    // accept
	if got := line(m, 0); got != "if err == nil {" {
		t.Fatalf("line 0 = %q, want the whole err.ni span replaced", got)
	}
	if got := line(m, 2); got != "}" {
		t.Fatalf("line 2 = %q", got)
	}
	// $0 sits on the body line: the caret lands inside the block.
	if m.cursor.Line != 1 {
		t.Fatalf("caret on line %d, want inside the block (1)", m.cursor.Line)
	}
	if m.snippet == nil {
		t.Fatal("accepting a postfix item must start the placeholder session")
	}
}

func TestPostfixAcceptWrapsCompoundExpression(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = send(m, key('i'))
	m = typeKeys(m, "foo(bar).")
	msg := postfixBatch("if", "if foo(bar) {\n\t$0\n}", "foo(bar).", 9)
	msg.Path = m.path
	m, _ = m.Update(msg)
	m = send(m, tab())
	if got := line(m, 0); got != "if foo(bar) {" {
		t.Fatalf("line 0 = %q, want the whole call wrapped", got)
	}
}

func TestPostfixAcceptReindentsToCursorLine(t *testing.T) {
	m, _ := loaded(t, "  \n")
	m.SetCursor(0, 2)
	m = send(m, key('a'))
	m = typeKeys(m, "err.")
	msg := postfixBatch("nil", "if err == nil {\n\t$0\n}", "err.", 6)
	msg.Path = m.path
	m, _ = m.Update(msg)
	m = send(m, tab())
	if got := line(m, 0); got != "  if err == nil {" {
		t.Fatalf("line 0 = %q", got)
	}
	if got := line(m, 1); !strings.HasPrefix(got, "  ") {
		t.Fatalf("body must inherit the line indent: %q", got)
	}
	if got := line(m, 2); got != "  }" {
		t.Fatalf("line 2 = %q", got)
	}
}

// TestPostfixAcceptWithoutMatchingPrefix pins the degradation: when the buffer
// does not carry the item's ReplacePrefix any more, the accept falls back to
// the ordinary identifier-span replacement instead of eating unrelated text.
func TestPostfixAcceptWithoutMatchingPrefix(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = send(m, key('i'))
	m = typeKeys(m, "re")
	msg := postfixBatch("ret", "return err", "err.", 2)
	msg.Path = m.path
	m, _ = m.Update(msg)
	m = send(m, tab())
	if got := line(m, 0); got != "return err" {
		t.Fatalf("line 0 = %q, want the plain identifier replacement", got)
	}
}
