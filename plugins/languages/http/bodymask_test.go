package langhttp

// bodymask_test.go covers the request-body half of .http secret masking
// (#2598): a body whose Content-Type resolves to a language with a mask
// producer masks its credential values exactly as a standalone file of that
// language would, through the region seam (lang.RegionMasks).

import (
	"strings"
	"testing"

	"ike/internal/httpfile"
	"ike/internal/secret"

	// The body masks come from the embedded language's own producer, so the
	// real JSON plugin must be registered — the same dependency folds_test.go
	// already takes for body folding.
	_ "ike/plugins/languages/json"
)

// bodyMasks returns the masked source text per line of a .http buffer.
func bodyMasks(t *testing.T, lines []string) map[int]string {
	t.Helper()
	f := httpfile.Parse(strings.Join(lines, "\n"))
	got := map[int]string{}
	for _, s := range maskSpans(f, lines) {
		if s.Capture != secret.Capture || s.Replace != secret.Mask {
			t.Fatalf("span %+v must be a secret stand-in (capture %q, replace %q)", s, secret.Capture, secret.Mask)
		}
		got[s.Line] = string([]rune(lines[s.Line])[s.StartCol:s.EndCol])
	}
	return got
}

// TestHTTPBodyMasksJSONSecret: the issue's example — the body's suspect key
// masks its value, a harmless one stays readable, and the quotes stay outside
// the span so the member still reads as a string.
func TestHTTPBodyMasksJSONSecret(t *testing.T) {
	lines := []string{
		`### Request Test`,               // 0
		`PUT http://example.com/text`,    // 1
		`Content-Type: application/json`, // 2
		``,                               // 3
		`{`,                              // 4
		`    "time_seconds": 600,`,       // 5
		`    "password": "abcdef"`,       // 6
		`}`,                              // 7
	}
	got := bodyMasks(t, lines)
	if got[6] != "abcdef" {
		t.Errorf("body masks %q on the password line, want the bare value", got[6])
	}
	if _, ok := got[5]; ok {
		t.Error("a harmless body member must stay readable")
	}
	if _, ok := got[2]; ok {
		t.Error("the Content-Type header is not a credential")
	}
}

// TestHTTPBodyMasksWithContentTypeParameters: the media type's parameters do
// not stop the body from being typed (bodyLanguage strips them).
func TestHTTPBodyMasksWithContentTypeParameters(t *testing.T) {
	lines := []string{
		`POST http://example.com/x`,
		`Content-Type: application/json; charset=utf-8`,
		``,
		`{"api_key": "sk_live_abc"}`,
	}
	if got := bodyMasks(t, lines); got[3] != "sk_live_abc" {
		t.Errorf("masked %q, want the credential of a parameterised JSON body", got[3])
	}
}

// TestHTTPBodyWithoutMaskProducerStaysReadable: a body language the registry
// does not type — or one without a mask producer — contributes nothing.
func TestHTTPBodyWithoutMaskProducerStaysReadable(t *testing.T) {
	lines := []string{
		`POST http://example.com/x`,
		`Content-Type: text/plain`,
		``,
		`password: abcdef`,
	}
	if got := bodyMasks(t, lines); len(got) != 0 {
		t.Errorf("masked %v, want a plain-text body untouched", got)
	}
}

// TestHTTPBodyMaskCarriesOverLines: a member broken over two lines masks the
// wrapped value, the JSON producer's carry rule reaching into the body.
func TestHTTPBodyMaskCarriesOverLines(t *testing.T) {
	lines := []string{
		`POST http://example.com/x`,
		`Content-Type: application/json`,
		``,
		`{`,
		`  "secret":`,
		`    "hunter2"`,
		`}`,
	}
	got := bodyMasks(t, lines)
	if got[5] != "hunter2" {
		t.Errorf("masked %q on the wrapped value line, want hunter2", got[5])
	}
}

// TestHTTPBodyMaskIsViewOnly: masking is a rendering overlay — the parsed
// body the runner, the snapshot and the curl export work on still carries the
// real value.
func TestHTTPBodyMaskIsViewOnly(t *testing.T) {
	lines := []string{
		`POST http://example.com/x`,
		`Content-Type: application/json`,
		``,
		`{"password": "abcdef"}`,
	}
	if got := bodyMasks(t, lines); got[3] != "abcdef" {
		t.Fatalf("masked %q, want the credential", got[3])
	}
	f := httpfile.Parse(strings.Join(lines, "\n"))
	if len(f.Requests) != 1 || !strings.Contains(f.Requests[0].Body, "abcdef") {
		t.Errorf("parsed body = %q, want the unmasked source text", f.Requests[0].Body)
	}
}

// TestHTTPHeaderMasksSurviveBodyMasking: the header and @variable masks keep
// their own spans when a body contributes its own (#2345 unaffected).
func TestHTTPHeaderMasksSurviveBodyMasking(t *testing.T) {
	lines := []string{
		`@token = sk_live_abc`,
		``,
		`POST http://example.com/x`,
		`X-Api-Key: abc123`,
		`Content-Type: application/json`,
		``,
		`{"password": "abcdef"}`,
	}
	got := bodyMasks(t, lines)
	for line, want := range map[int]string{0: "sk_live_abc", 3: "abc123", 6: "abcdef"} {
		if got[line] != want {
			t.Errorf("line %d masks %q, want %q", line, got[line], want)
		}
	}
}
