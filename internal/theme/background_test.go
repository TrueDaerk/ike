package theme

import (
	"image/color"
	"testing"
)

func rgb(r, g, b uint8) color.Color { return color.RGBA{R: r, G: g, B: b, A: 0xff} }

func TestIsDarkColor(t *testing.T) {
	cases := []struct {
		name string
		c    color.Color
		dark bool
	}{
		{"black", rgb(0, 0, 0), true},
		{"white", rgb(255, 255, 255), false},
		{"near-black editor bg", rgb(0x1e, 0x1e, 0x2e), true},
		{"near-white editor bg", rgb(0xfa, 0xfa, 0xfa), false},
		{"solarized dark base03", rgb(0x00, 0x2b, 0x36), true},
		{"solarized light base3", rgb(0xfd, 0xf6, 0xe3), false},
		{"mid grey", rgb(0x80, 0x80, 0x80), true},
		{"pure blue", rgb(0, 0, 0xff), true},
		{"pure yellow", rgb(0xff, 0xff, 0), false},
		{"nil (unknown)", nil, true},
	}
	for _, tc := range cases {
		if got := IsDarkColor(tc.c); got != tc.dark {
			t.Errorf("%s: IsDarkColor = %v, want %v", tc.name, got, tc.dark)
		}
	}
}
