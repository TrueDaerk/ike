package editor

import "ike/internal/lang"

// completionContext classifies the cursor position for a completion trigger
// (#2654): inside a comment or a string literal, right after a declaring
// keyword, on an import line, or plain code. The highlighter's capture at
// the character before the current word decides comment/string — not the
// capture at the cursor, because the syntax index lags one parse behind the
// keystroke and the just-typed rune is never inside a span yet, while the
// character before the word (the `/` of `//`, the opening quote, a blank
// inside the comment) was parsed long ago. Without a grammar there is no
// capture and the position reads as code. The language's DeclKeywords and
// ImportLine decide the rest from the line text alone.
func (m *Model) completionContext() lang.CompletionContext {
	l, _ := lang.ByPath(m.langPath())
	line := m.buf.Line(m.cursor.Line)
	probe := lang.WordStart(line, m.cursor.Col) - 1
	if probe < 0 {
		probe = 0
	}
	capture := m.hlIndex.CaptureAt(m.cursor.Line, probe)
	return lang.CompletionContextAt(l, capture, line, m.cursor.Col)
}
