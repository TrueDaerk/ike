package jinja

import (
	"strings"
	"testing"
	"unicode/utf8"

	"ike/internal/lang"
)

// tok is a token as text + class, the shape the tokenizer tests compare.
type tok struct {
	text  string
	class Class
}

// tokens scans text and returns its tokens as text + class.
func tokens(text string) []tok {
	t := Scan(strings.Split(text, "\n"))
	out := make([]tok, len(t.Tokens))
	for i, k := range t.Tokens {
		out[i] = tok{t.src[k.Start:k.End], k.Class}
	}
	return out
}

func equalToks(t *testing.T, text string, want []tok) {
	t.Helper()
	got := tokens(text)
	if len(got) != len(want) {
		t.Fatalf("Scan(%q):\n got %v\nwant %v", text, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Scan(%q) token %d = %v, want %v", text, i, got[i], want[i])
		}
	}
}

func TestExpression(t *testing.T) {
	equalToks(t, `a: {{ user.name | default('x', 1.5) }}`, []tok{
		{"{{", Delimiter},
		{"user", Variable},
		{".", Punctuation},
		{"name", Property},
		{"|", Operator},
		{"default", Filter},
		{"(", Bracket},
		{"'x'", String},
		{",", Punctuation},
		{"1.5", Number},
		{")", Bracket},
		{"}}", Delimiter},
	})
}

func TestStatement(t *testing.T) {
	equalToks(t, `{% for item in items if item is not none %}`, []tok{
		{"{%", Delimiter},
		{"for", Keyword},
		{"item", Variable},
		{"in", Keyword},
		{"items", Variable},
		{"if", Keyword},
		{"item", Variable},
		{"is", Keyword},
		{"not", Keyword},
		{"none", Filter}, // a test name
		{"%}", Delimiter},
	})
	equalToks(t, `{% set n = x ** 2 == true %}`, []tok{
		{"{%", Delimiter},
		{"set", Keyword},
		{"n", Variable},
		{"=", Operator},
		{"x", Variable},
		{"**", Operator},
		{"2", Number},
		{"==", Operator},
		{"true", Constant},
		{"%}", Delimiter},
	})
	equalToks(t, `{% import 'm.j2' as m with context %}`, []tok{
		{"{%", Delimiter},
		{"import", Keyword},
		{"'m.j2'", String},
		{"as", Keyword},
		{"m", Variable},
		{"with", Keyword},
		{"context", Keyword},
		{"%}", Delimiter},
	})
}

func TestComment(t *testing.T) {
	equalToks(t, "x {# a {{ b }} %} #} y", []tok{{"{# a {{ b }} %} #}", CommentText}})
	equalToks(t, "{#- trimmed -#}", []tok{{"{#- trimmed -#}", CommentText}})
}

func TestWhitespaceControl(t *testing.T) {
	equalToks(t, `{%- if a -%}{{- b -}}{%+ endif +%}`, []tok{
		{"{%-", Delimiter}, {"if", Keyword}, {"a", Variable}, {"-%}", Delimiter},
		{"{{-", Delimiter}, {"b", Variable}, {"-}}", Delimiter},
		{"{%+", Delimiter}, {"endif", Keyword}, {"+%}", Delimiter},
	})
}

// TestQuotedCloser: a "}}" or "%}" inside a string, or a dict literal's
// braces, never close the block.
func TestQuotedCloser(t *testing.T) {
	equalToks(t, `{{ "}}" ~ '%}' ~ "a\"}}" }} z`, []tok{
		{"{{", Delimiter},
		{`"}}"`, String}, {"~", Operator},
		{`'%}'`, String}, {"~", Operator},
		{`"a\"}}"`, String},
		{"}}", Delimiter},
	})
	equalToks(t, `{{ {'a': {'b': 1}} }}`, []tok{
		{"{{", Delimiter},
		{"{", Bracket}, {"'a'", String}, {":", Punctuation},
		{"{", Bracket}, {"'b'", String}, {":", Punctuation}, {"1", Number}, {"}", Bracket},
		{"}", Bracket},
		{"}}", Delimiter},
	})
}

