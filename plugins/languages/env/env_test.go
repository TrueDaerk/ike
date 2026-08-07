package langenv

import (
	"encoding/base64"
	"testing"

	"ike/internal/jwt"
	"ike/internal/lang"
	"ike/internal/nethint"
	"ike/internal/numhint"
)

// jwtToken builds a syntactically valid JWT for the span tests.
func jwtToken() string {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	return enc(`{"alg":"HS256","typ":"JWT"}`) + "." + enc(`{"sub":"1","exp":1722949200}`) + ".s1gn4tur3"
}

// spanAt returns the span covering col on line, matching capture.
func spanAt(spans []lang.Span, line, col int, capture string) (lang.Span, bool) {
	for _, s := range spans {
		if s.Line == line && s.Capture == capture && col >= s.StartCol && col < s.EndCol {
			return s, true
		}
	}
	return lang.Span{}, false
}

func TestEnvSpansPairs(t *testing.T) {
	spans := envSpans([]string{"# comment", "API_HOST = api.example.com", "export DEBUG=1", "BARE"})
	if _, ok := spanAt(spans, 0, 0, "comment"); !ok {
		t.Error("a # line must style as a comment")
	}
	if _, ok := spanAt(spans, 1, 0, "property"); !ok {
		t.Error("the key must style as a property")
	}
	if _, ok := spanAt(spans, 1, 9, "punctuation"); !ok {
		t.Error("the = must style as punctuation")
	}
	if _, ok := spanAt(spans, 1, 11, "string"); !ok {
		t.Error("the value must style as a string")
	}
	if _, ok := spanAt(spans, 2, 0, "keyword"); !ok {
		t.Error("a leading export must style as a keyword")
	}
	if _, ok := spanAt(spans, 2, 7, "property"); !ok {
		t.Error("the key after export must style as a property")
	}
	if _, ok := spanAt(spans, 3, 0, "property"); !ok {
		t.Error("a bare flag line must style as a property")
	}
}

func TestEnvSpansDimJWTSignature(t *testing.T) {
	tok := jwtToken()
	line := "TOKEN=" + tok
	spans := envSpans([]string{line})
	s, ok := spanAt(spans, 0, len(line)-1, jwt.Capture)
	if !ok {
		t.Fatalf("no %s span over the signature", jwt.Capture)
	}
	if got := line[s.StartCol:s.EndCol]; got != "s1gn4tur3" {
		t.Errorf("dimmed span covers %q, want the signature", got)
	}
	// The header/payload part keeps the plain value styling.
	if _, ok := spanAt(spans, 0, 8, jwt.Capture); ok {
		t.Error("the header segment must not be dimmed")
	}
}

func TestEnvSpansLeavePlainValuesAlone(t *testing.T) {
	spans := envSpans([]string{"URL=https://example.com/a.b.c"})
	if _, ok := spanAt(spans, 0, 20, jwt.Capture); ok {
		t.Error("a dotted plain value must not be taken for a JWT")
	}
}

func TestRegisteredForDotEnvFiles(t *testing.T) {
	for _, path := range []string{"/p/.env", "/p/.env.local", "/p/config/dev.env"} {
		l, ok := lang.ByPath(path)
		if !ok || l.ID != "dotenv" {
			t.Errorf("ByPath(%q) = %q, %v; want dotenv", path, l.ID, ok)
		}
	}
}

// TestEnvNumberHints (#1627): a dotenv byte-count key carries its size hint.
func TestEnvNumberHints(t *testing.T) {
	spans := envSpans([]string{"MAX_UPLOAD_BYTES=5242880"})
	var hint *lang.Span
	for i, s := range spans {
		if s.Capture == numhint.SizeCapture {
			hint = &spans[i]
		}
	}
	if hint == nil || hint.Replace != "5 MiB" {
		t.Errorf("spans = %+v, want a 5 MiB size hint", spans)
	}
}

// TestEnvNetworkHints (#1653): a punycode host in a dotenv value decodes to
// its Unicode form, homographs taking the warning capture.
func TestEnvNetworkHints(t *testing.T) {
	l, ok := lang.ByID("dotenv")
	if !ok || l.Spans == nil {
		t.Fatal("dotenv: no Spans producer registered")
	}
	spans := l.Spans([]string{"API_HOST=xn--80ak6aa92e.com"})
	var hint *lang.Span
	for i, s := range spans {
		if s.Capture == nethint.IDNMixedCapture {
			hint = &spans[i]
		}
	}
	if hint == nil {
		t.Fatalf("spans = %+v, want a homograph hint", spans)
	}
	if want := "xn--80ak6aa92e.com" + nethint.Gap + "аррӏе.com"; hint.Replace != want {
		t.Errorf("hint = %q, want %q", hint.Replace, want)
	}
}
