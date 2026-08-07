package langini

import (
	"testing"

	"ike/internal/lang"
	"ike/internal/nethint"
	"ike/internal/numhint"
)

func captureAt(spans []lang.Span, line, col int) string {
	for _, s := range spans {
		if s.Line == line && col >= s.StartCol && col < s.EndCol {
			return s.Capture
		}
	}
	return ""
}

// TestRegistration: .ini and .conf resolve to the ini language (#1595).
func TestRegistration(t *testing.T) {
	for _, path := range []string{"settings.ini", "nginx.conf"} {
		if l, ok := lang.ByPath(path); !ok || l.ID != "ini" {
			t.Errorf("ByPath(%q) = %+v, %v, want ini", path, l, ok)
		}
	}
}

// TestSectionSpans: `[section]` styles brackets as punctuation, the name as
// type; leading whitespace is skipped.
func TestSectionSpans(t *testing.T) {
	spans := iniSpans([]string{"[core]", "  [remote \"origin\"]"})
	if got := captureAt(spans, 0, 0); got != "punctuation" {
		t.Errorf("open bracket = %q, want punctuation", got)
	}
	if got := captureAt(spans, 0, 1); got != "type" {
		t.Errorf("section name = %q, want type", got)
	}
	if got := captureAt(spans, 0, 5); got != "punctuation" {
		t.Errorf("close bracket = %q, want punctuation", got)
	}
	if got := captureAt(spans, 1, 2); got != "punctuation" {
		t.Errorf("indented open bracket = %q, want punctuation", got)
	}
	if got := captureAt(spans, 1, 3); got != "type" {
		t.Errorf("indented section name = %q, want type", got)
	}
}

// TestPairSpans: key = value styles property / punctuation / string; the
// colon separator works too and a bare flag styles whole as property.
func TestPairSpans(t *testing.T) {
	spans := iniSpans([]string{"user = geant", "port: 8080", "hidepid"})
	if got := captureAt(spans, 0, 0); got != "property" {
		t.Errorf("key = %q, want property", got)
	}
	if got := captureAt(spans, 0, 4); got != "" {
		t.Errorf("space before separator = %q, want unstyled", got)
	}
	if got := captureAt(spans, 0, 5); got != "punctuation" {
		t.Errorf("separator = %q, want punctuation", got)
	}
	if got := captureAt(spans, 0, 7); got != "string" {
		t.Errorf("value = %q, want string", got)
	}
	if got := captureAt(spans, 1, 4); got != "punctuation" {
		t.Errorf("colon separator = %q, want punctuation", got)
	}
	if got := captureAt(spans, 1, 6); got != "string" {
		t.Errorf("colon value = %q, want string", got)
	}
	if got := captureAt(spans, 2, 3); got != "property" {
		t.Errorf("bare flag = %q, want property", got)
	}
}

// TestCommentSpans: full-line #/; comments style as comment; a # inside a
// value stays part of the value (dialects disagree on inline comments).
func TestCommentSpans(t *testing.T) {
	spans := iniSpans([]string{"# top", "  ; note", "url = http://x/#frag", ""})
	if got := captureAt(spans, 0, 0); got != "comment" {
		t.Errorf("hash comment = %q, want comment", got)
	}
	if got := captureAt(spans, 1, 2); got != "comment" {
		t.Errorf("semicolon comment = %q, want comment", got)
	}
	if got := captureAt(spans, 2, 15); got != "string" {
		t.Errorf("hash inside value = %q, want string", got)
	}
}

// TestININumberHints (#1627): an ini `key = value` pair carries the number
// hints alongside its plain styling.
func TestININumberHints(t *testing.T) {
	spans := iniSpans([]string{"buffer_size = 65536"})
	var hint *lang.Span
	for i, s := range spans {
		if s.Capture == numhint.SizeCapture {
			hint = &spans[i]
		}
	}
	if hint == nil || hint.Replace != "64 KiB" {
		t.Errorf("spans = %+v, want a 64 KiB size hint", spans)
	}
}

// TestININetworkHints (#1653): a CIDR prefix in an ini value carries its
// range and host count.
func TestININetworkHints(t *testing.T) {
	l, ok := lang.ByID("ini")
	if !ok || l.Spans == nil {
		t.Fatal("ini: no Spans producer registered")
	}
	spans := l.Spans([]string{"allow = 172.16.0.0/12"})
	var hint *lang.Span
	for i, s := range spans {
		if s.Capture == nethint.CIDRCapture {
			hint = &spans[i]
		}
	}
	if hint == nil || hint.Replace != "172.16.0.0/12"+nethint.Gap+"172.16.0.0–172.31.255.255, 1,048,574 hosts" {
		t.Errorf("spans = %+v, want the CIDR hint", spans)
	}
}
