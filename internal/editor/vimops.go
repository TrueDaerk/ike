package editor

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/mode"
	"ike/internal/editor/motion"
	"ike/internal/editor/operator"
	"ike/internal/editor/viewport"
)

// vimops.go implements the vim parity keys of #1193: the case operators
// (gu/gU/g~), = reindent, gq reflow, the gv/gi/gf/gJ sequences, visual-mode
// case/join/replace, zz/zt/zb scrolling, ZZ/ZQ, and the display-line motions.

// OpenPathMsg asks the root model to open the file named under the cursor
// ("gf"). Path is as written in the buffer; the app resolves it against the
// current file's directory, then the project root.
type OpenPathMsg struct {
	Path string
	// From is the buffer path of the requesting editor, so relative names
	// resolve against its directory first.
	From string
}

// caseFunc maps a case-operator rune to its transform.
func caseFunc(op rune) func(rune) rune {
	switch op {
	case 'u':
		return unicode.ToLower
	case 'U':
		return unicode.ToUpper
	default: // '~'
		return swapCase
	}
}

// armCaseRedo arms opRedo with the gesture behind a pending *case* operator,
// so its dot repeats the gesture rather than the resolved span (#2418). Only
// the case operators take part: d/c/y record their dot elsewhere, and the
// other reshaping operators (>, <, =, gq) keep their span-replaying dots.
// build receives the operator and register the gesture ran with.
func (m *Model) armCaseRedo(build func(op, reg rune) func(*Model)) {
	if !m.pending.HasOperator() {
		return
	}
	switch m.pending.Operator {
	case 'u', 'U', '~':
		m.opRedo = build(m.pending.Operator, m.pending.Register)
	}
}

// replayCaseMotion re-runs a case operator with its motion at the cursor "."
// was pressed at, re-arming itself so a second "." repeats again.
func (m *Model) replayCaseMotion(op, reg rune, s string, r rune, count int) {
	res, ok := m.resolveMotion(s, r, count)
	if !ok {
		return
	}
	m.opRedo = func(mm *Model) { mm.replayCaseMotion(op, reg, s, r, count) }
	m.runOperator(op, operator.Compose(m.buf, m.cursor, res.Pos, res.Kind), reg)
	m.opRedo = nil
}

// replayCaseObject is replayCaseMotion for a text object (gUiw and friends);
// around distinguishes "iw" from "aw".
func (m *Model) replayCaseObject(op, reg rune, r rune, around bool) {
	saved := m.around
	m.around = around
	res := m.resolveTextObject(r)
	m.around = saved
	if !res.OK {
		return
	}
	m.opRedo = func(mm *Model) { mm.replayCaseObject(op, reg, r, around) }
	m.runOperator(op, objectTarget(res), reg)
	m.opRedo = nil
}

// replayCaseLinewise re-runs a linewise case double (guu/gUU/g~~) over count
// lines from the cursor, re-arming itself like the two above.
func (m *Model) replayCaseLinewise(op rune, count int) {
	end := m.cursor.Line + count - 1
	if last := m.buf.LineCount() - 1; end > last {
		end = last
	}
	m.opRedo = func(mm *Model) { mm.replayCaseLinewise(op, count) }
	m.runOperator(op, operator.LineTarget(m.cursor.Line, end), m.pending.Register)
	m.opRedo = nil
}

// caseTarget applies a case operator over target, recording a dot. When the
// key layer armed opRedo the dot replays the gesture (gUw at the *next* word,
// like vim); otherwise — a visual selection, a command — it replays the span.
func (m *Model) caseTarget(target operator.Target, op rune) {
	m.mutate(func(rec *history.Recorder) buffer.Position {
		return operator.Transform(m.buf, rec, target, caseFunc(op))
	})
	if redo := m.opRedo; redo != nil {
		m.dot = &dotCommand{run: redo}
		return
	}
	m.dot = &dotCommand{run: func(mm *Model) { mm.caseTarget(target, op) }}
}