// TestRaw: the body of a raw block is inner-language text — no tokens, no
// mask — while its two tags are statements.
func TestRaw(t *testing.T) {
	text := "{% raw %}{{ not jinja }}{% if %}{% endraw %}{{ v }}"
	equalToks(t, text, []tok{
		{"{%", Delimiter}, {"raw", Keyword}, {"%}", Delimiter},
		{"{%", Delimiter}, {"endraw", Keyword}, {"%}", Delimiter},
		{"{{", Delimiter}, {"v", Variable}, {"}}", Delimiter},
	})
	got := Scan([]string{text}).Mask([]string{text})[0]
	want := "         {{ not jinja }}{% if %}            xxxxxxx"
	if got != want {
		t.Errorf("Mask raw:\n got %q\nwant %q", got, want)
	}
	// Whitespace control on the raw tags, and an endraw lookalike name.
	equalToks(t, "{%- raw -%}{% endrawx %}{%- endraw -%}", []tok{
		{"{%-", Delimiter}, {"raw", Keyword}, {"-%}", Delimiter},
		{"{%-", Delimiter}, {"endraw", Keyword}, {"-%}", Delimiter},
	})
	// An unterminated raw block owns the rest of the text.
	equalToks(t, "{% raw %}{{ a }}", []tok{{"{%", Delimiter}, {"raw", Keyword}, {"%}", Delimiter}})
}

// TestUnterminated: unterminated tags, comments and strings end at the end
// of the text without panicking.
func TestUnterminated(t *testing.T) {
	for _, text := range []string{
		"{{", "{%", "{#", "{{ a", "{% if a\nb: 1", "{# open\ncomment", `{{ "open`,
		"{{-", "{%+", "{", "x {", "{{ a -", "{{ {", "{{ 1e", "{{ 1.",
	} {
		tpl := Scan(strings.Split(text, "\n"))
		if len(tpl.Blocks) == 0 && strings.Contains(text, "{{") {
			t.Errorf("Scan(%q) found no block", text)
		}
		for _, b := range tpl.Blocks {
			if b.Closed {
				t.Errorf("Scan(%q) block %+v reported closed", text, b)
			}
			if b.End != len(tpl.src) {
				t.Errorf("Scan(%q) block ends at %d, want end of text %d", text, b.End, len(tpl.src))
			}
		}
		_ = tpl.Spans()
		_ = tpl.Mask(strings.Split(text, "\n"))
	}
}

