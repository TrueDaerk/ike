package httpfile

import (
	"strings"
	"testing"
)

func TestParseSingleRequestMinimal(t *testing.T) {
	f := Parse("GET https://example.com/api\n")
	if len(f.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", f.Errors)
	}
	if len(f.Requests) != 1 {
		t.Fatalf("want 1 request, got %d", len(f.Requests))
	}
	r := f.Requests[0]
	if r.Method != "GET" || r.Target != "https://example.com/api" {
		t.Errorf("request line parsed wrong: %+v", r)
	}
	if r.Proto != DefaultProto {
		t.Errorf("want default proto %q, got %q", DefaultProto, r.Proto)
	}
	if r.Body != "" || len(r.Headers) != 0 {
		t.Errorf("want no headers/body, got %+v", r)
	}
	if r.Line != 1 || r.Index != 0 {
		t.Errorf("want line 1 index 0, got line %d index %d", r.Line, r.Index)
	}
}

func TestParseExplicitVersion(t *testing.T) {
	f := Parse("GET / HTTP/2\n")
	if len(f.Errors) != 0 || len(f.Requests) != 1 {
		t.Fatalf("errors=%v requests=%d", f.Errors, len(f.Requests))
	}
	if f.Requests[0].Proto != "HTTP/2" {
		t.Errorf("want HTTP/2, got %q", f.Requests[0].Proto)
	}
}

func TestParseHeadersAndBody(t *testing.T) {
	src := strings.Join([]string{
		"POST https://api.test/things HTTP/1.1",
		"Content-Type: application/json",
		"X-Empty:",
		"",
		`{"a": 1}`,
		"",
	}, "\n")
	f := Parse(src)
	if len(f.Errors) != 0 || len(f.Requests) != 1 {
		t.Fatalf("errors=%v requests=%d", f.Errors, len(f.Requests))
	}
	r := f.Requests[0]
	if len(r.Headers) != 2 {
		t.Fatalf("want 2 headers, got %+v", r.Headers)
	}
	if r.Headers[0] != (Header{"Content-Type", "application/json"}) {
		t.Errorf("header 0 wrong: %+v", r.Headers[0])
	}
	if r.Headers[1] != (Header{"X-Empty", ""}) {
		t.Errorf("header 1 wrong: %+v", r.Headers[1])
	}
	if r.Body != `{"a": 1}` {
		t.Errorf("body wrong: %q", r.Body)
	}
}

func TestParseMultipleRequestsWithNames(t *testing.T) {
	src := strings.Join([]string{
		"### list things",
		"GET https://api.test/things",
		"",
		"###",
		"DELETE https://api.test/things/1",
		"### create",
		"POST https://api.test/things",
		"Content-Type: application/json",
		"",
		`{"name": "x"}`,
	}, "\n")
	f := Parse(src)
	if len(f.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", f.Errors)
	}
	if len(f.Requests) != 3 {
		t.Fatalf("want 3 requests, got %d", len(f.Requests))
	}
	wantNames := []string{"list things", "", "create"}
	wantKeys := []string{"list things", "1", "create"}
	for i, r := range f.Requests {
		if r.Name != wantNames[i] {
			t.Errorf("request %d name: want %q, got %q", i, wantNames[i], r.Name)
		}
		if r.Key() != wantKeys[i] {
			t.Errorf("request %d key: want %q, got %q", i, wantKeys[i], r.Key())
		}
		if r.Index != i {
			t.Errorf("request %d index: got %d", i, r.Index)
		}
	}
	if f.Requests[2].Body != `{"name": "x"}` {
		t.Errorf("body of request 2: %q", f.Requests[2].Body)
	}
}

func TestParseComments(t *testing.T) {
	src := strings.Join([]string{
		"# file comment",
		"// slash comment",
		"GET https://api.test/",
		"# between request line and headers",
		"Accept: text/plain",
		"",
		"# this is body content, not a comment",
	}, "\n")
	f := Parse(src)
	if len(f.Errors) != 0 || len(f.Requests) != 1 {
		t.Fatalf("errors=%v requests=%d", f.Errors, len(f.Requests))
	}
	r := f.Requests[0]
	if len(r.Headers) != 1 || r.Headers[0].Name != "Accept" {
		t.Errorf("headers wrong: %+v", r.Headers)
	}
	if r.Body != "# this is body content, not a comment" {
		t.Errorf("comment inside body must be kept, got %q", r.Body)
	}
}

func TestParseCRLF(t *testing.T) {
	f := Parse("GET https://api.test/\r\nAccept: */*\r\n\r\nbody\r\n")
	if len(f.Errors) != 0 || len(f.Requests) != 1 {
		t.Fatalf("errors=%v requests=%d", f.Errors, len(f.Requests))
	}
	if f.Requests[0].Body != "body" {
		t.Errorf("body: %q", f.Requests[0].Body)
	}
}

