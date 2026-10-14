package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func squarePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestPackLayout checks the ICONDIR header, the entry fields (256 stored as
// 0) and that every entry's offset/length points at its PNG payload.
func TestPackLayout(t *testing.T) {
	in := [][]byte{squarePNG(t, 16, 16), squarePNG(t, 256, 256)}
	ico, err := pack(in)
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	if le.Uint16(ico[0:]) != 0 || le.Uint16(ico[2:]) != 1 || le.Uint16(ico[4:]) != 2 {
		t.Fatalf("bad header % x", ico[:6])
	}
	for i, want := range []byte{16, 0} {
		e := ico[6+16*i:]
		if e[0] != want || e[1] != want {
			t.Errorf("entry %d: size bytes %d/%d, want %d", i, e[0], e[1], want)
		}
		if bpp := le.Uint16(e[6:]); bpp != 32 {
			t.Errorf("entry %d: bpp %d", i, bpp)
		}
		n, off := le.Uint32(e[8:]), le.Uint32(e[12:])
		if !bytes.Equal(ico[off:off+n], in[i]) {
			t.Errorf("entry %d: payload at %d+%d is not the input PNG", i, off, n)
		}
	}
}

func TestPackRejectsNonSquareAndOversized(t *testing.T) {
	for _, b := range [][]byte{squarePNG(t, 16, 32), squarePNG(t, 512, 512), []byte("not a png")} {
		if _, err := pack([][]byte{b}); err == nil {
			t.Error("pack accepted an invalid entry")
		}
	}
}

// TestCheckedInIcoIsCurrent fails when deploy/icon/ike.ico no longer matches
// the hicolor PNGs — run `go run ./deploy/icon/ico` from the repository root.
func TestCheckedInIcoIsCurrent(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	var pngs [][]byte
	for _, s := range sizes {
		b, err := os.ReadFile(hicolorPNG(root, s))
		if err != nil {
			t.Fatal(err)
		}
		pngs = append(pngs, b)
	}
	want, err := pack(pngs)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "deploy", "icon", "ike.ico"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("deploy/icon/ike.ico is stale: run `go run ./deploy/icon/ico`")
	}
}
