package imgview

import (
	"image"
	"image/color"
	"math/rand"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi/kitty"
)

func testImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(1)) // deterministic noise defeats PNG compression
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(rng.Intn(256)), G: uint8(rng.Intn(256)), B: uint8(rng.Intn(256)), A: 0xff})
		}
	}
	return img
}

func TestQuerySequence(t *testing.T) {
	q := Query()
	if !strings.HasPrefix(q, "\x1b_G") || !strings.HasSuffix(q, "\x1b\\") {
		t.Fatalf("not an APC sequence: %q", q)
	}
	for _, want := range []string{"a=q", "i=424242", "t=d", "f=24"} {
		if !strings.Contains(q, want) {
			t.Errorf("query lacks %q: %q", want, q)
		}
	}
}

func TestQueryResponseOK(t *testing.T) {
	if !QueryResponseOK(QueryID, []byte("OK")) {
		t.Error("OK response for the probe id must acknowledge")
	}
	if QueryResponseOK(QueryID, []byte("ENOTSUPPORTED:")) {
		t.Error("error payload must not acknowledge")
	}
	if QueryResponseOK(7, []byte("OK")) {
		t.Error("foreign id must not acknowledge")
	}
}

func TestTransmitOptionsAndChunking(t *testing.T) {
	seq, err := Transmit(9001, testImage(128, 128), 20, 10)
	if err != nil {
		t.Fatal(err)
	}
	chunks := strings.Split(seq, "\x1b\\")
	chunks = chunks[:len(chunks)-1] // trailing empty split
	if len(chunks) < 2 {
		t.Fatalf("a noisy 128×128 PNG must span several %d-byte chunks, got %d", kitty.MaxChunkSize, len(chunks))
	}
	first := chunks[0]
	for _, want := range []string{"a=T", "U=1", "q=2", "f=100", "t=d", "i=9001", "c=20", "r=10", "m=1"} {
		if !strings.Contains(first, want) {
			t.Errorf("first chunk lacks %q: %.120q", want, first)
		}
	}
	for _, c := range chunks[1 : len(chunks)-1] {
		if !strings.HasPrefix(c, "\x1b_Gm=1;") {
			t.Errorf("continuation chunk malformed: %.40q", c)
		}
	}
	if !strings.HasPrefix(chunks[len(chunks)-1], "\x1b_Gm=0") {
		t.Errorf("final chunk must carry m=0: %.40q", chunks[len(chunks)-1])
	}
	// Payload chunks respect the protocol's max chunk size.
	for _, c := range chunks {
		if i := strings.IndexByte(c, ';'); i >= 0 && len(c)-i-1 > kitty.MaxChunkSize {
			t.Errorf("chunk payload exceeds max: %d bytes", len(c)-i-1)
		}
	}
}

func TestDeleteSequence(t *testing.T) {
	d := Delete(9001)
	for _, want := range []string{"a=d", "d=I", "i=9001"} {
		if !strings.Contains(d, want) {
			t.Errorf("delete lacks %q: %q", want, d)
		}
	}
}

func TestPlaceholderGrid(t *testing.T) {
	rows := PlaceholderGrid(0x0a0b0c, 3, 2)
	if len(rows) != 2 {
		t.Fatalf("got %d rows", len(rows))
	}
	if !strings.Contains(rows[0], "\x1b[38;2;10;11;12m") {
		t.Errorf("row must encode the id in the foreground colour: %q", rows[0])
	}
	if got := strings.Count(rows[0], string(kitty.Placeholder)); got != 3 {
		t.Errorf("row 0 has %d placeholder cells, want 3", got)
	}
	// Row 1, column 2 carries the matching diacritics.
	if !strings.Contains(rows[1], string(kitty.Placeholder)+string(kitty.Diacritic(1))+string(kitty.Diacritic(2))) {
		t.Errorf("row/col diacritics missing: %q", rows[1])
	}
	if !strings.HasSuffix(rows[0], "\x1b[39m") {
		t.Errorf("row must reset the foreground: %q", rows[0])
	}
}

func TestFitGrid(t *testing.T) {
	cases := []struct {
		imgW, imgH, maxC, maxR, wantC, wantR int
	}{
		{100, 100, 80, 24, 48, 24}, // height-bound square: rows*2 columns
		{100, 100, 20, 24, 20, 10}, // width-bound square
		{400, 100, 80, 24, 80, 10}, // wide
		{100, 400, 80, 24, 12, 24}, // tall
		{1, 1, 80, 24, 48, 24},     // 1×1 scales up like any square
		{0, 0, 80, 24, 1, 1},       // degenerate input
	}
	for _, c := range cases {
		gotC, gotR := fitGrid(c.imgW, c.imgH, c.maxC, c.maxR)
		if gotC != c.wantC || gotR != c.wantR {
			t.Errorf("fitGrid(%d×%d in %d×%d) = %d×%d, want %d×%d",
				c.imgW, c.imgH, c.maxC, c.maxR, gotC, gotR, c.wantC, c.wantR)
		}
		if gotC > c.maxC || gotR > c.maxR {
			t.Errorf("fitGrid overflows the pane: %d×%d in %d×%d", gotC, gotR, c.maxC, c.maxR)
		}
	}
}