func TestParseEmptyAndCommentOnlyBlocks(t *testing.T) {
	src := strings.Join([]string{
		"# only a comment before the first separator",
		"### a",
		"GET https://api.test/",
		"###",
		"",
		"# nothing here",
		"### b",
		"GET https://api.test/b",
	}, "\n")
	f := Parse(src)
	if len(f.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", f.Errors)
	}
	if len(f.Requests) != 2 {
		t.Fatalf("want 2 requests, got %d", len(f.Requests))
	}
}

func TestParseMalformedBlockKeepsOthers(t *testing.T) {
	src := strings.Join([]string{
		"### good",
		"GET https://api.test/",
		"### bad",
		"this is not a request line at all really",
		"### also good",
		"GET https://api.test/2",
	}, "\n")
	f := Parse(src)
	if len(f.Requests) != 2 {
		t.Fatalf("want 2 surviving requests, got %d", len(f.Requests))
	}
	if len(f.Errors) != 1 {
		t.Fatalf("want 1 error, got %v", f.Errors)
	}
	if f.Errors[0].Line != 4 {
		t.Errorf("error line: want 4, got %d", f.Errors[0].Line)
	}
	if !strings.Contains(f.Errors[0].Error(), "line 4") {
		t.Errorf("error text must carry line number: %q", f.Errors[0].Error())
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name, src, wantMsg string
		wantLine           int
	}{
		{"bad version", "GET / HTTP/x\n", "invalid HTTP version", 1},
		{"bad method", "G=T{ /\n", "invalid method", 1},
		{"bad header", "GET /\nno colon here\n", "invalid header field", 2},
		{"colon first", "GET /\n: value\n", "invalid header field", 2},
		{"too many fields", "GET / HTTP/1.1 extra\n", "invalid request line", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := Parse(c.src)
			if len(f.Requests) != 0 {
				t.Fatalf("want no requests, got %d", len(f.Requests))
			}
			if len(f.Errors) != 1 {
				t.Fatalf("want 1 error, got %v", f.Errors)
			}
			if f.Errors[0].Line != c.wantLine {
				t.Errorf("line: want %d, got %d", c.wantLine, f.Errors[0].Line)
			}
			if !strings.Contains(f.Errors[0].Msg, c.wantMsg) {
				t.Errorf("msg: want %q in %q", c.wantMsg, f.Errors[0].Msg)
			}
		})
	}
}

func TestHeaderLookup(t *testing.T) {
	r := &Request{Headers: []Header{{"Content-Type", "a"}, {"content-type", "b"}}}
	v, ok := r.Header("CONTENT-TYPE")
	if !ok || v != "a" {
		t.Errorf("want first match a, got %q ok=%v", v, ok)
	}
	if _, ok := r.Header("Authorization"); ok {
		t.Error("missing header must report ok=false")
	}
}

