package nethint

import (
	"testing"

	"ike/internal/lang"
)

// find returns the span covering [start, end) on line li, if any.
func find(spans []lang.Span, li, start, end int) (lang.Span, bool) {
	for _, s := range spans {
		if s.Line == li && s.StartCol == start && s.EndCol == end {
			return s, true
		}
	}
	return lang.Span{}, false
}

// TestSpansCIDR: a CIDR literal on a config line gets a stand-in repeating the
// literal with its range appended.
func TestSpansCIDR(t *testing.T) {
	spans := Spans([]string{"cidr: 10.0.0.0/8"})
	s, ok := find(spans, 0, 6, 16)
	if !ok {
		t.Fatalf("spans = %+v, want one covering the literal", spans)
	}
	if s.Capture != CIDRCapture {
		t.Errorf("capture = %q, want %q", s.Capture, CIDRCapture)
	}
	want := "10.0.0.0/8" + Gap + "10.0.0.0–10.255.255.255, 16,777,214 hosts"
	if s.Replace != want {
		t.Errorf("Replace = %q, want %q", s.Replace, want)
	}
}

// TestSpansCIDRQuoted: quoting changes nothing but the columns — the span
// covers the literal, not the quotes.
func TestSpansCIDRQuoted(t *testing.T) {
	spans := Spans([]string{`{"net": "2001:db8::/32"}`})
	if _, ok := find(spans, 0, 9, 22); !ok {
		t.Fatalf("spans = %+v, want one covering the quoted literal", spans)
	}
}

// TestSpansCIDRGuards: a prefix-shaped run that is part of something larger —
// a URL path, a longer path, a date — is not a prefix.
func TestSpansCIDRGuards(t *testing.T) {
	for _, line := range []string{
		"https://example.com/10.0.0.0/8",
		"path = /srv/10.0.0.0/8",
		"ratio = 10.0.0.0/8/4",
		"version 1.2.3/4",
		"date 2024/12",
		"cidr: 10.0.0.0/33",
	} {
		if spans := Spans([]string{line}); len(spans) != 0 {
			t.Errorf("Spans(%q) = %+v, want none", line, spans)
		}
	}
}

// TestSpansCIDRSentence: a prefix ending a sentence keeps its hint — a
// trailing period is punctuation, not part of the number.
func TestSpansCIDRSentence(t *testing.T) {
	spans := Spans([]string{"# allocated 192.168.1.0/24."})
	if _, ok := find(spans, 0, 12, 26); !ok {
		t.Fatalf("spans = %+v, want one covering the literal", spans)
	}
}

// TestSpansIDN: a punycode host decodes inline, and a homograph takes the
// warning capture instead.
func TestSpansIDN(t *testing.T) {
	spans := Spans([]string{"host: xn--mnchen-3ya.de"})
	s, ok := find(spans, 0, 6, 23)
	if !ok {
		t.Fatalf("spans = %+v, want one covering the host", spans)
	}
	if s.Capture != IDNCapture {
		t.Errorf("capture = %q, want %q", s.Capture, IDNCapture)
	}
	if want := "xn--mnchen-3ya.de" + Gap + "münchen.de"; s.Replace != want {
		t.Errorf("Replace = %q, want %q", s.Replace, want)
	}

	spans = Spans([]string{"url = https://xn--80ak6aa92e.com/login"})
	s, ok = find(spans, 0, 14, 32)
	if !ok {
		t.Fatalf("spans = %+v, want one covering the URL host", spans)
	}
	if s.Capture != IDNMixedCapture {
		t.Errorf("capture = %q, want %q — a homograph draws in the warning colour", s.Capture, IDNMixedCapture)
	}
}

// TestSpansIDNPort: a `:port` suffix rides along unchanged instead of
// breaking the decode.
func TestSpansIDNPort(t *testing.T) {
	spans := Spans([]string{"addr: xn--mnchen-3ya.de:8443"})
	s, ok := find(spans, 0, 6, 28)
	if !ok {
		t.Fatalf("spans = %+v, want one covering host and port", spans)
	}
	if want := "xn--mnchen-3ya.de:8443" + Gap + "münchen.de:8443"; s.Replace != want {
		t.Errorf("Replace = %q, want %q", s.Replace, want)
	}
}

// TestSpansPlainHost: a hostname without an ACE label carries no hint.
func TestSpansPlainHost(t *testing.T) {
	if spans := Spans([]string{"host: example.com", "note: xnormal.txt"}); len(spans) != 0 {
		t.Errorf("spans = %+v, want none", spans)
	}
}

// TestQuotedSpans: in a source file only string literals are scanned, so an
// expression outside one is never read as a network literal.
func TestQuotedSpans(t *testing.T) {
	lines := []string{
		`const subnet = "10.0.0.0/24"`,
		`ratio := width / 8`,
		"var host = `xn--mnchen-3ya.de`",
	}
	spans := QuotedSpans(lines)
	if _, ok := find(spans, 0, 16, 27); !ok {
		t.Fatalf("spans = %+v, want the quoted CIDR", spans)
	}
	if _, ok := find(spans, 2, 12, 29); !ok {
		t.Fatalf("spans = %+v, want the back-quoted host", spans)
	}
	for _, s := range spans {
		if s.Line == 1 {
			t.Errorf("span %+v: code outside a string literal must not be scanned", s)
		}
	}
}

// TestQuotedSpansUnterminated: an unterminated quote ends the scan of the
// line rather than swallowing the rest of it.
func TestQuotedSpansUnterminated(t *testing.T) {
	if spans := QuotedSpans([]string{`x := "10.0.0.0/8`}); len(spans) != 0 {
		t.Errorf("spans = %+v, want none", spans)
	}
}

// TestSpansMultiple: several literals on one line each get their own span, in
// column order.
func TestSpansMultiple(t *testing.T) {
	spans := Spans([]string{"allow = 10.0.0.0/8, 192.168.0.0/16"})
	if len(spans) != 2 {
		t.Fatalf("spans = %+v, want two", spans)
	}
	if spans[0].StartCol >= spans[1].StartCol {
		t.Errorf("spans %+v are not in column order", spans)
	}
}
