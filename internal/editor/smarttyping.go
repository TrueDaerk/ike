package editor

import (
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/lang"
)

// smarttyping.go implements typing assistance beyond auto-close pairs (#1326):
// punctuation a language declares in lang.SpaceAfter gets its conventional
// space written for you as you type — ':' in JSON, so `"key":` becomes
// `"key": ` mid-keystroke. Gated on editor.typing.space_after_punctuation.
//
// Context detection is a pure text heuristic, deliberately: highlighting is
// parsed off the event loop and lags the keystroke by one frame, so a
// capture-based check would be stale exactly when it matters. Quote parity and
// the language's line-comment marker are exact for the case that pays — a
// colon inside a JSON string — and cost one line scan per typed character.

// spaceAfterLang returns the language governing line: the embedded region's
// language when one covers it (#1304, the .http request body the issue came
// from — a JSON body follows JSON's typing conventions, not the host's),
// otherwise the buffer's own. ok=false when no language is known.
func (m *Model) spaceAfterLang(line int) (lang.Language, bool) {
	host, ok := lang.ByPath(m.langPath())
	if !ok {
		return lang.Language{}, false
	}
	if host.Regions != nil {
		r, inRegion := lang.RegionAt(host.ID, m.buf.Lines(), line)
		if !inRegion {
			// Outside every region only the host's own conventions apply.
			return host, true
		}
		el, known := lang.ByID(r.Lang)
		if !known {
			return lang.Language{}, false
		}
		return el, true
	}
	return host, true
}

// spacesAfter reports whether ch is punctuation the language governing line
// writes a space after.
func (m *Model) spacesAfter(line int, ch rune) bool {
	l, ok := m.spaceAfterLang(line)
	if !ok {
		return false
	}
	for _, r := range l.SpaceAfter {
		if r == ch {
			return true
		}
	}
	return false
}

// smartSpaceWrite intercepts a typed rune for the space-after-punctuation aid,
// returning false when it does not apply so the plain insert path proceeds.
// Like auto-close, every caret decides on its own context, so one fan-out can
// mix assisted and plain inserts.
func (m *Model) smartSpaceWrite(text string) bool {
	if !m.spaceAfterPunct {
		return false
	}
	r := []rune(text)
	if len(r) != 1 {
		return false // paste and multi-rune input are never assisted
	}
	primary := m.spaceAfterOK(m.cursor, r[0])
	anywhere := primary
	for _, c := range m.carets {
		anywhere = anywhere || m.spaceAfterOK(c.pos, r[0])
	}
	if !anywhere {
		return false
	}
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	m.fanApply(func(pos, _ buffer.Position) buffer.Position {
		ins := string(r[0])
		if m.spaceAfterOK(pos, r[0]) {
			ins += " "
		}
		return m.insert.rec.Apply(buffer.Insert(pos, ins))
	})
	// "." replays what the primary caret produced, so a dot-repeat reproduces
	// what the screen showed rather than the bare keystroke.
	m.insert.typed += string(r[0])
	if primary {
		m.insert.typed += " "
	}
	m.dirtyFromInsert()
	return true
}

// spaceAfterOK reports whether typing ch at pos should append a space: the
// language declares the rune, the caret is not inside a string or a line
// comment, and no space (or line end) already follows.
func (m *Model) spaceAfterOK(pos buffer.Position, ch rune) bool {
	if !m.spacesAfter(pos.Line, ch) {
		return false
	}
	switch m.runeAt(pos) {
	case ' ', '\t':
		return false // a separator is already there; do not double it
	}
	return !m.inStringOrLineComment(pos)
}

// inStringOrLineComment reports whether pos sits inside a string literal or
// behind the governing language's line-comment marker, scanning the line's
// text left of pos. Quote parity handles the common single-line literal; a
// multi-line string (or a block comment) is not modelled — the aid firing once
// inside one is a cosmetic miss, not a corruption.
func (m *Model) inStringOrLineComment(pos buffer.Position) bool {
	line := []rune(m.buf.Line(pos.Line))
	col := min(max(pos.Col, 0), len(line))
	marker := ""
	if l, ok := m.spaceAfterLang(pos.Line); ok {
		marker = l.LineComment
	}
	var quote rune
	for i := 0; i < col; i++ {
		c := line[i]
		if quote != 0 {
			switch c {
			case '\\':
				i++ // an escaped rune never closes the literal
			case quote:
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		default:
			if marker != "" && strings.HasPrefix(string(line[i:]), marker) {
				return true // the rest of the line is a comment
			}
		}
	}
	return quote != 0
}
