package manager

import (
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/lsp/protocol"
)

// incremental.go builds incremental didChange events (Roadmap 0100, #13).
// The editor's change seam delivers the full document text (every mutation
// path funnels through one emit), so the minimal contiguous change region is
// recovered here by common-prefix/suffix diffing against the previously
// synced lines — one range + replacement text per keystroke instead of the
// whole document. Positions cross into LSP coordinates through
// protocol/convert.go only, honouring the negotiated encoding.

// incrementalEvent computes the single contiguous change turning the old
// document into newText, as an LSP content-change event under enc. changed
// is false when the texts are identical (nothing to send).
func incrementalEvent(oldLines []string, newText, enc string) (ev protocol.TextDocumentContentChangeEvent, changed bool) {
	oldText := strings.Join(oldLines, "\n")
	if oldText == newText {
		return ev, false
	}
	oldR, newR := []rune(oldText), []rune(newText)

	// Longest common prefix, then longest common suffix of the remainders.
	max := len(oldR)
	if len(newR) < max {
		max = len(newR)
	}
	p := 0
	for p < max && oldR[p] == newR[p] {
		p++
	}
	s := 0
	for s < max-p && oldR[len(oldR)-1-s] == newR[len(newR)-1-s] {
		s++
	}

	start := runeOffsetToPosition(oldLines, p)
	end := runeOffsetToPosition(oldLines, len(oldR)-s)
	r := protocol.ToLSPRange(oldLines, buffer.Range{
		Start: buffer.Position{Line: start.line, Col: start.col},
		End:   buffer.Position{Line: end.line, Col: end.col},
	}, enc)
	return protocol.TextDocumentContentChangeEvent{
		Range: &r,
		Text:  string(newR[p : len(newR)-s]),
	}, true
}

// runeOffsetToPosition maps a rune offset within the joined document (each
// line separated by one \n rune) to a line/column position.
func runeOffsetToPosition(lines []string, off int) position {
	for i, line := range lines {
		n := len([]rune(line))
		if off <= n {
			return position{line: i, col: off}
		}
		off -= n + 1 // the joining newline
	}
	last := len(lines) - 1
	if last < 0 {
		return position{}
	}
	return position{line: last, col: len([]rune(lines[last]))}
}

// position is a tiny line/col pair local to the diff (kept separate from
// buffer.Position to make the offset math self-contained).
type position struct{ line, col int }
