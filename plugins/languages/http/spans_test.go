package langhttp

import (
	"encoding/base64"
	"strings"
	"testing"

	"ike/internal/highlight"
	"ike/internal/jwt"
	"ike/internal/lang"
	"ike/internal/nethint"
	"ike/internal/numhint"
)

// spanAt returns the first span covering (line, col), like the editor's
// CaptureAt would resolve it after the overlay prepend.
func spanAt(spans []lang.Span, line, col int) (lang.Span, bool) {
	for _, s := range spans {
		if s.Line == line && col >= s.StartCol && col < s.EndCol {
			return s, true
		}
	}
	return lang.Span{}, false
}

func captureAt(spans []lang.Span, line, col int) string {
	s, _ := spanAt(spans, line, col)
	return s.Capture
}

// TestQuerySpansRequestLine: keys, values and separators of a request-line
// query get their own captures (#1585 items 1–2).
func TestQuerySpansRequestLine(t *testing.T) {
	line := "GET https://api.example.com/search?q=term&limit=10 HTTP/1.1"
	spans := querySpans([]string{line})

	col := func(sub string) int { return strings.Index(line, sub) }
	for _, tc := range []struct {
		sub  string
		want string
	}{
		{"?", "punctuation"},
		{"&", "punctuation"},
		{"q=", "property"},    // key start
		{"term", "constant"},  // value — distinct from the url string (#1594)
		{"limit", "property"}, // second key
		{"10 ", "constant"},   // second value
	} {
		if got := captureAt(spans, 0, col(tc.sub)); got != tc.want {
			t.Errorf("capture at %q = %q, want %q", tc.sub, got, tc.want)
		}
	}
	if got := captureAt(spans, 0, col("=")); got != "punctuation" {
		t.Errorf("capture at '=' = %q, want punctuation", got)
	}
	// The path gets its own capture (#1594); the authority keeps the url
	// string colour through string.special (#1740).
	if got := captureAt(spans, 0, col("/search")); got != "label" {
		t.Errorf("path segment = %q, want label", got)
	}
	if got := captureAt(spans, 0, col("api.example.com")); got != "string.special" {
		t.Errorf("authority = %q, want string.special", got)
	}
}

// TestTargetPrefixSpans (#1740): the "scheme://authority" prefix reads as
// three segments — dim scheme, punctuation separator, url-coloured authority
// — including with a port, and the query/path spans keep their columns.
func TestTargetPrefixSpans(t *testing.T) {
	line := "GET https://api.example.com:8080/users?limit=10 HTTP/1.1"
	spans := querySpans([]string{line})
	col := func(sub string) int { return strings.Index(line, sub) }
	for _, tc := range []struct{ sub, want string }{
		{"https", "comment"},
		{"://", "punctuation"},
		{"api.example.com", "string.special"},
		{":8080", "string.special"},
		{"/users", "label"},
		{"?", "punctuation"},
		{"limit", "property"},
	} {
		if got := captureAt(spans, 0, col(tc.sub)); got != tc.want {
			t.Errorf("capture at %q = %q, want %q", tc.sub, got, tc.want)
		}
	}
	// The separator covers all three of "://" and nothing more.
	sep := col("://")
	for c := sep; c < sep+3; c++ {
		if got := captureAt(spans, 0, c); got != "punctuation" {
			t.Errorf("separator col %d = %q, want punctuation", c, got)
		}
	}
	if got := captureAt(spans, 0, sep-1); got != "comment" {
		t.Errorf("last scheme col = %q, want comment", got)
	}
	// The trailing " HTTP/1.1" is outside the target and stays unstyled.
	if got := captureAt(spans, 0, col("HTTP/1.1")); got != "" {
		t.Errorf("version got capture %q, want none", got)
	}
}

