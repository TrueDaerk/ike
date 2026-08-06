package idcolor

import (
	"strings"
	"testing"

	"ike/internal/highlight"
)

// text returns the source text of a span on line.
func text(line string, s Span) string {
	r := []rune(line)
	return string(r[s.Start:s.End])
}

// TestScanUUID (#1626): the canonical 8-4-4-4-12 form is detected whole.
func TestScanUUID(t *testing.T) {
	line := `{"trace":"550e8400-e29b-41d4-a716-446655440000"}`
	got := Scan(line, DefaultMinLength)
	if len(got) != 1 {
		t.Fatalf("want 1 span, got %d (%v)", len(got), got)
	}
	if s := text(line, got[0]); s != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("span text = %q", s)
	}
}

// TestScanHexHash (#1626): git SHAs and trace IDs of at least the minimum
// length are detected, shorter runs and plain numbers are not.
func TestScanHexHash(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{"commit 3f2a9c1b8e4d5f6a7b8c9d0e1f2a3b4c5d6e7f80 fixed it", []string{"3f2a9c1b8e4d5f6a7b8c9d0e1f2a3b4c5d6e7f80"}},
		{"short 3f2a9c1", []string{"3f2a9c1"}},
		{"too short 3f2a9c", nil},        // 6 < DefaultMinLength
		{"count 12345678 items", nil},    // all digits: a number
		{"color #deadbe stays", nil},     // color literal (#790)
		{"literal 0xdeadbeef here", nil}, // part of a longer word
		{"id=abc123def and id=abc123def", []string{"abc123def", "abc123def"}},
	}
	for _, c := range cases {
		got := Scan(c.line, DefaultMinLength)
		if len(got) != len(c.want) {
			t.Fatalf("%q: want %d spans, got %d (%v)", c.line, len(c.want), len(got), got)
		}
		for i, w := range c.want {
			if s := text(c.line, got[i]); s != w {
				t.Fatalf("%q: span %d = %q, want %q", c.line, i, s, w)
			}
		}
	}
}

// TestScanMinLength (#1626): the minimum is configurable and clamped at the
// floor, so a `#rrggbb` payload never starts matching.
func TestScanMinLength(t *testing.T) {
	line := "req abc123 done"
	if got := Scan(line, DefaultMinLength); len(got) != 0 {
		t.Fatalf("abc123 must not match at the default minimum: %v", got)
	}
	if got := Scan(line, 6); len(got) != 1 {
		t.Fatalf("abc123 must match at minimum 6: %v", got)
	}
	if got := Scan(line, 2); len(got) != 1 {
		t.Fatalf("minimum below the floor must clamp, got %v", got)
	}
	if Clamp(1) != MinLengthFloor {
		t.Fatalf("Clamp(1) = %d, want %d", Clamp(1), MinLengthFloor)
	}
}

// TestScanColumnsAreRunes (#1626): spans address rune columns, so a line with
// multibyte text stays aligned with the render loop.
func TestScanColumnsAreRunes(t *testing.T) {
	line := "üü abc123def"
	got := Scan(line, DefaultMinLength)
	if len(got) != 1 {
		t.Fatalf("want 1 span, got %v", got)
	}
	if got[0].Start != 3 || got[0].End != 12 {
		t.Fatalf("span = [%d,%d), want [3,12)", got[0].Start, got[0].End)
	}
}

// TestScanOrder (#1626): merged UUID and hex results come out in column order.
func TestScanOrder(t *testing.T) {
	line := "abc123def 550e8400-e29b-41d4-a716-446655440000 9f8e7d6c5b"
	got := Scan(line, DefaultMinLength)
	if len(got) != 3 {
		t.Fatalf("want 3 spans, got %d (%v)", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Start < got[i-1].Start {
			t.Fatalf("spans out of order: %v", got)
		}
	}
	if s := text(line, got[1]); s != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("second span = %q", s)
	}
}

// TestSlotStable (#1626): the same identifier always lands on the same slot,
// case-insensitively, and slots stay inside the palette.
func TestSlotStable(t *testing.T) {
	id := "3f2a9c1b8e4d5f6a"
	first := Slot(id)
	for i := 0; i < 100; i++ {
		if Slot(id) != first {
			t.Fatal("Slot is not deterministic")
		}
	}
	if Slot(strings.ToUpper(id)) != first {
		t.Fatal("Slot must fold case")
	}
	if first < 0 || first >= highlight.RainbowColors {
		t.Fatalf("slot %d outside the palette", first)
	}
	if Slot("aaaaaaaa") == Slot("bbbbbbbb") && Slot("aaaaaaaa") == Slot("cccccccc") {
		t.Fatal("distinct identifiers all collapse onto one slot")
	}
}

// TestSpanSlotMatchesSlot (#1626): a detected span carries the slot of its own
// text, so the same identifier colors identically on every line.
func TestSpanSlotMatchesSlot(t *testing.T) {
	a := Scan("first  3f2a9c1b8e", DefaultMinLength)
	b := Scan("second 3F2A9C1B8E tail", DefaultMinLength)
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("want one span each, got %v / %v", a, b)
	}
	if a[0].Slot != b[0].Slot || a[0].Slot != Slot("3f2a9c1b8e") {
		t.Fatalf("slots differ: %d / %d", a[0].Slot, b[0].Slot)
	}
}

// TestCapture (#1626): identifiers render through the shared rainbow palette.
func TestCapture(t *testing.T) {
	for i := 0; i < highlight.RainbowColors; i++ {
		if want := "rainbow." + string(rune('0'+i)); Capture(i) != want {
			t.Fatalf("Capture(%d) = %q, want %q", i, Capture(i), want)
		}
	}
	if Capture(highlight.RainbowColors) != Capture(0) {
		t.Fatal("Capture must cycle with the palette")
	}
}

// TestGlobalToggle (#1626): the package-level gate the .http response pane
// reads defaults to on, with the default minimum length.
func TestGlobalToggle(t *testing.T) {
	t.Cleanup(func() { SetEnabled(true); SetMinLength(DefaultMinLength) })
	if !Enabled() {
		t.Fatal("identifier coloring must default to on")
	}
	if MinLength() != DefaultMinLength {
		t.Fatalf("MinLength = %d, want %d", MinLength(), DefaultMinLength)
	}
	SetEnabled(false)
	if Enabled() {
		t.Fatal("SetEnabled(false) must disable")
	}
	SetMinLength(2)
	if MinLength() != MinLengthFloor {
		t.Fatalf("MinLength = %d, want the floor %d", MinLength(), MinLengthFloor)
	}
}
