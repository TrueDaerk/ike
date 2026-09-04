package langhttp

import (
	"strings"
	"testing"

	"ike/internal/escapes"
	"ike/internal/httpfile"
	"ike/internal/secret"
)

func TestHTTPMaskHeaders(t *testing.T) {
	lines := []string{
		`@token = sk_live_abc`,
		``,
		`GET https://example.com/api HTTP/1.1`,
		`Authorization: Bearer eyJhbGciOi.eyJzdWIi.sig`,
		`X-Api-Key: abc123`,
		`Accept: application/json`,
	}
	f := httpfile.Parse(strings.Join(lines, "\n"))
	got := map[int]string{}
	for _, s := range maskSpans(f, lines) {
		if s.Capture != secret.Capture {
			t.Fatalf("span %+v must carry the mask capture", s)
		}
		got[s.Line] = string([]rune(lines[s.Line])[s.StartCol:s.EndCol])
	}
	if got[0] != "sk_live_abc" {
		t.Errorf("the suspect @token definition masks %q, want its value", got[0])
	}
	if got[3] != "eyJhbGciOi.eyJzdWIi.sig" {
		t.Errorf("Authorization masks %q, want the credential with the scheme readable", got[3])
	}
	if got[4] != "abc123" {
		t.Errorf("X-Api-Key masks %q, want its value", got[4])
	}
	if _, ok := got[5]; ok {
		t.Error("a harmless header must stay readable")
	}
}

func TestHTTPBasicAuthDecodes(t *testing.T) {
	lines := []string{
		`GET https://example.com/api HTTP/1.1`,
		`Authorization: Basic dXNlcjpwYXNz`,
	}
	f := httpfile.Parse(strings.Join(lines, "\n"))
	spans := basicAuthSpans(f, lines)
	if len(spans) != 1 || spans[0].Replace != "user:pass" || spans[0].Capture != escapes.Base64Capture {
		t.Fatalf("got %+v, want the decoded Basic credential", spans)
	}
	if got := string([]rune(lines[1])[spans[0].StartCol:spans[0].EndCol]); got != "dXNlcjpwYXNz" {
		t.Errorf("span covers %q, want only the base64 token", got)
	}
}

// TestHTTPMaskPrecedesBasicDecode: through the full hook, the first span
// covering a Basic credential must be the mask, so masking beats decoding.
func TestHTTPMaskPrecedesBasicDecode(t *testing.T) {
	lines := []string{
		`GET https://example.com/api HTTP/1.1`,
		`Authorization: Basic dXNlcjpwYXNz`,
	}
	col := strings.Index(lines[1], "dXNlcjpwYXNz")
	for _, s := range querySpans(lines) {
		if s.Line != 1 || s.StartCol > col || s.EndCol <= col {
			continue
		}
		if s.Capture != secret.Capture {
			t.Fatalf("first span covering the credential is %q, want the mask", s.Capture)
		}
		return
	}
	t.Fatal("no span covers the credential")
}
