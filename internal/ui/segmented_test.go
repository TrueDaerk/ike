package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSegmentedViewPadsAndHighlights(t *testing.T) {
	s := Segmented{Segments: []Segment{{Label: "Source"}, {Label: "Preview", On: true}}}
	if got := s.Width(); got != len("[Source] [Preview]") {
		t.Fatalf("Width = %d, want %d", got, len("[Source] [Preview]"))
	}
	v := s.View(30, nil)
	if w := ansi.StringWidth(v); w != 30 {
		t.Fatalf("view width = %d, want 30", w)
	}
	if plain := ansi.Strip(v); !strings.HasPrefix(plain, "[Source] [Preview]") {
		t.Fatalf("view = %q, want the buttons flush left", plain)
	}
	// The on button is styled differently from the off one.
	off := Segmented{Segments: []Segment{{Label: "Source"}, {Label: "Preview"}}}.View(30, nil)
	if v == off {
		t.Fatal("an on segment must be highlighted")
	}
}

func TestSegmentedViewDropsButtonsThatDoNotFit(t *testing.T) {
	s := Segmented{Segments: []Segment{{Label: "Source"}, {Label: "Preview"}, {Label: "Browser"}}}
	v := ansi.Strip(s.View(12, nil))
	if v != "[Source]    " {
		t.Fatalf("view = %q, want only the first button, padded", v)
	}
	if got := s.View(0, nil); got != "" {
		t.Fatalf("zero width = %q, want empty", got)
	}
}

func TestSegmentedAt(t *testing.T) {
	s := Segmented{Segments: []Segment{{Label: "Source"}, {Label: "Preview"}, {Label: "Browser"}}}
	cases := []struct{ x, width, want int }{
		{0, 40, 0},
		{7, 40, 0},  // "]" of [Source]
		{8, 40, -1}, // gap
		{9, 40, 1},  // "[" of [Preview]
		{17, 40, 1}, // "]" of [Preview]
		{18, 40, -1},
		{19, 40, 2}, // "[" of [Browser]
		{27, 40, 2},
		{28, 40, -1}, // padding
		{19, 20, -1}, // [Browser] cut off at width 20
		{-1, 40, -1},
		{40, 40, -1},
	}
	for _, c := range cases {
		if got := s.At(c.x, c.width); got != c.want {
			t.Errorf("At(%d, %d) = %d, want %d", c.x, c.width, got, c.want)
		}
	}
}
