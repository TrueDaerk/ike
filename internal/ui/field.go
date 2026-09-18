package ui

// field.go is the value type every single-line input should hold (#2459).
//
// EditKey, PasteText and CursorView are the behaviour; Field is the state
// they operate on. Before it, each of the ~60 one-line inputs in the IDE
// carried its own `text string` + `cur int` pair and its own four-line
// "call EditKey, store the result if handled" dance — the same boilerplate
// written sixty times, and sixty places where a caller could forget to write
// the cursor back, forget to route paste, or render without a cursor cell.
//
// A Field is a plain struct with exported fields, deliberately: a host that
// has to read the text for a matcher, a renderer or a completion source just
// reads Text, and one that seeds a cursor writes Cur. The zero value is an
// empty field with the cursor at position 0.

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Field is one single-line text input's state: the text and the caret, as a
// rune index into it.
type Field struct {
	Text string
	Cur  int

	// sel is the whole-text selection cmd+a arms (#2633): the next insertion
	// replaces the text instead of adding to it. It is deliberately not a
	// range — a one-line input has no selection gestures, and "all or
	// nothing" is the only selection its keys can produce.
	sel bool
	// undo is the bounded edit history ctrl+z walks back (#2633), newest
	// last, and lastKind/lastCur coalesce a run of same-kind edits into one
	// step. Unexported: they are behaviour, not state a host seeds.
	undo     []fieldState
	lastKind editKind
	lastCur  int
}

// NewField returns a field holding text with the cursor at its end.
func NewField(text string) Field {
	return Field{Text: text, Cur: len([]rune(text))}
}

// Key applies one editing key through EditKey. handled reports the key was an
// editing key (an unhandled key is the caller's to interpret); changed reports
// the text actually differs, which is the signal to re-filter a list, re-run a
// preview or refresh completions.
//
// Caller chords win: run the field's own bindings first and hand Key what is
// left over.
func (f *Field) Key(msg tea.KeyPressMsg) (handled, changed bool) {
	// The two chords the field owns before EditKey sees them (#2633).
	// Select-all only arms a state, so it never reports a change; undo
	// reports one whenever it restores a different text.
	if IsSelectAllKey(msg) {
		f.SelectAll()
		return true, false
	}
	if IsUndoKey(msg) {
		return true, f.Undo()
	}
	if f.sel {
		// An armed selection is replaced by the next insertion and cleared by
		// a plain backspace/delete; every other key just drops it and acts on
		// the text as it stands (a motion moves, a kill kills).
		f.sel = false
		if ins, ok := replacesSelection(msg); ok {
			f.noteEdit(editInsert, len([]rune(ins)))
			f.Text, f.Cur = ins, len([]rune(ins))
			return true, true
		}
	}
	out, cur, handled, changed := EditKey(msg, f.Text, f.Cur)
	if handled {
		if changed {
			kind := editDelete
			if Typing(msg) {
				kind = editInsert
			}
			f.noteEdit(kind, cur)
		}
		f.Text, f.Cur = out, cur
	}
	return handled, changed
}

// replacesSelection reports whether msg is a key that replaces an armed
// whole-text selection, and what it replaces it with: a typed rune (the text
// it inserts) or a plain backspace/delete (the empty string). A multi-line
// paste-as-typing is refused for the same reason EditKey refuses it — a
// one-line field never holds a break.
func replacesSelection(msg tea.KeyPressMsg) (string, bool) {
	if Typing(msg) {
		if hasLineBreak([]rune(msg.Text)) {
			return "", false
		}
		return msg.Text, true
	}
	if msg.Mod == 0 && (msg.Code == tea.KeyBackspace || msg.Code == tea.KeyDelete) {
		return "", true
	}
	return "", false
}

// Paste inserts a pasted block at the cursor through PasteText, reporting
// whether anything changed (a block that flattens to nothing does not).
func (f *Field) Paste(paste string) (changed bool) {
	// A paste over an armed selection replaces it, like typing does (#2633):
	// the block is spliced into an empty field rather than into the text it
	// was meant to replace.
	text, cur := f.Text, f.Cur
	sel := f.Selected()
	if sel {
		text, cur = "", 0
	}
	out, ncur, inserted := PasteText(text, cur, paste)
	f.sel = false
	if !inserted && !sel {
		return false
	}
	// A paste is always its own undo step — never coalesced into a run of
	// typing around it.
	f.pushUndo()
	f.lastKind, f.lastCur = editNone, -1
	f.Text, f.Cur = out, ncur
	return true
}

// View renders the text with the reverse-video cursor cell. An armed
// select-all (#2633) paints the whole text as selected, so the affordance the
// next keystroke acts on is visible.
func (f Field) View() string {
	if f.Selected() {
		return CursorViewSel(f.Text, f.Cur, 0, f.Len(), SelectionStyle())
	}
	return CursorView(f.Text, f.Cur)
}

// SelectionStyle is how a field paints its armed select-all. Reverse video
// rather than a theme colour: internal/ui knows no theme, and reverse is the
// one "selected" look every terminal renders.
func SelectionStyle() lipgloss.Style { return lipgloss.NewStyle().Reverse(true) }

