package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// guardprompt.go holds the shared option-line rendering of the modal guard
// prompts (#1356). Every guard answers on its letter key plus esc; enter is an
// additional alias for the prompt's primary option — the first one listed, i.e.
// "save all, then …" whenever dirty buffers are involved and the plain confirm
// otherwise. The primary line advertises the alias so the body stays the only
// documentation the prompt needs.

// guardKeyWidth is the padded width of an option label, wide enough for the
// longest one ("[s/enter]") so the descriptions stay aligned.
const guardKeyWidth = 10

// guardLine renders one option line of a guard prompt body, terminated by a
// newline. A primary line lists enter alongside its letter key.
func guardLine(key, text string, primary bool) string {
	label := "[" + key + "]"
	if primary {
		label = "[" + key + "/enter]"
	}
	if pad := guardKeyWidth - len(label); pad > 0 {
		label += strings.Repeat(" ", pad)
	}
	return "  " + label + text + "\n"
}

// guardAnswer resolves a key press into the guard's answer key: enter stands
// in for the prompt's primary option (#1356), every other key answers for
// itself. The letter shortcuts keep working unchanged.
func guardAnswer(msg tea.KeyPressMsg, primary string) string {
	if s := msg.String(); s != "enter" {
		return s
	}
	return primary
}

// guardCancel renders the trailing esc line of a guard prompt body (no
// newline — it is always last).
func guardCancel(text string) string {
	return "  " + "[esc]" + strings.Repeat(" ", guardKeyWidth-len("[esc]")) + text
}
