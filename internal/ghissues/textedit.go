package ghissues

// textedit.go is the pane's half of "edit my own issue texts" (#2087). The pane
// itself never talks to a forge and never opens a buffer: it decides *what*
// may be edited — the capability + ownership gate — and emits one
// EditTextRequestMsg naming the target and its current text. The app answers
// by opening a markdown buffer bound to that target (internal/app/forgeedit.go).
//
// Not to be confused with mutations.go's label/assignee *editors*: those change
// an issue's metadata through the forge's mutation API, this changes prose.
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
// Without a successful capability probe (#2088's one-shot repository metadata
// fetch, which carries the login too) nothing is offered: an action that fails
// on push would be worse than an absent one.

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

// textTarget is one row of the edit picker: the forge text, the text it
// currently holds, and the label the overlay lists it under.
type textTarget struct {
	target forge.TextTarget
	base   string
	label  string
}

// textTargets lists what the open detail offers to edit, in reading order:
// the issue body first, then the own comments of the loaded timeline in
// timeline order. It is empty when nothing is editable — the action then
// never appears — and always empty before the capability probe answered: a
// permission the pane could not read is never guessed at.
func (m *Model) textTargets() []textTarget {
	is := m.Selected()
	if is == nil || !m.detail || m.tab != TabIssues || !m.capsOK {
		return nil
	}
	var out []textTarget
	if m.canEditBody(is) {
		out = append(out, textTarget{
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
		out = append(out, textTarget{
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

// editEntry is one row of the unified edit picker (#2114): what it is called
// there and what enter runs. The picker is the single meaning of 'e' across
// the issue views — metadata (labels, assignees) and prose (body, own
// comments, a new comment) behind one key instead of the pre-#2114
// e/u/E/n islands.
type editEntry struct {
	label string
	run   func(*Model) tea.Cmd
}

// editEntries lists what 'e' can edit right now, in reading order: the
// metadata pickers first (they exist wherever a forge backend is bound — the
// capability gate explains itself when it is closed, #2088), then the texts
// that are the user's own (#2087). Composing a *new* comment is not an edit
// and keeps its own key ('n'). Empty on the PR views and on an empty list.
func (m *Model) editEntries() []editEntry {
	if m.tab != TabIssues || m.Selected() == nil {
		return nil
	}
	var out []editEntry
	if m.mutate != nil {
		out = append(out,
			editEntry{label: "Labels…", run: (*Model).openLabelEditor},
			editEntry{label: "Assignees…", run: (*Model).openAssigneeEditor})
	}
	for _, t := range m.textTargets() {
		t := t
		out = append(out, editEntry{label: t.label,
			run: func(m *Model) tea.Cmd { return m.requestTextEdit(t) }})
	}
	return out
}

// commentAction is the detail view's direct 'n' (#2087), appended to the
// action table only when it applies. Unlike the mutation actions (#2088),
// which stay listed and explain a closed permission gate, it is absent: the
// login simply has not resolved.
func (m *Model) commentAction() []action {
	if !m.canComment() {
		return nil
	}
	return []action{act("n", "new comment", "Write a new comment", (*Model).startComment)}
}

// editAction is the 'e' entry of the action table, absent while nothing is
// editable (no forge backend, an empty list, the PR views).
func (m *Model) editAction() []action {
	if len(m.editEntries()) == 0 {
		return nil
	}
	return []action{act("e", "edit", "Edit… (labels / assignees / texts)", (*Model).startEdit)}
}

// startEdit is 'e' on the issue views (#2114): with a single editable target
// it runs straight away, with several it raises the unified edit picker.
// Choosing from a list of one would be a keystroke that never carries
// information.
func (m *Model) startEdit() tea.Cmd {
	entries := m.editEntries()
	switch len(entries) {
	case 0:
		return nil
	case 1:
		return entries[0].run(m)
	}
	m.ov, m.ovCursor, m.ovTop = ovEdit, 0, 0
	m.clampOverlay()
	return nil
}

// startComment is the detail view's 'n': compose a new comment on the open
// issue. The buffer starts empty — there is no text to prefill — and the
// stale-base check does not apply to a text that does not exist yet.
func (m *Model) startComment() tea.Cmd {
	is := m.Selected()
	if is == nil || !m.canComment() {
		return nil
	}
	return m.requestTextEdit(textTarget{
		target: forge.TextTarget{Kind: forge.TextNewComment, Issue: is.Number},
	})
}

// requestTextEdit emits the app's open-buffer request for one target.
func (m *Model) requestTextEdit(t textTarget) tea.Cmd {
	title := ""
	if is := m.Selected(); is != nil {
		title = is.Title
	}
	msg := EditTextRequestMsg{Target: t.target, Base: t.base, Title: title}
	return func() tea.Msg { return msg }
}

// editPickerKey handles the unified edit picker overlay: enter runs the
// selected entry (closing first, so an entry that opens another modal wins),
// esc closes without touching anything.
func (m *Model) editPickerKey(key string) tea.Cmd {
	switch key {
	case "enter":
		entries := m.editEntries()
		if m.ovCursor < 0 || m.ovCursor >= len(entries) {
			m.closeOverlay()
			return nil
		}
		run := entries[m.ovCursor].run
		m.closeOverlay()
		return run(m)
	case "esc", "q":
		m.closeOverlay()
	}
	return nil
}

// EditEntryLabels lists the edit picker's rows (tests, and the proof that the
// gate and the overlay read the same table).
func (m *Model) EditEntryLabels() []string {
	entries := m.editEntries()
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.label)
	}
	return out
}

// EditPickerOpen reports whether the unified edit picker owns the keyboard
// (tests).
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
