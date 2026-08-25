package app

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/ui"
)

// forgenotify.go is the prominent forge event surface (#2086). A toast in the
// bottom-right corner is too easy to miss for something as actionable as a new
// issue appearing on the forge, so each event kind picks its own style
// (forge.notify.<kind>):
//
//   - dialog — a centered, bordered, dismissable dialog over the workspace,
//     following the project's dialog convention (the terminal's dead-process
//     dialog). Several pending events collapse into *one* dialog carrying a
//     count; dialogs never stack.
//   - badge  — a persistent "● 2 new issues" segment in the status line that
//     stays until the events are viewed.
//   - toast  — the ordinary notification toast.
//   - off    — nothing beyond the history.
//
// While the user is typing in an editor or terminal, a dialog would steal the
// keyboard mid-word; the do-not-interrupt guard holds it back and shows the
// badge instead. Whatever the style, every event is recorded in the
// notification history ring, so nothing is lost after a dismissal.

// forgeNotifyStyle is the resolved style of one event kind.
type forgeNotifyStyle int

const (
	// forgeStyleOff records the event in the history only.
	forgeStyleOff forgeNotifyStyle = iota
	// forgeStyleToast raises the ordinary bottom-right toast.
	forgeStyleToast
	// forgeStyleBadge marks the status line unread without interrupting.
	forgeStyleBadge
	// forgeStyleDialog raises the centered dialog (badge while typing).
	forgeStyleDialog
)

// forgeTypingWindow is the do-not-interrupt guard: a key press into an editor
// or terminal within this window counts as "actively typing", and a dialog is
// deferred to the badge. Long enough to cover the pauses inside a sentence,
// short enough that a dialog still arrives while the user reads.
const forgeTypingWindow = 3 * time.Second

// parseForgeStyle maps a config value onto a style; an unrecognised value
// (validation already reports it) reads as the least intrusive one.
func parseForgeStyle(v string) forgeNotifyStyle {
	switch v {
	case "dialog":
		return forgeStyleDialog
	case "badge":
		return forgeStyleBadge
	case "toast":
		return forgeStyleToast
	}
	return forgeStyleOff
}

// forgeStyle resolves the notification style configured for kind.
func (m Model) forgeStyle(kind forge.EventKind) forgeNotifyStyle {
	if v, ok := m.host.Config().Get(kind.ConfigKey()); ok {
		return parseForgeStyle(v)
	}
	// No config view (tests, early startup): the shipped default — only a new
	// issue interrupts.
	if kind == forge.IssueOpened {
		return forgeStyleDialog
	}
	return forgeStyleToast
}

// noteTypingInput records a key press that landed in an editor or terminal, so
// the guard knows the user is mid-interaction. Keys consumed by an overlay do
// not count: the user is driving a dialog, not writing text.
func (m *Model) noteTypingInput() {
	if m.overlayCapturesKeyboard() {
		return
	}
	ws := m.activeWS()
	if ws == nil {
		return
	}
	p := ws.Panes.Get(ws.Panes.Focused())
	if p == nil {
		return
	}
	switch p.Kind() {
	case pane.KindEditor, pane.KindTerminal:
		m.lastInputAt = time.Now()
	}
}

// userTyping reports whether the do-not-interrupt guard is active.
func (m Model) userTyping() bool {
	return !m.lastInputAt.IsZero() && time.Since(m.lastInputAt) < forgeTypingWindow
}

// dialogBlocked reports whether another overlay currently owns the floating
// shell or the keyboard. Stealing it would drop whatever the user has open
// (the help view, a prompt, the settings panel), so the event takes the badge
// and waits — the same "never interrupt" rule as the typing guard.
func (m Model) dialogBlocked() bool {
	return !m.forgeDialogOpen() && (m.shell.IsOpen() || m.overlayCapturesKeyboard())
}

