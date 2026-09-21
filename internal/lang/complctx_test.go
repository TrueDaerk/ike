package lang

import (
	"regexp"
	"testing"
)

// TestCompletionContextAt (#2654): the position classification the editor
// feeds both completion producers — comment and string from the highlight
// capture, import lines from the language's regex, declaration positions
// from the keyword left of the current word.
func TestCompletionContextAt(t *testing.T) {
	goish := Language{
		ID:           "goish",
		DeclKeywords: []string{"func", "type", "var", "const", "package"},
		ImportLine:   regexp.MustCompile(`^\s*import\b`),
	}
	cases := []struct {
		name    string
		capture string
		line    string
		col     int
		want    CompletionContext
	}{
		{"plain code", "", "x := foo", 8, CtxCode},
		{"comment capture", "comment", "// hel", 6, CtxComment},
		{"doc comment capture", "comment.doc", "/// hel", 7, CtxComment},
		{"string capture", "string", `s := "hel`, 9, CtxString},
		{"special string capture", "string.special", "`raw`", 4, CtxString},
		{"after func", "", "func na", 7, CtxDecl},
		{"after func, empty word", "", "func ", 5, CtxDecl},
		{"after type with tab", "", "type\tNa", 7, CtxDecl},
		{"keyword case-insensitive", "", "FUNC na", 7, CtxDecl},
		{"parameter list is not a declaration", "", "func(x", 6, CtxCode},
		{"argument list is not a declaration", "", "f(a, b", 6, CtxCode},
		{"receiver closes with a bracket", "", "func (r *T) Na", 14, CtxCode},
		{"keyword not adjacent", "", "func name arg", 13, CtxCode},
		{"keyword inside a longer word", "", "defunc na", 9, CtxCode},
		{"import line", "", "import lo", 9, CtxImport},
		{"indented import line", "", "\timport lo", 10, CtxImport},
		{"import prefix of a longer word", "", "imports lo", 10, CtxCode},
		{"capture beats import", "comment", "import lo", 9, CtxComment},
		{"col past line end clamps", "", "func na", 99, CtxDecl},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CompletionContextAt(goish, c.capture, c.line, c.col); got != c.want {
				t.Fatalf("CompletionContextAt(%q, %q, %d) = %q, want %q", c.capture, c.line, c.col, got, c.want)
			}
		})
	}
}

// TestCompletionContextNoLanguageData: a language without DeclKeywords and
// ImportLine (or no language at all) only ever reports what the capture says.
func TestCompletionContextNoLanguageData(t *testing.T) {
	if got := CompletionContextAt(Language{}, "", "func na", 7); got != CtxCode {
		t.Fatalf("keyword-less language after func = %q, want code", got)
	}
	if got := CompletionContextAt(Language{}, "", "import lo", 9); got != CtxCode {
		t.Fatalf("regex-less language on import line = %q, want code", got)
	}
	if got := CompletionContextAt(Language{}, "comment", "// x", 4); got != CtxComment {
		t.Fatalf("capture without language data = %q, want comment", got)
	}
}

// TestCompletionContextPolicy pins the gating table of #2654: which
// contexts auto-open, dispatch the ordinary local sources, and ask the
// server.
func TestCompletionContextPolicy(t *testing.T) {
	cases := []struct {
		ctx                 CompletionContext
		auto, local, server bool
	}{
		{CtxCode, true, true, true},
		{CtxComment, false, false, false},
		{CtxString, false, false, true},
		{CtxDecl, false, true, true},
		{CtxImport, true, false, true},
	}
	for _, c := range cases {
		if got := c.ctx.AutoTriggers(); got != c.auto {
			t.Errorf("%q.AutoTriggers() = %v, want %v", c.ctx, got, c.auto)
		}
		if got := c.ctx.LocalSources(); got != c.local {
			t.Errorf("%q.LocalSources() = %v, want %v", c.ctx, got, c.local)
		}
		if got := c.ctx.AsksServer(); got != c.server {
			t.Errorf("%q.AsksServer() = %v, want %v", c.ctx, got, c.server)
		}
	}
}

func TestWordStart(t *testing.T) {
	cases := []struct {
		line string
		col  int
		want int
	}{
		{"func name", 9, 5},
		{"func name", 5, 5},
		{"func name", 4, 0},
		{"", 0, 0},
		{"héllo", 5, 0},
		{"a.b", 3, 2},
		{"abc", 10, 0},
	}
	for _, c := range cases {
		if got := WordStart(c.line, c.col); got != c.want {
			t.Errorf("WordStart(%q, %d) = %d, want %d", c.line, c.col, got, c.want)
		}
	}
}
