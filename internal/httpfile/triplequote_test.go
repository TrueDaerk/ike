package httpfile

import (
	"encoding/json"
	"strings"
	"testing"
)

// decodeProperty expands body and returns its "property" member.
func decodeProperty(t *testing.T, body string) string {
	t.Helper()
	out := ExpandTripleQuotes(body)
	var v struct {
		Property string `json:"property"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("expanded body is not valid JSON: %v\n%s", err, out)
	}
	return v.Property
}

func TestExpandTripleQuotesDedentFromOpeningLine(t *testing.T) {
	body := strings.Join([]string{
		`{`,
		`  "property": """my multi-`,
		`                 line property`,
		`                 with triple quotes"""`,
		`}`,
	}, "\n")
	if got, want := decodeProperty(t, body), "my multi-\nline property\nwith triple quotes"; got != want {
		t.Errorf("value = %q, want %q", got, want)
	}
	want := "{\n  \"property\": \"my multi-\\nline property\\nwith triple quotes\"\n}"
	if got := ExpandTripleQuotes(body); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestExpandTripleQuotesTrim(t *testing.T) {
	body := strings.Join([]string{
		`{`,
		`  "property": """`,
		`    abc`,
		`    def`,
		`"""`,
		`}`,
	}, "\n")
	if got := decodeProperty(t, body); got != "abc\ndef" {
		t.Errorf("value = %q, want %q", got, "abc\ndef")
	}
	// whitespace after the opening quotes and before the closing ones
	body = "{\"property\": \"\"\"  \t\n    abc  \n    \"\"\"}"
	if got := decodeProperty(t, body); got != "abc" {
		t.Errorf("value = %q, want %q", got, "abc")
	}
}

func TestExpandTripleQuotesLineLeftOfMargin(t *testing.T) {
	body := strings.Join([]string{
		`{"property": """`,
		`      abc`,
		`   left`,
		`        deeper`,
		"\tx",
		`"""}`,
	}, "\n")
	want := "abc\nleft\n  deeper\nx"
	if got := decodeProperty(t, body); got != want {
		t.Errorf("value = %q, want %q", got, want)
	}
}

func TestExpandTripleQuotesTabCountsOneColumn(t *testing.T) {
	body := "{\"property\": \"\"\"\n\t\tabc\n\t\t\tdef\n\"\"\"}"
	if got := decodeProperty(t, body); got != "abc\n\tdef" {
		t.Errorf("value = %q", got)
	}
}

func TestExpandTripleQuotesEscaping(t *testing.T) {
	body := `{"property": """say "hi" \n C:\path	<tag> & """}`
	want := `say "hi" \n C:\path` + "\t<tag> & "
	if got := decodeProperty(t, body); got != want {
		t.Errorf("value = %q, want %q", got, want)
	}
	if out := ExpandTripleQuotes(body); strings.Contains(out, `\u003c`) {
		t.Errorf("HTML characters must not be escaped: %s", out)
	}
	// a run of more than three closing quotes keeps the extra ones as content
	if got := decodeProperty(t, `{"property": """ends with "quote""""}`); got != `ends with "quote"` {
		t.Errorf("value = %q", got)
	}
}

func TestExpandTripleQuotesLeavesOtherText(t *testing.T) {
	for _, body := range []string{
		`{"a": "x\"\"\"y", "b": ""}`,
		`{"a": "plain"}`,
		"{\"a\": \"\"\"unterminated\n}",
	} {
		if got := ExpandTripleQuotes(body); got != body {
			t.Errorf("ExpandTripleQuotes(%q) = %q, want unchanged", body, got)
		}
	}
	// a second literal after the first
	got := ExpandTripleQuotes(`{"a": """x""", "b": """y"""}`)
	if got != `{"a": "x", "b": "y"}` {
		t.Errorf("got %q", got)
	}
}

func TestTripleQuoteLiteralsPositions(t *testing.T) {
	lines := []string{`{"k": "a\"""", "p": """x`, `y"""}`, `"""open`}
	lits := TripleQuoteLiterals(lines)
	want := []TripleQuoteLiteral{
		{StartLine: 0, StartCol: 20, EndLine: 1, EndCol: 4, Terminated: true},
		{StartLine: 2, StartCol: 0, EndLine: 2, EndCol: 7},
	}
	if len(lits) != len(want) {
		t.Fatalf("literals = %+v, want %+v", lits, want)
	}
	for i := range want {
		if lits[i] != want[i] {
			t.Errorf("literal %d = %+v, want %+v", i, lits[i], want[i])
		}
	}
}

func TestResolveVarsExpandsTripleQuotesInJSONBody(t *testing.T) {
	src := strings.Join([]string{
		"POST https://example.com/a",
		"Content-Type: application/json; charset=utf-8",
		"",
		`{"property": """`,
		`  hello {{name}}`,
		`  "quoted"`,
		`"""}`,
	}, "\n")
	r := Parse(src).Requests[0]
	got, err := r.ResolveVars(&Vars{Lookup: func(n string) (string, bool) { return "world", n == "name" }})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"property": "hello world\n\"quoted\""}`; got.Body != want {
		t.Errorf("body = %q, want %q", got.Body, want)
	}
	if !strings.Contains(r.Body, `"""`) {
		t.Error("the parsed request must keep the literal as written")
	}
}

func TestResolveVarsKeepsTripleQuotesOutsideJSON(t *testing.T) {
	for _, ct := range []string{"text/plain", "application/x-ndjson", ""} {
		src := "POST https://example.com/a\n"
		if ct != "" {
			src += "Content-Type: " + ct + "\n"
		}
		src += "\n{\"property\": \"\"\"\n  x\n\"\"\"}"
		r := Parse(src).Requests[0]
		got, err := r.ResolveVars(&Vars{Lookup: func(string) (string, bool) { return "", false }})
		if err != nil {
			t.Fatal(err)
		}
		if got.Body != r.Body {
			t.Errorf("%q: body = %q, want it unchanged", ct, got.Body)
		}
	}
}

func TestExportsCarryExpandedBody(t *testing.T) {
	src := "POST https://example.com/a\nContent-Type: application/json\n\n{\"property\": \"\"\"\n  a \"b\"\n\"\"\"}"
	r, err := Parse(src).Requests[0].ResolveVars(&Vars{Lookup: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	if got := ExportCurl(r); !strings.Contains(got, `{"property": "a \"b\""}`) || strings.Contains(got, `"""`) {
		t.Errorf("curl export = %s", got)
	}
	if got := ExportHTTPie(r); !strings.Contains(got, `'property=a "b"'`) || strings.Contains(got, `"""`) {
		t.Errorf("httpie export = %s", got)
	}
}

func TestBodyLanguageTag(t *testing.T) {
	cases := map[string]string{
		"application/json":                "json",
		"application/json; charset=utf-8": "json",
		"application/vnd.api+json":        "json",
		"text/json":                       "json",
		"application/x-json":              "json",
		"application/x-ndjson":            "ndjson",
		"application/xml":                 "xml",
		"application/x-yaml":              "yaml",
	}
	for ct, want := range cases {
		if got, ok := BodyLanguageTag(ct); !ok || got != want {
			t.Errorf("BodyLanguageTag(%q) = %q,%v; want %q", ct, got, ok, want)
		}
	}
	for _, ct := range []string{"text/plain", "multipart/form-data", "json"} {
		if got, ok := BodyLanguageTag(ct); ok {
			t.Errorf("BodyLanguageTag(%q) = %q, want none", ct, got)
		}
	}
}
