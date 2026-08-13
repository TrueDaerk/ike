package terminal

import (
	"strings"
	"testing"
)

func TestReverseCellNarrowOnly(t *testing.T) {
	line := "abcde"
	got := reverseCell(line, 2)
	want := "ab" + cursorStyle.Render("c") + "de"
	if got != want {
		t.Fatalf("reverseCell(%q, 2) = %q, want %q", line, got, want)
	}
}

func TestReverseCellWideGlyphReversesWholeUnit(t *testing.T) {
	line := "a🙂b"
	want := "a" + cursorStyle.Render("🙂") + "b"

	// Cursor on either cell of the wide glyph reverses the whole glyph, not
	// just the half it lands on.
	for _, col := range []int{1, 2} {
		if got := reverseCell(line, col); got != want {
			t.Fatalf("reverseCell(%q, %d) = %q, want %q", line, col, got, want)
		}
	}

	wantB := "a🙂" + cursorStyle.Render("b")
	if got := reverseCell(line, 3); got != wantB {
		t.Fatalf("reverseCell(%q, 3) = %q, want %q", line, got, wantB)
	}
}

func TestReverseCellNoDriftAfterEmoji(t *testing.T) {
	// "echo 🙂🙂" leaves the cursor in grid cell 4 (two glyphs, two cells
	// each). The cursor must land right after the content, not drift right
	// by one column per emoji.
	line := "🙂🙂"
	got := reverseCell(line, 4)
	want := line + cursorStyle.Render(" ")
	if got != want {
		t.Fatalf("reverseCell(%q, 4) = %q, want %q (cursor drifted)", line, got, want)
	}
}

func TestReverseCellCombiningMark(t *testing.T) {
	// "e" + combining acute accent forms a single, narrow grapheme cluster;
	// it must not consume an extra cell.
	line := "éx"
	want0 := cursorStyle.Render("é") + "x"
	if got := reverseCell(line, 0); got != want0 {
		t.Fatalf("reverseCell(%q, 0) = %q, want %q", line, got, want0)
	}
	want1 := "é" + cursorStyle.Render("x")
	if got := reverseCell(line, 1); got != want1 {
		t.Fatalf("reverseCell(%q, 1) = %q, want %q", line, got, want1)
	}
}

func TestReverseCellMixedAnsiStyling(t *testing.T) {
	prefix := "\x1b[31mab\x1b[0m"
	target := "🙂"
	suffix := "cd"
	line := prefix + target + suffix
	want := prefix + cursorStyle.Render(target) + suffix

	for _, col := range []int{2, 3} {
		if got := reverseCell(line, col); got != want {
			t.Fatalf("reverseCell(%q, %d) = %q, want %q", line, col, got, want)
		}
	}
}

func TestReverseSpanNarrowOnly(t *testing.T) {
	line := "abcde"
	got := reverseSpan(line, 1, 3)
	want := "a" + cursorStyle.Render("b") + cursorStyle.Render("c") + "de"
	if got != want {
		t.Fatalf("reverseSpan(%q, 1, 3) = %q, want %q", line, got, want)
	}
}

func TestReverseSpanWideGlyphPartialOverlap(t *testing.T) {
	line := "a🙂b"

	// [0, 2): fully covers "a" and only the first cell of the emoji — the
	// whole glyph must still be reversed.
	want1 := cursorStyle.Render("a") + cursorStyle.Render("🙂") + "b"
	if got := reverseSpan(line, 0, 2); got != want1 {
		t.Fatalf("reverseSpan(%q, 0, 2) = %q, want %q", line, got, want1)
	}

	// [2, 4): covers only the second cell of the emoji plus "b" — again the
	// whole glyph reverses.
	want2 := "a" + cursorStyle.Render("🙂") + cursorStyle.Render("b")
	if got := reverseSpan(line, 2, 4); got != want2 {
		t.Fatalf("reverseSpan(%q, 2, 4) = %q, want %q", line, got, want2)
	}
}

func TestReverseSpanPastContentPads(t *testing.T) {
	line := "ab"
	got := reverseSpan(line, 1, 4)
	want := "a" + cursorStyle.Render("b") + cursorStyle.Render(strings.Repeat(" ", 2))
	if got != want {
		t.Fatalf("reverseSpan(%q, 1, 4) = %q, want %q", line, got, want)
	}
}
