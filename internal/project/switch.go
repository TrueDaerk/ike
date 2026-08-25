package project

import tea "charm.land/bubbletea/v2"

// switch.go starts the switch transaction (Roadmap 0090, #3): validate the
// candidate root off the Update loop and hand the result to the root model as
// a msg. Everything after — the unsaved-changes guard, the re-root sequence,
// recording the open — is routed by internal/app; a validation failure leaves
// the current project untouched.

// SwitchTo returns a tea.Cmd that validates path (expand ~, absolute, exists,
// is-dir, readable) and yields SwitchProjectMsg on success or SwitchFailedMsg
// with the actionable error. The stat runs inside the Cmd, never in Update.
func SwitchTo(path string) tea.Cmd {
	return func() tea.Msg {
		abs, err := Validate(path)
		if err != nil {
			return SwitchFailedMsg{Path: path, Err: err}
		}
		return SwitchProjectMsg{Root: abs}
	}
}

// PeekTo is SwitchTo's peek twin (#2136): the same off-loop validation, but a
// valid root yields PeekProjectMsg so the root model runs the switch as a
// peek. Failures share SwitchFailedMsg — the error handling is identical.
func PeekTo(path string) tea.Cmd {
	return func() tea.Msg {
		abs, err := Validate(path)
		if err != nil {
			return SwitchFailedMsg{Path: path, Err: err}
		}
		return PeekProjectMsg{Root: abs}
	}
}
