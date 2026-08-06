package epochtime

import "testing"

// epochtime_test.go covers the detection heuristics (#1618): what decodes, in
// which context, and what is deliberately left alone.

// TestScanDecodesSecondsAndMillis: both widths decode to the same instant,
// with a millisecond fraction only when the value carries one.
func TestScanDecodesSecondsAndMillis(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{`{"ts": 1722945600}`, "2024-08-06 12:00:00Z"},
		{`{"ts": 1722945600000}`, "2024-08-06 12:00:00Z"},
		{`{"ts": 1722945600123}`, "2024-08-06 12:00:00.123Z"},
		// 9-digit seconds: 2001-01-01 is the lower bound of the range.
		{`{"ts": 978307200}`, "2001-01-01 00:00:00Z"},
		// A quoted value decodes too; the match covers the digits only.
		{`{"ts": "1722945600"}`, "2024-08-06 12:00:00Z"},
	}
	for _, c := range cases {
		got := Scan(c.line, JSONValue)
		if len(got) != 1 {
			t.Fatalf("Scan(%q) = %d matches, want 1", c.line, len(got))
		}
		if got[0].Text != c.want {
			t.Errorf("Scan(%q) decoded %q, want %q", c.line, got[0].Text, c.want)
		}
		if raw := []rune(c.line)[got[0].Start:got[0].End]; !allDigits(string(raw)) {
			t.Errorf("Scan(%q) range covers %q, want digits only", c.line, string(raw))
		}
	}
}

// TestScanRangeHeuristic: values outside 2001–2100, and digit runs of an
// implausible width, are ordinary numbers.
func TestScanRangeHeuristic(t *testing.T) {
	for _, line := range []string{
		`{"n": 1234}`,          // too short
		`{"n": 100000000}`,     // 1973, below the range
		`{"n": 9999999999}`,    // year 2286, above the range
		`{"n": 99999999999}`,   // 11 digits: neither seconds nor millis
		`{"n": 100000000000}`,  // 12 digits but 1973 in millis
		`{"n": 9999999999999}`, // 13 digits, year 2286
		`{"n": 0172294560}`,    // leading zero: a padded id, not a timestamp
	} {
		if got := Scan(line, JSONValue); len(got) != 0 {
			t.Errorf("Scan(%q) = %v, want no match", line, got)
		}
	}
}

// TestScanJSONContext: only value positions match — keys, digits inside prose
// strings and numbers glued to other tokens stay raw.
func TestScanJSONContext(t *testing.T) {
	match := []string{
		`{"ts":1722945600}`,               // no space after the colon
		`[1722945600, 1722945601]`,        // array elements
		`  "created_at": 1722945600`,      // pretty-printed, no trailing comma
		`{"a": 1, "ts": 1722945600, "b":`, // mid-line member
	}
	for _, line := range match {
		if got := Scan(line, JSONValue); len(got) == 0 {
			t.Errorf("Scan(%q) found nothing, want a match", line)
		}
	}
	skip := []string{
		`{"1722945600": "x"}`,             // key, not value
		`{"msg": "call 1722945600 now"}`,  // inside prose
		`{"msg": "1722945600, and more"}`, // string starting with digits
		`{"v": "v1722945600"}`,            // glued to a letter
		`{"n": 1722945600.5}`,             // a float
		`{"d": 2024-08-06T1722945600}`,    // glued to a date
		`1722945600`,                      // no value position at all
		`total 1722945600 bytes`,          // prose, not JSON
	}
	for _, line := range skip {
		if got := Scan(line, JSONValue); len(got) != 0 {
			t.Errorf("Scan(%q) = %v, want no match", line, got)
		}
	}
}

// TestScanLooseContext: log lines have no value grammar, so any run the
// delimiters do not glue to something larger decodes — but ISO dates, clock
// times, versions and identifiers still must not.
func TestScanLooseContext(t *testing.T) {
	match := []string{
		`INFO expires=1722945600 user=bob`,
		`[1722945600123] request done`,
		`(1722945600) done`,
		`1722945600 INFO boot`,
	}
	for _, line := range match {
		if got := Scan(line, Loose); len(got) != 1 {
			t.Errorf("Scan(%q, Loose) = %d matches, want 1", line, len(got))
		}
	}
	skip := []string{
		`2024-08-06 12:00:00,123 INFO up`, // an already-readable timestamp
		`build 1.1722945600`,              // a version-ish float
		`req-1722945600 failed`,           // an identifier
		`/api/v1/1722945600/items`,        // a path segment
		`GET /x?id=1722945600x`,           // glued to a letter
	}
	for _, line := range skip {
		if got := Scan(line, Loose); len(got) != 0 {
			t.Errorf("Scan(%q, Loose) = %v, want no match", line, got)
		}
	}
}

// TestSpansCarryStandIn: the produced spans are conceal-with-stand-in spans on
// the right line, with the decoded form as the replacement.
func TestSpansCarryStandIn(t *testing.T) {
	spans := Spans([]string{`{`, `  "ts": 1722945600`, `}`}, JSONValue)
	if len(spans) != 1 {
		t.Fatalf("Spans returned %d spans, want 1", len(spans))
	}
	s := spans[0]
	if s.Line != 1 || s.StartCol != 8 || s.EndCol != 18 {
		t.Errorf("span = line %d cols [%d,%d), want line 1 cols [8,18)", s.Line, s.StartCol, s.EndCol)
	}
	if s.Capture != Capture || s.Replace != "2024-08-06 12:00:00Z" {
		t.Errorf("span = capture %q replace %q, want %q / the decoded form", s.Capture, s.Replace, Capture)
	}
}

// TestLineSpansIndexesTheGivenLine: producers scanning only part of a buffer
// (a .http body) get spans on the line index they pass.
func TestLineSpansIndexesTheGivenLine(t *testing.T) {
	spans := LineSpans(7, `{"ts": 1722945600}`, JSONValue)
	if len(spans) != 1 || spans[0].Line != 7 {
		t.Fatalf("LineSpans = %v, want one span on line 7", spans)
	}
}

func allDigits(s string) bool {
	for _, r := range s {
		if !isDigit(r) {
			return false
		}
	}
	return s != ""
}