// TestTargetPrefixNoScheme (#1740): an origin-form target has no scheme or
// authority spans — the path capture stays the only one.
func TestTargetPrefixNoScheme(t *testing.T) {
	line := "GET /api/users"
	spans := querySpans([]string{line})
	for _, capture := range []string{"comment", "string.special"} {
		for _, s := range spans {
			if s.Capture == capture {
				t.Errorf("origin-form target got a %q span: %+v", capture, s)
			}
		}
	}
	if got := captureAt(spans, 0, strings.Index(line, "/api")); got != "label" {
		t.Errorf("origin-form path = %q, want label", got)
	}
}

// TestTargetPrefixPlaceholder (#1740, #1880): a placeholder inside the
// authority (or standing in for the whole scheme) gets its own punctuation +
// @variable styling — the prefix spans split around it.
func TestTargetPrefixPlaceholder(t *testing.T) {
	line := "GET {{scheme}}://{{host}}.test:{{port}}/p?a=1"
	spans := querySpans([]string{line})
	for _, sub := range []string{"{{scheme}}", "{{host}}", "{{port}}"} {
		start := strings.Index(line, sub)
		if got := captureAt(spans, 0, start); got != "punctuation" {
			t.Errorf("%s opening brace = %q, want punctuation", sub, got)
		}
		if got := captureAt(spans, 0, start+2); got != "variable" {
			t.Errorf("%s name = %q, want variable", sub, got)
		}
		if got := captureAt(spans, 0, start+len(sub)-1); got != "punctuation" {
			t.Errorf("%s closing brace = %q, want punctuation", sub, got)
		}
	}
	if got := captureAt(spans, 0, strings.Index(line, "://")); got != "punctuation" {
		t.Errorf("separator = %q, want punctuation", got)
	}
	if got := captureAt(spans, 0, strings.Index(line, ".test")); got != "string.special" {
		t.Errorf("authority remainder = %q, want string.special", got)
	}
	if got := captureAt(spans, 0, strings.Index(line, "/p")); got != "label" {
		t.Errorf("path = %q, want label", got)
	}
}

// TestQuerySpansOriginFormPath: a target without scheme ("/api/users") is all
// path up to the query.
func TestQuerySpansOriginFormPath(t *testing.T) {
	line := "GET /api/users?id=1"
	spans := querySpans([]string{line})
	if got := captureAt(spans, 0, strings.Index(line, "/api")); got != "label" {
		t.Errorf("origin-form path = %q, want label", got)
	}
	if got := captureAt(spans, 0, strings.Index(line, "id")); got != "property" {
		t.Errorf("query key = %q, want property", got)
	}
}

// TestQuerySpansFoldedLines: indented ?/& continuation lines (#1269) get the
// same treatment, with whitespace around tokens left unstyled.
func TestQuerySpansFoldedLines(t *testing.T) {
	lines := []string{
		"GET https://api.example.com/search",
		"    ?q=term",
		"    & limit = 10",
		"Accept: application/json",
	}
	spans := querySpans(lines)
	if got := captureAt(spans, 1, strings.Index(lines[1], "?")); got != "punctuation" {
		t.Errorf("folded '?' capture = %q", got)
	}
	if got := captureAt(spans, 1, strings.Index(lines[1], "q")); got != "property" {
		t.Errorf("folded key capture = %q", got)
	}
	if got := captureAt(spans, 2, strings.Index(lines[2], "limit")); got != "property" {
		t.Errorf("folded spaced key capture = %q", got)
	}
	if got := captureAt(spans, 2, strings.Index(lines[2], "10")); got != "constant" {
		t.Errorf("folded spaced value capture = %q", got)
	}
	// The header line after the fold gets nothing.
	for _, s := range spans {
		if s.Line == 3 {
			t.Fatalf("header line got span %+v", s)
		}
	}
}