func lookupMap(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestSubstitute(t *testing.T) {
	env := lookupMap(map[string]string{"HOST": "api.test", "TOKEN": "s3cret", "_X1": "y"})
	cases := []struct{ in, want string }{
		{"https://${HOST}/v1", "https://api.test/v1"},
		{"Bearer {{$env TOKEN}}", "Bearer s3cret"},
		{"{{ $env TOKEN }}", "s3cret"},
		{"${_X1}", "y"},
		{"no placeholders", "no placeholders"},
		{"$HOST and {plain}", "$HOST and {plain}"}, // not placeholder syntax
		{"${HOST}${HOST}", "api.testapi.test"},
	}
	for _, c := range cases {
		got, err := Substitute(c.in, env)
		if err != nil {
			t.Errorf("%q: unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q: want %q, got %q", c.in, c.want, got)
		}
	}
}

func TestSubstituteUnresolved(t *testing.T) {
	got, err := Substitute("${A} ${B} ${A}", lookupMap(map[string]string{}))
	if err == nil {
		t.Fatal("want error for unresolved placeholders")
	}
	if got != "${A} ${B} ${A}" {
		t.Errorf("input must be returned unchanged, got %q", got)
	}
	if !strings.Contains(err.Error(), "A, B") {
		t.Errorf("error must list variables sorted once: %q", err.Error())
	}
}

func TestResolve(t *testing.T) {
	f := Parse(strings.Join([]string{
		"### login",
		"POST https://${HOST}/login",
		"Authorization: Bearer {{$env TOKEN}}",
		"",
		`{"user": "${USER_NAME}"}`,
	}, "\n"))
	if len(f.Errors) != 0 || len(f.Requests) != 1 {
		t.Fatalf("errors=%v requests=%d", f.Errors, len(f.Requests))
	}
	env := lookupMap(map[string]string{"HOST": "api.test", "TOKEN": "t", "USER_NAME": "u"})
	r, err := f.Requests[0].Resolve(env)
	if err != nil {
		t.Fatal(err)
	}
	if r.Target != "https://api.test/login" {
		t.Errorf("target: %q", r.Target)
	}
	if v, _ := r.Header("Authorization"); v != "Bearer t" {
		t.Errorf("header: %q", v)
	}
	if r.Body != `{"user": "u"}` {
		t.Errorf("body: %q", r.Body)
	}
	// Original untouched.
	if f.Requests[0].Target != "https://${HOST}/login" {
		t.Errorf("original mutated: %q", f.Requests[0].Target)
	}
}

func TestResolveUnresolvedAborts(t *testing.T) {
	f := Parse("GET https://${HOST}/x\nX-A: ${MISSING}\n")
	r, err := f.Requests[0].Resolve(lookupMap(map[string]string{"HOST": "h"}))
	if err == nil {
		t.Fatal("want error")
	}
	if r != nil {
		t.Error("resolved request must be nil on error")
	}
	if !strings.Contains(err.Error(), "MISSING") {
		t.Errorf("error must name the variable: %q", err.Error())
	}
}

func TestRequestAt(t *testing.T) {
	src := strings.Join([]string{
		"# leading comment", // 1
		"GET https://a/",    // 2
		"### second",        // 3
		"GET https://b/",    // 4
		"Accept: */*",       // 5
		"",                  // 6
		"body",              // 7
		"###",               // 8
		"GET https://c/",    // 9
	}, "\n")
	f := Parse(src)
	if len(f.Requests) != 3 || len(f.Errors) != 0 {
		t.Fatalf("requests=%d errors=%v", len(f.Requests), f.Errors)
	}
	cases := []struct {
		line, wantIdx int
		ok            bool
	}{
		{1, 0, true}, // comment above first request still belongs to its block
		{2, 0, true},
		{3, 1, true}, // separator line belongs to the following request
		{5, 1, true},
		{7, 1, true}, // body line
		{8, 2, true},
		{9, 2, true},
		{42, 0, false},
	}
	for _, c := range cases {
		r, ok := f.RequestAt(c.line)
		if ok != c.ok {
			t.Errorf("line %d: ok=%v want %v", c.line, ok, c.ok)
			continue
		}
		if ok && r.Index != c.wantIdx {
			t.Errorf("line %d: request %d, want %d", c.line, r.Index, c.wantIdx)
		}
	}
}

// TestParseFoldedQueryLines covers the JetBrains query-folding form (#1269):
// indented continuation lines starting with "?" or "&" extend the target.
func TestParseFoldedQueryLines(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{
			"jetbrains example",
			"GET https://example.net:9210/_cat/indices\n    ? v =\n    & s = i\n",
			"https://example.net:9210/_cat/indices?v=&s=i",
		},
		{
			"tight spelling",
			"GET https://api.test/x\n?a=1\n&b=2\n",
			"https://api.test/x?a=1&b=2",
		},
		{
			"several params per line",
			"GET https://api.test/x\n  ? a = 1 & b = 2\n  & c=3\n",
			"https://api.test/x?a=1&b=2&c=3",
		},
		{
			"valueless flag param",
			"GET https://api.test/x\n  ? pretty\n",
			"https://api.test/x?pretty",
		},
		{
			"value containing =",
			"GET https://api.test/x\n  ? filter = a=b\n",
			"https://api.test/x?filter=a=b",
		},
		{
			"query already on the request line",
			"GET https://api.test/x?a=1\n  & b = 2\n",
			"https://api.test/x?a=1&b=2",
		},
		{
			"request line ends with the query opener",
			"GET https://api.test/x?\n  & b = 2\n",
			"https://api.test/x?b=2",
		},
		{
			"comment between folded lines",
			"GET https://api.test/x\n  ? a = 1\n  # why\n  & b = 2\n",
			"https://api.test/x?a=1&b=2",
		},
		{
			"folding stops at the blank line",
			"GET https://api.test/x\n  ? a = 1\n\n?not=a-param\n",
			"https://api.test/x?a=1",
		},
		{
			"no folded lines leaves the target untouched",
			"GET https://api.test/x\nAccept: application/json\n",
			"https://api.test/x",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := Parse(c.src)
			if len(f.Errors) != 0 {
				t.Fatalf("unexpected errors: %v", f.Errors)
			}
			if len(f.Requests) != 1 {
				t.Fatalf("want 1 request, got %d", len(f.Requests))
			}
			if got := f.Requests[0].Target; got != c.want {
				t.Errorf("target: got %q, want %q", got, c.want)
			}
		})
	}
}

