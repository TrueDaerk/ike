package editor

// labeljump.go implements the label-jump motion (#787), an easymotion /
// leap.nvim-style "type what you see" navigation: gs (or the editor.labelJump
// action) opens a session, the next one or two typed characters select the
// visible matches, and every match is overlaid with a short label drawn from a
// home-row-first alphabet. Typing a label moves the caret there through the
// navigation-history seam (jumpTo → EventJump); esc cancels with the cursor
// untouched. A typed key is never ambiguous between "narrow the query" and
// "pick this label": label assignment excludes every rune that could still
// extend a match, so the two key sets are disjoint by construction.

import (
	"sort"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
)

// labelAlphabet orders the label keys by typing comfort — home row first,
// then the upper and lower rows. Labels are assigned in this order and the
// targets are sorted nearest-first, so the closest targets get the most
// comfortable keys.
const labelAlphabet = "asdfghjklqwertyuiopzxcvbnm"

// leapMaxQuery caps the target query length: after two characters every
// further key is a label pick (or cancels).
const leapMaxQuery = 2

// leapTarget is one visible match: its buffer position and assigned label
// (empty when more targets than labels are on screen — narrowing the query
// is the way to reach the unlabeled tail).
type leapTarget struct {
	pos   buffer.Position
	label string
}

// leapState is a live label-jump session, held on the Model while the wait
// machine is parked in awaitLabel.
type leapState struct {
	query   []rune       // typed target characters (up to leapMaxQuery)
	targets []leapTarget // visible matches, nearest to the cursor first
	prefix  rune         // typed first key of a two-character label (0: none)
}

// find returns the target carrying exactly the given label.
func (s *leapState) find(label string) (leapTarget, bool) {
	for _, t := range s.targets {
		if t.label == label {
			return t, true
		}
	}
	return leapTarget{}, false
}

// hasPrefix reports whether any two-character label starts with r.
func (s *leapState) hasPrefix(r rune) bool {
	for _, t := range s.targets {
		if len(t.label) == 2 && []rune(t.label)[0] == r {
			return true
		}
	}
	return false
}

// startLabelJump opens a label-jump session: the next keys route through
// labelJumpKey until a label lands or the session cancels. Jumps are
// single-caret, like search.
func (m *Model) startLabelJump() {
	m.collapseCarets()
	m.leap = &leapState{}
	m.wait = awaitLabel
	m.cmdMsg = "label jump: type target"
}

// cancelLabelJump drops the session; the cursor and viewport stay where the
// session found them.
func (m *Model) cancelLabelJump() {
	m.leap = nil
	m.wait = awaitNone
	m.cmdMsg = ""
	m.pending.Reset()
}

// labelJumpKey resolves one key of the session: a target character while the
// query is still open, a label key (or the second half of a two-character
// label) once labels are up. Esc — any non-text key — cancels.
func (m Model) labelJumpKey(r rune, hasRune bool) (Model, tea.Cmd) {
	if m.leap == nil || !hasRune {
		m.cancelLabelJump()
		return m, nil
	}
	st := m.leap
	// Second key of a two-character label: exact pick or cancel.
	if st.prefix != 0 {
		if t, ok := st.find(string([]rune{st.prefix, r})); ok {
			return m.finishLabelJump(t)
		}
		m.cancelLabelJump()
		return m, nil
	}
	// A label key wins over extending the query — the assignment excluded
	// every rune that could continue a match, so both can never apply.
	if len(st.query) > 0 {
		if t, ok := st.find(string(r)); ok {
			return m.finishLabelJump(t)
		}
		if st.hasPrefix(r) {
			st.prefix = r
			m.wait = awaitLabel
			m.cmdMsg = "label jump: " + string(st.query) + " " + string(r) + "…"
			return m, nil
		}
	}
	if len(st.query) >= leapMaxQuery {
		// The query is full and the key named no label: cancel, like the
		// other await states drop an unusable continuation.
		m.cancelLabelJump()
		return m, nil
	}
	st.query = append(st.query, r)
	st.targets = m.collectLeapTargets(st.query)
	if len(st.targets) == 0 {
		query := string(st.query)
		m.cancelLabelJump()
		m.cmdMsg = "label jump: no match for " + strconv.Quote(query)
		return m, nil
	}
	if len(st.targets) == 1 {
		// A unique match needs no label: jump straight there (leap's autojump).
		return m.finishLabelJump(st.targets[0])
	}
	m.assignLeapLabels(st)
	m.wait = awaitLabel
	m.cmdMsg = "label jump: " + string(st.query) + " (" + strconv.Itoa(len(st.targets)) + ")"
	return m, nil
}

// finishLabelJump lands the session on t: the departure position enters the
// navigation history via jumpTo's EventJump, then the caret moves.
func (m Model) finishLabelJump(t leapTarget) (Model, tea.Cmd) {
	m.leap = nil
	m.wait = awaitNone
	m.cmdMsg = ""
	m.pending.Reset()
	m.jumpTo(t.pos)
	return m, nil
}