// TestQuerySpansPercentEncoding: %XX sequences conceal as their decoded
// character; multi-byte encodings span all their triples (#1585 item 3).
func TestQuerySpansPercentEncoding(t *testing.T) {
	line := "GET https://x.test/p?a=hello%20world&b=%C3%A4"
	spans := querySpans([]string{line})

	sp, ok := spanAt(spans, 0, strings.Index(line, "%20"))
	if !ok || sp.Replace != " " || sp.EndCol-sp.StartCol != 3 {
		t.Errorf("%%20 span = %+v, want 3-col Replace \" \"", sp)
	}
	if sp.Capture != "escape" {
		t.Errorf("%%20 capture = %q, want escape", sp.Capture)
	}
	umlaut := strings.Index(line, "%C3%A4")
	sp, ok = spanAt(spans, 0, umlaut)
	if !ok || sp.Replace != "ä" || sp.StartCol != umlaut || sp.EndCol != umlaut+6 {
		t.Errorf("%%C3%%A4 span = %+v, want 6-col Replace \"ä\"", sp)
	}
}

// TestQuerySpansPercentInvalid: a triple that does not decode to a printable
// rune keeps its escape styling but never conceals — hiding it would lie.
func TestQuerySpansPercentInvalid(t *testing.T) {
	line := "GET https://x.test/p?a=%FF&b=%0A"
	spans := querySpans([]string{line})
	for _, sub := range []string{"%FF", "%0A"} {
		sp, ok := spanAt(spans, 0, strings.Index(line, sub))
		if !ok || sp.Capture != "escape" {
			t.Errorf("%s span = %+v, want escape", sub, sp)
		}
		if sp.Replace != "" {
			t.Errorf("%s must not conceal, got Replace %q", sub, sp.Replace)
		}
	}
}

// TestQuerySpansPlaceholdersSkipped: a "{{…}}" query value gets its own
// punctuation + @variable styling (#1880) instead of the query pass's own
// key/value captures; "${…}" is a different placeholder form (out of scope
// for #1880) and keeps getting no overlay span at all.
func TestQuerySpansPlaceholdersSkipped(t *testing.T) {
	line := "GET https://x.test/p?a={{host}}&b=${NAME}&c=1"
	spans := querySpans([]string{line})
	hostStart := strings.Index(line, "{{host}}")
	if got := captureAt(spans, 0, hostStart); got != "punctuation" {
		t.Errorf("{{host}} opening brace = %q, want punctuation", got)
	}
	if got := captureAt(spans, 0, hostStart+2); got != "variable" {
		t.Errorf("{{host}} name = %q, want variable", got)
	}
	envStart := strings.Index(line, "${NAME}")
	for col := envStart; col < envStart+len("${NAME}"); col++ {
		if s, ok := spanAt(spans, 0, col); ok {
			t.Fatalf("${…} placeholder col %d covered by %+v", col, s)
		}
	}
	if got := captureAt(spans, 0, strings.Index(line, "c=")); got != "property" {
		t.Errorf("key after placeholders = %q, want property", got)
	}
}

// TestQuerySpansFormBody: an application/x-www-form-urlencoded body gets the
// same key/value/percent treatment; other bodies stay untouched.
func TestQuerySpansFormBody(t *testing.T) {
	lines := []string{
		"POST https://x.test/submit",
		"Content-Type: application/x-www-form-urlencoded",
		"",
		"name=J%C3%BCrgen&city=K%C3%B6ln",
	}
	spans := querySpans(lines)
	if got := captureAt(spans, 3, 0); got != "property" {
		t.Errorf("body key capture = %q, want property", got)
	}
	sp, ok := spanAt(spans, 3, strings.Index(lines[3], "%C3%BC"))
	if !ok || sp.Replace != "ü" {
		t.Errorf("body percent span = %+v, want Replace \"ü\"", sp)
	}

	json := []string{
		"POST https://x.test/submit",
		"Content-Type: application/json",
		"",
		`{"a": "b=c&d=e"}`,
	}
	for _, s := range querySpans(json) {
		if s.Line == 3 {
			t.Fatalf("json body got span %+v", s)
		}
	}
}

