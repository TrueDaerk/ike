package editor

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/escapes"
)

// escapeedit.go implements the two *rewriting* escape commands (#2338):
// editor.escapeSelection turns the non-ASCII characters of the selection into
// escapes of the buffer's language, editor.unescapeSelection turns escapes
// back into the characters they name. They are the counterpart of the #1620
// decoding layer (escapes.go), which only ever *renders* an escape decoded —
// here the buffer really changes, which is what working on an i18n file, a
// JSON fixture or a pasted log excerpt needs.
//
// The transformation itself lives in internal/escapes (encode.go), so the
// per-language knowledge — which escape forms exist, which quotes process
// them — stays where the detection side already keeps it. This file only
// decides *what* to transform:
//
//   - a visual selection, charwise or linewise, is the scope when there is one;
//   - without one, the string literal under the caret is, so the everyday case
//     ("fix this one literal") needs no selection gesture at all;
//   - several carets refuse: a caret is a position, not a range, and escaping
//     "the literal under each caret" would fan a single undo unit out over
//     places the user cannot see at once.
//
// A buffer whose language has no escape syntax of its own (a log, a .txt, an
// unsaved scratch) falls back to escapes.UnicodeFallback — the plain \uXXXX
// form — rather than declining: unfolding escapes in a log dump is one of the
// reasons the commands exist.

// escapeSelection rewrites the selection (or the literal under the caret) in
// the buffer's escape dialect. decode picks the direction: false escapes the
// non-ASCII characters, true resolves the escapes. One undo unit,
// dot-repeatable; the notice explains every refusal.
func (m *Model) escapeSelection(decode bool) tea.Cmd {
	if m.insert.active {
		m.commitInsert()
	}
	if m.hasCarets() {
		return notice("escape/unescape works on one selection — clear the extra carets first")
	}
	d := m.escapeDialect()
	rng, ok, why := m.escapeRange(d)
	if !ok {
		return notice(why)
	}
	text := m.buf.Slice(rng)
	out := text
	if decode {
		out = escapes.DecodeUnicode(text, d)
	} else {
		out = escapes.EncodeUnicode(text, d)
	}
	if out == text {
		// Nothing to do: the selection stays, so the other direction is one
		// key away.
		if decode {
			return notice("no escapes to resolve here")
		}
		return notice("nothing to escape here — the text is plain ASCII")
	}
	m.mode = Normal
	m.mutate(func(rec *history.Recorder) buffer.Position {
		rec.Apply(buffer.Edit{Range: rng, Text: out})
		return rng.Start
	})
	m.scroll()
	m.dot = &dotCommand{run: func(mm *Model) { mm.escapeSelection(decode) }}
	return nil
}

// escapeDialect returns the escape dialect for the buffer's language, or the
// language-neutral fallback when the language has none (or the buffer has no
// language at all).
func (m Model) escapeDialect() escapes.UnicodeDialect {
	if d, ok := escapes.DialectFor(m.langID()); ok {
		return d
	}
	return escapes.UnicodeFallback
}

// escapeRange resolves the range the commands act on: the visual selection —
// a linewise one covers its lines whole — else the body of the string literal
// under the caret. why carries the notice text when nothing resolves.
func (m *Model) escapeRange(d escapes.UnicodeDialect) (rng buffer.Range, ok bool, why string) {
	if m.mode.IsVisual() {
		t := m.visualSelection()
		if !t.Linewise {
			return t.Range, true, ""
		}
		a, z := t.Range.Start.Line, t.Range.End.Line
		if last := m.buf.LineCount() - 1; z > last {
			z = last
		}
		return buffer.Range{
			Start: buffer.Position{Line: a, Col: 0},
			End:   buffer.Position{Line: z, Col: m.buf.RuneLen(z)},
		}, true, ""
	}
	line := m.cursor.Line
	start, end, found := escapes.LiteralAt(m.buf.Line(line), m.cursor.Col, d)
	if !found {
		return buffer.Range{}, false, "select the text to convert, or put the caret in a string literal"
	}
	return buffer.Range{
		Start: buffer.Position{Line: line, Col: start},
		End:   buffer.Position{Line: line, Col: end},
	}, true, ""
}
