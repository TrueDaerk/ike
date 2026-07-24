package editor

import (
	"fmt"
	"regexp"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
)

// subHit is one precomputed substitute match: its line, the match's rune-column
// span in the original line, and the capture-group strings for the replacement.
type subHit struct {
	line       int
	start, end int
	groups     []string
}

// subConfirmState drives the interactive ":s///c" confirmation. It walks the
// precomputed hits in reading order, applying accepted replacements into one
// open recorder (a single undo unit). lineDelta tracks the running rune-column
// shift already applied to each line so a later hit on the same line maps from
// its original span to the current buffer; curLine/curStart/curEnd is the span
// the view highlights.
type subConfirmState struct {
	repl                      string
	hits                      []subHit
	idx                       int
	lineDelta                 map[int]int
	rec                       *history.Recorder
	replaced                  int
	touched                   map[int]bool
	curLine, curStart, curEnd int
}

// beginSubstituteConfirm collects the matches over [start,end] and enters the
// confirmation sub-state on the first one. Without the "g" flag only the first
// match per line is offered, matching vim.
func (m Model) beginSubstituteConfirm(re *regexp.Regexp, repl string, global bool, start, end int, pat string) Model {
	var hits []subHit
	for i := start; i <= end; i++ {
		line := m.buf.Line(i)
		for _, loc := range re.FindAllStringSubmatchIndex(line, -1) {
			if loc[0] == loc[1] {
				continue // skip empty matches
			}
			hits = append(hits, subHit{
				line:   i,
				start:  byteToRune(line, loc[0]),
				end:    byteToRune(line, loc[1]),
				groups: submatchStrings(line, loc),
			})
			if !global {
				break
			}
		}
	}
	if len(hits) == 0 {
		m.cmdMsg = "E: pattern not found: " + pat
		return m
	}
	m.subConfirm = &subConfirmState{
		repl:      repl,
		hits:      hits,
		lineDelta: map[int]int{},
		rec:       history.NewRecorder(m.buf, m.cursor),
		touched:   map[int]bool{},
	}
	m.mode = Command // capture single-letter keys; the prompt renders on the ":" row
	m = m.focusSubMatch()
	m.cmdMsg = "replace (y/n/a/q/l)?"
	return m
}

// updateSubConfirm handles one keypress while the confirmation prompt is open.
func (m Model) updateSubConfirm(key tea.KeyPressMsg) Model {
	if key.Code == tea.KeyEscape {
		return m.finishSubConfirm()
	}
	r, ok := firstRune(key)
	if !ok {
		return m
	}
	switch r {
	case 'y':
		m.applyCurrentMatch()
		return m.advanceSubConfirm()
	case 'n':
		return m.advanceSubConfirm()
	case 'l': // replace this one, then stop
		m.applyCurrentMatch()
		return m.finishSubConfirm()
	case 'q': // stop without replacing this one
		return m.finishSubConfirm()
	case 'a': // replace this and every remaining match
		sc := m.subConfirm
		for sc.idx < len(sc.hits) {
			m.applyCurrentMatch()
			sc.idx++
		}
		return m.finishSubConfirm()
	}
	return m // any other key waits for a valid answer
}

// focusSubMatch positions the cursor on the current hit and records its span for
// the view to highlight, mapping the original span through the line's delta.
func (m Model) focusSubMatch() Model {
	sc := m.subConfirm
	h := sc.hits[sc.idx]
	d := sc.lineDelta[h.line]
	sc.curLine, sc.curStart, sc.curEnd = h.line, h.start+d, h.end+d
	m.cursor = m.buf.ClampCursor(buffer.Position{Line: h.line, Col: h.start + d})
	m.desiredCol = m.cursor.Col
	return m
}

// applyCurrentMatch replaces the current hit through the open recorder and grows
// the line's delta by the length change so later hits on that line stay aligned.
func (m *Model) applyCurrentMatch() {
	sc := m.subConfirm
	h := sc.hits[sc.idx]
	d := sc.lineDelta[h.line]
	text := expandRepl(sc.repl, h.groups)
	sc.rec.Apply(buffer.Edit{
		Range: buffer.Range{
			Start: buffer.Position{Line: h.line, Col: h.start + d},
			End:   buffer.Position{Line: h.line, Col: h.end + d},
		},
		Text: text,
	})
	sc.lineDelta[h.line] = d + utf8.RuneCountInString(text) - (h.end - h.start)
	sc.replaced++
	sc.touched[h.line] = true
}

// advanceSubConfirm moves to the next hit, or finishes when none remain.
func (m Model) advanceSubConfirm() Model {
	sc := m.subConfirm
	sc.idx++
	if sc.idx >= len(sc.hits) {
		return m.finishSubConfirm()
	}
	return m.focusSubMatch()
}

// finishSubConfirm commits the accumulated replacements as one undo unit, leaves
// the sub-state, and reports the outcome. Already-applied replacements are kept.
func (m Model) finishSubConfirm() Model {
	sc := m.subConfirm
	m.subConfirm = nil
	m.mode = Normal

	lastLine := m.cursor.Line
	if sc.replaced > 0 {
		hi := -1
		for l := range sc.touched {
			if l > hi {
				hi = l
			}
		}
		if hi >= 0 {
			lastLine = hi
		}
	}
	cursorAfter := m.buf.ClampCursor(buffer.Position{Line: lastLine, Col: 0})
	if !sc.rec.Empty() {
		m.pushChange(sc.rec.Commit(cursorAfter))
		m.dirty = true
		m.emit(EventChange)
		m.cursor = cursorAfter
		m.desiredCol = cursorAfter.Col
	}
	if sc.replaced > 0 {
		m.cmdMsg = fmt.Sprintf("%d substitution%s on %d line%s", sc.replaced, plural(sc.replaced, "s"), len(sc.touched), plural(len(sc.touched), "s"))
	} else {
		m.cmdMsg = ""
	}
	return m
}

// byteToRune converts a byte offset within s to a rune column.
func byteToRune(s string, byteOff int) int {
	n := 0
	for i := range s {
		if i >= byteOff {
			break
		}
		n++
	}
	return n
}