// TestBodyEpochSpans (#1618): a numeric timestamp in a JSON request body
// becomes a conceal-with-stand-in span rendering its UTC form, while a plain
// number and the request line's own digits stay untouched.
func TestBodyEpochSpans(t *testing.T) {
	lines := []string{
		"POST https://api.example.com/events?limit=1722945600",
		"Content-Type: application/json",
		"",
		`{"ts": 1722945600, "count": 42}`,
	}
	spans := querySpans(lines)
	s, ok := spanAt(spans, 3, 8)
	if !ok || s.Replace != "2024-08-06 12:00:00Z" {
		t.Fatalf("body timestamp span = %+v, want the decoded UTC stand-in", s)
	}
	if s.StartCol != 7 || s.EndCol != 17 {
		t.Errorf("span covers [%d,%d), want the digits [7,17)", s.StartCol, s.EndCol)
	}
	for _, col := range []int{25, 26} { // the "42" value
		if s, ok := spanAt(spans, 3, col); ok && s.Replace != "" {
			t.Errorf("plain number at col %d concealed as %q", col, s.Replace)
		}
	}
	// Query-parameter values are a value position too (#1684).
	found := false
	for _, s := range spans {
		if s.Line == 0 && s.StartCol == 42 && s.EndCol == 52 && s.Replace == "2024-08-06 12:00:00Z" {
			found = true
		}
	}
	if !found {
		t.Errorf("query timestamp got no stand-in; spans = %+v", spans)
	}
}

// TestQuerySpansJWTSignature: a JWT anywhere in the buffer — an Authorization
// header, a body, a variable line — gets its signature segment dimmed (#1619),
// while the readable header and payload segments keep the grammar's styling.
func TestQuerySpansJWTSignature(t *testing.T) {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	tok := enc(`{"alg":"HS256","typ":"JWT"}`) + "." + enc(`{"sub":"1","exp":1722949200}`) + ".s1gn4tur3"
	lines := []string{
		"@auth = " + tok,
		"GET https://api.example.com/me",
		"Authorization: Bearer " + tok,
		"Accept: application/json",
	}
	spans := querySpans(lines)
	for _, li := range []int{0, 2} {
		sigCol := strings.LastIndex(lines[li], ".s1gn4tur3") + 1
		if got := captureAt(spans, li, sigCol); got != jwt.Capture {
			t.Errorf("line %d signature capture = %q, want %q", li, got, jwt.Capture)
		}
		payloadCol := strings.Index(lines[li], tok) + 5
		if got := captureAt(spans, li, payloadCol); got == jwt.Capture {
			t.Errorf("line %d: the header segment must not be dimmed", li)
		}
	}
	if got := captureAt(spans, 3, strings.Index(lines[3], "application")); got == jwt.Capture {
		t.Error("a plain header value must not be taken for a JWT")
	}
}

// TestHTTPNetworkHints (#1653): a punycode authority in a request target
// decodes inline, and a homograph takes the warning capture.
func TestHTTPNetworkHints(t *testing.T) {
	lines := []string{"GET https://xn--80ak6aa92e.com/login HTTP/1.1", "X-Allow: 10.0.0.0/8", ""}
	spans := querySpans(lines)
	host, ok := spanAt(spans, 0, strings.Index(lines[0], "xn--"))
	if !ok || host.Capture != nethint.IDNMixedCapture {
		t.Errorf("host span = %+v, want the homograph capture", host)
	}
	cidr, ok := spanAt(spans, 1, strings.Index(lines[1], "10."))
	if !ok || cidr.Capture != nethint.CIDRCapture {
		t.Errorf("header span = %+v, want the CIDR capture", cidr)
	}
}

// standIn returns the stand-in span covering (line, col) — the conceal spans
// are appended after the structural ones, so spanAt would find the structure.
func standIn(spans []lang.Span, line, col int) (lang.Span, bool) {
	for _, s := range spans {
		if s.Line == line && s.Replace != "" && col >= s.StartCol && col < s.EndCol {
			return s, true
		}
	}
	return lang.Span{}, false
}

