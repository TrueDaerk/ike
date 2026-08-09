package logline

import (
	"testing"
	"time"
)

// TestParseStampLayouts: every layout the renderer recognizes decodes to the
// instant it denotes, and the ones that carry no calendar date say so.
func TestParseStampLayouts(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    time.Time
		hasDate bool
	}{
		{
			"logback", "2024-01-02 10:11:12,345 [main] INFO com.example.Foo - msg",
			time.Date(2024, 1, 2, 10, 11, 12, 345e6, time.UTC), true,
		},
		{
			"rfc3339", "2024-01-02T10:11:12.5Z INFO started",
			time.Date(2024, 1, 2, 10, 11, 12, 5e8, time.UTC), true,
		},
		{
			"offset zone", "2024-01-02T10:11:12+02:00 INFO started",
			time.Date(2024, 1, 2, 8, 11, 12, 0, time.UTC), true,
		},
		{
			"slash date", "2024/01/02 10:11:12 INFO started",
			time.Date(2024, 1, 2, 10, 11, 12, 0, time.UTC), true,
		},
		{
			"bracketed", "[2024-01-02 10:11:12] INFO started",
			time.Date(2024, 1, 2, 10, 11, 12, 0, time.UTC), true,
		},
		{
			"time only", "10:11:12.345 INFO started",
			time.Date(baseYear, 1, 1, 10, 11, 12, 345e6, time.UTC), false,
		},
		{
			"syslog", "Jan  2 10:11:12 host proc[1]: msg",
			time.Date(baseYear, 1, 2, 10, 11, 12, 0, time.UTC), true,
		},
		{
			"logfmt pair", `time=2024-01-02T10:11:12Z level=info msg="up"`,
			time.Date(2024, 1, 2, 10, 11, 12, 0, time.UTC), true,
		},
		{
			"epoch seconds", "time=1704189072 level=info msg=up",
			time.Unix(1704189072, 0), true,
		},
		{
			"ansi styled", "\x1b[32m2024-01-02 10:11:12\x1b[0m INFO started",
			time.Date(2024, 1, 2, 10, 11, 12, 0, time.UTC), true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseStamp(tc.line)
			if !ok {
				t.Fatalf("ParseStamp(%q) found no timestamp", tc.line)
			}
			if !got.Time.Equal(tc.want) {
				t.Errorf("ParseStamp(%q) = %v; want %v", tc.line, got.Time.UTC(), tc.want)
			}
			if got.HasDate != tc.hasDate {
				t.Errorf("HasDate = %v; want %v", got.HasDate, tc.hasDate)
			}
		})
	}
}

// TestParseStampUnrecognized: lines with no timestamp — a stack-trace frame,
// prose, a bare number that is not a plausible epoch — report none.
func TestParseStampUnrecognized(t *testing.T) {
	for _, line := range []string{
		"\tat com.example.Foo.bar(Foo.java:42)",
		"Caused by: java.lang.IllegalStateException: boom",
		"",
		"time=42 level=info msg=up",
	} {
		if s, ok := ParseStamp(line); ok {
			t.Errorf("ParseStamp(%q) = %v, true; want no timestamp", line, s.Time)
		}
	}
}

// TestFormatDelta: the hint picks the coarsest form that still carries signal.
func TestFormatDelta(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{450 * time.Millisecond, "+450ms"},
		{time.Millisecond, "+1ms"},
		{2100 * time.Millisecond, "+2.1s"},
		{3 * time.Second, "+3s"},
		{30 * time.Second, "+30s"},
		{90 * time.Second, "+1m30s"},
		{2 * time.Minute, "+2m"},
		{time.Hour + 5*time.Minute, "+1h5m"},
		{2 * time.Hour, "+2h"},
		{26 * time.Hour, "+1d2h"},
		{0, ""},
		{-time.Second, ""},
	}
	for _, tc := range tests {
		if got := FormatDelta(tc.d); got != tc.want {
			t.Errorf("FormatDelta(%v) = %q; want %q", tc.d, got, tc.want)
		}
	}
}

