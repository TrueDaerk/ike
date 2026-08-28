package filterexpr

// The shared filter core's own cases (#2156). Each pane's dialect is tested
// in its own package; here the vocabulary itself is pinned: the tokenizer,
// the schema's validation and error wording, the round trip, and the two
// match helpers every pane gates its rows with.

import (
	"reflect"
	"strings"
	"testing"
)

// demo is a schema exercising all three field shapes: a closed vocabulary
// with an alias, a repeatable free-form field, and a value-documented one.
var demo = Schema{Fields: []Field{
	{Name: "severity", Aliases: []string{"sev"}, Values: []string{"error", "warning"}},
	{Name: "file", ValueDoc: "a path or glob"},
	{Name: "scope", Values: []string{"file", "project"}},
}}

func TestParseSplitsTermsAndMatchText(t *testing.T) {
	q, err := demo.Parse(`severity:error file:"src dir/*.go" missing return`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []Term{{Field: "severity", Value: "error"}, {Field: "file", Value: "src dir/*.go"}}
	if !reflect.DeepEqual(q.Terms, want) {
		t.Fatalf("terms = %+v, want %+v", q.Terms, want)
	}
	if q.Match != "missing return" {
		t.Fatalf("match = %q", q.Match)
	}
	if q.Empty() {
		t.Fatal("a filled query must not report Empty")
	}
}

func TestParseAliasCanonicalizes(t *testing.T) {
	q, err := demo.Parse("SEV:warning")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := q.Value("severity"); got != "warning" {
		t.Fatalf("severity = %q, want warning (alias + case folded)", got)
	}
}

func TestValuesDedupesAndValueTakesTheLast(t *testing.T) {
	q, err := demo.Parse("file:a file:b file:a scope:file scope:project")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := q.Values("file"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("Values = %v, want [a b]", got)
	}
	if got := q.Value("scope"); got != "project" {
		t.Fatalf("Value = %q, want the last one", got)
	}
	if !q.Has("file") || q.Has("severity") {
		t.Fatal("Has must report exactly the named fields")
	}
}

func TestNonQualifierTokensStayMatchText(t *testing.T) {
	// A colon inside quotes, a leading colon and a non-letter name are all
	// plain text — the rule the Issues pane's live input applies.
	q, err := demo.Parse(`"file:x" :y fix#2:tests`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(q.Terms) != 0 {
		t.Fatalf("terms = %+v, want none", q.Terms)
	}
	if q.Match != "file:x :y fix#2:tests" {
		t.Fatalf("match = %q", q.Match)
	}
}

func TestParseErrors(t *testing.T) {
	for _, tt := range []struct {
		name, expr, want string
	}{
		{"unknown field", "author:me", `unknown qualifier "author:"`},
		{"unknown field names the schema", "author:me", "use severity:, file: or scope:"},
		{"bad vocabulary value", "severity:fatal", "severity: wants error, warning"},
		{"bad value keeps the written alias", "sev:fatal", "sev: wants error, warning"},
		{"empty documented value", "file:", "file: wants a path or glob"},
		{"unterminated quote", `file:"src`, "unterminated quote"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := demo.Parse(tt.expr)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse(%q) err = %v, want it to mention %q", tt.expr, err, tt.want)
			}
		})
	}
}

func TestEmptySchemaTakesEverythingAsMatchText(t *testing.T) {
	var none Schema
	q, err := none.Parse("hello world")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if q.Match != "hello world" {
		t.Fatalf("match = %q", q.Match)
	}
	if _, err := none.Parse("file:x"); err == nil || !strings.Contains(err.Error(), "no qualifiers") {
		t.Fatalf("err = %v, want the no-qualifier hint", err)
	}
}

func TestNamesAndHint(t *testing.T) {
	want := []string{"severity:", "sev:", "file:", "scope:"}
	if got := demo.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names = %v, want %v", got, want)
	}
	if got := demo.Hint(); got != "use severity:, file: or scope:" {
		t.Fatalf("Hint = %q", got)
	}
	one := Schema{Fields: []Field{{Name: "tag"}}}
	if got := one.Hint(); got != "use tag:" {
		t.Fatalf("single-field hint = %q", got)
	}
}

// Format is Parse's inverse: everything Parse accepts comes back as an
// expression that parses to the same query.
func TestFormatRoundTrips(t *testing.T) {
	for _, expr := range []string{
		"",
		"severity:error",
		`file:"src dir/*.go"`,
		"file:a file:b crash here",
		"scope:file plain text",
	} {
		q, err := demo.Parse(expr)
		if err != nil {
			t.Fatalf("parse %q: %v", expr, err)
		}
		back, err := demo.Parse(Format(q))
		if err != nil {
			t.Fatalf("re-parse %q: %v", Format(q), err)
		}
		if !reflect.DeepEqual(q, back) {
			t.Fatalf("%q round-tripped to %+v, want %+v", expr, back, q)
		}
	}
}

func TestQuote(t *testing.T) {
	if got := Quote("plain"); got != "plain" {
		t.Fatalf("Quote(plain) = %q", got)
	}
	if got := Quote(`good first issue`); got != `"good first issue"` {
		t.Fatalf("Quote = %q", got)
	}
}

func TestMatchText(t *testing.T) {
	if _, ok := MatchText("", "anything"); !ok {
		t.Fatal("an empty pattern must pass everything")
	}
	if _, ok := MatchText("undef", "undefined: foo"); !ok {
		t.Fatal("a subsequence must match")
	}
	if _, ok := MatchText("zzz", "undefined: foo"); ok {
		t.Fatal("a non-subsequence must not match")
	}
}

func TestMatchPath(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		// A meta-free pattern is a case-insensitive substring.
		{"app", "internal/app/app.go", true},
		{"APP", "internal/app/app.go", true},
		{"cmd", "internal/app/app.go", false},
		// A glob matches the whole path…
		{"internal/**/*.go", "internal/app/app.go", true},
		// …or any tail of it, so "*.go" reads as "somewhere below".
		{"*.go", "internal/app/app.go", true},
		{"*.ts", "internal/app/app.go", false},
		{"app/*.go", "internal/app/app.go", true},
		// An empty value never widens back to everything.
		{"", "internal/app/app.go", false},
	}
	for _, c := range cases {
		if got := MatchPath(c.pattern, c.path); got != c.want {
			t.Errorf("MatchPath(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestTokenizeKeepsQuotedRunsTogether(t *testing.T) {
	toks, err := Tokenize(`a "b c" d:"e f"`)
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	if len(toks) != 3 || toks[1].Text != "b c" {
		t.Fatalf("tokens = %+v", toks)
	}
	name, val, ok := toks[2].Qualifier()
	if !ok || name != "d" || val != "e f" {
		t.Fatalf("qualifier = %q/%q/%v", name, val, ok)
	}
}