// TestQueryValueNumberHints (#1684): a query-parameter value carries its
// number-readability hint, keyed off the parameter name, while the parameter
// names and the request line's structure stay raw.
func TestQueryValueNumberHints(t *testing.T) {
	line := "GET https://api.example.com/search?max_size=10485760&timeout=90000&page=2 HTTP/1.1"
	spans := querySpans([]string{line})
	col := func(sub string) int { return strings.Index(line, sub) }

	if s, ok := standIn(spans, 0, col("10485760")); !ok || s.Replace != "10 MiB" {
		t.Errorf("size value stand-in = %+v, want 10 MiB", s)
	}
	if s, ok := standIn(spans, 0, col("90000")); !ok || s.Replace != "1m30s" {
		t.Errorf("timeout value stand-in = %+v, want 1m30s", s)
	}
	// Parameter names are keys: never concealed, whatever they look like.
	for _, key := range []string{"max_size", "timeout", "page"} {
		if s, ok := standIn(spans, 0, col(key)); ok {
			t.Errorf("query key %q concealed as %q", key, s.Replace)
		}
	}
	// A short value has no reading to add, and the protocol version is not a
	// value position at all.
	if s, ok := standIn(spans, 0, col("HTTP/1.1")); ok {
		t.Errorf("protocol version concealed as %q", s.Replace)
	}
}

// TestFoldedQueryValueHints (#1684): the folded continuation lines of a target
// (#1269) are query values like the request line's own.
func TestFoldedQueryValueHints(t *testing.T) {
	lines := []string{
		"GET https://api.example.com/search",
		"  ?q=term",
		"  &max_bytes=1048576",
		"",
	}
	if s, ok := standIn(querySpans(lines), 2, strings.Index(lines[2], "1048576")); !ok || s.Replace != "1 MiB" {
		t.Errorf("folded query value stand-in = %+v, want 1 MiB", s)
	}
}

// TestHeaderValueNumberHints (#1684): a header value is a value position, its
// name is not.
func TestHeaderValueNumberHints(t *testing.T) {
	lines := []string{
		"POST https://api.example.com/upload",
		"Content-Length: 10485760",
		"",
		"x",
	}
	spans := querySpans(lines)
	if s, ok := standIn(spans, 1, strings.Index(lines[1], "10485760")); !ok || s.Replace != "10 MiB" {
		t.Errorf("header value stand-in = %+v, want 10 MiB", s)
	}
	if s, ok := standIn(spans, 1, 0); ok {
		t.Errorf("header name concealed as %q", s.Replace)
	}
}

// TestJSONBodyValueHints (#1684): numeric values of a JSON request body carry
// their hints; a member name that happens to be numeric never does.
func TestJSONBodyValueHints(t *testing.T) {
	lines := []string{
		"POST https://api.example.com/jobs",
		"Content-Type: application/json",
		"",
		`{"max_size": 10485760, "timeout_ms": 90000, "count": 123456}`,
		`{"10485760": "a numeric key", "ts": 1722945600}`,
	}
	spans := querySpans(lines)
	col := func(li int, sub string) int { return strings.Index(lines[li], sub) }
	for _, tc := range []struct{ sub, want string }{
		{"10485760", "10 MiB"},
		{"90000", "1m30s"},
		{"123456", "123_456"},
	} {
		if s, ok := standIn(spans, 3, col(3, tc.sub)); !ok || s.Replace != tc.want {
			t.Errorf("body value %s stand-in = %+v, want %q", tc.sub, s, tc.want)
		}
	}
	// Keys are never concealed — not the names, not a numeric one.
	for _, key := range []string{`"max_size"`, `"timeout_ms"`, `"count"`} {
		if s, ok := standIn(spans, 3, col(3, key)+1); ok {
			t.Errorf("body key %s concealed as %q", key, s.Replace)
		}
	}
	if s, ok := standIn(spans, 4, col(4, "10485760")); ok {
		t.Errorf("numeric body key concealed as %q", s.Replace)
	}
	// The epoch family still wins over the number hints on the same digits.
	if s, ok := standIn(spans, 4, col(4, "1722945600")); !ok || s.Replace != "2024-08-06 12:00:00Z" {
		t.Errorf("body timestamp stand-in = %+v, want the decoded UTC form", s)
	}
}

