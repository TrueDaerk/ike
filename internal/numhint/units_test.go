package numhint

import (
	"testing"

	"ike/internal/epochtime"
	"ike/internal/lang"
)

// units_test.go covers the field-name precedence and the user mapping (#1685):
// what a field name wins against, what a mapped unit renders, and that an
// empty mapping leaves the #1627 heuristics exactly as they were.

// mapping installs a field-unit mapping for the duration of one test.
func mapping(t *testing.T, entries ...string) {
	t.Helper()
	SetFieldUnits(entries)
	t.Cleanup(func() { SetFieldUnits(nil) })
}

// stamps produces the epoch stand-ins a config-format producer would hand to
// SpansWith for a one-line buffer.
func stamps(line string) []lang.Span {
	return epochtime.Spans([]string{line}, epochtime.Value)
}

// TestFieldContextBeatsTimestamp: a value in a field that names a byte count
// is a byte count, even when its digits also read as a Unix timestamp — a
// large enough byte count simply lands in that range.
func TestFieldContextBeatsTimestamp(t *testing.T) {
	const line = `bytes = 1722945600`
	if len(stamps(line)) != 1 {
		t.Fatalf("%q: the epoch family must claim the value for the test to mean anything", line)
	}
	hints, kept := SpansWith([]string{line}, stamps(line))
	if len(kept) != 0 {
		t.Errorf("%q kept %d timestamps, want the byte size to win: %+v", line, len(kept), kept)
	}
	if len(hints) != 1 || hints[0].Capture != SizeCapture || hints[0].Replace != "1.6 GiB" {
		t.Fatalf("%q = %+v; want one %s span reading 1.6 GiB", line, hints, SizeCapture)
	}
}

// TestTimestampBeatsShapeHint: without a field naming the unit, the timestamp
// still wins — the #1627 shape triggers keep giving way to the older family.
func TestTimestampBeatsShapeHint(t *testing.T) {
	const line = `id = 1722945600`
	hints, kept := SpansWith([]string{line}, stamps(line))
	if len(kept) != 1 || kept[0].Capture != epochtime.Capture {
		t.Fatalf("%q kept %+v; want the timestamp", line, kept)
	}
	if len(hints) != 0 {
		t.Errorf("%q produced %+v; want the number hints to step aside", line, hints)
	}
}

// TestMappedUnits: every unit a mapping entry can name, applied to a field the
// heuristics would read differently or not at all.
func TestMappedUnits(t *testing.T) {
	cases := []struct {
		entry   string
		line    string
		capture string
		replace string
	}{
		// A duration field the heuristics count in milliseconds, remapped to
		// seconds — the ambiguity the mapping exists for.
		{"duration=s", `duration: 86400`, DurationCapture, "24h"},
		{"retention=h", `retention: 72`, DurationCapture, "3d"},
		// A field the heuristics read as a duration, remapped to bytes.
		{"period=bytes", `period: 1048576`, SizeCapture, "1 MiB"},
		// Timestamps where the field pins the unit rather than the digit count.
		{"created=timestamp-s", `created: 1722945600`, epochtime.Capture, "2024-08-06 12:00:00Z"},
		{"created=timestamp-ms", `created: 1722945600000`, epochtime.Capture, "2024-08-06 12:00:00Z"},
		// Radix and grouping on fields no key word covers.
		{"acl=octal", `acl: 420`, RadixCapture, "420" + Gap + "= 0o644"},
		{"opts=hex", `opts: 255`, RadixCapture, "255" + Gap + "= 0xFF"},
		{"serial=group", `serial: 1000000`, GroupCapture, "1_000_000"},
		// Wildcards, camel case and case-insensitivity.
		{"*_bytes=bytes", `heap_bytes: 2097152`, SizeCapture, "2 MiB"},
		{"max_body_size=ms", `maxBodySize: 90000`, DurationCapture, "1m30s"},
		{"TIMEOUT=s", `timeout: 3600`, DurationCapture, "1h"},
	}
	for _, c := range cases {
		func() {
			mapping(t, c.entry)
			spans := Spans([]string{c.line})
			if len(spans) != 1 {
				t.Errorf("%q with %q produced %d spans, want 1: %+v", c.line, c.entry, len(spans), spans)
				return
			}
			if spans[0].Capture != c.capture || spans[0].Replace != c.replace {
				t.Errorf("%q with %q = %s %q; want %s %q", c.line, c.entry,
					spans[0].Capture, spans[0].Replace, c.capture, c.replace)
			}
		}()
	}
}

// TestMappedUnitIsFinal: a mapped field is read in its unit and no other. A
// value the unit says nothing about stays bare rather than falling through to
// the shape triggers, and the field still keeps the other families off it.
func TestMappedUnitIsFinal(t *testing.T) {
	mapping(t, "chunk=ms")
	// 4096 is a multiple of 1024 — the shape trigger would call it 4 KiB — but
	// the field is mapped to milliseconds, where it reads as 4s96ms.
	if capture, replace := hint(t, `chunk = 4096`); capture != DurationCapture || replace != "4s96ms" {
		t.Errorf("chunk = 4096 = %s %q; want %s %q", capture, replace, DurationCapture, "4s96ms")
	}
	mapping(t, "chunk=bytes")
	// Below a KiB the byte unit renders nothing, and no other family may step
	// in: the field said what the number is.
	none(t, `chunk = 512`)
}

