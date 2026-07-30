package editor

import (
	"ike/internal/editor/buffer"
	"ike/internal/format"
)

// reformat.go exposes what the formatter registry's app handler needs from
// the editor (Roadmap 0470, #1401): the effective per-buffer format options
// and the active visual selection. The edits themselves come back through the
// existing ApplyTextEdits path (one undo unit, cursor clamped).

// FormatOptions returns the buffer's effective formatting settings — indent
// style/width, max line length, final newline — after the editor's usual
// layering (built-in defaults < IKE config < language default < .editorconfig,
// see editorconfig.go). Formatter providers must honour these so their output
// agrees with the editor.
func (m Model) FormatOptions() format.Options {
	opts := format.Options{
		TabWidth:           m.tabWidth,
		UseSpaces:          m.useSpaces,
		InsertFinalNewline: m.insertFinalNewline,
	}
	if m.editorconfigEnabled() {
		if n, ok := m.ec.MaxLineLength(); ok {
			opts.MaxLineLength = n
		}
	}
	return opts
}

// SelectionSpan returns the active visual selection as a normalized
// end-exclusive [start, end) range — char-wise selections include the cursor
// cell, line-wise selections cover whole lines — mirroring the convention the
// LSP bridge derives from editor events. ok is false outside visual mode.
func (m Model) SelectionSpan() (start, end buffer.Position, ok bool) {
	if !m.mode.IsVisual() {
		return start, end, false
	}
	start, end = m.anchor, m.cursor
	if end.Before(start) {
		start, end = end, start
	}
	if m.mode == VisualLine {
		start = buffer.Position{Line: start.Line, Col: 0}
		end = buffer.Position{Line: end.Line + 1, Col: 0}
	} else {
		end.Col++
	}
	return start, end, true
}
