//go:build cgo

package langjson

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/secret"
)

// TestMaskThroughHighlightPipeline: the mask reaches the editor through the
// registry, not only through the local producer — and it is the covering
// capture over the value, since overlapping spans resolve first-covering-wins
// and the grammar paints the same runes as a string.
func TestMaskThroughHighlightPipeline(t *testing.T) {
	for _, path := range []string{"/p/config.json", "/p/stream.ndjson"} {
		lines := []string{`{"password": "hunter2"}`}
		spans := highlight.Highlight(path, lines)
		ix := highlight.NewIndex(spans)
		// Column of the "h" of hunter2.
		col := 14
		if got := ix.CaptureAt(0, col); got != secret.Capture {
			t.Errorf("%s: capture over the value is %q, want %q", path, got, secret.Capture)
		}
		found := false
		for _, s := range spans {
			if s.Capture == secret.Capture && s.Replace == secret.Mask {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no mask stand-in reached the highlight pipeline", path)
		}
	}
}
