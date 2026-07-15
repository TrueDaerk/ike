package app

import (
	"testing"

	"charm.land/lipgloss/v2"
)

// TestJoinHMatchesLipgloss verifies the measurement-free horizontal stitch equals
// lipgloss.JoinHorizontal for equal-height, exact-width columns (#612).
func TestJoinHMatchesLipgloss(t *testing.T) {
	a := "AA\nAA"
	div := "|\n|"
	b := "BBB\nBBB"
	got := joinH(2, a, div, b)
	want := lipgloss.JoinHorizontal(lipgloss.Top, a, div, b)
	if got != want {
		t.Fatalf("joinH = %q, want %q", got, want)
	}
	if got != "AA|BBB\nAA|BBB" {
		t.Fatalf("joinH = %q", got)
	}
}

// TestJoinHFallsBackOnMismatch verifies an unexpected line count falls back to
// lipgloss (defensive; should not happen with exact-size boxes).
func TestJoinHFallsBackOnMismatch(t *testing.T) {
	a := "A\nA\nA" // 3 lines
	b := "B\nB"    // 2 lines
	// rows says 2 but a has 3 → fall back; must not panic and must equal lipgloss.
	got := joinH(2, a, b)
	want := lipgloss.JoinHorizontal(lipgloss.Top, a, b)
	if got != want {
		t.Fatalf("fallback joinH = %q, want %q", got, want)
	}
}

// TestJoinVStacks verifies vertical stacking is a plain newline join.
func TestJoinVStacks(t *testing.T) {
	if got := joinV("X\nY", "----", "Z"); got != "X\nY\n----\nZ" {
		t.Fatalf("joinV = %q", got)
	}
}

// TestHashStringDeterministicAndDistinct verifies the box-cache hash is stable
// per input and separates different content (a collision would reuse a stale
// box, so distinctness matters for the common cases).
func TestHashStringDeterministicAndDistinct(t *testing.T) {
	if hashString("hello world") != hashString("hello world") {
		t.Fatal("hashString not deterministic")
	}
	if hashString("line one\nline two") == hashString("line one\nline three") {
		t.Fatal("hashString collided on distinct content")
	}
	if hashString("") != hashString("") {
		t.Fatal("empty hash not stable")
	}
}
