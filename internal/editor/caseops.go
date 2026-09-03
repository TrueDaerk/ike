package editor

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/textobject"
	"ike/internal/idcase"
)

// caseops.go is the command half of the case family (#2418). The vim half —
// the `gu` / `gU` / `g~` operators with their linewise doubles — already runs
// through the operator framework (vimops.go, #1193); this file adds the four
// palette/keymap commands JetBrains users reach for instead:
//
//   - editor.case.lower / .upper / .toggle rewrite the selection, or the word
//     under each caret when there is none (cmd+shift+u is JetBrains' Toggle
//     Case);
//   - editor.case.cycle walks the identifier under each caret through
//     camelCase → snake_case → kebab-case → PascalCase → SCREAMING_SNAKE and
//     back, detecting the style it is written in (internal/idcase).
//
// All four are multi-caret aware: every caret's span is rewritten in a single
// history.Change, so one undo takes the whole fan-out back — the same contract
// the other #145 fan-outs keep.
//
// Case mapping is rune-wise (unicode.ToUpper/ToLower), never the language-aware
// special casing: `ß` stays one rune rather than becoming `SS`, so the rune
// count — and with it every caret, mark and selection to the right — is stable
// across the edit.

// caseKind names what a case command does to a span of text.
type caseKind int

const (
	caseLower  caseKind = iota // gu
	caseUpper                  // gU
	caseToggle                 // g~
	caseCycle                  // the identifier-style rotation
)

// mapCase applies kind's rune transform to text. caseCycle never reaches here:
// it works on whole identifiers, not on runes (see cycleToken).
func mapCase(text string, kind caseKind) string {
	switch kind {
	case caseLower:
		return strings.Map(unicode.ToLower, text)
	case caseUpper:
		return strings.Map(unicode.ToUpper, text)
	default:
		return strings.Map(swapCase, text)
	}
}

// runCaseCommand is the entry point for the four editor.case.* actions. It
// rewrites the visual selection when there is one, else the token under every
// caret, as a single undo unit; the notice explains a no-op the user would
// otherwise read as a broken key.
func (m *Model) runCaseCommand(kind caseKind) tea.Cmd {
	if m.insert.active {
		m.commitInsert()
	}
	if m.mode.IsVisual() {
		cmd := m.caseSelection(kind)
		m.scroll()
		return cmd
	}
	var touched, skipped int
	m.fanMutate(func(rec *history.Recorder, pos, _ buffer.Position) buffer.Position {
		rng, ok := m.caseSpanAt(pos, kind)
		if !ok {
			skipped++
			return pos
		}
		text := m.buf.Slice(rng)
		out, ok := m.caseText(text, kind)
		if !ok {
			skipped++
			return pos
		}
		if out != text {
			touched++
			rec.Apply(buffer.Edit{Range: rng, Text: out})
		}
		return rng.Start
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.runCaseCommand(kind) }}
	m.scroll()
	if touched == 0 && skipped > 0 {
		return notice(caseMiss(kind))
	}
	return nil
}

// caseSelection rewrites the visual selection and leaves visual mode. A
// linewise selection covers its lines whole, like the operators do.
func (m *Model) caseSelection(kind caseKind) tea.Cmd {
	t := m.visualSelection()
	rng := t.Range
	if t.Linewise {
		z := t.Range.End.Line
		if last := m.buf.LineCount() - 1; z > last {
			z = last
		}
		rng = buffer.Range{
			Start: buffer.Position{Line: t.Range.Start.Line, Col: 0},
			End:   buffer.Position{Line: z, Col: m.buf.RuneLen(z)},
		}
	}
	text := m.buf.Slice(rng)
	out, ok := m.caseText(text, kind)
	m.mode = Normal
	m.pending.Reset()
	if !ok {
		return notice(caseMiss(kind))
	}
	if out != text {
		m.mutate(func(rec *history.Recorder) buffer.Position {
			rec.Apply(buffer.Edit{Range: rng, Text: out})
			return rng.Start
		})
		m.dot = &dotCommand{run: func(mm *Model) { mm.runCaseCommand(kind) }}
	}
	return nil
}

// caseText maps text for kind. ok is false only for a cycle over something
// that is not an identifier — the rune transforms always apply.
func (m Model) caseText(text string, kind caseKind) (string, bool) {
	if kind != caseCycle {
		return mapCase(text, kind), true
	}
	return idcase.Cycle(text)
}

// caseMiss is the notice for a command that found nothing to work on.
func caseMiss(kind caseKind) string {
	if kind == caseCycle {
		return "no identifier at the caret to cycle — camelCase, snake_case, kebab-case, PascalCase and SCREAMING_SNAKE rotate"
	}
	return "no word at the caret to convert"
}

// caseSpanAt resolves the span a caret-based case command acts on at pos: the
// identifier token for a cycle (it spans the `-` of a kebab name, which vim's
// word object stops at), the inner word for the rune transforms.
func (m Model) caseSpanAt(pos buffer.Position, kind caseKind) (buffer.Range, bool) {
	if kind == caseCycle {
		line := []rune(m.buf.Line(pos.Line))
		a, z, ok := identifierAt(line, pos.Col)
		if !ok {
			return buffer.Range{}, false
		}
		return buffer.Range{
			Start: buffer.Position{Line: pos.Line, Col: a},
			End:   buffer.Position{Line: pos.Line, Col: z},
		}, true
	}
	res := textobject.Word(m.buf, pos, false, false)
	if !res.OK || res.Linewise {
		return buffer.Range{}, false
	}
	return res.Range, true
}

// identifierAt returns the half-open column span of the identifier token
// covering col, extending across `_` and across a `-` that joins two name
// runes (so `kebab-case` is one token while `a - b` is not).
func identifierAt(line []rune, col int) (start, end int, ok bool) {
	if len(line) == 0 {
		return 0, 0, false
	}
	if col >= len(line) {
		col = len(line) - 1
	}
	namePart := func(i int) bool {
		if i < 0 || i >= len(line) {
			return false
		}
		return unicode.IsLetter(line[i]) || unicode.IsDigit(line[i]) || line[i] == '_'
	}
	// A caret parked on the joining `-` still names the whole token.
	if !namePart(col) && !(line[col] == '-' && namePart(col-1) && namePart(col+1)) {
		return 0, 0, false
	}
	start, end = col, col+1
	for {
		if namePart(start - 1) {
			start--
			continue
		}
		if start-2 >= 0 && line[start-1] == '-' && namePart(start-2) {
			start -= 2
			continue
		}
		break
	}
	for {
		if namePart(end) {
			end++
			continue
		}
		if end+1 < len(line) && line[end] == '-' && namePart(end+1) {
			end += 2
			continue
		}
		break
	}
	return start, end, true
}