// TestGapThreshold: the stall bar is ten times the median cadence, floored at
// a second so a fast file does not flag ordinary jitter.
func TestGapThreshold(t *testing.T) {
	tests := []struct {
		name string
		in   []time.Duration
		want time.Duration
	}{
		{"empty", nil, time.Second},
		{"fast file floors", []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond}, time.Second},
		{"slow file scales", []time.Duration{time.Minute, time.Minute, time.Minute}, 10 * time.Minute},
		{"median ignores outlier", []time.Duration{time.Second, time.Second, time.Hour}, 10 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := GapThreshold(tc.in); got != tc.want {
				t.Errorf("GapThreshold(%v) = %v; want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestGapThresholdKeepsInput: the sort works on a copy — the caller's slice
// order must survive.
func TestGapThresholdKeepsInput(t *testing.T) {
	in := []time.Duration{3 * time.Second, time.Second, 2 * time.Second}
	GapThreshold(in)
	if in[0] != 3*time.Second {
		t.Errorf("GapThreshold reordered its input: %v", in)
	}
}

// TestDeltasChain: consecutive stamped lines measure against their
// predecessor, the first line has no hint, and equal stamps produce none
// either (second-resolution logs would otherwise show "+0ms" everywhere).
func TestDeltasChain(t *testing.T) {
	ds := Deltas([]string{
		"2024-01-02 10:11:12 INFO a",
		"2024-01-02 10:11:12 INFO b",
		"2024-01-02 10:11:14 INFO c",
		"2024-01-02 10:11:14.500 INFO d",
	})
	if ds[0].OK {
		t.Error("the first line has nothing to measure against")
	}
	if ds[1].OK {
		t.Error("an identical stamp must produce no hint")
	}
	if !ds[2].OK || ds[2].D != 2*time.Second {
		t.Errorf("line 2 = %+v; want 2s", ds[2])
	}
	if !ds[3].OK || ds[3].D != 500*time.Millisecond {
		t.Errorf("line 3 = %+v; want 500ms", ds[3])
	}
}

// TestDeltasUnparseableLineKeepsChain: a stack-trace frame shows no hint but
// does not restart the chain — the next stamped line measures against the last
// real timestamp, not against the frame.
func TestDeltasUnparseableLineKeepsChain(t *testing.T) {
	ds := Deltas([]string{
		"2024-01-02 10:11:12 ERROR boom",
		"\tat com.example.Foo.bar(Foo.java:42)",
		"\tat com.example.Foo.baz(Foo.java:11)",
		"2024-01-02 10:11:15 INFO recovered",
	})
	for _, l := range []int{1, 2} {
		if ds[l].OK {
			t.Errorf("line %d has no timestamp and must show no hint", l)
		}
	}
	if !ds[3].OK || ds[3].D != 3*time.Second {
		t.Errorf("line 3 = %+v; want 3s measured across the frames", ds[3])
	}
}

// TestDeltasGapFlag: against a one-second cadence, a half-minute stall is
// emphasized and the ordinary ticks are not.
func TestDeltasGapFlag(t *testing.T) {
	ds := Deltas([]string{
		"10:11:10 INFO a",
		"10:11:11 INFO b",
		"10:11:12 INFO c",
		"10:11:42 WARN stalled",
		"10:11:43 INFO d",
	})
	for _, l := range []int{1, 2, 4} {
		if !ds[l].OK || ds[l].Gap {
			t.Errorf("line %d = %+v; want an unemphasized 1s tick", l, ds[l])
		}
	}
	if !ds[3].OK || !ds[3].Gap || ds[3].D != 30*time.Second {
		t.Errorf("line 3 = %+v; want an emphasized 30s gap", ds[3])
	}
}

// TestDeltasMidnightWrap: a time-only file crossing midnight wraps forward
// instead of reporting a negative day.
func TestDeltasMidnightWrap(t *testing.T) {
	ds := Deltas([]string{"23:59:59 INFO a", "00:00:01 INFO b"})
	if !ds[1].OK || ds[1].D != 2*time.Second {
		t.Errorf("line 1 = %+v; want 2s across midnight", ds[1])
	}
}

// TestDeltasMixedDatedness: a dated stamp is never subtracted from a dateless
// one — their base days are unrelated, so the difference would be nonsense.
func TestDeltasMixedDatedness(t *testing.T) {
	ds := Deltas([]string{
		"10:11:12 INFO boot",
		"2024-01-02 10:11:13 INFO up",
		"2024-01-02 10:11:15 INFO ready",
	})
	if ds[1].OK {
		t.Errorf("line 1 = %+v; want no hint across incomparable stamps", ds[1])
	}
	if !ds[2].OK || ds[2].D != 2*time.Second {
		t.Errorf("line 2 = %+v; want 2s", ds[2])
	}
}

// TestDeltasOutOfOrder: a stamp that goes backwards (interleaved writers, a
// clock step) shows no hint rather than a bogus one.
func TestDeltasOutOfOrder(t *testing.T) {
	ds := Deltas([]string{
		"2024-01-02 10:11:12 INFO a",
		"2024-01-02 10:11:10 INFO b",
		"2024-01-02 10:11:11 INFO c",
	})
	if ds[1].OK {
		t.Errorf("line 1 = %+v; want no hint for a backwards stamp", ds[1])
	}
	if !ds[2].OK || ds[2].D != time.Second {
		t.Errorf("line 2 = %+v; want 1s from the reset predecessor", ds[2])
	}
}

// TestSpanDelta: the selection span measures the outermost stamps of a block,
// ignoring everything unstamped in between.
func TestSpanDelta(t *testing.T) {
	d, ok := SpanDelta([]string{
		"10:11:10 INFO request start",
		"\tat com.example.Foo.bar(Foo.java:42)",
		"continued message without a stamp",
		"10:13:40 INFO response sent",
	})
	if !ok || d != 2*time.Minute+30*time.Second {
		t.Errorf("SpanDelta = %v, %v; want 2m30s, true", d, ok)
	}
}

// TestSpanDeltaTooFewStamps: fewer than two timestamped lines has nothing to
// measure.
func TestSpanDeltaTooFewStamps(t *testing.T) {
	for _, lines := range [][]string{
		nil,
		{"no stamp here"},
		{"10:11:10 INFO only one", "\tat Foo.bar(Foo.java:42)"},
	} {
		if d, ok := SpanDelta(lines); ok {
			t.Errorf("SpanDelta(%q) = %v, true; want no span", lines, d)
		}
	}
}

// TestSpanDeltaMixedKinds: a dated stamp is never subtracted from a time-only
// one — the base day of the dateless layout is arbitrary. The first stamp's
// kind wins and the other kind is skipped.
func TestSpanDeltaMixedKinds(t *testing.T) {
	d, ok := SpanDelta([]string{
		"10:11:10 INFO a",
		"2024-01-02 11:00:00 INFO dated interloper",
		"10:11:20 INFO b",
	})
	if !ok || d != 10*time.Second {
		t.Errorf("SpanDelta = %v, %v; want 10s, true", d, ok)
	}
	if _, ok := SpanDelta([]string{
		"10:11:10 INFO a",
		"2024-01-02 11:00:00 INFO dated only",
	}); ok {
		t.Error("a dateless and a dated stamp are not comparable")
	}
}

// TestSpanDeltaMidnight: a time-only block whose clock wraps past midnight
// counts forward instead of reporting a negative span.
func TestSpanDeltaMidnight(t *testing.T) {
	d, ok := SpanDelta([]string{
		"23:59:59 INFO a",
		"00:00:04 INFO b",
	})
	if !ok || d != 5*time.Second {
		t.Errorf("SpanDelta = %v, %v; want 5s, true", d, ok)
	}
}

// TestSpanDeltaEpochAndSyslog: the span reuses ParseStamp, so every layout the
// per-line chain knows works here too.
func TestSpanDeltaEpochAndSyslog(t *testing.T) {
	if d, ok := SpanDelta([]string{
		"time=1704189072 level=info msg=a",
		"time=1704189162 level=info msg=b",
	}); !ok || d != 90*time.Second {
		t.Errorf("epoch span = %v, %v; want 1m30s, true", d, ok)
	}
	if d, ok := SpanDelta([]string{
		"Jan  2 10:11:12 host app: a",
		"Jan  2 10:12:12 host app: b",
	}); !ok || d != time.Minute {
		t.Errorf("syslog span = %v, %v; want 1m, true", d, ok)
	}
}