// TestMappedNoneSilencesField: `none` is how a user turns a field off, for the
// number hints and for every other family over the same columns.
func TestMappedNoneSilencesField(t *testing.T) {
	mapping(t, "size=none", "ts=none")
	none(t, `size: 10485760`)
	const line = `ts = 1722945600`
	hints, kept := SpansWith([]string{line}, stamps(line))
	if len(hints) != 0 || len(kept) != 0 {
		t.Errorf("%q produced hints %+v and timestamps %+v; want the field silenced", line, hints, kept)
	}
}

// TestMappedFieldBeatsTimestamp: the mapping wins the same collision the
// built-in key words win, in units the built-ins never pick.
func TestMappedFieldBeatsTimestamp(t *testing.T) {
	mapping(t, "counter=group")
	const line = `counter = 1722945600`
	hints, kept := SpansWith([]string{line}, stamps(line))
	if len(kept) != 0 {
		t.Errorf("%q kept %+v; want the mapped unit to win", line, kept)
	}
	if len(hints) != 1 || hints[0].Capture != GroupCapture || hints[0].Replace != "1_722_945_600" {
		t.Fatalf("%q = %+v; want the grouped reading", line, hints)
	}
}

// TestFirstEntryWins: a specific name placed before a wildcard covering it
// keeps its own unit.
func TestFirstEntryWins(t *testing.T) {
	mapping(t, "cache_size=s", "*size=bytes")
	if capture, replace := hint(t, `cache_size: 3600`); capture != DurationCapture || replace != "1h" {
		t.Errorf("cache_size: 3600 = %s %q; want %s %q", capture, replace, DurationCapture, "1h")
	}
	if capture, _ := hint(t, `heap_size: 1048576`); capture != SizeCapture {
		t.Errorf("heap_size: 1048576 = %s; want %s", capture, SizeCapture)
	}
}

// TestUnmappedFieldsKeepDefaults: an installed mapping only touches the fields
// it names; everything else keeps the built-in reading.
func TestUnmappedFieldsKeepDefaults(t *testing.T) {
	mapping(t, "duration=s")
	if capture, replace := hint(t, `request_timeout: 90000`); capture != DurationCapture || replace != "1m30s" {
		t.Errorf("request_timeout: 90000 = %s %q; want the built-in millisecond reading", capture, replace)
	}
	if capture, replace := hint(t, `max_size: 10485760`); capture != SizeCapture || replace != "10 MiB" {
		t.Errorf("max_size: 10485760 = %s %q; want the built-in byte reading", capture, replace)
	}
}

// TestMalformedEntriesSkipped: a typo in one entry must not take the rest of
// the mapping down with it.
func TestMalformedEntriesSkipped(t *testing.T) {
	mapping(t, "no-separator", "=bytes", "ttl=parsecs", "count=bytes")
	if capture, replace := hint(t, `count: 1048576`); capture != SizeCapture || replace != "1 MiB" {
		t.Errorf("count: 1048576 = %s %q; want the surviving entry to apply", capture, replace)
	}
	// The unknown unit left `ttl` unmapped, so its built-in seconds reading stands.
	if capture, replace := hint(t, `ttl = 3600`); capture != DurationCapture || replace != "1h" {
		t.Errorf("ttl = 3600 = %s %q; want the built-in reading", capture, replace)
	}
}

// TestParseUnit: the accepted unit words, and what a wrong one does.
func TestParseUnit(t *testing.T) {
	cases := map[string]UnitKind{
		"none": UnitOff, "off": UnitOff, "bytes": UnitBytes,
		"timestamp": UnitTimestampSeconds, "timestamp-s": UnitTimestampSeconds,
		"timestamp-ms": UnitTimestampMillis, "octal": UnitOctal, "hex": UnitHex,
		"group": UnitGroup, "ms": UnitDuration, "seconds": UnitDuration, " H ": UnitDuration,
	}
	for name, kind := range cases {
		u, ok := ParseUnit(name)
		if !ok || u.Kind != kind {
			t.Errorf("ParseUnit(%q) = %+v, %v; want kind %v", name, u, ok, kind)
		}
	}
	if _, ok := ParseUnit("furlongs"); ok {
		t.Error("ParseUnit(\"furlongs\") accepted an unknown unit")
	}
}

// TestSetFieldUnitsReportsChange: the app re-parses open editors only when the
// mapping actually moved.
func TestSetFieldUnitsReportsChange(t *testing.T) {
	mapping(t, "size=bytes")
	if SetFieldUnits([]string{"size=bytes"}) {
		t.Error("re-installing the same mapping reported a change")
	}
	if !SetFieldUnits([]string{"size=ms"}) {
		t.Error("a changed mapping reported no change")
	}
}
