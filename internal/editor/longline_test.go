package editor

import (
	"strings"
	"testing"
)

// Tests for the windowed decoration scans on very long lines (#2734).

func TestDecorWindowCoversWholeShortLine(t *testing.T) {
	if lo, hi := decorWindow(100, 40, -1, 80); lo != 0 || hi != 100 {
		t.Fatalf("a short line is scanned whole, got [%d,%d)", lo, hi)
	}
}

func TestDecorWindowPadsTheRenderedSpanOnLongLines(t *testing.T) {
	n := longLineRunes * 4
	lo, hi := decorWindow(n, 10000, -1, 100)
	if lo != 9900 || hi != 10200 {
		t.Fatalf("window = [%d,%d), want one span width of margin on each side", lo, hi)
	}
	// Clamped at the line ends; an explicit span end bounds the window too.
	if lo, hi := decorWindow(n, 20, -1, 100); lo != 0 || hi != 220 {
		t.Fatalf("window at the line start = [%d,%d)", lo, hi)
	}
	if lo, hi := decorWindow(n, n-50, n, 100); lo != n-150 || hi != n {
		t.Fatalf("window at the line end = [%d,%d)", lo, hi)
	}
}

func TestWindowedLinksShiftIntoLineColumns(t *testing.T) {
	m := New()
	m.hyperlinks = true
	filler := strings.Repeat("x", longLineRunes)
	line := filler + " https://example.com/page " + filler
	runes := []rune(line)
	lo, hi := decorWindow(len(runes), longLineRunes, -1, 60)
	links := m.windowedLinks(runes, lo, hi)
	if len(links) != 1 {
		t.Fatalf("one link in the window, got %d", len(links))
	}
	if links[0].start != longLineRunes+1 || string(runes[links[0].start:links[0].end]) != "https://example.com/page" {
		t.Fatalf("link span must be in line columns, got %+v", links[0])
	}
	// The same scan over the whole line finds the same span.
	whole := m.lineLinks(runes)
	if len(whole) != 1 || whole[0] != links[0] {
		t.Fatalf("windowed %+v vs whole-line %+v", links, whole)
	}
}

func TestWindowedSwatchesShiftIntoLineColumns(t *testing.T) {
	m, _ := loaded(t, "x")
	m.colorPreview = true
	filler := strings.Repeat("x", longLineRunes)
	line := filler + " color: #ff8800; " + filler
	m.buf.ReplaceAll(line)
	runes := []rune(line)
	lo, hi := decorWindow(len(runes), longLineRunes, -1, 60)
	sw := m.windowedColorSwatches(0, runes, lo, hi)
	if len(sw) != 1 || sw[0].Start != longLineRunes+8 || sw[0].R != 0xff || sw[0].G != 0x88 {
		t.Fatalf("swatch must sit at the literal's line column, got %+v", sw)
	}
}
