package logline

import "testing"

// TestRepeatKeyIgnoresTimestamps: two lines differing only in their timestamps
// — the leading header stamp and the logfmt `time=` value — share a key.
func TestRepeatKeyIgnoresTimestamps(t *testing.T) {
	a := `2026-08-06T19:06:13.303770698Z time="2026-08-06T19:06:13Z" level=info msg="Session done" Failed=0`
	b := `2026-08-06T19:16:43.253386315Z time="2026-08-06T19:16:43Z" level=info msg="Session done" Failed=0`
	if RepeatKey(a) != RepeatKey(b) {
		t.Errorf("keys differ:\n%q\n%q", RepeatKey(a), RepeatKey(b))
	}
}

// TestRepeatKeyKeepsMessage: a different message never collapses, however
// close the timestamps are.
func TestRepeatKeyKeepsMessage(t *testing.T) {
	a := `time=2026-08-06T19:06:13Z level=info msg="Session done"`
	b := `time=2026-08-06T19:06:13Z level=info msg="Session failed"`
	if RepeatKey(a) == RepeatKey(b) {
		t.Error("different messages must not share a repeat key")
	}
}

// TestRepeatKeyOtherLayouts: the logback-style header stamp blanks too, and a
// changed severity still separates two lines.
func TestRepeatKeyOtherLayouts(t *testing.T) {
	a := "2024-01-02 10:11:12,345 [main] INFO com.example.Foo - poll"
	b := "2024-01-02 10:11:42,001 [main] INFO com.example.Foo - poll"
	c := "2024-01-02 10:11:42,001 [main] WARN com.example.Foo - poll"
	if RepeatKey(a) != RepeatKey(b) {
		t.Errorf("logback repeats must share a key:\n%q\n%q", RepeatKey(a), RepeatKey(b))
	}
	if RepeatKey(b) == RepeatKey(c) {
		t.Error("different severities must not share a repeat key")
	}
}

// TestRepeatKeyUnparsedLine: a line with no recognized timestamp compares
// verbatim, so unrelated prose never collides.
func TestRepeatKeyUnparsedLine(t *testing.T) {
	line := "\tat com.example.Foo.run(Foo.java:42)"
	if got := RepeatKey(line); got != line {
		t.Errorf("RepeatKey = %q, want the line unchanged", got)
	}
	if RepeatKey("first line") == RepeatKey("second line") {
		t.Error("distinct prose lines must not share a key")
	}
}

// same asserts that two lines count as repeats of each other.
func same(t *testing.T, a, b string) {
	t.Helper()
	if RepeatKey(a) != RepeatKey(b) {
		t.Errorf("lines must share a repeat key:\n%q\n%q\nkeys:\n%q\n%q",
			a, b, RepeatKey(a), RepeatKey(b))
	}
}

// differ asserts that two lines never collapse into one run.
func differ(t *testing.T, a, b string) {
	t.Helper()
	if RepeatKey(a) == RepeatKey(b) {
		t.Errorf("lines must not share a repeat key:\n%q\n%q\nkey: %q",
			a, b, RepeatKey(a))
	}
}

// TestRepeatKeyDurationPairs: the value of a duration key moves between
// repeats without making them different statements (#1758).
func TestRepeatKeyDurationPairs(t *testing.T) {
	same(t, `level=info msg="poll done" elapsed=1.2s`, `level=info msg="poll done" elapsed=1.4s`)
	same(t, `msg="poll done" took=340ms`, `msg="poll done" took=2m30s`)
	same(t, `msg=sync duration=00:00:12`, `msg=sync duration=00:01:47`)
	same(t, `msg=sync latency_ms=17`, `msg=sync latency_ms=2043`)
	differ(t, `msg="poll done" elapsed=1.2s`, `msg="poll failed" elapsed=1.2s`)
}