// TestParseFoldedQueryThenHeadersAndBody: folding ends at the first header
// line; headers and body parse as usual behind it.
func TestParseFoldedQueryThenHeadersAndBody(t *testing.T) {
	src := strings.Join([]string{
		"POST https://api.test/things",
		"    ? dry = true",
		"    & mode = fast",
		"Content-Type: application/json",
		"",
		`{"name":"x"}`,
	}, "\n")
	f := Parse(src)
	if len(f.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", f.Errors)
	}
	r := f.Requests[0]
	if r.Target != "https://api.test/things?dry=true&mode=fast" {
		t.Errorf("target: %q", r.Target)
	}
	if len(r.Headers) != 1 || r.Headers[0].Name != "Content-Type" {
		t.Errorf("headers: %+v", r.Headers)
	}
	if r.Body != `{"name":"x"}` {
		t.Errorf("body: %q", r.Body)
	}
}

// TestParseFoldedQueryPlaceholders: placeholders inside folded params resolve
// like anywhere else in the target.
func TestParseFoldedQueryPlaceholders(t *testing.T) {
	f := Parse("GET https://${HOST}/search\n  ? q = {{$env TERM}}\n  & lang = ${LANG}\n")
	if len(f.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", f.Errors)
	}
	r := f.Requests[0]
	if r.Target != "https://${HOST}/search?q={{$env TERM}}&lang=${LANG}" {
		t.Fatalf("target kept verbatim: %q", r.Target)
	}
	env := map[string]string{"HOST": "api.test", "TERM": "cats", "LANG": "de"}
	out, err := r.Resolve(func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Target != "https://api.test/search?q=cats&lang=de" {
		t.Errorf("resolved target: %q", out.Target)
	}
}

// TestParseInvalidLineAfterRequestStillErrors: a line that is neither a
// folded query line nor a header keeps its clear parse error.
func TestParseInvalidLineAfterRequestStillErrors(t *testing.T) {
	f := Parse("GET https://api.test/x\nthis is not a header\n")
	if len(f.Errors) != 1 {
		t.Fatalf("want 1 error, got %v", f.Errors)
	}
	if !strings.Contains(f.Errors[0].Msg, "invalid header field") {
		t.Errorf("error message: %q", f.Errors[0].Msg)
	}
	if f.Errors[0].Line != 2 {
		t.Errorf("error line: %d", f.Errors[0].Line)
	}
}

// TestParseExternalBody guards #1305: a body that is nothing but a
// "< path" directive is recorded as an external body, not sent verbatim.
func TestParseExternalBody(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       string
		wantPath   string
		wantSubst  bool
		wantInline string
	}{
		{name: "plain", body: "< ./payload.json", wantPath: "./payload.json"},
		{name: "tab separated", body: "<\t/abs/payload.json", wantPath: "/abs/payload.json"},
		{name: "substituting", body: "<@ ./payload.json", wantPath: "./payload.json", wantSubst: true},
		{name: "encoded", body: "<@utf-8 ./payload.json", wantPath: "./payload.json", wantSubst: true},
		{name: "path with spaces", body: "< ./my payload.json", wantPath: "./my payload.json"},
		{name: "indented", body: "  < ./payload.json  ", wantPath: "./payload.json"},
		// Not directives: no space, no path, or part of a larger payload.
		{name: "no separator", body: "<./payload.json", wantInline: "<./payload.json"},
		{name: "bare", body: "<", wantInline: "<"},
		{name: "xml payload", body: "<root>\n  <a/>\n</root>", wantInline: "<root>\n  <a/>\n</root>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := Parse("POST https://example.com/x\nContent-Type: application/json\n\n" + tc.body + "\n")
			if len(f.Requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(f.Requests))
			}
			r := f.Requests[0]
			if r.BodyFile != tc.wantPath {
				t.Errorf("BodyFile = %q, want %q", r.BodyFile, tc.wantPath)
			}
			if r.BodyFileSubstitute != tc.wantSubst {
				t.Errorf("BodyFileSubstitute = %v, want %v", r.BodyFileSubstitute, tc.wantSubst)
			}
			if r.Body != tc.wantInline {
				t.Errorf("Body = %q, want %q", r.Body, tc.wantInline)
			}
		})
	}
}

// TestResolveSubstitutesTheBodyPath: `< ./{{$env FIXTURE}}.json` resolves like
// every other placeholder-bearing field.
func TestResolveSubstitutesTheBodyPath(t *testing.T) {
	f := Parse("POST https://example.com/x\n\n< ./{{$env FIXTURE}}.json\n")
	if len(f.Requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(f.Requests))
	}
	out, err := f.Requests[0].Resolve(func(name string) (string, bool) {
		if name == "FIXTURE" {
			return "order", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if out.BodyFile != "./order.json" {
		t.Fatalf("BodyFile = %q, want ./order.json", out.BodyFile)
	}
}
