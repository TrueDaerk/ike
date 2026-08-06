package numhint

import (
	"testing"
	"time"
)

// numhint_test.go covers the formatting half of the number hints (#1627): the
// unit choice, the rounding edges and the thresholds below which a hint says
// nothing the literal did not already say. The context heuristics are in
// spans_test.go.

// TestFormatBytes: binary units, exact multiples whole and everything else to
// one decimal, with nothing below 1 KiB.
func TestFormatBytes(t *testing.T) {
	cases := []struct {
		v    uint64
		want string
		ok   bool
	}{
		{0, "", false},
		{512, "", false},
		{1023, "", false},
		{1024, "1 KiB", true},
		{1536, "1.5 KiB", true},
		{4096, "4 KiB", true},
		{10485760, "10 MiB", true},
		{500000, "488.3 KiB", true},
		{1 << 30, "1 GiB", true},
		{3 << 40, "3 TiB", true},
		{5 << 50, "5 PiB", true},
		// Rounding must not carry into a full unit's worth of the smaller one:
		// 1023.99 KiB reads 1.0 MiB, never 1024.0 KiB.
		{1048570, "1.0 MiB", true},
	}
	for _, c := range cases {
		got, ok := FormatBytes(c.v)
		if ok != c.ok || got != c.want {
			t.Errorf("FormatBytes(%d) = %q, %v; want %q, %v", c.v, got, ok, c.want, c.ok)
		}
	}
}

// TestFormatDuration: at most two components, days only past 48h, and no hint
// when the largest unit is the base unit the key already named.
func TestFormatDuration(t *testing.T) {
	cases := []struct {
		v    uint64
		base time.Duration
		want string
		ok   bool
	}{
		{0, time.Millisecond, "", false},
		{500, time.Millisecond, "", false}, // still milliseconds: says nothing
		{86400000, time.Millisecond, "24h", true},
		{90000, time.Millisecond, "1m30s", true},
		{1500, time.Millisecond, "1s500ms", true},
		{7200000, time.Millisecond, "2h", true},
		{5400000, time.Millisecond, "1h30m", true},
		{45, time.Second, "", false}, // still seconds
		{3600, time.Second, "1h", true},
		{604800, time.Second, "7d", true},
		{216000, time.Second, "2d12h", true},
		{1500000, time.Microsecond, "1s500ms", true},
		{30, 24 * time.Hour, "", false}, // a day count is already readable
		// Overflow is a non-hint rather than a wrapped one.
		{1 << 63, time.Millisecond, "", false},
	}
	for _, c := range cases {
		got, ok := FormatDuration(c.v, c.base)
		if ok != c.ok || got != c.want {
			t.Errorf("FormatDuration(%d, %v) = %q, %v; want %q, %v", c.v, c.base, got, ok, c.want, c.ok)
		}
	}
}

// TestFormatDurationDayThreshold: exactly one day still reads in hours, so two
// timeouts an hour apart stay comparable at a glance.
func TestFormatDurationDayThreshold(t *testing.T) {
	if got, _ := FormatDuration(24*3600, time.Second); got != "24h" {
		t.Errorf("one day = %q, want %q", got, "24h")
	}
	if got, _ := FormatDuration(48*3600, time.Second); got != "2d" {
		t.Errorf("two days = %q, want %q", got, "2d")
	}
}

// TestGroup: five digits and up, never a zero-padded run, never past what is
// still a quantity.
func TestGroup(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"1000", "", false},
		{"8080", "", false},
		{"10000", "10_000", true},
		{"1000000", "1_000_000", true},
		{"123456789", "123_456_789", true},
		{"0001234567", "", false},
		{"1234567890123456789012345", "", false},
	}
	for _, c := range cases {
		got, ok := Group(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("Group(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestRadixRenderings: hex reads decimal, decimal reads hex or octal, and a
// run long enough to be a hash gets nothing.
func TestRadixRenderings(t *testing.T) {
	if got, ok := DecimalOf("1F4"); !ok || got != "500" {
		t.Errorf("DecimalOf(1F4) = %q, %v; want 500, true", got, ok)
	}
	if _, ok := DecimalOf("9"); ok {
		t.Error("a single hex digit below 10 reads identically in both bases")
	}
	if _, ok := DecimalOf("deadbeefcafe"); ok {
		t.Error("a hash-length hex run must get no decimal hint")
	}
	if got := HexOf(255); got != "0xFF" {
		t.Errorf("HexOf(255) = %q, want 0xFF", got)
	}
	if got := OctalOf(420); got != "0o644" {
		t.Errorf("OctalOf(420) = %q, want 0o644", got)
	}
}