// collectLeapTargets scans the visible region for case-sensitive matches of
// query, mirroring the View body loop: from the first row below the sticky
// headers, skipping lines hidden in collapsed folds, until the pane height is
// filled. Without soft wrap only the horizontally visible column window
// counts. Targets sort nearest-to-the-cursor first (line distance, then
// document order) so the closest ones receive the shortest labels; the match
// under the cursor itself is skipped — jumping there is a no-op.
func (m *Model) collectLeapTargets(query []rune) []leapTarget {
	lineCount := m.buf.LineCount()
	top := m.view.Top + len(m.stickyLines())
	height := m.view.Height()
	if height <= 0 {
		// An unsized pane shows every line, matching viewport.Bottom.
		height = lineCount
	}
	var out []leapTarget
	rows := 0
	for line := top; rows < height && line < lineCount; line++ {
		if m.lineHidden(line) {
			continue
		}
		rows++
		runes := []rune(m.buf.Line(line))
		lo, hi := 0, len(runes)
		if !m.softWrap {
			lo = m.view.Left
			if tw := m.view.TextWidth(lineCount); tw > 0 && lo+tw < hi {
				hi = lo + tw
			}
		}
		for col := lo; col < hi && col+len(query) <= len(runes); col++ {
			match := true
			for i, q := range query {
				if runes[col+i] != q {
					match = false
					break
				}
			}
			if !match || (line == m.cursor.Line && col == m.cursor.Col) {
				continue
			}
			out = append(out, leapTarget{pos: buffer.Position{Line: line, Col: col}})
		}
	}
	cur := m.cursor.Line
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := absInt(out[i].pos.Line-cur), absInt(out[j].pos.Line-cur)
		if di != dj {
			return di < dj
		}
		if out[i].pos.Line != out[j].pos.Line {
			return out[i].pos.Line < out[j].pos.Line
		}
		return out[i].pos.Col < out[j].pos.Col
	})
	return out
}

// assignLeapLabels gives every target a unique label over labelAlphabet,
// excluding the runes that could still extend the query (the character each
// match shows right after the typed span) so a label key is never also a
// narrowing key. When the remaining alphabet is smaller than the target
// count, its tail keys become two-character prefixes (…, ma, ms, …), keeping
// the label set prefix-free; targets past the pair capacity stay unlabeled
// and are reached by narrowing the query.
func (m *Model) assignLeapLabels(st *leapState) {
	excluded := map[rune]bool{}
	if len(st.query) < leapMaxQuery {
		for _, t := range st.targets {
			runes := []rune(m.buf.Line(t.pos.Line))
			if i := t.pos.Col + len(st.query); i < len(runes) {
				excluded[runes[i]] = true
			}
		}
	}
	var alpha []rune
	for _, r := range labelAlphabet {
		if !excluded[r] {
			alpha = append(alpha, r)
		}
	}
	labels := leapLabels(alpha, len(st.targets))
	for i := range st.targets {
		if i < len(labels) {
			st.targets[i].label = labels[i]
		} else {
			st.targets[i].label = ""
		}
	}
}

// leapLabels builds up to n prefix-free labels over alpha: singles from the
// front, and — when n exceeds the alphabet — pairs under prefixes reserved
// from the back, so the most comfortable keys stay single-press. It reserves
// the smallest prefix count k with (len-k) singles + k*len pairs ≥ n.
func leapLabels(alpha []rune, n int) []string {
	size := len(alpha)
	if size == 0 {
		return nil
	}
	k := 0
	for (size-k)+k*size < n && k < size {
		k++
	}
	labels := make([]string, 0, n)
	for i := 0; i < size-k && len(labels) < n; i++ {
		labels = append(labels, string(alpha[i]))
	}
	for i := size - k; i < size && len(labels) < n; i++ {
		for j := 0; j < size && len(labels) < n; j++ {
			labels = append(labels, string(alpha[i])+string(alpha[j]))
		}
	}
	return labels
}

// leapLabelAt returns the label character overlaying (line, col), the render
// probe for the label cells. A label's characters occupy the cells from the
// match start; once the first key of a two-character label is typed, only its
// targets keep an overlay — showing just the key that is left to type.
func (m Model) leapLabelAt(line, col int) (rune, bool) {
	if m.leap == nil {
		return 0, false
	}
	for _, t := range m.leap.targets {
		if t.label == "" || t.pos.Line != line {
			continue
		}
		label := []rune(t.label)
		if m.leap.prefix != 0 {
			if label[0] != m.leap.prefix {
				continue
			}
			label = label[1:]
		}
		if off := col - t.pos.Col; off >= 0 && off < len(label) {
			return label[off], true
		}
	}
	return 0, false
}

// leapMatchAt reports whether (line, col) lies inside the typed span of a
// target, for the search-match-style background under the labels.
func (m Model) leapMatchAt(line, col int) bool {
	if m.leap == nil || len(m.leap.query) == 0 {
		return false
	}
	for _, t := range m.leap.targets {
		if t.pos.Line == line && col >= t.pos.Col && col < t.pos.Col+len(m.leap.query) {
			return true
		}
	}
	return false
}

// absInt is the integer absolute value (no float round trip).
func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
