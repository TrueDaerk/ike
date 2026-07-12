package vcs

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// MessageDraft is the shared in-progress commit message (Roadmap 0330, #483):
// the modal commit dialog and the VCS tool window edit the same draft, so the
// two entry points never diverge. It survives closing either UI and clears
// only on a successful commit.
type MessageDraft struct {
	Text string
	Pos  int // rune cursor
}

// Clear drops the draft (after a successful commit).
func (d *MessageDraft) Clear() { d.Text, d.Pos = "", 0 }

// Edit applies one key to the draft — insert, newline, backspace, cursor
// moves — and reports whether the key was consumed.
func (d *MessageDraft) Edit(msg tea.KeyPressMsg) bool {
	runes := []rune(d.Text)
	switch msg.String() {
	case "enter":
		d.Text = string(runes[:d.Pos]) + "\n" + string(runes[d.Pos:])
		d.Pos++
	case "backspace":
		if d.Pos > 0 {
			d.Text = string(runes[:d.Pos-1]) + string(runes[d.Pos:])
			d.Pos--
		}
	case "left":
		if d.Pos > 0 {
			d.Pos--
		}
	case "right":
		if d.Pos < len(runes) {
			d.Pos++
		}
	case "home":
		d.Pos = 0
	case "end":
		d.Pos = len(runes)
	default:
		t := msg.Text
		if t == "" || strings.ContainsAny(t, "\x00") {
			return false
		}
		d.Text = string(runes[:d.Pos]) + t + string(runes[d.Pos:])
		d.Pos += len([]rune(t))
	}
	return true
}