// ViewSel renders the text like View, with the rune range [selStart, selEnd)
// painted in selStyle — a preselected prefill the cursor still sits inside.
func (f Field) ViewSel(selStart, selEnd int, selStyle lipgloss.Style) string {
	return CursorViewSel(f.Text, f.Cur, selStart, selEnd, selStyle)
}

// Set replaces the text and puts the cursor at its end. The content comes
// from outside the field — a history step, a prefill, a restored draft — so
// it starts a fresh edit history (#2633): ctrl+z undoes what was typed *into*
// this content, never the act of putting it there, which is the host's own
// key (a history walk steps back with its own arrow).
func (f *Field) Set(text string) {
	f.Text = text
	f.Cur = len([]rune(text))
	f.reset()
}

// Clear empties the field and, like Set, drops its edit history: a cleared
// field is a fresh one, and a prompt reopened for the next use must not
// resurrect the previous one's text.
func (f *Field) Clear() {
	f.Text, f.Cur = "", 0
	f.reset()
}

// reset drops the selection and the edit history.
func (f *Field) reset() {
	f.sel, f.undo = false, nil
	f.lastKind, f.lastCur = editNone, -1
}

// Empty reports whether the field holds no text.
func (f Field) Empty() bool { return f.Text == "" }

// Runes is the text as runes — what a host slicing around the caret needs,
// since Cur is a rune index and byte slicing would corrupt multi-byte text.
func (f Field) Runes() []rune { return []rune(f.Text) }

// Len is the text's length in runes, i.e. the cursor's maximum position.
func (f Field) Len() int { return len([]rune(f.Text)) }

// --- Select-all and undo (#2633) ---------------------------------------
//
// Both were missing from every one-line input in the IDE: cmd+a and ctrl+z
// are bound in the *Editor* context, so a field hosted outside one (the
// playground's query line, a filter row) saw them resolve to nothing, and a
// field hosted inside one watched the chord edit the document behind it.
// They live on Field rather than in a host so the answer is the same in all
// ~60 of them.

// fieldUndoDepth caps the per-field undo stack. A one-line input is not a
// document: a handful of steps back covers the "I just wrecked my query"
// case, and the cap keeps a long-lived field (a search prompt that survives
// a session) from growing without bound.
const fieldUndoDepth = 50

// editKind classifies the last applied edit so a run of the same kind
// coalesces into one undo step: typing a word is one ctrl+z, not one per
// rune, which is what makes undo usable in a field at all.
type editKind int

const (
	editNone editKind = iota
	editInsert
	editDelete
)

// fieldState is one undo entry: the text and caret from before an edit.
type fieldState struct {
	text string
	cur  int
}

// SelectAll arms the whole-text selection (cmd+a): the next insertion — a
// typed rune, a paste — replaces the text, and backspace/delete clears it.
// Any other key drops the selection and acts normally. The caret moves to the
// end, so a field that renders through View shows the selection with the
// cursor sitting behind it. It reports whether anything is selected; an empty
// field has nothing to select.
func (f *Field) SelectAll() bool {
	if f.Text == "" {
		f.sel = false
		return false
	}
	f.sel = true
	f.Cur = f.Len()
	f.lastKind, f.lastCur = editNone, -1
	return true
}

// Selected reports whether the whole-text selection is armed — what a host
// rendering the field itself (the playground highlights its query by hand)
// asks to paint it.
func (f Field) Selected() bool { return f.sel && f.Text != "" }

// Deselect drops an armed selection without touching the text.
func (f *Field) Deselect() { f.sel = false }

// Undo restores the text and caret from before the last edit (ctrl+z),
// reporting whether anything changed. Consecutive insertions (or deletions)
// count as one edit; Set and Clear drop the history, because a field that was
// refilled from somewhere else has no earlier state of its own worth
// restoring.
func (f *Field) Undo() (changed bool) {
	if len(f.undo) == 0 {
		return false
	}
	prev := f.undo[len(f.undo)-1]
	f.undo = f.undo[:len(f.undo)-1]
	changed = prev.text != f.Text
	f.Text, f.Cur = prev.text, prev.cur
	f.sel = false
	f.lastKind, f.lastCur = editNone, -1
	return changed
}

// pushUndo stores the current state as the next undo step. The slice is
// copied rather than appended in place: a Field is a value type that hosts
// copy freely, and two copies must not share (and overwrite) one backing
// array.
func (f *Field) pushUndo() {
	next := make([]fieldState, len(f.undo), len(f.undo)+1)
	copy(next, f.undo)
	f.undo = append(next, fieldState{text: f.Text, cur: f.Cur})
	if len(f.undo) > fieldUndoDepth {
		f.undo = f.undo[len(f.undo)-fieldUndoDepth:]
	}
}

// noteEdit records an edit of kind about to be applied, pushing an undo step
// unless it continues the run of the previous one (same kind, starting where
// that one ended). ncur is where the caret lands after the edit.
func (f *Field) noteEdit(kind editKind, ncur int) {
	if kind != f.lastKind || f.Cur != f.lastCur {
		f.pushUndo()
	}
	f.lastKind, f.lastCur = kind, ncur
}
