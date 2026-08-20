package logline

import "testing"

// origin_test.go covers the merged rotation set's origin separator (#1996):
// the format round-trips, a line that merely looks like one is not one, and
// the span stream styles it whole-line instead of parsing it as a log entry.

func TestOriginLineRoundTrip(t *testing.T) {
	for _, name := range []string{"app.log", "app.log.2026-08-01", "app.log.2.gz"} {
		line := OriginLine(name)
		got, ok := OriginName(line)
		if !ok || got != name {
			t.Fatalf("OriginName(%q) = %q/%v, want %q", line, got, ok, name)
		}
	}
}

func TestOriginNameRejectsOrdinaryLines(t *testing.T) {
	for _, line := range []string{
		"",
		"2026-08-10 10:11:12 INFO boot ok",
		"──── ",
		"──── ────",
		"see ──── app.log ────",
		"──── app.log",
		"──── a ──── b ────",
	} {
		if name, ok := OriginName(line); ok {
			t.Fatalf("OriginName(%q) = %q, want no match", line, name)
		}
	}
}

// TestOriginLineSpansWholeLine: the separator is structure, not a log entry —
// one span over the whole line, no header parsing underneath it.
func TestOriginLineSpansWholeLine(t *testing.T) {
	line := OriginLine("app.log.1")
	spans := Spans([]string{line, "2026-08-10 10:11:12 ERROR boom"})
	got := 0
	for _, s := range spans {
		if s.Line != 0 {
			continue
		}
		got++
		if s.Capture != "log.origin" {
			t.Fatalf("separator capture = %q, want log.origin", s.Capture)
		}
		if s.StartCol != 0 || s.EndCol != len([]rune(line)) {
			t.Fatalf("separator span = %d..%d, want the whole line (0..%d)", s.StartCol, s.EndCol, len([]rune(line)))
		}
	}
	if got != 1 {
		t.Fatalf("separator produced %d spans, want exactly one", got)
	}
	// The log line below it still parses as one.
	found := false
	for _, s := range spans {
		if s.Line == 1 && s.Capture == "log.error" {
			found = true
		}
	}
	if !found {
		t.Fatal("a separator must not disturb the lines around it")
	}
}