// handleForgeEvents routes one poll round's events (#2085) onto their surface.
// Every event lands in the notification history first, then the configured
// style decides: dialog (queued, collapsing into the single open dialog),
// badge, toast, or nothing.
func (m *Model) handleForgeEvents(msg forge.EventsMsg) tea.Cmd {
	if len(msg.Events) == 0 {
		return nil
	}
	// Events from a project that has since been switched away from are stale.
	if msg.Root != "" && m.projectRootTag() != "" && msg.Root != m.projectRootTag() {
		return nil
	}
	typing := m.userTyping()
	var cmds []tea.Cmd
	for _, e := range msg.Events {
		style := m.forgeStyle(e.Kind)
		if style != forgeStyleToast {
			// The toast route records itself when the host queue is drained;
			// every other style files the entry here, so the ring holds each
			// event exactly once.
			m.recordForgeHistory(e)
		}
		switch style {
		case forgeStyleOff:
		case forgeStyleToast:
			m.host.Notify(host.Info, e.Summary())
		case forgeStyleBadge:
			m.forgeUnread = append(m.forgeUnread, e)
		case forgeStyleDialog:
			if typing || m.dialogBlocked() {
				// Do-not-interrupt: the badge carries it instead (#2086).
				m.forgeUnread = append(m.forgeUnread, e)
				continue
			}
			m.forgeQueue = append(m.forgeQueue, e)
		}
	}
	if len(m.forgeQueue) > 0 && !m.dialogBlocked() {
		m.openForgeDialog()
	}
	return tea.Batch(cmds...)
}

// recordForgeHistory files the event in the notification history ring (#78)
// without toasting it: the ring is the "nothing is lost" backstop for every
// style, including the dialog and the badge.
func (m *Model) recordForgeHistory(e forge.Event) {
	m.history = append([]histEntry{{
		at:   time.Now(),
		sev:  host.Info,
		text: e.Summary(),
		root: m.projectRootTag(),
	}}, m.history...)
	if len(m.history) > historyCap {
		m.history = m.history[:historyCap]
	}
	m.notifUnseen++
}

