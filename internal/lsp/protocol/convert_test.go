package protocol

import (
	"testing"

	"ike/internal/editor/buffer"
)

func TestRuneColToUnitsUTF16(t *testing.T) {
	// "a😀b": 😀 is U+1F600, two UTF-16 code units, one rune, 4 UTF-8 bytes.
	line := "a😀b"
	cases := []struct {
		col, utf16, utf8 int
	}{
		{0, 0, 0},
		{1, 1, 1}, // after 'a'
		{2, 3, 5}, // after emoji: 1 + 2 utf16 units; 1 + 4 utf8 bytes
		{3, 4, 6}, // after 'b'
	}
	for _, c := range cases {
		if got := runeColToUnits(line, c.col, EncodingUTF16); got != c.utf16 {
			t.Errorf("utf16 col %d = %d, want %d", c.col, got, c.utf16)
		}
		if got := runeColToUnits(line, c.col, EncodingUTF8); got != c.utf8 {
			t.Errorf("utf8 col %d = %d, want %d", c.col, got, c.utf8)
		}
		if got := runeColToUnits(line, c.col, EncodingUTF32); got != c.col {
			t.Errorf("utf32 col %d = %d, want %d", c.col, got, c.col)
		}
	}
}

func TestUnitsToRuneColRoundTrip(t *testing.T) {
	line := "x😀y_λ" // mix BMP, astral, ascii, BMP-2byte-utf8
	runes := []rune(line)
	for col := 0; col <= len(runes); col++ {
		for _, enc := range []string{EncodingUTF16, EncodingUTF8, EncodingUTF32} {
			units := runeColToUnits(line, col, enc)
			back := unitsToRuneCol(line, units, enc)
			if back != col {
				t.Errorf("enc=%s col=%d -> units=%d -> col=%d", enc, col, units, back)
			}
		}
	}
}

func TestPositionConvertRoundTrip(t *testing.T) {
	lines := []string{"func main() {", "\t😀 := 1", "}"}
	for _, enc := range []string{EncodingUTF16, EncodingUTF8} {
		p := buffer.Position{Line: 1, Col: 3}
		lsp := ToLSPPosition(lines, p, enc)
		back := FromLSPPosition(lines, lsp, enc)
		if back != p {
			t.Errorf("enc=%s %v -> %v -> %v", enc, p, lsp, back)
		}
	}
}

func TestRangeConvert(t *testing.T) {
	lines := []string{"abc", "de😀f"}
	r := buffer.Range{Start: buffer.Position{Line: 1, Col: 2}, End: buffer.Position{Line: 1, Col: 4}}
	lsp := ToLSPRange(lines, r, EncodingUTF16)
	if lsp.Start.Character != 2 || lsp.End.Character != 5 { // 😀 is 2 utf16 units
		t.Fatalf("range = %+v", lsp)
	}
	back := FromLSPRange(lines, lsp, EncodingUTF16)
	if back != r {
		t.Fatalf("round trip = %+v, want %+v", back, r)
	}
}

func TestPathURIRoundTrip(t *testing.T) {
	cases := []string{"/tmp/foo bar/main.go", "/a/b/c.py"}
	for _, p := range cases {
		uri := PathToURI(p)
		if back := URIToPath(uri); back != p {
			t.Errorf("path %q -> uri %q -> %q", p, uri, back)
		}
	}
}

func TestURIToPathEncodesSpace(t *testing.T) {
	uri := PathToURI("/tmp/a b.go")
	if want := "file:///tmp/a%20b.go"; uri != want {
		t.Errorf("uri = %q, want %q", uri, want)
	}
}