// reindentTarget implements "=": re-derive the indent of every line the target
// covers from the language's block-opener heuristic (smartIndent), dropping one
// level for a line that starts with a closing bracket. A pure text heuristic —
// consistent with auto-indent — not an LSP format.
func (m *Model) reindentTarget(target operator.Target) {
	a, z := target.Range.Start.Line, target.Range.End.Line
	if z < a {
		a, z = z, a
	}
	if z > m.buf.LineCount()-1 {
		z = m.buf.LineCount() - 1
	}
	m.mutate(func(rec *history.Recorder) buffer.Position {
		for l := a; l <= z; l++ {
			line := m.buf.Line(l)
			body := strings.TrimLeft(line, " \t")
			if body == "" {
				// Blank lines lose their whitespace.
				if line != "" {
					rec.Apply(buffer.Delete(buffer.Range{Start: buffer.Position{Line: l}, End: buffer.Position{Line: l, Col: len([]rune(line))}}))
				}
				continue
			}
			indent := ""
			if ref := m.prevNonBlank(l); ref >= 0 {
				indent = m.smartIndent(ref, m.buf.Line(ref))
			}
			if r := []rune(body)[0]; r == '}' || r == ')' || r == ']' {
				indent = strings.TrimSuffix(strings.TrimSuffix(indent, m.tabText()), "\t")
			}
			old := leadingWhitespace(line)
			if old != indent {
				rec.Apply(buffer.Edit{
					Range: buffer.Range{Start: buffer.Position{Line: l}, End: buffer.Position{Line: l, Col: len([]rune(old))}},
					Text:  indent,
				})
			}
		}
		return buffer.Position{Line: a, Col: firstNonBlankColM(m.buf, a)}
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.reindentTarget(target) }}
}

// prevNonBlank returns the nearest line above l with non-blank content, or -1.
func (m *Model) prevNonBlank(l int) int {
	for r := l - 1; r >= 0; r-- {
		if strings.TrimSpace(m.buf.Line(r)) != "" {
			return r
		}
	}
	return -1
}

// firstNonBlankColM mirrors operator.firstNonBlankCol for editor-side cursor
// placement after linewise operations.
func firstNonBlankColM(b *buffer.Buffer, l int) int {
	r := []rune(b.Line(l))
	for c, ch := range r {
		if ch != ' ' && ch != '\t' {
			return c
		}
	}
	return 0
}

// reflowTarget implements "gq": hard-wrap the covered lines at
// editor.text_width. Paragraphs (separated by blank lines) reflow
// independently; each keeps the indent and comment leader of its first line.
func (m *Model) reflowTarget(target operator.Target) {
	a, z := target.Range.Start.Line, target.Range.End.Line
	if z < a {
		a, z = z, a
	}
	if z > m.buf.LineCount()-1 {
		z = m.buf.LineCount() - 1
	}
	width := m.textWidth
	if width < 1 {
		width = 79
	}
	var lines []string
	for l := a; l <= z; l++ {
		lines = append(lines, m.buf.Line(l))
	}
	out := reflowLines(lines, width)
	if strings.Join(out, "\n") == strings.Join(lines, "\n") {
		return
	}
	m.mutate(func(rec *history.Recorder) buffer.Position {
		rec.Apply(buffer.Edit{
			Range: buffer.Range{Start: buffer.Position{Line: a}, End: buffer.Position{Line: z, Col: m.buf.RuneLen(z)}},
			Text:  strings.Join(out, "\n"),
		})
		return buffer.Position{Line: a, Col: firstNonBlankColM(m.buf, a)}
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.reflowTarget(target) }}
}

// commentLeaders are the line-comment prefixes gq preserves on wrapped lines.
var commentLeaders = []string{"///", "//", "#", "--", ";;", ";", "*", ">"}

