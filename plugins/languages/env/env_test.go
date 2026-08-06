package langenv

import (
	"encoding/base64"
	"testing"

	"ike/internal/jwt"
	"ike/internal/lang"
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
