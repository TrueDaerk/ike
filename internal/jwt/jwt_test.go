package jwt

import (
	"encoding/base64"
	"strings"
	"testing"
)

// token builds a JWT-shaped string from raw header/payload JSON, so tests read
// as the JSON they mean instead of as base64 blobs.
func token(header, payload, sig string) string {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	return enc(header) + "." + enc(payload) + "." + sig
}

const sig = "dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"

func sample() string {
	return token(`{"alg":"HS256","typ":"JWT"}`, `{"sub":"1234567890","iat":1722945600,"exp":1722949200}`, sig)
}

func TestScanFindsTokenInHeaderLine(t *testing.T) {
	tok := sample()
	line := "Authorization: Bearer " + tok
	ms := Scan(line)
	if len(ms) != 1 {
		t.Fatalf("Scan found %d tokens, want 1", len(ms))
	}
	m := ms[0]
	if got := line[m.Start:m.End]; got != tok {
		t.Errorf("token range covers %q, want %q", got, tok)
	}
	if got := line[m.SigStart:m.End]; got != sig {
		t.Errorf("signature range covers %q, want %q", got, sig)
	}
	if !strings.Contains(m.Token.Payload, `"sub"`) {
		t.Errorf("payload = %q, want the decoded JSON", m.Token.Payload)
	}
}

func TestScanRuneColumns(t *testing.T) {
	// Columns are rune columns: a multi-byte prefix must not shift them.
	tok := sample()
	line := "# tökén: " + tok
	ms := Scan(line)
	if len(ms) != 1 {
		t.Fatalf("Scan found %d tokens, want 1", len(ms))
	}
	runes := []rune(line)
	if got := string(runes[ms[0].Start:ms[0].End]); got != tok {
		t.Errorf("token range covers %q, want the token", got)
	}
}

func TestScanIgnoresNonTokens(t *testing.T) {
	for _, line := range []string{
		"version = 1.2.3",
		"HOST=api.example.com",
		"sha=d41d8cd98f00b204e9800998ecf8427e",
		"eyJhbGciOiJIUzI1NiJ9",                        // one segment only
		"eyJhbGciOiJIUzI1NiJ9." + sig,                 // two segments
		"notbase64json.alsonot." + sig,                // segments decode, but not to JSON
		token(`{"alg":"HS256"}`, `["a"]`, sig),        // payload is not an object
		token(`{"alg":"HS256"}`, `{"a":1}`, "") + ".", // empty signature
	} {
		if ms := Scan(line); len(ms) != 0 {
			t.Errorf("Scan(%q) = %d matches, want none", line, len(ms))
		}
	}
}

func TestScanTrailingPunctuation(t *testing.T) {
	// A token ending a sentence pulls the period into the character run; the
	// match must stop at the signature.
	tok := sample()
	ms := Scan("use " + tok + ".")
	if len(ms) != 1 {
		t.Fatalf("Scan found %d tokens, want 1", len(ms))
	}
	if got := ("use " + tok + ".")[ms[0].Start:ms[0].End]; got != tok {
		t.Errorf("match covers %q, want the token without the period", got)
	}
}

func TestScanMultipleTokensPerLine(t *testing.T) {
	a, b := sample(), token(`{"alg":"none"}`, `{"sub":"x"}`, "AAAA")
	if ms := Scan(a + " " + b); len(ms) != 2 {
		t.Fatalf("Scan found %d tokens, want 2", len(ms))
	}
}

func TestDecodeMalformed(t *testing.T) {
	for _, raw := range []string{
		"",
		"a.b",
		"a.b.c.d",
		"eyJhbGciOiJIUzI1NiJ9..sig", // empty payload
		"@@@." + base64.RawURLEncoding.EncodeToString([]byte(`{}`)) + ".sig", // not base64url
		token(`{"alg":"HS256"`, `{"a":1}`, sig),                              // truncated header JSON
	} {
		if _, ok := Decode(raw); ok {
			t.Errorf("Decode(%q) accepted a malformed token", raw)
		}
	}
}

func TestDecodeToleratesPadding(t *testing.T) {
	enc := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	raw := enc(`{"alg":"HS256"}`) + "." + enc(`{"sub":"1"}`) + "." + sig
	if !strings.Contains(raw, "=") {
		t.Fatal("test fixture is not padded")
	}
	tok, ok := Decode(raw)
	if !ok {
		t.Fatal("Decode rejected a padded token")
	}
	if tok.Header != `{"alg":"HS256"}` {
		t.Errorf("header = %q", tok.Header)
	}
}

func TestPopupDecodesRegisteredClaims(t *testing.T) {
	tok, ok := Decode(sample())
	if !ok {
		t.Fatal("Decode rejected the sample token")
	}
	out := tok.Popup()
	for _, want := range []string{
		"Header", "Payload", `"alg": "HS256"`,
		`"iat": 1722945600,  // 2024-08-06 12:00:00Z`,
		`"exp": 1722949200  // 2024-08-06 13:00:00Z`,
		"```json",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("popup missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "Signature:") || strings.Contains(out, "\n"+sig) {
		t.Errorf("popup must name the signature without decoding it:\n%s", out)
	}
}

func TestPopupLeavesImplausibleClaimsRaw(t *testing.T) {
	// The epoch decoding is a soft dependency: a value outside the plausible
	// range keeps its raw number rather than inventing a date.
	tok, ok := Decode(token(`{"alg":"HS256"}`, `{"exp":42,"nbf":1722945600}`, sig))
	if !ok {
		t.Fatal("Decode rejected the token")
	}
	out := tok.Popup()
	if !strings.Contains(out, `"exp": 42`) || strings.Contains(out, `"exp": 42  //`) {
		t.Errorf("implausible exp must stay raw:\n%s", out)
	}
	if !strings.Contains(out, `"nbf": 1722945600  // 2024-08-06`) {
		t.Errorf("plausible nbf must decode:\n%s", out)
	}
}

func TestAtPrefersTheTokenUnderTheCaret(t *testing.T) {
	a, b := sample(), token(`{"alg":"none"}`, `{"sub":"x"}`, "AAAA")
	line := a + " " + b
	m, ok := At(line, len(a)+3)
	if !ok {
		t.Fatal("At found no token")
	}
	if m.Token.Raw != b {
		t.Error("At must return the token the caret is inside")
	}
	// Off any token, the line's first one answers.
	m, ok = At(line, 0)
	if !ok || m.Token.Raw != a {
		t.Error("At must fall back to the line's first token")
	}
	if _, ok := At("KEY=value", 0); ok {
		t.Error("At must report false without a token")
	}
}

func TestLineSpansDimOnlyTheSignature(t *testing.T) {
	tok := sample()
	line := "TOKEN=" + tok
	spans := LineSpans(3, line)
	if len(spans) != 1 {
		t.Fatalf("LineSpans = %d spans, want 1", len(spans))
	}
	s := spans[0]
	if s.Line != 3 || s.Capture != Capture {
		t.Errorf("span = %+v, want line 3 capture %q", s, Capture)
	}
	if got := line[s.StartCol:s.EndCol]; got != sig {
		t.Errorf("span covers %q, want the signature", got)
	}
}

func TestSpansScansEveryLine(t *testing.T) {
	spans := Spans([]string{"plain", "A=" + sample(), ""})
	if len(spans) != 1 || spans[0].Line != 1 {
		t.Fatalf("Spans = %+v, want one span on line 1", spans)
	}
}
