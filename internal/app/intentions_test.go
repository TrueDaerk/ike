package app

import (
	"strings"
	"testing"

	"ike/internal/debug"
	"ike/internal/httpfile"
)

// TestCurlCommandAtContinuations guards the multi-line gather: a curl
// command continued with trailing backslashes flattens to one parseable
// line, and a non-curl caret line yields nothing.
func TestCurlCommandAtContinuations(t *testing.T) {
	m := intentionModel(t, "notes.md",
		"curl -X POST https://api.example.com/things \\\n  -H 'Content-Type: application/json' \\\n  -d '{\"a\":1}'\nplain text\n", 0, 0)
	ed := m.activeEditor()
	cmd, end := curlCommandAt(ed, 0)
	if end != 2 {
		t.Fatalf("endLine = %d, want 2", end)
	}
	if !strings.Contains(cmd, "-H 'Content-Type: application/json'") || strings.Contains(cmd, "\\") {
		t.Fatalf("flattened command = %q", cmd)
	}
	if got, _ := curlCommandAt(ed, 3); got != "" {
		t.Fatalf("plain text line yielded %q", got)
	}
}

// TestInsertCurlAsRequestInHTTPBuffer guards the in-place conversion
// (#2020): the caret's curl line in an .http buffer becomes a named request
// block, reusing the #1994 parser.
func TestInsertCurlAsRequestInHTTPBuffer(t *testing.T) {
	m := intentionModel(t, "api.http",
		"### existing\nGET https://example.com/a\n\ncurl -X POST https://api.example.com/things -H 'X-A: 1' -d '{\"a\":1}'\n", 3, 0)
	out, _ := m.Update(InsertCurlAsRequestMsg{})
	m = out.(Model)
	text := m.activeEditor().Text()
	if strings.Contains(text, "curl -X POST") {
		t.Fatalf("curl line should be replaced, text:\n%s", text)
	}
	if !strings.Contains(text, "### POST /things") || !strings.Contains(text, "POST https://api.example.com/things") {
		t.Fatalf("request block missing, text:\n%s", text)
	}
	if !strings.Contains(text, "X-A: 1") || !strings.Contains(text, "{\"a\":1}") {
		t.Fatalf("headers/body missing, text:\n%s", text)
	}
	// The block must parse back into a runnable request.
	if _, ok := httpfile.Parse(text).RequestAt(5); !ok {
		t.Fatalf("converted block does not parse, text:\n%s", text)
	}
}

// TestInsertCurlAsRequestUnsupportedFlagNotice guards the honesty carry-over
// from #1994: flags the parser drops surface in the toast.
func TestInsertCurlAsRequestUnsupportedFlagNotice(t *testing.T) {
	m := intentionModel(t, "api.http", "curl --retry 3 https://example.com/a\n", 0, 0)
	out, _ := m.Update(InsertCurlAsRequestMsg{})
	m = out.(Model)
	if !noticed(m, "ignored flags") || !noticed(m, "--retry") {
		t.Fatalf("dropped-flag notice missing, history = %+v", m.history)
	}
}

// TestInsertCurlAsRequestScratch guards the anywhere-else path: from a
// non-.http buffer the block lands in a fresh scratch .http file, which
// opens focused.
func TestInsertCurlAsRequestScratch(t *testing.T) {
	m := intentionModel(t, "notes.md", "curl https://example.com/a\n", 0, 0)
	out, _ := m.Update(InsertCurlAsRequestMsg{})
	m = out.(Model)
	ed := m.activeEditor()
	if ed == nil || !strings.HasSuffix(ed.Path(), ".http") {
		t.Fatalf("scratch .http should be focused, path = %q", ed.Path())
	}
	if !strings.Contains(ed.Text(), "GET https://example.com/a") {
		t.Fatalf("scratch content = %q", ed.Text())
	}
}

// TestInsertCurlAsRequestReadOnlyFallsBackToScratch guards #2026: a
// read-only .http buffer (#1762) drops the in-place edit through the locked
// recorder, so the conversion takes the scratch route rather than appearing
// to do nothing.
func TestInsertCurlAsRequestReadOnlyFallsBackToScratch(t *testing.T) {
	m := intentionModel(t, "api.http", "curl https://example.com/a\n", 0, 0)
	source := m.activeEditor().Text()
	m.activeEditor().SetReadOnly(true)
	out, _ := m.Update(InsertCurlAsRequestMsg{})
	m = out.(Model)
	ed := m.activeEditor()
	if ed == nil || ed.Text() == source {
		t.Fatal("the conversion should have opened a scratch buffer")
	}
	if !strings.Contains(ed.Text(), "GET https://example.com/a") {
		t.Fatalf("scratch content = %q", ed.Text())
	}
}

// TestIntentionContextCarriesTheBreakpointFacts covers #2405: a breakpoint on
// the caret line brings the condition form into the alt+enter popup, and its
// condition decides whether the entry adds or edits.
func TestIntentionContextCarriesTheBreakpointFacts(t *testing.T) {
	m := intentionModel(t, "prog.php", "<?php\n$a = 1;\n$b = 2;\n", 1, 0)
	ed := m.activeEditor()
	key := bpKey(ed.Path())

	cx, ok := m.intentionContext()
	if !ok {
		t.Fatal("an open editor must yield a context")
	}
	if cx.BreakpointAtCaret {
		t.Fatal("no breakpoint yet")
	}

	m.bpts.Toggle(key, 1)
	cx, _ = m.intentionContext()
	if !cx.BreakpointAtCaret || cx.BreakpointConditional {
		t.Fatalf("plain breakpoint facts = %v/%v", cx.BreakpointAtCaret, cx.BreakpointConditional)
	}

	m.bpts.SetMeta(key, 1, debug.Meta{Condition: "$a > 1"})
	cx, _ = m.intentionContext()
	if !cx.BreakpointConditional {
		t.Fatal("a conditional breakpoint must be reported as one")
	}
}
