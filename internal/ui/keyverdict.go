package ui

// keyverdict.go is the tool panes' half of the deferred "unbound" verdict
// (#2889). The keymap layer sees a chord before the focused pane does, so a
// chord the pane answers itself — alt+backspace in a search prompt, a word
// jump in a filter row — looks unbound from up there. Since #2303 the host
// holds the verdict back for an editor and asks editor.HandledLastKey after
// dispatch; KeyVerdict gives a tool pane the same answer to give.
//
// A pane embeds one and calls BeginKey when a key press arrives; the key then
// counts as unanswered until a path claims it — HitKey on a branch that acted,
// KeyAnswered with the handled result of Field.Key / LineSearch.Key /
// SpeedSearch.Key / hiertree's Key. A branch that forgets to claim its key
// therefore keeps the pre-#2889 behaviour (the chord is logged unbound)
// instead of hiding a genuinely missing keybind. MissKey takes a claim back,
// for a switch whose default branch is the only path that does nothing.
//
// The embedded HandledLastKey satisfies the pane layer's KeyReporter
// capability without further code.

// KeyVerdict records whether the last key press a pane was given did anything.
type KeyVerdict struct {
	handled bool
}

// BeginKey starts the verdict for a new key press: unanswered until claimed.
func (v *KeyVerdict) BeginKey() { v.handled = false }

// HitKey claims the current key press: the pane acted on it.
func (v *KeyVerdict) HitKey() { v.handled = true }

// MissKey marks the current key press as unanswered after all.
func (v *KeyVerdict) MissKey() { v.handled = false }

// KeyAnswered folds a building block's handled result into the verdict — a
// true claims the key — and returns it unchanged, so the call can wrap the
// block's own Key result inline.
func (v *KeyVerdict) KeyAnswered(handled bool) bool {
	if handled {
		v.handled = true
	}
	return handled
}

// HandledLastKey reports whether the last key press did anything in the pane.
func (v KeyVerdict) HandledLastKey() bool { return v.handled }
