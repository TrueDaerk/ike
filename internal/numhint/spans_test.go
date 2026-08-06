package numhint

import "testing"

// spans_test.go covers the context half of the number hints (#1627): which key
// names which family, which shapes trigger on their own, and everything that
// must stay untouched — floats, versions, dates, ports, comments, keys.

// hint returns the single span on a one-line buffer, failing when the line
// produced anything but exactly one.
func hint(t *testing.T, line string) (capture, replace string) {
	t.Helper()
	spans := Spans([]string{line})
	if len(spans) != 1 {
		t.Fatalf("%q produced %d spans, want 1: %+v", line, len(spans), spans)
	}
	return spans[0].Capture, spans[0].Replace
}

// none asserts a line produces no hint at all.
func none(t *testing.T, line string) {
	t.Helper()
	if spans := Spans([]string{line}); len(spans) != 0 {
		t.Errorf("%q produced %d spans, want none: %+v", line, len(spans), spans)
	}
}

// TestFamilies: one line per family, in the format each is written in.
func TestFamilies(t *testing.T) {
	cases := []struct {
		line    string
		capture string
		replace string
	}{
		// Byte sizes: named by the key, or a multiple of 1024 on shape alone.
		{`max_size: 10485760`, SizeCapture, "10 MiB"},
		{`MAX_UPLOAD_BYTES=5242880`, SizeCapture, "5 MiB"},
		{`chunk = 4096`, SizeCapture, "4 KiB"},
		{`"memory": 1610612736`, SizeCapture, "1.5 GiB"},
		// Durations: the unit word pins the base, the family word implies it.
		{`ttl_ms: 86400000`, DurationCapture, "24h"},
		{`request_timeout: 90000`, DurationCapture, "1m30s"},
		{`ttl = 3600`, DurationCapture, "1h"},
		{`cacheTtlSeconds: 604800`, DurationCapture, "7d"},
		// Digit grouping: plain large integers.
		{`count: 1000000`, GroupCapture, "1_000_000"},
		{`rate_limit: 100000`, GroupCapture, "100_000"},
		// Radix: hex reads decimal, permissions and flags read the other way.
		{`mask: 0x1F4`, RadixCapture, "0x1F4" + Gap + "= 500"},
		{`mode: 420`, RadixCapture, "420" + Gap + "= 0o644"},
		{`flags = 255`, RadixCapture, "255" + Gap + "= 0xFF"},
	}
	for _, c := range cases {
		capture, replace := hint(t, c.line)
		if capture != c.capture || replace != c.replace {
			t.Errorf("%q = %s %q; want %s %q", c.line, capture, replace, c.capture, c.replace)
		}
	}
}

// TestNoHint: everything the heuristics must leave alone. A wrong hint is
// worse than none, so the guards are what carry the feature.
func TestNoHint(t *testing.T) {
	lines := []string{
		`port: 8080`,              // four digits: a port, not a quantity
		`retries: 3`,              // small counts read fine
		`version: 1.2.3`,          // glued by "."
		`released: 2024-01-15`,    // glued by "-"
		`ratio: 0.75`,             // a float
		`usage: 85%`,              // glued by "%"
		`path: /var/log/1048576`,  // glued by "/"
		`started: 12:30`,          // a clock time
		`id: 0001234567`,          // zero-padded: an id
		`# 1000000 requests`,      // a comment line
		`; buffer_size = 65536`,   // an ini comment line
		`count: 5000 # 1000000`,   // a trailing comment
		`"1000000": true`,         // a key, not a value
		`created: 1722945600`,     // an epoch timestamp: #1618's family
		`file_mode: 7`,            // identical in both bases
		`addr: 0x9`,               // identical in both bases
		`sha: 0xdeadbeefcafe1234`, // a hash, not a number
	}
	for _, line := range lines {
		none(t, line)
	}
}

// TestWeakKeysAreNotSizes: a limit is not a byte count. `limit` alone must not
// pull a number into the byte-size family — a rate limit would read as KiB.
func TestWeakKeysAreNotSizes(t *testing.T) {
	if capture, _ := hint(t, `rate_limit: 100000`); capture != GroupCapture {
		t.Errorf("rate_limit landed in %s, want %s", capture, GroupCapture)
	}
	if capture, replace := hint(t, `body_limit_bytes: 10485760`); capture != SizeCapture || replace != "10 MiB" {
		t.Errorf("a limit key naming bytes = %s %q, want %s 10 MiB", capture, replace, SizeCapture)
	}
}

// TestDurationKeyBeatsSizeShape: 86400000 is a multiple of 1024 by accident, so
// the key has to win over the shape trigger.
func TestDurationKeyBeatsSizeShape(t *testing.T) {
	if capture, replace := hint(t, `ttl_ms: 86400000`); capture != DurationCapture || replace != "24h" {
		t.Errorf("ttl_ms: 86400000 = %s %q, want %s 24h", capture, replace, DurationCapture)
	}
}

// TestUnitWordBoundary: the unit is the key's last *word*, so `params` is not
// a millisecond key and camelCase splits like snake_case does.
func TestUnitWordBoundary(t *testing.T) {
	if capture, _ := hint(t, `params: 1500000`); capture != GroupCapture {
		t.Errorf("params landed in %s, want %s", capture, GroupCapture)
	}
	if capture, replace := hint(t, `flushMs: 90000`); capture != DurationCapture || replace != "1m30s" {
		t.Errorf("flushMs = %s %q, want %s 1m30s", capture, replace, DurationCapture)
	}
	if capture, replace := hint(t, `FLUSH_MS=90000`); capture != DurationCapture || replace != "1m30s" {
		t.Errorf("FLUSH_MS = %s %q, want %s 1m30s", capture, replace, DurationCapture)
	}
}

// TestJSONLine: a one-line object keeps each value with its own key, and an
// array shares the key it hangs off.
func TestJSONLine(t *testing.T) {
	spans := Spans([]string{`{"max_size": 10485760, "ttl_ms": 86400000, "count": 1000000}`})
	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3: %+v", len(spans), spans)
	}
	want := []string{"10 MiB", "24h", "1_000_000"}
	for i, s := range spans {
		if s.Replace != want[i] {
			t.Errorf("span %d = %q, want %q", i, s.Replace, want[i])
		}
	}
	spans = Spans([]string{`"sizes": [1024, 2048]`})
	if len(spans) != 2 || spans[0].Replace != "1 KiB" || spans[1].Replace != "2 KiB" {
		t.Errorf("array values must share their key's family: %+v", spans)
	}
}

// TestSpanColumns: the span covers exactly the literal, quotes excluded, so
// the caret reveal lands on the digits themselves.
func TestSpanColumns(t *testing.T) {
	spans := Spans([]string{`size = 10485760`})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if s := spans[0]; s.Line != 0 || s.StartCol != 7 || s.EndCol != 15 {
		t.Errorf("span = %+v, want line 0 cols [7,15)", s)
	}
	spans = Spans([]string{`SIZE="10485760"`})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if s := spans[0]; s.StartCol != 6 || s.EndCol != 14 {
		t.Errorf("quoted span = %+v, want cols [6,14)", s)
	}
}