// reflowLines rewraps lines at width, paragraph by paragraph.
func reflowLines(lines []string, width int) []string {
	var out []string
	i := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) == "" {
			out = append(out, lines[i])
			i++
			continue
		}
		j := i
		for j < len(lines) && strings.TrimSpace(lines[j]) != "" {
			j++
		}
		out = append(out, reflowParagraph(lines[i:j], width)...)
		i = j
	}
	return out
}

// reflowParagraph joins para's words and wraps them at width, prefixing every
// line with the first line's indent and comment leader.
func reflowParagraph(para []string, width int) []string {
	indent := leadingWhitespace(para[0])
	leader := ""
	rest := strings.TrimPrefix(para[0], indent)
	for _, l := range commentLeaders {
		if strings.HasPrefix(rest, l+" ") || rest == l {
			leader = l + " "
			break
		}
	}
	prefix := indent + leader
	var words []string
	for _, line := range para {
		s := strings.TrimLeft(line, " \t")
		if leader != "" {
			s = strings.TrimPrefix(s, strings.TrimRight(leader, " "))
			s = strings.TrimLeft(s, " \t")
		}
		words = append(words, strings.Fields(s)...)
	}
	if len(words) == 0 {
		return []string{strings.TrimRight(prefix, " ")}
	}
	var out []string
	cur := prefix + words[0]
	for _, w := range words[1:] {
		if len([]rune(cur))+1+len([]rune(w)) > width && len([]rune(cur)) > len([]rune(prefix)) {
			out = append(out, cur)
			cur = prefix + w
			continue
		}
		cur += " " + w
	}
	return append(out, cur)
}

// replaceSelection implements visual-mode "r": overwrite every rune of the
// selection with ch, preserving line breaks.
func (m *Model) replaceSelection(ch rune) {
	target := m.visualSelection()
	m.mode = Normal
	r := target.Range
	if target.Linewise {
		r = buffer.Range{
			Start: buffer.Position{Line: r.Start.Line, Col: 0},
			End:   buffer.Position{Line: r.End.Line, Col: m.buf.RuneLen(r.End.Line)},
		}
	}
	m.mutate(func(rec *history.Recorder) buffer.Position {
		text := m.buf.Slice(r)
		mapped := strings.Map(func(c rune) rune {
			if c == '\n' {
				return c
			}
			return ch
		}, text)
		if mapped != text {
			rec.Apply(buffer.Edit{Range: r, Text: mapped})
		}
		return r.Start
	})
	m.pending.Reset()
}

// joinVisual implements visual-mode "J"/"gJ": join the selected lines.
func (m *Model) joinVisual(space bool) {
	lo, hi := m.anchor.Line, m.cursor.Line
	if lo > hi {
		lo, hi = hi, lo
	}
	m.mode = Normal
	m.cursor = m.buf.ClampCursor(buffer.Position{Line: lo})
	count := hi - lo + 1
	if count < 2 {
		count = 2
	}
	m.joinLines(count, space)
	m.pending.Reset()
}

// visualSnapshot remembers the last visual selection for "gv".
type visualSnapshot struct {
	anchor, cursor buffer.Position
	mode           mode.Mode
	ok             bool
}

// reselectVisual implements "gv": re-enter the last visual mode with its
// anchor and cursor restored (clamped — the buffer may have changed since).
func (m *Model) reselectVisual() {
	if !m.lastVis.ok {
		return
	}
	m.collapseCarets()
	m.mode = m.lastVis.mode
	m.anchor = m.buf.ClampCursor(m.lastVis.anchor)
	m.cursor = m.buf.ClampCursor(m.lastVis.cursor)
	m.desiredCol = m.cursor.Col
	m.shiftSelect = false
	m.clickVisual = false
	m.emit(EventCursorMove)
}

