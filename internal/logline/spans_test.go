package logline

import (
	"strings"
	"testing"

	"ike/internal/lang"
)

func findSpan(t *testing.T, spans []spanLike, capture string) (int, int) {
	t.Helper()
	for _, s := range spans {
		if s.capture == capture {
			return s.start, s.end
		}
	}
	t.Fatalf("no %q span in %+v", capture, spans)
	return 0, 0
}

type spanLike struct {
	start, end int
	capture    string
}

func flat(lines []string) []spanLike {
	var out []spanLike
	for _, s := range Spans(lines) {
		out = append(out, spanLike{s.StartCol, s.EndCol, s.Capture})
	}
	return out
}

func TestSpansHeaderCaptures(t *testing.T) {
	spans := flat([]string{"2024-01-02 10:11:12 [main] ERROR com.example.Foo - boom"})
	if s, e := findSpan(t, spans, "log.time"); s != 0 || e != 19 {
		t.Errorf("log.time = [%d,%d)", s, e)
	}
	if s, e := findSpan(t, spans, "log.error"); s != 27 || e != 32 {
		t.Errorf("log.error = [%d,%d)", s, e)
	}
	want := map[string]bool{
		rainbowCapture("main"):            false,
		rainbowCapture("com.example.Foo"): false,
	}
	for _, s := range spans {
		if _, ok := want[s.capture]; ok {
			want[s.capture] = true
		}
	}
	for c, seen := range want {
		if !seen {
			t.Errorf("missing rainbow span %q", c)
		}
	}
}

// TestSpansDebugLineDims: a debug line gets a whole-line log.debug span after
// the header spans, so the header keeps its own styling.
func TestSpansDebugLineDims(t *testing.T) {
	line := "10:11:12 DEBUG polling"
	spans := flat([]string{line})
	last := spans[len(spans)-1]
	if last.capture != "log.debug" || last.start != 0 || last.end != len(line) {
		t.Errorf("whole-line dim span = %+v", last)
	}
	if spans[0].capture != "log.time" {
		t.Errorf("header span must come first: %+v", spans[0])
	}
}

// TestSpansANSIConcealAndRemap: escape bytes conceal, the styled run carries
// an ansi.* capture, and header fields parsed on the cleaned text map back to
// raw columns.
func TestSpansANSIConcealAndRemap(t *testing.T) {
	// "\x1b[31mERROR\x1b[0m boom" — raw cols: esc 0-5, ERROR 5-10, esc 10-14.
	spans := flat([]string{"\x1b[31mERROR\x1b[0m boom"})
	var conceals []spanLike
	for _, s := range spans {
		if s.capture == "conceal" {
			conceals = append(conceals, s)
		}
	}
	if len(conceals) != 2 || conceals[0] != (spanLike{0, 5, "conceal"}) || conceals[1] != (spanLike{10, 14, "conceal"}) {
		t.Fatalf("conceal spans = %+v", conceals)
	}
	if s, e := findSpan(t, spans, "log.error"); s != 5 || e != 10 {
		t.Errorf("remapped log.error = [%d,%d)", s, e)
	}
	if s, e := findSpan(t, spans, "ansi.fg1"); s != 5 || e != 10 {
		t.Errorf("ansi run = [%d,%d)", s, e)
	}
}

// TestSpansHeaderBeatsANSIRun: the span index takes the first covering span,
// so the severity capture must be emitted before the ANSI run covering it.
func TestSpansHeaderBeatsANSIRun(t *testing.T) {
	spans := flat([]string{"\x1b[31mERROR boom\x1b[0m"})
	for _, s := range spans {
		switch s.capture {
		case "log.error":
			return
		case "ansi.fg1":
			t.Fatal("ansi run emitted before the severity span")
		}
	}
	t.Fatal("no log.error span")
}

func TestSpansPlainLineEmpty(t *testing.T) {
	if spans := flat([]string{"nothing to see here"}); len(spans) != 0 {
		t.Errorf("plain line must yield no spans: %+v", spans)
	}
}

// TestSpansEpochTimestamps (#1618): a plausible epoch number anywhere on a log
// line becomes a conceal-with-stand-in span; implausible numbers and the
// already-readable header timestamp are left raw.
func TestSpansEpochTimestamps(t *testing.T) {
	line := "2024-08-06 12:00:00 INFO expires=1722945600 tries=3 port=8080"
	spans := lineSpans(0, line)
	var stamps []lang.Span
	for _, s := range spans {
		if s.Replace != "" {
			stamps = append(stamps, s)
		}
	}
	if len(stamps) != 1 {
		t.Fatalf("got %d stand-in spans, want 1: %+v", len(stamps), stamps)
	}
	if stamps[0].Replace != "2024-08-06 12:00:00Z" {
		t.Errorf("stand-in = %q, want the decoded UTC form", stamps[0].Replace)
	}
	if col := strings.Index(line, "1722945600"); stamps[0].StartCol != col {
		t.Errorf("stand-in starts at col %d, want %d", stamps[0].StartCol, col)
	}
}

// TestSpansEpochRemapsThroughANSI: an epoch number on an ANSI-coloured line
// maps back onto the raw columns, like the header spans do.
func TestSpansEpochRemapsThroughANSI(t *testing.T) {
	raw := "\x1b[31mERROR\x1b[0m at 1722945600"
	spans := lineSpans(0, raw)
	for _, s := range spans {
		if s.Replace == "" {
			continue
		}
		if got := []rune(raw)[s.StartCol:s.EndCol]; string(got) != "1722945600" {
			t.Errorf("stand-in covers %q, want the raw digits", string(got))
		}
		return
	}
	t.Fatal("no timestamp stand-in produced for the coloured line")
}