// TestRepeatKeyDurationTokens: a free-standing duration in the message blanks
// too, so "took 340ms" and "took 1.2s" are one run.
func TestRepeatKeyDurationTokens(t *testing.T) {
	same(t, "10:11:12 INFO request served, took 340ms", "10:11:42 INFO request served, took 1.2s")
	same(t, "INFO finished in 2m30s", "INFO finished in 1h2m3s")
	same(t, "INFO waited 3 seconds", "INFO waited 12 seconds")
	// The unit blanks with the number, so the plural does not separate them.
	same(t, "INFO waited 1 second", "INFO waited 3 seconds")
	same(t, "INFO backup ran 00:00:12", "INFO backup ran 01:12:44")
}

// TestRepeatKeyPagination: page numbers, offsets, cursors and counters blank,
// whether they arrive as a logfmt value or inside the message.
func TestRepeatKeyPagination(t *testing.T) {
	same(t, "fetching page 17 of 240", "fetching page 18 of 240")
	same(t, "fetching page 17 of 240", "fetching page 239 of 241")
	same(t, `msg="fetch" offset=3200`, `msg="fetch" offset=3400`)
	same(t, `msg="fetch" cursor=eyJpZCI6MX0`, `msg="fetch" cursor=eyJpZCI6Mn0`)
	same(t, `msg="upload" attempt=3 retry=2`, `msg="upload" attempt=4 retry=5`)
	same(t, "retry 2/5 for job sync", "retry 3/5 for job sync")
	same(t, "processed 1500 rows", "processed 3000 rows")
	same(t, "import at 42%", "import at 43%")
	same(t, "fetching page 17 of 1,500", "fetching page 18 of 1,500")
	same(t, "Step 3/14 : RUN make", "Step 4/14 : RUN make")
	differ(t, "fetching page 17 of 240", "flushing page 17 of 240")
	differ(t, "Step 3/14 : RUN make", "Step 4/14 : RUN test")
	// The noun says what was counted, so only the count blanks.
	differ(t, "processed 1500 rows", "processed 1500 files")
}

// TestRepeatKeyCombined: timestamp, duration and page number moving at the
// same time still folds — the ranges merge into one splice.
func TestRepeatKeyCombined(t *testing.T) {
	a := `2026-08-06T19:06:13.303770698Z time="2026-08-06T19:06:13Z" level=info msg="fetching page 17 of 240" elapsed=1.2s`
	b := `2026-08-06T19:16:43.253386315Z time="2026-08-06T19:16:43Z" level=info msg="fetching page 18 of 240" elapsed=980ms`
	same(t, a, b)
}

// TestRepeatKeyKeepsFixedNumbers: the conservative half of #1758 — a number
// whose key or shape says nothing about counting up is part of the statement
// and must keep the lines apart.
func TestRepeatKeyKeepsFixedNumbers(t *testing.T) {
	differ(t, `msg="request done" status=200`, `msg="request done" status=500`)
	differ(t, `msg=listen port=8080`, `msg=listen port=9090`)
	differ(t, `msg=lookup user_id=4711`, `msg=lookup user_id=4712`)
	differ(t, "GET /api/v1/users/42 200", "GET /api/v1/users/43 200")
	differ(t, "connect to 10.0.0.1:5432 failed", "connect to 10.0.0.2:5432 failed")
	differ(t, "loaded plugin v1.2.3", "loaded plugin v1.2.4")
	differ(t, "\tat com.example.Foo.run(Foo.java:42)", "\tat com.example.Foo.run(Foo.java:43)")
}

// TestTimestampRangesMerge: overlapping stamp ranges (the header stamp is also
// a `ts=` value) merge into one, so the splice stays ordered.
func TestTimestampRangesMerge(t *testing.T) {
	rs := timestampRanges(`ts=2024-01-02T10:11:12Z level=info msg=x time=2024-01-02T10:11:12Z`)
	for i := 1; i < len(rs); i++ {
		if rs[i].Start < rs[i-1].End {
			t.Fatalf("ranges overlap or unsorted: %+v", rs)
		}
	}
	if len(rs) == 0 {
		t.Fatal("both timestamp values must be recognized")
	}
}
