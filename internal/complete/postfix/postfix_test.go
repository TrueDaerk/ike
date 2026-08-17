package postfix

import (
	"context"
	"strings"
	"testing"

	"ike/internal/complete"
	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// postfix_test.go covers the parts that hold with or without cgo: the token
// fallback for expression detection, template expansion into completion items
// and the enable switch. The Tree-sitter path — the widest expression node
// ending at the dot in a mid-typing broken tree — is exercised against real
// grammars in tree_cgo_test.go.

// goTemplates mirrors the shape the Go plugin registers, without pulling the
// plugin (and its cgo grammar) into this package's tests.
var goTemplates = []lang.PostfixTemplate{
	{Trigger: "if", Body: "if EXPR {\n\t$0\n}", Detail: "if EXPR { … }"},
	{Trigger: "nil", Body: "if EXPR == nil {\n\t$0\n}", Detail: "if EXPR == nil { … }"},
	{Trigger: "err", Body: "if EXPR != nil {\n\t$0\n}", Detail: "if EXPR != nil { … }", ErrorLike: true},
	{Trigger: "ret", Body: "return EXPR", Detail: "return EXPR"},
}

// regLang registers a private language carrying the templates but no grammar,
// so ExpressionBefore takes the token fallback.
func regLang(t *testing.T, ext string, templates []lang.PostfixTemplate) string {
	t.Helper()
	lang.Register(lang.Language{ID: "pf" + ext, Extensions: []string{ext}, Postfix: templates})
	return "/x/main." + ext
}

// feed builds a source holding text for path, as the engine's change events do.
func feed(path, text string) *Source {
	s := New(nil)
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: path, Text: text})
	return s
}

func TestExpressionBeforeFallback(t *testing.T) {
	cases := []struct {
		name string
		line string
		col  int
		want string
		ok   bool
	}{
		{name: "bare identifier", line: "\terr.", col: 5, want: "err", ok: true},
		{name: "partial trigger typed", line: "\terr.ni", col: 7, want: "err", ok: true},
		{name: "call chain", line: "\tfoo(bar).", col: 10, want: "foo(bar)", ok: true},
		{name: "member chain", line: "\ta.b.c.", col: 7, want: "a.b.c", ok: true},
		{name: "index expression", line: "\tm[\"k\"].", col: 8, want: "m[\"k\"]", ok: true},
		{name: "nested call", line: "\tf(g(1), h).", col: 12, want: "f(g(1), h)", ok: true},
		{name: "stops at operator", line: "\ta + b.", col: 7, want: "b", ok: true},
		{name: "stops at open paren", line: "\tfoo(bar.", col: 9, want: "bar", ok: true},
		{name: "no dot", line: "\terr", col: 4, ok: false},
		{name: "float literal", line: "\tx := 1.", col: 8, ok: false},
		{name: "range operator", line: "\ta..", col: 4, ok: false},
		{name: "leading dot", line: "\t.", col: 2, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, dot, ok := ExpressionBefore("/x/main.nogrammar", []string{tc.line}, 0, tc.col, nil)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (expr %q)", ok, tc.ok, expr)
			}
			if !ok {
				return
			}
			if expr != tc.want {
				t.Errorf("expr = %q, want %q", expr, tc.want)
			}
			if got := []rune(tc.line)[dot]; got != '.' {
				t.Errorf("dot col %d points at %q, want '.'", dot, string(got))
			}
		})
	}
}

func TestExpressionBeforeOutOfRange(t *testing.T) {
	if _, _, ok := ExpressionBefore("/x/main.nogrammar", []string{"err."}, 3, 4, nil); ok {
		t.Error("a line past the buffer must not yield an expression")
	}
	// A column past the line end clamps instead of panicking.
	if expr, _, ok := ExpressionBefore("/x/main.nogrammar", []string{"err."}, 0, 99, nil); !ok || expr != "err" {
		t.Errorf("clamped column: expr = %q, ok = %v", expr, ok)
	}
}

func TestErrorLike(t *testing.T) {
	for _, s := range []string{"err", "Err", "myErr", "readError", "read_error", "e.err", "f().err"} {
		if !ErrorLike(s) {
			t.Errorf("%q must read as an error value", s)
		}
	}
	for _, s := range []string{"x", "server", "terror", "buffer", "errands", "foo(bar)"} {
		if ErrorLike(s) {
			t.Errorf("%q must not read as an error value", s)
		}
	}
}

