// Command ico packs the hicolor PNGs into deploy/icon/ike.ico (#3002), the
// icon of the Windows "Ike" Start-menu entry. Each size up to 256×256 is
// stored as a PNG-compressed entry (supported since Windows Vista), so the
// file is a lossless re-container of the checked-in PNGs: pure stdlib,
// deterministic, and it runs on any OS — `make icons` calls it after the
// hicolor set is regenerated.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/png"
	"log"
	"os"
	"path/filepath"
)

// sizes are the ICO entries, smallest first. 256 is the format's maximum
// (stored as 0 in the one-byte width/height fields).
var sizes = []int{16, 24, 32, 48, 64, 128, 256}

func main() {
	var pngs [][]byte
	for _, s := range sizes {
		b, err := os.ReadFile(hicolorPNG(".", s))
		if err != nil {
			log.Fatal(err)
		}
		pngs = append(pngs, b)
	}
	ico, err := pack(pngs)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("deploy", "icon", "ike.ico"), ico, 0o644); err != nil {
		log.Fatal(err)
	}
}

// hicolorPNG is the checked-in s×s PNG below the repository root.
func hicolorPNG(root string, s int) string {
	return filepath.Join(root, "deploy", "icon", "hicolor", fmt.Sprintf("%dx%d", s, s), "apps", "ike.png")
}

// pack builds an ICO file from square PNG images: the 6-byte ICONDIR header,
// one 16-byte ICONDIRENTRY per image, then the PNG payloads in order.
func pack(pngs [][]byte) ([]byte, error) {
	var buf bytes.Buffer
	le := binary.LittleEndian
	hdr := make([]byte, 6)
	le.PutUint16(hdr[2:], 1) // type: icon
	le.PutUint16(hdr[4:], uint16(len(pngs)))
	buf.Write(hdr)

	offset := 6 + 16*len(pngs)
	for i, b := range pngs {
		cfg, err := png.DecodeConfig(bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
		if cfg.Width != cfg.Height || cfg.Width < 1 || cfg.Width > 256 {
			return nil, fmt.Errorf("entry %d: %dx%d is not a square of at most 256", i, cfg.Width, cfg.Height)
		}
		e := make([]byte, 16)
		e[0] = byte(cfg.Width % 256) // 256 is written as 0
		e[1] = byte(cfg.Height % 256)
		le.PutUint16(e[4:], 1)  // colour planes
		le.PutUint16(e[6:], 32) // bits per pixel
		le.PutUint32(e[8:], uint32(len(b)))
		le.PutUint32(e[12:], uint32(offset))
		buf.Write(e)
		offset += len(b)
	}
	for _, b := range pngs {
		buf.Write(b)
	}
	return buf.Bytes(), nil
}