// TestFieldUnitPrecedence (#1685): a header or body field whose name says the
// unit keeps its own reading, even where the digits also decode as a Unix
// timestamp, and a field mapped to none conceals nothing at all.
func TestFieldUnitPrecedence(t *testing.T) {
	lines := []string{
		"POST https://api.example.com/upload",
		"X-Payload-Bytes: 1722945600",
		"",
		`{"bytes": 1722945600}`,
	}
	spans := querySpans(lines)
	for _, line := range []int{1, 3} {
		s, ok := spanAt(spans, line, strings.Index(lines[line], "1722945600"))
		if !ok || s.Capture != numhint.SizeCapture || s.Replace != "1.6 GiB" {
			t.Errorf("line %d stand-in = %+v, %v; want the byte size to win", line, s, ok)
		}
	}
	numhint.SetFieldUnits([]string{"*bytes=none"})
	defer numhint.SetFieldUnits(nil)
	for _, s := range querySpans(lines) {
		if s.Replace != "" {
			t.Errorf("field mapped to none still concealed: %+v", s)
		}
	}
}

// TestVariableDefinitionValueSpan (#1880): an "@name = value" definition's
// value gets its own capture, distinct from the name the grammar already
// styles as @variable.
func TestVariableDefinitionValueSpan(t *testing.T) {
	lines := []string{"@token = abc123", "@empty =", "not.a.def = x"}
	spans := querySpans(lines)
	if got := captureAt(spans, 0, strings.Index(lines[0], "abc123")); got != "string" {
		t.Errorf("definition value = %q, want string", got)
	}
	for _, s := range spans {
		if s.Line == 1 {
			t.Errorf("empty-value definition got span %+v", s)
		}
		if s.Line == 2 {
			t.Errorf("non-definition line got span %+v", s)
		}
	}
}

// TestVariableDefinitionValuePlaceholder (#1880): a definition whose value is
// itself a placeholder ("@api = {{host}}/api") keeps the placeholder's own
// punctuation + @variable styling rather than reading as one flat value.
func TestVariableDefinitionValuePlaceholder(t *testing.T) {
	line := "@api = {{host}}/api"
	spans := querySpans([]string{line})
	start := strings.Index(line, "{{host}}")
	if got := captureAt(spans, 0, start); got != "punctuation" {
		t.Errorf("{{host}} opening brace = %q, want punctuation", got)
	}
	if got := captureAt(spans, 0, start+2); got != "variable" {
		t.Errorf("{{host}} name = %q, want variable", got)
	}
	if got := captureAt(spans, 0, strings.Index(line, "/api")); got != "string" {
		t.Errorf("trailing value text = %q, want string", got)
	}
}

// TestPlaceholderSpansEverywhere (#1880): a "{{name}}" placeholder gets the
// same punctuation + @variable styling wherever it appears — URL, header
// value and (via the merged highlight.Highlight pipeline, so the embedded
// JSON body region overlay is exercised too) request body.
func TestPlaceholderSpansEverywhere(t *testing.T) {
	lines := []string{
		"POST https://api.test/things/{{token}} HTTP/1.1",
		"Authorization: Bearer {{token}}",
		"Content-Type: application/json",
		"",
		`{"id": "{{token}}"}`,
	}
	spans := highlight.Highlight("req.http", lines)
	ix := highlight.NewIndex(spans)
	for _, tc := range []struct {
		line int
		sub  string
	}{
		{0, "{{token}}"},
		{1, "{{token}}"},
		{4, "{{token}}"},
	} {
		start := strings.Index(lines[tc.line], tc.sub)
		if got := ix.CaptureAt(tc.line, start); got != "punctuation" {
			t.Errorf("line %d opening brace = %q, want punctuation", tc.line, got)
		}
		if got := ix.CaptureAt(tc.line, start+2); got != "variable" {
			t.Errorf("line %d name = %q, want variable", tc.line, got)
		}
		if got := ix.CaptureAt(tc.line, start+len(tc.sub)-1); got != "punctuation" {
			t.Errorf("line %d closing brace = %q, want punctuation", tc.line, got)
		}
	}
}