func TestCompleteOffersTemplates(t *testing.T) {
	path := regLang(t, "pfgo", goTemplates)
	s := feed(path, "func f() {\n\terr.\n}\n")
	items, err := s.Complete(context.Background(), complete.Request{Path: path, Line: 1, Col: 5, Char: "."})
	if err != nil {
		t.Fatal(err)
	}
	byLabel := map[string]ilsp.CompletionItem{}
	for _, it := range items {
		byLabel[it.Label] = it
	}
	for _, want := range []string{"if", "nil", "err", "ret"} {
		if _, ok := byLabel[want]; !ok {
			t.Fatalf("template %q missing from %d items", want, len(items))
		}
	}
	nilItem := byLabel["nil"]
	if nilItem.InsertText != "if err == nil {\n\t$0\n}" {
		t.Errorf("expansion = %q", nilItem.InsertText)
	}
	if nilItem.ReplacePrefix != "err." {
		t.Errorf("ReplacePrefix = %q, want %q", nilItem.ReplacePrefix, "err.")
	}
	if !nilItem.IsSnippet || nilItem.Kind != protocol.KindSnippet {
		t.Errorf("postfix items must be snippet items: %+v", nilItem)
	}
	if !strings.HasPrefix(nilItem.Detail, "postfix ") {
		t.Errorf("detail must mark the item as postfix: %q", nilItem.Detail)
	}
	if s.Priority() != ilsp.PriorityPostfix || s.Name() != ilsp.SourcePostfix {
		t.Errorf("source identity = %q/%d", s.Name(), s.Priority())
	}
	if !s.TriggerChar(".") || s.TriggerChar("x") {
		t.Error("the source must claim the dot and nothing else")
	}
}

func TestCompleteWrapsCompoundExpression(t *testing.T) {
	path := regLang(t, "pfgo2", goTemplates)
	s := feed(path, "func f() {\n\tfoo(bar).\n}\n")
	items, err := s.Complete(context.Background(), complete.Request{Path: path, Line: 1, Col: 10, Char: "."})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Label != "if" {
			continue
		}
		if it.InsertText != "if foo(bar) {\n\t$0\n}" {
			t.Fatalf("expansion = %q, want the whole call wrapped", it.InsertText)
		}
		if it.ReplacePrefix != "foo(bar)." {
			t.Fatalf("ReplacePrefix = %q", it.ReplacePrefix)
		}
		return
	}
	t.Fatal("no if template offered")
}

func TestCompleteErrorLikeGating(t *testing.T) {
	path := regLang(t, "pfgo3", goTemplates)
	s := feed(path, "func f() {\n\titems.\n}\n")
	items, err := s.Complete(context.Background(), complete.Request{Path: path, Line: 1, Col: 7, Char: "."})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Label == "err" {
			t.Fatalf("the err guard must not be offered on %q", it.ReplacePrefix)
		}
	}
}

func TestCompleteSilentCases(t *testing.T) {
	path := regLang(t, "pfgo4", goTemplates)
	// No language templates at all: another extension entirely.
	s := feed("/x/main.untouched", "err.\n")
	if items, _ := s.Complete(context.Background(), complete.Request{Path: "/x/main.untouched", Line: 0, Col: 4}); len(items) != 0 {
		t.Errorf("a language without templates must stay silent, got %d items", len(items))
	}
	// No dot before the cursor.
	s = feed(path, "func f() {\n\terr\n}\n")
	if items, _ := s.Complete(context.Background(), complete.Request{Path: path, Line: 1, Col: 4}); len(items) != 0 {
		t.Errorf("no dot must yield no items, got %d", len(items))
	}
	// Unobserved buffer.
	if items, _ := New(nil).Complete(context.Background(), complete.Request{Path: path, Line: 1, Col: 5}); len(items) != 0 {
		t.Errorf("an unseen buffer must yield no items, got %d", len(items))
	}
	// Large-file mode drops the text again.
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: path, Large: true})
	if items, _ := s.Complete(context.Background(), complete.Request{Path: path, Line: 1, Col: 5}); len(items) != 0 {
		t.Errorf("a large-file buffer must yield no items, got %d", len(items))
	}
}

func TestCompleteDisabled(t *testing.T) {
	path := regLang(t, "pfgo5", goTemplates)
	on := false
	s := New(func() bool { return on })
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: path, Text: "func f() {\n\terr.\n}\n"})
	req := complete.Request{Path: path, Line: 1, Col: 5, Char: "."}
	if items, _ := s.Complete(context.Background(), req); len(items) != 0 {
		t.Fatalf("the disabled source must stay silent, got %d items", len(items))
	}
	on = true
	if items, _ := s.Complete(context.Background(), req); len(items) == 0 {
		t.Fatal("re-enabling must take effect without re-wiring")
	}
}

func TestExpressionDollarIsEscaped(t *testing.T) {
	path := regLang(t, "pfgo6", goTemplates)
	s := feed(path, "func f() {\n\tos.Getenv(\"$HOME\").\n}\n")
	items, err := s.Complete(context.Background(), complete.Request{Path: path, Line: 1, Col: 20, Char: "."})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Label != "ret" {
			continue
		}
		// The $ must reach the buffer literally, so it is escaped for the
		// snippet expander.
		if it.InsertText != `return os.Getenv("\$HOME")` {
			t.Fatalf("expansion = %q, want the $ escaped", it.InsertText)
		}
		return
	}
	t.Fatal("no ret template offered")
}
