package register

// entrycap_test.go covers the per-entry size cap on the clipboard history
// (#2250): a copy larger than the cap is skipped by the ring — never truncated
// into it — while the registers keep it in full.

import (
	"strings"
	"testing"
)

func TestOversizedEntrySkippedByHistory(t *testing.T) {
	s := New()
	s.SetEntryMaxBytes(8)
	big := strings.Repeat("x", 9)

	s.Yank(0, Entry{Text: "small"})
	s.Yank(0, Entry{Text: big})
	s.Delete(0, Entry{Text: big + "\n", Linewise: true})
	s.PushHistory(Entry{Text: big})

	h := s.History()
	if len(h) != 1 || h[0].Text != "small" {
		t.Fatalf("only the in-budget entry may enter the ring, got %v", h)
	}
	// The registers are untouched by the cap: the payload still pastes in full.
	if got := s.Get(0).Text; got != big+"\n" {
		t.Fatalf("unnamed register = %q, want the full oversized delete", got)
	}
	if got := s.Get('1').Text; got != big+"\n" {
		t.Fatalf("numbered register = %q, want the full oversized delete", got)
	}
}

func TestEntryCapBoundaryAndDefaults(t *testing.T) {
	s := New()
	if s.EntryMaxBytes() != DefaultEntryMaxBytes {
		t.Fatalf("default cap = %d, want %d", s.EntryMaxBytes(), DefaultEntryMaxBytes)
	}
	// The zero value behaves like New().
	var zero Store
	if zero.EntryMaxBytes() != DefaultEntryMaxBytes {
		t.Fatalf("zero-value cap = %d, want %d", zero.EntryMaxBytes(), DefaultEntryMaxBytes)
	}
	// A cap below 1 is ignored rather than disabling the ring entirely.
	s.SetEntryMaxBytes(4)
	s.SetEntryMaxBytes(0)
	s.SetEntryMaxBytes(-3)
	if s.EntryMaxBytes() != 4 {
		t.Fatalf("cap = %d, want the last valid 4", s.EntryMaxBytes())
	}
	// Exactly at the cap is admitted; one byte over is not.
	s.Yank(0, Entry{Text: "abcd"})
	s.Yank(0, Entry{Text: "abcde"})
	h := s.History()
	if len(h) != 1 || h[0].Text != "abcd" {
		t.Fatalf("cap must be inclusive, got %v", h)
	}
}

func TestEntryCapCountsBytesNotRunes(t *testing.T) {
	s := New()
	s.SetEntryMaxBytes(3)
	// Two runes, four bytes: the cap is a memory bound, so bytes decide.
	s.Yank(0, Entry{Text: "üü"})
	if h := s.History(); len(h) != 0 {
		t.Fatalf("a 4-byte payload must not fit a 3-byte cap, got %v", h)
	}
}