// TestPlaceholderTargetPath (#2218): a target opening with a placeholder and
// no scheme of its own ("{{host}}/my/path") keeps highlighting past the
// variable — the path reads as label, the query keeps its structure and the
// placeholder keeps its punctuation + @variable styling.
func TestPlaceholderTargetPath(t *testing.T) {
	line := "GET {{host}}/my/path?x=1"
	spans := querySpans([]string{line})
	start := strings.Index(line, "{{host}}")
	if got := captureAt(spans, 0, start); got != "punctuation" {
		t.Errorf("{{host}} opening brace = %q, want punctuation", got)
	}
	if got := captureAt(spans, 0, start+2); got != "variable" {
		t.Errorf("{{host}} name = %q, want variable", got)
	}
	if got := captureAt(spans, 0, strings.Index(line, "/my")); got != "label" {
		t.Errorf("path = %q, want label", got)
	}
	if got := captureAt(spans, 0, strings.Index(line, "path")); got != "label" {
		t.Errorf("path tail = %q, want label", got)
	}
	if got := captureAt(spans, 0, strings.Index(line, "x=")); got != "property" {
		t.Errorf("query key = %q, want property", got)
	}
}

// TestPlaceholderTargetAuthorityRemainder (#2218): the stretch between a
// leading placeholder and the path ("{{host}}.test:8080/p") reads as the
// authority remainder, matching the #1740 authority colour.
func TestPlaceholderTargetAuthorityRemainder(t *testing.T) {
	line := "GET {{host}}.test:8080/p"
	spans := querySpans([]string{line})
	if got := captureAt(spans, 0, strings.Index(line, ".test")); got != "string.special" {
		t.Errorf("authority remainder = %q, want string.special", got)
	}
	if got := captureAt(spans, 0, strings.Index(line, "8080")); got != "string.special" {
		t.Errorf("port = %q, want string.special", got)
	}
	if got := captureAt(spans, 0, strings.Index(line, "/p")); got != "label" {
		t.Errorf("path = %q, want label", got)
	}
}

// TestVariableDefinitionURLValue (#2218): a url-shaped definition value
// ("@host=http://www.example.com/base?a=1") reads as the same segments a
// request target does instead of one flat string.
func TestVariableDefinitionURLValue(t *testing.T) {
	line := "@host=http://www.example.com/base?a=1"
	spans := querySpans([]string{line})
	if got := captureAt(spans, 0, strings.Index(line, "@")); got != "" {
		// The grammar styles "@host" and "="; querySpans must not overlay them.
		t.Errorf("name = %q, want no querySpans capture", got)
	}
	if got := captureAt(spans, 0, strings.Index(line, "http")); got != "comment" {
		t.Errorf("scheme = %q, want comment", got)
	}
	if got := captureAt(spans, 0, strings.Index(line, "://")); got != "punctuation" {
		t.Errorf("separator = %q, want punctuation", got)
	}
	if got := captureAt(spans, 0, strings.Index(line, "www")); got != "string.special" {
		t.Errorf("authority = %q, want string.special", got)
	}
	if got := captureAt(spans, 0, strings.Index(line, "/base")); got != "label" {
		t.Errorf("path = %q, want label", got)
	}
	if got := captureAt(spans, 0, strings.Index(line, "a=")); got != "property" {
		t.Errorf("query key = %q, want property", got)
	}
}
