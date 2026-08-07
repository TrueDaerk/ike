package logline

import (
	"strings"
	"testing"

	"ike/internal/lang"
	"ike/internal/numhint"
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

// TestSpansLogfmtPairs (#1633): on the docker+logrus line every key dims, the
// msg value gains the message capture, the secondary time= value dims like the
// leading stamp, and plain values stay unstyled.
func TestSpansLogfmtPairs(t *testing.T) {
	line := `2026-08-06T19:06:13.303770698Z time="2026-08-06T19:06:13Z" level=info msg="Session done" Failed=0 notify=no`
	spans := flat([]string{line})
	runes := []rune(line)
	text := func(s spanLike) string { return string(runes[s.start:s.end]) }

	var keys, times, msgs, infos []string
	for _, s := range spans {
		switch s.capture {
		case "log.key":
			keys = append(keys, text(s))
		case "log.time":
			times = append(times, text(s))
		case "log.message":
			msgs = append(msgs, text(s))
		case "log.info":
			infos = append(infos, text(s))
		}
	}
	wantKeys := []string{"time=", "level=", "msg=", "Failed=", "notify="}
	if len(keys) != len(wantKeys) {
		t.Fatalf("log.key spans = %+v, want %v", keys, wantKeys)
	}
	for i, w := range wantKeys {
		if keys[i] != w {
			t.Errorf("log.key %d = %q, want %q", i, keys[i], w)
		}
	}
	if len(times) != 2 || times[0] != "2026-08-06T19:06:13.303770698Z" || times[1] != "2026-08-06T19:06:13Z" {
		t.Errorf("log.time spans = %+v, want the leading and the time= stamp", times)
	}
	if len(msgs) != 1 || msgs[0] != "Session done" {
		t.Errorf("log.message spans = %+v", msgs)
	}
	// level=info keeps its single severity span — the header emitted it, the
	// pair pass must not duplicate it.
	if len(infos) != 1 || infos[0] != "info" {
		t.Errorf("log.info spans = %+v", infos)
	}
	for _, s := range spans {
		if s.capture == "log.key" || s.capture == "conceal" {
			continue
		}
		if txt := text(s); txt == "0" || txt == "no" {
			t.Errorf("plain value %q styled as %q", txt, s.capture)
		}
	}
}

// TestSpansLogfmtNamePairRainbow: a logger= pair past the header still colors
// through the rainbow mechanic, once.
func TestSpansLogfmtNamePairRainbow(t *testing.T) {
	spans := flat([]string{`msg="up" logger=db.pool retries=0`})
	var got int
	for _, s := range spans {
		if s.capture == rainbowCapture("db.pool") {
			got++
		}
	}
	if got != 1 {
		t.Errorf("rainbow spans for logger value = %d, want 1", got)
	}
}

// TestSpansLogfmtFallback: a prose line carrying a single unknown pair is left
// alone — no key dimming, no message emphasis.
func TestSpansLogfmtFallback(t *testing.T) {
	if spans := flat([]string{"solving for x=42 today"}); len(spans) != 0 {
		t.Errorf("prose line styled: %+v", spans)
	}
}

// TestSpansLogfmtThroughANSI: pair ranges are scanned on the visible text and
// map back onto the raw columns like the header spans do.
func TestSpansLogfmtThroughANSI(t *testing.T) {
	raw := "\x1b[31mlevel=error\x1b[0m msg=\"boom\""
	spans := flat([]string{raw})
	runes := []rune(raw)
	for _, s := range spans {
		if s.capture != "log.message" {
			continue
		}
		if got := string(runes[s.start:s.end]); got != "boom" {
			t.Errorf("log.message covers %q, want the raw message", got)
		}
		return
	}
	t.Fatal("no log.message span on the coloured line")
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

// TestSpansNumberHints (#1684): the logfmt pairs and JSON tail of a log line
// are value positions, so their numbers carry the #1627 readability hints —
// while the keys, the header fields and the epoch stand-in stay off limits.
func TestSpansNumberHints(t *testing.T) {
	line := "2024-08-06 12:00:00 INFO [worker-1] upload done bytes=10485760 timeout_ms=90000 expires=1722945600"
	spans := lineSpans(0, line)
	standIn := func(sub string) (lang.Span, bool) {
		col := strings.Index(line, sub)
		for _, s := range spans {
			if s.Replace != "" && col >= s.StartCol && col < s.EndCol {
				return s, true
			}
		}
		return lang.Span{}, false
	}
	for _, tc := range []struct{ sub, want string }{
		{"10485760", "10 MiB"},
		{"90000", "1m30s"},
		// The epoch family keeps the digits it already claimed (#1618).
		{"1722945600", "2024-08-06 12:00:00Z"},
	} {
		if s, ok := standIn(tc.sub); !ok || s.Replace != tc.want {
			t.Errorf("stand-in at %q = %+v, want %q", tc.sub, s, tc.want)
		}
	}
	for _, key := range []string{"bytes=", "timeout_ms=", "expires="} {
		if s, ok := standIn(key); ok {
			t.Errorf("logfmt key %q concealed as %q", key, s.Replace)
		}
	}
	// The header fields keep their own captures: the readable timestamp must
	// not be re-read as a grouped number, nor the thread name touched.
	for _, sub := range []string{"2024-08-06", "12:00:00", "worker-1"} {
		if s, ok := standIn(sub); ok {
			t.Errorf("header field %q concealed as %q", sub, s.Replace)
		}
	}
}

// TestSpansFieldUnitPrecedence (#1685): a logfmt key that names the unit beats
// the epoch reading of its value — a byte count in the timestamp range is
// still a byte count — and a user mapping silences a field outright.
func TestSpansFieldUnitPrecedence(t *testing.T) {
	line := "2024-08-06 12:00:00 INFO upload bytes=1722945600"
	standIn := func(spans []lang.Span, sub string) (lang.Span, bool) {
		col := strings.Index(line, sub)
		for _, s := range spans {
			if s.Replace != "" && col >= s.StartCol && col < s.EndCol {
				return s, true
			}
		}
		return lang.Span{}, false
	}
	s, ok := standIn(lineSpans(0, line), "1722945600")
	if !ok || s.Capture != numhint.SizeCapture || s.Replace != "1.6 GiB" {
		t.Errorf("stand-in = %+v, %v; want the byte size to win over the timestamp", s, ok)
	}
	numhint.SetFieldUnits([]string{"bytes=none"})
	defer numhint.SetFieldUnits(nil)
	if s, ok := standIn(lineSpans(0, line), "1722945600"); ok {
		t.Errorf("stand-in = %+v; want the field mapped to none silenced", s)
	}
}
