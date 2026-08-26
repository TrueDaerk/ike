package editor

import (
	"math"
	"testing"

	"ike/internal/editor/buffer"
	ilsp "ike/internal/lsp"
)

// TestDiagnosticRangeClampedToBuffer (#2163): servers express "to end of
// document" as an enormous end line (2^31-1 is a common LSP idiom), and only
// columns are clamped at the protocol layer. Without the buffer clamp the
// per-line index loop would iterate billions of times on the update loop —
// a hard freeze. The whole test finishing at all is the regression check.
func TestDiagnosticRangeClampedToBuffer(t *testing.T) {
	m := sbEditor(t, 40, 20, 10) // 10-line buffer
	m.setDiagnostics([]ilsp.Diagnostic{{
		Range: buffer.Range{
			Start: buffer.Position{Line: 2},
			End:   buffer.Position{Line: math.MaxInt32},
		},
		Severity: 1,
		Message:  "whole-file diagnostic",
	}})
	lines := m.buf.LineCount()
	if got := len(m.diagByLine); got != lines-2 {
		t.Fatalf("indexed %d lines, want %d (start..last line of the buffer)", got, lines-2)
	}
	if _, ok := m.diagByLine[lines]; ok {
		t.Fatal("indexed a line past the buffer end")
	}
	// A negative start (defensive — nothing valid produces it) clamps to 0.
	m.setDiagnostics([]ilsp.Diagnostic{{
		Range:    buffer.Range{Start: buffer.Position{Line: -3}, End: buffer.Position{Line: 1}},
		Severity: 1,
		Message:  "negative start",
	}})
	if _, ok := m.diagByLine[0]; !ok {
		t.Fatal("negative start line should clamp to 0")
	}
}