// TestMaskKeepsOffsets: every masked line keeps its byte length and rune
// count, statements and comments turn blank, expressions turn into x-runs,
// and text outside blocks is untouched.
func TestMaskKeepsOffsets(t *testing.T) {
	lines := []string{
		"env:",
		"{% if prod -%}",
		"  debug: {{ dbg | default(false) }} {# é note #}",
		"  name: {{ 'café' }}",
		"{%- endif %}",
		"multi: {{ a",
		"  | b }}",
	}
	got := Scan(lines).Mask(lines)
	want := []string{
		"env:",
		"              ",
		"  debug: " + strings.Repeat("x", 26) + " " + strings.Repeat(" ", 12),
		"  name: xxxxxxxéxxxx",
		"            ",
		"multi: xxxx",
		"xxxxxxxx",
	}
	if len(got) != len(lines) {
		t.Fatalf("Mask returned %d lines, want %d", len(got), len(lines))
	}
	for i := range lines {
		if len(got[i]) != len(lines[i]) || utf8.RuneCountInString(got[i]) != utf8.RuneCountInString(lines[i]) {
			t.Errorf("line %d: length changed: %q -> %q", i, lines[i], got[i])
		}
		if i != 2 && got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
	// The é inside the comment masks to a two-byte blank.
	if !strings.HasPrefix(got[2], want[2][:36]) || strings.TrimRight(got[2][36:], "  ") != "" {
		t.Errorf("line 2: got %q", got[2])
	}
	// Text without a block comes back as is.
	plain := []string{"a: 1", "b: {not jinja}"}
	if out := Scan(plain).Mask(plain); &out[0] != &plain[0] {
		t.Error("Mask copied a text without blocks")
	}
}

// TestSpansRuneColumns: spans use rune columns and split per line.
func TestSpansRuneColumns(t *testing.T) {
	lines := []string{"é {{ x }}", "{# a", "b #}"}
	got := Scan(lines).Spans()
	want := []lang.Span{
		{Line: 0, StartCol: 2, EndCol: 4, Capture: "punctuation.special"},
		{Line: 0, StartCol: 5, EndCol: 6, Capture: "variable"},
		{Line: 0, StartCol: 7, EndCol: 9, Capture: "punctuation.special"},
		{Line: 1, StartCol: 0, EndCol: 4, Capture: "comment"},
		{Line: 2, StartCol: 0, EndCol: 4, Capture: "comment"},
	}
	if len(got) != len(want) {
		t.Fatalf("Spans = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("span %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if Scan(nil).Spans() != nil || Scan([]string{""}).Spans() != nil {
		t.Error("empty text yields spans")
	}
}

// TestMaskPath: only Jinja-suffixed paths are masked.
func TestMaskPath(t *testing.T) {
	lines := []string{"a: {{ b }}"}
	if got := MaskPath("x.yml.j2", lines)[0]; got != "a: xxxxxxx" {
		t.Errorf("MaskPath(.yml.j2) = %q", got)
	}
	for _, p := range []string{"x.yml", "x.tmpl", "x.yml.tpl", "motd"} {
		if got := MaskPath(p, lines)[0]; got != lines[0] {
			t.Errorf("MaskPath(%s) = %q, want unchanged", p, got)
		}
	}
}

// FuzzScan: no input panics, and masking always keeps every line's byte
// length and rune count.
func FuzzScan(f *testing.F) {
	for _, s := range []string{"{{ a }}", "{% raw %}x{% endraw %}", "{# c", `{{ "}}`, "é{{ ü }}\n{%- x -%}"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		lines := strings.Split(s, "\n")
		tpl := Scan(lines)
		_ = tpl.Spans()
		got := tpl.Mask(lines)
		if len(got) != len(lines) {
			t.Fatalf("Mask changed the line count")
		}
		for i := range lines {
			if len(got[i]) != len(lines[i]) || utf8.RuneCountInString(got[i]) != utf8.RuneCountInString(lines[i]) {
				t.Fatalf("line %d length changed: %q -> %q", i, lines[i], got[i])
			}
		}
	})
}

// TestExpressionTokens (#3061): a bare expression tokenizes like the body of
// {{ }}, and a "}}" in it closes nothing.
func TestExpressionTokens(t *testing.T) {
	text := "x is not defined and y }}"
	var got []tok
	for _, k := range ExpressionTokens(text) {
		got = append(got, tok{text[k.Start:k.End], k.Class})
	}
	want := []tok{
		{"x", Variable}, {"is", Keyword}, {"not", Keyword}, {"defined", Filter},
		{"and", Keyword}, {"y", Variable}, {"}", Bracket}, {"}", Bracket},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestBlockSpans (#3061): BlockSpans keeps only the tokens of one block.
func TestBlockSpans(t *testing.T) {
	tpl := Scan([]string{"a {{ b }}", "{# c #} {% d %}"})
	if len(tpl.Blocks) != 3 {
		t.Fatalf("blocks = %d, want 3", len(tpl.Blocks))
	}
	got := tpl.BlockSpans(tpl.Blocks[1])
	want := lang.Span{Line: 1, StartCol: 0, EndCol: 7, Capture: "comment"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("BlockSpans(comment) = %v, want [%v]", got, want)
	}
	if n := len(tpl.BlockSpans(tpl.Blocks[2])); n != 3 {
		t.Errorf("BlockSpans(statement) = %d spans, want 3", n)
	}
	if line, col := tpl.Position(tpl.Blocks[2].Start); line != 1 || col != 8 {
		t.Errorf("Position = %d:%d, want 1:8", line, col)
	}
}
