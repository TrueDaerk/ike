package ghissues

// edit.go is the pane's half of "edit my own issue texts" (#2087). The pane
// itself never talks to a forge and never opens a buffer: it decides *what*
// may be edited — the capability + ownership gate — and emits one
// EditTextRequestMsg naming the target and its current text. The app answers
// by opening a markdown buffer bound to that target (internal/app/forgeedit.go).
//
// Which texts are offered:
//
//   - the issue body, when the authenticated user opened the issue, or has
//     write access to the repository;
//   - each *own* comment of the loaded timeline (the TimelineEntry.Own flag
//     #2084 already carries);
//   - a new comment, which needs no more than a resolved login — anyone who
//     can authenticate against the forge may comment.
//
// Without a successful capability probe nothing is offered: an action that
// fails on push would be worse than an absent one.

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
)

// EditTextRequestMsg asks the app to open a markdown edit buffer for one
// forge text (#2087). Base is the text as the pane currently knows it: the
// buffer's prefill *and* the reference the stale-base check compares the
// server state against before overwriting. Title names the issue, for the
// buffer's toast and the conflict dialog.
type EditTextRequestMsg struct {
	Target forge.TextTarget
	Base   string
	Title  string
}

// SetCapabilities applies one finished capability probe (#2087). A failed
// probe leaves the pane with no capabilities at all, which is exactly the
// state that hides every edit action.
func (m *Model) SetCapabilities(msg forge.CapabilitiesMsg) {
	if msg.Err != nil {
		m.caps, m.capsOK = forge.Capabilities{}, false
		return
	}
	m.caps, m.capsOK = msg.Caps, true
}

// Capabilities returns the probed permissions and whether a probe succeeded
// (tests).
func (m *Model) Capabilities() (forge.Capabilities, bool) { return m.caps, m.capsOK }

// canComment reports whether the "new comment" action applies: a resolved
// login is enough — commenting needs no repository permission.
func (m *Model) canComment() bool { return m.capsOK && m.caps.Login != "" }

// canEditBody reports whether the open issue's body is the user's to edit:
// their own issue, or any issue in a repository they can write to.
func (m *Model) canEditBody(is *forge.Issue) bool {
	if is == nil || !m.capsOK {
		return false
	}
	if m.caps.Push {
		return true
	}
	return m.caps.Login != "" && is.Author == m.caps.Login
}

// editTarget is one row of the edit picker: the forge text, the text it
// currently holds, and the label the overlay lists it under.
type editTarget struct {
	target forge.TextTarget
	base   string
	label  string
}

// editTargets lists what the open detail offers to edit, in reading order:
// the issue body first, then the own comments of the loaded timeline in
// timeline order. It is empty when nothing is editable — the action then
// never appears — and always empty before the capability probe answered: a
// permission the pane could not read is never guessed at.
func (m *Model) editTargets() []editTarget {
	is := m.Selected()
	if is == nil || !m.detail || m.tab != TabIssues || !m.capsOK {
		return nil
	}
	var out []editTarget
	if m.canEditBody(is) {
		out = append(out, editTarget{
			target: forge.TextTarget{Kind: forge.TextIssueBody, Issue: is.Number},
			base:   is.Body,
			label:  "Issue body",
		})
	}
	if m.tlFor != is.Number {
		return out
	}
	for _, e := range m.tl {
		if e.Kind != forge.TimelineComment || !e.Own || e.ID == "" {
			continue
		}
		out = append(out, editTarget{
			target: forge.TextTarget{Kind: forge.TextComment, Issue: is.Number, ID: e.ID},
			base:   e.Body,
			label:  "Your comment — " + summarize(e.Body),
		})
	}
	return out
}

// summarizeLen bounds the picker's comment preview: enough to tell two
// comments apart, short enough to keep the overlay narrow.
const summarizeLen = 48

// summarize reduces a comment body to one preview line for the picker: its
// first non-empty line, whitespace-collapsed and capped.
func summarize(body string) string {
	line := ""
	for _, l := range strings.Split(body, "\n") {
		if s := strings.TrimSpace(l); s != "" {
			line = s
			break
		}
	}
	if line == "" {
		return "(empty)"
	}
	line = strings.Join(strings.Fields(line), " ")
	if r := []rune(line); len(r) > summarizeLen {
		return string(r[:summarizeLen-1]) + "…"
	}
	return line
}

// canEditAnything reports whether the edit action ('e') applies at all.
func (m *Model) canEditAnything() bool { return len(m.editTargets()) > 0 }

// startEdit is the detail view's 'e': with a single editable text it opens
// straight away, with several it raises the picker. Choosing from a list of
// one would be a keystroke that never carries information.
func (m *Model) startEdit() tea.Cmd {
	targets := m.editTargets()
	switch len(targets) {
	case 0:
		return nil
	case 1:
		return m.requestEdit(targets[0])
	}
	m.ov, m.ovCursor, m.ovTop = ovEdit, 0, 0
	m.clampOverlay()
	return nil
}

// startComment is the detail view's 'c': compose a new comment on the open
// issue. The buffer starts empty — there is no text to prefill — and the
// stale-base check does not apply to a text that does not exist yet.
func (m *Model) startComment() tea.Cmd {
	is := m.Selected()
	if is == nil || !m.canComment() {
		return nil
	}
	return m.requestEdit(editTarget{
		target: forge.TextTarget{Kind: forge.TextNewComment, Issue: is.Number},
	})
}

// requestEdit emits the app's open-buffer request for one target.
func (m *Model) requestEdit(t editTarget) tea.Cmd {
	title := ""
	if is := m.Selected(); is != nil {
		title = is.Title
	}
	msg := EditTextRequestMsg{Target: t.target, Base: t.base, Title: title}
	return func() tea.Msg { return msg }
}

// editPickerKey handles the edit picker overlay: enter opens the selected
// text, esc closes without opening anything.
func (m *Model) editPickerKey(key string) tea.Cmd {
	switch key {
	case "enter":
		targets := m.editTargets()
		if m.ovCursor < 0 || m.ovCursor >= len(targets) {
			m.closeOverlay()
			return nil
		}
		t := targets[m.ovCursor]
		m.closeOverlay()
		return m.requestEdit(t)
	case "esc", "q", "e":
		m.closeOverlay()
	}
	return nil
}

// EditTargetLabels lists the picker's rows (tests, and the proof that the
// gate and the overlay read the same table).
func (m *Model) EditTargetLabels() []string {
	targets := m.editTargets()
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		out = append(out, t.label)
	}
	return out
}

// EditPickerOpen reports whether the edit picker owns the keyboard (tests).
func (m *Model) EditPickerOpen() bool { return m.ov == ovEdit }

// RefreshAfterSave re-fetches what a successful push changed (#2087): the
// listing — an edited body lives in the issue listing, not in the timeline —
// and, when the pushed text belongs to the issue still on screen, that
// issue's timeline, so a new or edited comment appears where it was written.
func (m *Model) RefreshAfterSave(issue int) tea.Cmd {
	if is := m.Selected(); is == nil || is.Number != issue {
		return m.startRefresh()
	}
	return tea.Batch(m.startRefresh(), m.refetchTimeline())
}
