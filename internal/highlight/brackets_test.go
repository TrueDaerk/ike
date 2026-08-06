package highlight

import (
	"testing"

	"ike/internal/bracket"
	"ike/internal/lang"
)

// captureAt returns the capture of the single-cell span at (line, col), or "".
func captureAt(spans []Span, line, col int) string {
	for _, s := range spans {
		if s.Line == line && s.StartCol == col && s.EndCol == col+1 {
			return s.Capture
		}
	}
	return ""
}

// TestBracketSpansDepthCapture (#1628): a pair renders in its depth's rainbow
// slot, both halves alike.
func TestBracketSpansDepthCapture(t *testing.T) {
	l := lang.Language{ID: "test", LineComment: "//"}
	spans := bracketSpans(l, []string{"f(g[x])"}, nil)
	for _, c := range []struct {
		col  int
		want string
	}{{1, RainbowCapture(0)}, {3, RainbowCapture(1)}, {5, RainbowCapture(1)}, {6, RainbowCapture(0)}} {
		if got := captureAt(spans, 0, c.col); got != c.want {
			t.Errorf("capture at col %d = %q want %q", c.col, got, c.want)
		}
	}
}

// TestBracketSpansUnmatched (#1628): a bracket with no partner takes the error
// capture instead of a rainbow slot.
func TestBracketSpansUnmatched(t *testing.T) {
	spans := bracketSpans(lang.Language{ID: "test"}, []string{"f(x"}, nil)
	if got := captureAt(spans, 0, 1); got != bracket.Unmatched {
		t.Fatalf("capture = %q want %q", got, bracket.Unmatched)
	}
}

// TestBracketSpansRespectToggle (#789): rainbow off means no bracket spans at
// all — including the unmatched ones.
func TestBracketSpansRespectToggle(t *testing.T) {
	SetRainbow(false)
	defer SetRainbow(true)
	if spans := bracketSpans(lang.Language{ID: "test"}, []string{"f(x)"}, nil); spans != nil {
		t.Fatalf("spans = %v want none while rainbow is off", spans)
	}
}

// TestBracketSpansUseGrammarMask (#1628): when the grammar produced spans, its
// string/comment captures decide what the scanner skips — the quote
// heuristics stay out of it. Here the grammar calls the whole line a string,
// so no bracket pairs.
func TestBracketSpansUseGrammarMask(t *testing.T) {
	parsed := []Span{{Line: 0, StartCol: 0, EndCol: 8, Capture: "string.special"}}
	if spans := bracketSpans(lang.Language{ID: "test"}, []string{"f(g[x])"}, parsed); spans != nil {
		t.Fatalf("spans = %v want none inside a masked string", spans)
	}
}

// TestBracketSpansFallBackToHeuristics (#1628): with no grammar spans (a
// CGO-free build, a language without a grammar) the language's own comment
// syntax masks the scan instead.
func TestBracketSpansFallBackToHeuristics(t *testing.T) {
	l := lang.Language{ID: "test", LineComment: "#"}
	spans := bracketSpans(l, []string{"a: [1] # (unclosed"}, nil)
	if got := captureAt(spans, 0, 3); got != RainbowCapture(0) {
		t.Errorf("bracket outside the comment = %q want a rainbow slot", got)
	}
	if got := captureAt(spans, 0, 9); got != "" {
		t.Errorf("bracket inside the comment = %q want none", got)
	}
}

// TestProseLanguagesAreNotScanned (#1628): prose keeps its punctuation. A
// paragraph with a lone "(" is not a broken bracket, so markdown, plain text,
// logs and separator-delimited tables are left alone entirely — depth colours
// included, which also keeps the csv column rainbow (#1589) unclouded.
func TestProseLanguagesAreNotScanned(t *testing.T) {
	lines := []string{"See (below and [a link](x)"}
	for _, id := range []string{"markdown", "text", "log", "csv"} {
		if spans := bracketSpans(lang.Language{ID: id}, lines, nil); spans != nil {
			t.Errorf("%s: spans = %v want none", id, spans)
		}
	}
	if spans := bracketSpans(lang.Language{ID: "json"}, lines, nil); spans == nil {
		t.Error("a structural language must still be scanned")
	}
}

// TestMaskedCaptures (#1628): sub-captures follow their head, unrelated
// captures do not mask.
func TestMaskedCaptures(t *testing.T) {
	for _, c := range []string{"string", "string.escape", "comment", "comment.doc", "char"} {
		if !masked(c) {
			t.Errorf("%q must mask the bracket scan", c)
		}
	}
	for _, c := range []string{"keyword", "constant", "stringify", "punctuation"} {
		if masked(c) {
			t.Errorf("%q must not mask the bracket scan", c)
		}
	}
}