// gotoLastInsert implements "gi": enter insert mode at the position of the
// last insert-mode exit.
func (m *Model) gotoLastInsert() {
	if !m.lastInsert.ok {
		return
	}
	m.collapseCarets()
	m.cursor = m.buf.Clamp(m.lastInsert.pos)
	m.desiredCol = m.cursor.Col
	m.startInsertWith(m.newRecorder(), nil)
}

// openFileUnderCursor implements "gf": extract the path-like token under the
// cursor and ask the app to open it.
func (m *Model) openFileUnderCursor() tea.Cmd {
	line := []rune(m.buf.Line(m.cursor.Line))
	if m.cursor.Col >= len(line) {
		return nil
	}
	isPath := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r) ||
			strings.ContainsRune("_-./~+#$%@\\", r)
	}
	if !isPath(line[m.cursor.Col]) {
		return nil
	}
	a, z := m.cursor.Col, m.cursor.Col
	for a > 0 && isPath(line[a-1]) {
		a--
	}
	for z < len(line)-1 && isPath(line[z+1]) {
		z++
	}
	path := strings.Trim(string(line[a:z+1]), ".")
	if path == "" {
		return nil
	}
	from := m.path
	return func() tea.Msg { return OpenPathMsg{Path: path, From: from} }
}

// scrollCountLine implements [count]zz/zt/zb (#2144): a count first moves the
// cursor to that line (same column, like vim), then the line is positioned.
func (m *Model) scrollCountLine(dir int) {
	if n := countOrZero(m.pending); n > 0 {
		m.moveTo(buffer.Position{Line: n - 1, Col: m.cursor.Col})
	}
	m.scrollCursorLine(dir)
}

// scrollCursorLine implements zz/zt/zb: place the cursor's line at the centre,
// top or bottom of the view. dir is -1 (top), 0 (centre) or 1 (bottom).
func (m *Model) scrollCursorLine(dir int) {
	h := m.view.Height()
	if h < 1 {
		return
	}
	off := m.view.ScrollOff
	if max := (h - 1) / 2; off > max {
		off = max
	}
	switch dir {
	case -1:
		m.view.Top = m.cursor.Line - off
	case 0:
		m.view.Top = m.cursor.Line - h/2
	default:
		m.view.Top = m.cursor.Line - h + 1 + off
	}
	if m.view.Top > m.buf.LineCount()-1 {
		m.view.Top = m.buf.LineCount() - 1
	}
	if m.view.Top < 0 {
		m.view.Top = 0
	}
}

// displayMotion resolves the g-prefixed display-line motions (g0, g$, gj, gk).
// Without soft wrap they degrade to their buffer-line counterparts.
func (m *Model) displayMotion(s string, count int) (motion.Result, bool) {
	if !m.softWrap {
		switch s {
		case "0":
			return motion.LineStart(m.buf, m.cursor, count), true
		case "$":
			return motion.LineEnd(m.buf, m.cursor, count), true
		case "j":
			if m.hasFolds() {
				return m.foldVertical(count, 1), true
			}
			return motion.Down(m.buf, m.cursor, count), true
		case "k":
			if m.hasFolds() {
				return m.foldVertical(count, -1), true
			}
			return motion.Up(m.buf, m.cursor, count), true
		}
		return motion.Result{}, false
	}
	segs := m.wrapSegs(m.cursor.Line)
	si := viewport.SegmentIndex(segs, m.cursor.Col)
	switch s {
	case "0":
		return motion.Result{Pos: buffer.Position{Line: m.cursor.Line, Col: segs[si]}, Kind: motion.Exclusive}, true
	case "$":
		end := viewport.SegmentEnd(segs, si, m.buf.RuneLen(m.cursor.Line))
		col := end - 1
		if col < segs[si] {
			col = segs[si]
		}
		return motion.Result{Pos: buffer.Position{Line: m.cursor.Line, Col: col}, Kind: motion.Inclusive}, true
	case "j":
		return m.wrapVertical(count, 1), true
	case "k":
		return m.wrapVertical(count, -1), true
	}
	return motion.Result{}, false
}
