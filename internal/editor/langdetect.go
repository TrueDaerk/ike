package editor

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/lang"
)

// langdetect.go is the paste-time trigger of the content sniff (#2037), the
// automatic half of "Treat Buffer as …" (#2033).
//
// The manual override answers "this buffer is Markdown" once the user says
// so. The case that actually dominates needs no saying: a fresh, file-less
// buffer, content pasted into it — a JSON response, a CSV export, notes in
// Markdown, a curl command — where the type is plain from the first line.
// Every paste that fills such a buffer therefore runs lang.DetectContent over
// the result and, on a confident verdict, installs it as the buffer language.
//
// Three properties keep the automatism from becoming a nuisance:
//
//   - It fires only into a blank slate: no file (a path classifies), no
//     override already set (a decision — the user's or an earlier detect —
//     is never overwritten), and nothing but whitespace in the buffer before
//     the paste. A paste into existing content never retypes it.
//   - It is silent about failure and quiet about success: an unrecognized
//     paste says nothing at all, a recognized one emits one toast naming the
//     type. No modal, no confirmation, nothing that interrupts the paste.
//   - It is one keystroke to undo, in the sense that matters: the language is
//     not part of the document, and the #2033 picker (alt+enter → "Treat
//     Buffer as …", or the status-line segment) changes or clears it without
//     touching the text.
//
// The detection itself lives in lang.DetectContent — pure, table-tested, and
// deliberately conservative: no verdict is the normal outcome.

// detectCandidate reports whether this buffer is one an incoming paste may
// classify. Callers evaluate it *before* the paste (the emptiness test is
// about the state the paste lands in) and pass the answer to detectPastedLang.
func (m *Model) detectCandidate() bool {
	if m.path != "" || m.langOverride != "" {
		return false
	}
	for _, l := range m.buf.Lines() {
		if strings.TrimSpace(l) != "" {
			return false
		}
	}
	return true
}

// detectPastedLang classifies the buffer after a paste that landed in an
// empty, file-less one and installs the verdict. cand is what detectCandidate
// answered before the paste; false makes this a no-op, which is what lets
// nested paste paths (clipboardPaste → paste, pasteText → pasteIntoInsert)
// each wrap themselves without detecting twice.
//
// The feedback is parked in detectSignal rather than returned: the vim paste
// paths run inside key handlers that hand no command back. Update drains it
// through maybeReparse, which every buffer change passes.
func (m *Model) detectPastedLang(cand bool) {
	if !cand || m.path != "" || m.langOverride != "" {
		return
	}
	id := lang.DetectContent(strings.Join(m.buf.Lines(), "\n"))
	if id == "" {
		return
	}
	if _, ok := m.SetLangOverride(id); !ok {
		return
	}
	// The parse command SetLangOverride returns is dropped on purpose: the
	// paste changed the document, so maybeReparse schedules one anyway —
	// and it does so after this ran, with the new language in place.
	m.detectSignal = "buffer language: detected " + id + " — alt+enter to change"
}

// takeDetectSignal drains the pending auto-detect toast into a command, the
// way takeClipboardSignal drains a failed clipboard write. Draining is
// destructive: one detection produces one toast.
func (m *Model) takeDetectSignal() tea.Cmd {
	if m.detectSignal == "" {
		return nil
	}
	text := m.detectSignal
	m.detectSignal = ""
	return notice(text)
}
