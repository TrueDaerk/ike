package imgview

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

// kitty.go is the Kitty graphics protocol layer (#1479): pure sequence
// builders, unit-testable without a terminal. IKE uses the Unicode
// placeholder flavour (U=1): the image is transmitted once as a virtual
// placement and the pane renders ordinary placeholder cells (U+10EEEE with
// row/column diacritics, the image id in the foreground colour), which the
// terminal composites the image over. Placeholders are plain text, so
// bubbletea's cell renderer, the pane box cache and overlays all stay
// correct — no raw cursor positioning, no ghost graphics on repaints.

// QueryID is the image id of the capability probe; terminals answer with an
// APC response carrying the same id ("OK" on support).
const QueryID = 424242

// Query returns the capability probe: a 1×1 RGB direct transmission with
// a=q, which supporting terminals acknowledge and others ignore silently.
func Query() string {
	return ansi.KittyGraphics([]byte("AAAA"),
		"a=q", fmt.Sprintf("i=%d", QueryID), "s=1", "v=1", "t=d", "f=24")
}

// QueryResponseOK reports whether a Kitty graphics APC response (id +
// payload, as delivered by the input decoder) acknowledges the capability
// probe.
func QueryResponseOK(id int, payload []byte) bool {
	return id == QueryID && strings.HasPrefix(string(payload), "OK")
}

// Transmit encodes img as PNG and returns the chunked transmission creating
// a virtual placement of id scaled to cols×rows cells. The first chunk
// carries the options, continuation chunks only m=1/m=0, per the protocol.
func Transmit(id int, img image.Image, cols, rows int) (string, error) {
	var raw bytes.Buffer
	enc := kitty.Encoder{Format: kitty.PNG}
	if err := enc.Encode(&raw, img); err != nil {
		return "", err
	}
	payload := base64.StdEncoding.EncodeToString(raw.Bytes())

	opts := []string{
		"a=T", "U=1", "q=2", "t=d", "f=100",
		fmt.Sprintf("i=%d", id),
		fmt.Sprintf("c=%d", cols),
		fmt.Sprintf("r=%d", rows),
	}
	var sb strings.Builder
	for first := true; ; first = false {
		chunk := payload
		if len(chunk) > kitty.MaxChunkSize {
			chunk = payload[:kitty.MaxChunkSize]
		}
		payload = payload[len(chunk):]
		m := "m=0"
		if len(payload) > 0 {
			m = "m=1"
		}
		if first {
			sb.WriteString(ansi.KittyGraphics([]byte(chunk), append(opts, m)...))
		} else {
			sb.WriteString(ansi.KittyGraphics([]byte(chunk), m))
		}
		if len(payload) == 0 {
			return sb.String(), nil
		}
	}
}

// Delete returns the sequence removing image id's placements and freeing its
// data (a=d with an uppercase d=I).
func Delete(id int) string {
	return ansi.KittyGraphics(nil, "a=d", "d=I", fmt.Sprintf("i=%d", id), "q=2")
}

// PlaceholderGrid renders the cols×rows placeholder cells for image id: one
// string per row, each opened with the id-encoding foreground colour and
// closed with a reset. The terminal replaces these cells with the scaled
// image; a terminal that lost the transmission shows blank cells, never
// garbage.
func PlaceholderGrid(id, cols, rows int) []string {
	fg := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", (id>>16)&0xff, (id>>8)&0xff, id&0xff)
	out := make([]string, 0, rows)
	for r := 0; r < rows; r++ {
		var sb strings.Builder
		sb.WriteString(fg)
		for c := 0; c < cols; c++ {
			sb.WriteRune(kitty.Placeholder)
			sb.WriteRune(kitty.Diacritic(r))
			sb.WriteRune(kitty.Diacritic(c))
		}
		sb.WriteString("\x1b[39m")
		out = append(out, sb.String())
	}
	return out
}

// fitGrid computes the largest cols×rows cell grid inside maxCols×maxRows
// that preserves the pixel aspect ratio of imgW×imgH, assuming terminal
// cells are twice as tall as wide. Both dimensions are at least 1.
func fitGrid(imgW, imgH, maxCols, maxRows int) (cols, rows int) {
	if imgW <= 0 || imgH <= 0 || maxCols <= 0 || maxRows <= 0 {
		return 1, 1
	}
	// Aspect in cell units: a cell column is ~half a cell row of pixels.
	cols = maxCols
	rows = (cols*imgH + imgW - 1) / (imgW * 2)
	if rows > maxRows {
		rows = maxRows
		cols = rows * 2 * imgW / imgH
		if cols > maxCols {
			cols = maxCols
		}
	}
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	return cols, rows
}
