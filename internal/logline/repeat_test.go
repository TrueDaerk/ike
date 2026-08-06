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
