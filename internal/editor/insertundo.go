package editor

import "time"

// insertundo.go gives the insert session its undo granularity (#1818, #2189).
// An insert used to commit as exactly one change, so a single undo threw away
// everything typed since entering insert mode. The session now commits a
// sequence of changes, split at the boundaries a user thinks in:
//
//   - a pasted block is its own change — one undo removes exactly the block,
//     and anything typed after it undoes separately (the block stays);
//   - typing splits word-wise: a new segment opens in front of a word run that
//     follows a separator, so a change is "one word plus the separators typed
//     after it". Typing `foo bar baz` therefore leaves the changes `foo `,
//     `bar `, `baz`, and three undos peel the words off from the right — the
//     JetBrains typing granularity (vim commits the whole insert, which is what
//     this issue set out to fix). Trailing whitespace rides with the word
//     before it, so undoing `baz` leaves `foo bar ` and the caret where the
//     next word would start.
//   - a pause splits too (#2189): a typing keystroke arriving more than
//     insertPauseBreak after the segment's previous one closes the segment
//     first, so "undo the text since the last pause" works even inside one
//     long word run. The boundary is drawn lazily at the next keystroke — no
//     timer runs while the user thinks — which yields the same units a live
//     debounce would. Only elapsed time between two typed runes of the *same*
//     segment counts: a segment that holds no typed rune yet (the `cw`
//     deletion, the line `o` opened) never pause-splits, so thinking before
//     the first character cannot tear the structural edit off the typing.
//   - an accepted completion and a Tab-triggered snippet expansion are their
//     own change (#2189), like a paste: the typed prefix commits as one step,
//     the accept (with its auto-import edits) as the next, so one undo removes
//     exactly the accepted text and puts the typed prefix back.
//
// A segment that holds no word yet never word-splits: separators typed at the
// start of a segment — the indent after `Enter`, the `(` that auto-closed into
// `()`, the space `o` opened a line with — belong to the word that follows
// them, so no undo can tear a pair or an indent off the keystroke that
// produced it.
//
// Everything else that edits mid-insert — backspace and the word/line kills,
// `Tab`/`Shift+Tab`, auto-close pairs, smart indent — joins the running
// segment: a correction belongs to the word it corrects. Those paths clear the
// segment's typing state (`dirtyFromInsert`), so typing on after a backspace
// never splits mid-word.
//
// The split lives entirely in the editor: `history` stays change-based and
// unchanged, `commitInsert` just commits the remaining tail, and normal-mode
// operations (`dd`, `ciw`, `:%s`, …) keep their one-change semantics.

// insertPauseBreak is the typing-pause undo boundary (#2189): a gap of at
// least this long between two typed runes of one segment closes it. Fixed
// rather than configurable — long enough that hunt-and-peck typing stays in
// one word unit, short enough that "what I typed after stopping to think"
// undoes on its own.
const insertPauseBreak = 2 * time.Second

// typedNow is the insert-session clock (overridable via typeNow in tests).
func (m *Model) typedNow() time.Time {
	if m.typeNow != nil {
		return m.typeNow()
	}
	return time.Now()
}

// breakInsertUndo closes the insert session's current undo segment: the open
// recorder commits as its own change and a fresh one takes its place, so the
// following edits form the next undo step. A segment that recorded nothing
// commits nothing — no empty or duplicate change comes out of a break at the
// very start of an insert, or of two breaks in a row (paste).
func (m *Model) breakInsertUndo() {
	if !m.insert.active {
		return
	}
	m.insert.last, m.insert.word = 0, false
	if m.insert.rec == nil || m.insert.rec.Empty() {
		return
	}
	m.pushChange(m.insert.rec.Commit(m.cursor))
	// The segment's edits already emitted their EventChange (and with it the
	// fold/mark/change-list shift), so the ring entry lands now instead of
	// riding along on the next event — see changelist.go's pushChange note.
	m.recordPendingChange()
	m.insert.rec = m.newRecorder()
}

// typedInsert wraps a typing keystroke (printable input, Enter, Tab) with the
// word-wise and pause undo splits: text opens a new segment when it starts a
// word run that follows a separator inside a segment that already holds a
// word, or when it arrives insertPauseBreak after the segment's previous rune
// (#2189). apply performs the actual insert; the segment's typing state is
// restored across it because every edit path clears it (dirtyFromInsert).
func (m *Model) typedInsert(text string, apply func()) {
	now := m.typedNow()
	// last != 0 means this segment already holds a typed rune, so typeAt is
	// its timestamp — a fresh segment (or one only holding structural edits
	// or corrections) never pause-splits.
	if m.insert.last != 0 && now.Sub(m.insert.typeAt) >= insertPauseBreak {
		m.breakInsertUndo()
	}
	if startsWord(text) && m.insert.word && !isWordRune(m.insert.last) {
		m.breakInsertUndo()
	}
	word := m.insert.word || hasWordRune(text)
	apply()
	m.insert.last, m.insert.word = lastRune(text), word
	m.insert.typeAt = now
}

// startsWord reports whether text begins with a word rune (letter, digit or
// underscore) — the left edge of an undo segment.
func startsWord(text string) bool {
	for _, r := range text {
		return isWordRune(r)
	}
	return false
}

// hasWordRune reports whether text contains a word rune, i.e. whether typing it
// gives the open segment something a later word boundary can split off.
func hasWordRune(text string) bool {
	for _, r := range text {
		if isWordRune(r) {
			return true
		}
	}
	return false
}

// lastRune returns text's final rune, 0 for empty text.
func lastRune(text string) rune {
	last := rune(0)
	for _, r := range text {
		last = r
	}
	return last
}

// pasteIntoInsert splices a pasted block into the open insert session as its
// own undo step (#1818): the running segment commits first, the block lands in
// a recorder of its own, and that one closes right after — so one undo removes
// exactly the block, and characters typed afterwards form the next segment
// rather than joining it.
func (m *Model) pasteIntoInsert(text string) {
	// Filling an empty, file-less buffer classifies it (#2037).
	cand := m.detectCandidate()
	defer func() { m.detectPastedLang(cand) }()
	m.breakInsertUndo()
	m.insertText(text)
	m.breakInsertUndo()
}