// openForgeDialog shows (or refreshes) the single event dialog. Opening it is
// itself "viewing" the events, so the unread badge clears — the events behind
// it join the one dialog rather than lingering in two surfaces at once. That
// is also why there is never a second dialog: a further event grows the queue
// (and the heading's count) of the dialog already on screen.
func (m *Model) openForgeDialog() {
	m.forgeQueue = append(m.forgeQueue, m.forgeUnread...)
	m.forgeUnread = nil
	if m.forgeCursor >= len(m.forgeQueue) {
		m.forgeCursor = len(m.forgeQueue) - 1
	}
	if m.forgeCursor < 0 {
		m.forgeCursor = 0
	}
	m.forgeDialog = true
	m.shell.SetContent(ui.ModelContent{Heading: m.forgeDialogTitle(), Body: m.forgeDialogBody})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// forgeDialogOpen reports whether the dialog owns the keyboard.
func (m Model) forgeDialogOpen() bool { return m.forgeDialog && m.shell.IsOpen() }

// forgeDialogTitle names the dialog and carries the count when several events
// queued — one dialog for all of them, never a stack.
func (m Model) forgeDialogTitle() string {
	if len(m.forgeQueue) > 1 {
		return "Forge events (" + strconv.Itoa(len(m.forgeQueue)) + ")"
	}
	return "Forge event"
}

// forgeDialogBody renders the selected event — number, title, author, labels —
// plus the queue rows and the key legend.
func (m Model) forgeDialogBody() string {
	if len(m.forgeQueue) == 0 {
		return "no pending forge events"
	}
	cur := m.forgeQueue[m.forgeCursor]
	var b strings.Builder
	b.WriteString(cur.Kind.Label() + " #" + strconv.Itoa(cur.Number) + "\n")
	b.WriteString(cur.Title + "\n")
	if cur.Author != "" {
		b.WriteString("by @" + cur.Author + "\n")
	}
	if len(cur.Labels) > 0 {
		names := make([]string, 0, len(cur.Labels))
		for _, l := range cur.Labels {
			names = append(names, l.Name)
		}
		b.WriteString("labels: " + strings.Join(names, ", ") + "\n")
	}
	if len(m.forgeQueue) > 1 {
		b.WriteString("\n")
		for i, e := range m.forgeQueue {
			marker := "  "
			if i == m.forgeCursor {
				marker = "▸ "
			}
			b.WriteString(marker + e.Summary() + "\n")
		}
	}
	b.WriteString("\n")
	legend := "  [enter] open in issues   [d/esc] dismiss"
	if len(m.forgeQueue) > 1 {
		legend = "  [j/k] move   [enter] open in issues   [d/esc] dismiss   [a] dismiss all"
	}
	b.WriteString(legend)
	return b.String()
}

// updateForgeDialog consumes every key while the dialog is open: enter opens
// the selected item in the issues window, d/esc dismisses it (the dialog stays
// for the rest of the queue), a dismisses all, j/k walk the queue.
func (m Model) updateForgeDialog(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if len(m.forgeQueue) == 0 {
		return m.closeForgeDialog(), nil
	}
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if m.pickerNav(msg.String(), &m.forgeCursor, len(m.forgeQueue), nil) {
		m.refreshForgeDialog()
		return m, nil
	}
	switch msg.String() {
	case "enter", "o":
		e := m.forgeQueue[m.forgeCursor]
		m.dismissForgeEvent()
		m.forgeDialog = false
		m.shell.Close()
		return m, m.openForgeEvent(e)
	case "d", "esc":
		m.dismissForgeEvent()
		if len(m.forgeQueue) == 0 {
			return m.closeForgeDialog(), nil
		}
		m.refreshForgeDialog()
	case "a":
		m.forgeQueue = nil
		return m.closeForgeDialog(), nil
	}
	return m, nil
}

// dismissForgeEvent drops the selected event from the queue, keeping the
// cursor on the next one.
func (m *Model) dismissForgeEvent() {
	if len(m.forgeQueue) == 0 {
		return
	}
	m.forgeQueue = append(m.forgeQueue[:m.forgeCursor], m.forgeQueue[m.forgeCursor+1:]...)
	if m.forgeCursor >= len(m.forgeQueue) {
		m.forgeCursor = len(m.forgeQueue) - 1
	}
	if m.forgeCursor < 0 {
		m.forgeCursor = 0
	}
}

// refreshForgeDialog re-binds the shell content so the heading's count and the
// body follow the queue after a dismissal or a cursor move.
func (m *Model) refreshForgeDialog() {
	m.shell.SetContent(ui.ModelContent{Heading: m.forgeDialogTitle(), Body: m.forgeDialogBody})
	m.shell.SetSize(m.width, m.height)
}

// closeForgeDialog dismisses the dialog. The events it showed stay in the
// history ring; the badge stays clear — they have been seen.
func (m Model) closeForgeDialog() tea.Model {
	m.forgeDialog = false
	m.shell.Close()
	return m
}

// openForgeEvent lands the user on the event's item: the issues tool window,
// jumped straight to that issue's detail view. A pull request event has no
// pane of its own, so it opens in the browser instead.
func (m *Model) openForgeEvent(e forge.Event) tea.Cmd {
	m.clearForgeUnread()
	if !e.Kind.IsIssue() {
		if e.URL == "" {
			return nil
		}
		return m.openIssueURL(e.URL)
	}
	cmd := m.showIssuesPanel()
	if p := m.issuesPanel(); p != nil && p.Reveal(e.Number) {
		// The reveal opened the issue's detail: fetch its timeline (#2084).
		return tea.Batch(cmd, p.PendingTimelineCmd())
	}
	// The listing does not carry the issue yet (the pane just opened, or its
	// snapshot predates the event): reveal as soon as the fetch lands.
	m.forgeReveal = e.Number
	return cmd
}

// showIssuesPanel makes sure the issues tool window exists and is focused,
// returning the first fetch when it had to be opened.
func (m *Model) showIssuesPanel() tea.Cmd {
	if !m.activeWS().Panes.Has(pane.IssuesKey) {
		m.issuesReturnFocus = m.activeWS().Panes.Focused()
		return m.openIssuesPanel()
	}
	if m.activeWS().Panes.Focused() != pane.IssuesKey {
		m.issuesReturnFocus = m.activeWS().Panes.Focused()
		m.setFocus(pane.IssuesKey)
	}
	return nil
}

// applyForgeReveal runs a pending reveal after a fetch landed in the pane,
// returning the revealed issue's timeline fetch (#2084) when it succeeded.
func (m *Model) applyForgeReveal() tea.Cmd {
	if m.forgeReveal == 0 {
		return nil
	}
	p := m.issuesPanel()
	if p == nil {
		m.forgeReveal = 0
		return nil
	}
	if p.Reveal(m.forgeReveal) {
		m.forgeReveal = 0
		return p.PendingTimelineCmd()
	}
	return nil
}

// clearForgeUnread drops the badge: the events behind it have been viewed.
func (m *Model) clearForgeUnread() { m.forgeUnread = nil }

// forgeBadgeSegment is the persistent unread badge in the status line: the
// events held back by the typing guard, plus every badge-style event, until
// they are viewed. It stays put — unlike a toast it does not expire.
func (m Model) forgeBadgeSegment() string {
	n := len(m.forgeUnread)
	if n == 0 {
		return ""
	}
	issues := 0
	for _, e := range m.forgeUnread {
		if e.Kind == forge.IssueOpened {
			issues++
		}
	}
	switch {
	case issues == n && n == 1:
		return "● 1 new issue"
	case issues == n:
		return "● " + strconv.Itoa(n) + " new issues"
	case n == 1:
		return "● 1 forge event"
	}
	return "● " + strconv.Itoa(n) + " forge events"
}
