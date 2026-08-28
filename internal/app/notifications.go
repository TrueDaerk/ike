package app

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"image/color"

	"ike/internal/host"
	"ike/internal/overlay"
	"ike/internal/ui"
)

// notifications.go renders host.Notify toasts (Roadmap 0130): short event
// messages stacked bottom-right above the status line. Info/Warn toasts expire
// after notifications.timeout_seconds; Error toasts persist until dismissed
// with Esc. The permanent status line is never covered.

// maxVisibleToasts bounds the rendered stack; older toasts wait underneath and
// surface as newer ones expire.
const maxVisibleToasts = 3

// defaultToastTimeout applies when notifications.timeout_seconds is unset.
const defaultToastTimeout = 4 * time.Second

// historyCap bounds the notification history ring (#78): the newest 100 stay.
const historyCap = 100

// toast is one active notification.
type toast struct {
	id   int
	sev  host.Severity
	text string
}

// histEntry is one recorded notification in the history ring. root is the
// project root the notification was emitted in (#1514): the ring survives a
// seamless project switch, so the view labels entries from other projects.
type histEntry struct {
	at   time.Time
	sev  host.Severity
	text string
	root string
}

// toastExpireMsg removes the identified toast when its timeout elapses.
type toastExpireMsg struct{ id int }

// drainNotifications records host-queued notifications in the history ring,
// moves those at or above notifications.min_severity onto the toast stack
// (newest first) and returns the batched expiry ticks for non-error entries.
func (m *Model) drainNotifications() tea.Cmd {
	pending := m.host.DrainNotifications()
	if len(pending) == 0 {
		return nil
	}
	timeout := m.toastTimeout()
	floor := m.minSeverity()
	root := m.projectRootTag()
	var ticks []tea.Cmd
	for _, n := range pending {
		m.history = append([]histEntry{{at: time.Now(), sev: n.Severity, text: n.Text, root: root}}, m.history...)
		if len(m.history) > historyCap {
			m.history = m.history[:historyCap]
		}
		m.notifUnseen++ // status line counter (#101), reset by notifications.history
		if n.Severity < floor {
			continue // below the toast floor: history only
		}
		m.toastSeq++
		m.toasts = append([]toast{{id: m.toastSeq, sev: n.Severity, text: n.Text}}, m.toasts...)
		if n.Severity != host.Error {
			id := m.toastSeq
			ticks = append(ticks, tea.Tick(timeout, func(time.Time) tea.Msg {
				return toastExpireMsg{id: id}
			}))
		}
	}
	return tea.Batch(ticks...)
}

// expireToast removes the toast with the given id (no-op if already gone).
func (m *Model) expireToast(id int) {
	for i, t := range m.toasts {
		if t.id == id {
			m.toasts = append(m.toasts[:i], m.toasts[i+1:]...)
			return
		}
	}
}

// dismissErrorToasts drops every persistent error toast; Esc calls this and
// then keeps its normal meaning (pass-through, never swallowed).
func (m *Model) dismissErrorToasts() {
	kept := m.toasts[:0]
	for _, t := range m.toasts {
		if t.sev != host.Error {
			kept = append(kept, t)
		}
	}
	m.toasts = kept
}

// toastTimeout reads notifications.timeout_seconds (default 4s).
func (m Model) toastTimeout() time.Duration {
	if v, ok := m.host.Config().Get("notifications.timeout_seconds"); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return defaultToastTimeout
}

// minSeverity reads notifications.min_severity, the toast floor (default info:
// everything toasts). Below-floor notifications land in the history only.
func (m Model) minSeverity() host.Severity {
	if v, ok := m.host.Config().Get("notifications.min_severity"); ok {
		switch v {
		case "warn":
			return host.Warn
		case "error":
			return host.Error
		}
	}
	return host.Info
}

// notifCenterTitle is the floating shell heading of the notification center.
// It doubles as the identity check that routes the center's own keys (#2152),
// so clear-all never fires over a different shell content.
const notifCenterTitle = "NOTIFICATIONS"

// notifCenterLegend names the center's own keys, the way the other shell
// dialogs footer theirs.
const notifCenterLegend = "[c] clear all   [esc] close"

// ageColumn is the width the relative-age column is padded to, wide enough for
// every ui.ShortAge form ("now", "59m", "23h", "13d", "52w").
const ageColumn = 3

// historyView renders the history ring for the notification center (#2152):
// newest first, severity glyph, relative age ("now", "5m", "3h"), and the
// clear-all/close legend. An entry emitted in another project (#1514: the ring
// survives project switches) carries a dimmed [project] label so its context is
// clear; entries from the current project stay unlabeled.
func (m Model) historyView() string {
	dim := lipgloss.NewStyle().Foreground(m.pal().InlayHint)
	if len(m.history) == 0 {
		return "no notifications yet\n\n" + dim.Render(notifCenterLegend)
	}
	cur := m.projectRootTag()
	now := time.Now()
	var b strings.Builder
	for _, e := range m.history {
		age := ui.ShortAge(e.at, now)
		for lipgloss.Width(age) < ageColumn {
			age = " " + age
		}
		line := age + " " + toastIcon(e.sev) + " " + e.text
		b.WriteString(lipgloss.NewStyle().Foreground(m.toastColor(e.sev)).Render(line))
		if e.root != "" && e.root != cur {
			b.WriteString(dim.Render("  [" + filepath.Base(e.root) + "]"))
		}
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString(dim.Render(notifCenterLegend))
	return b.String()
}

// openNotifCenter shows the history ring in the floating shell and marks
// everything seen: the status line counter resets (#101). The body is a method
// value, so ages stay live while the center is open.
func (m *Model) openNotifCenter() {
	m.notifUnseen = 0
	m.bindNotifCenter()
	m.shell.Open()
}

// bindNotifCenter (re-)binds the center's content to the current model copy —
// the body is a method value, so a cleared ring only renders after a re-bind.
func (m *Model) bindNotifCenter() {
	m.shell.SetContent(ui.ModelContent{Heading: notifCenterTitle, Body: m.historyView})
	m.shell.SetSize(m.width, m.height)
}

// notifCenterOpen reports whether the notification center owns the shell — and
// with it the keys the center handles itself.
func (m Model) notifCenterOpen() bool {
	if !m.shell.IsOpen() {
		return false
	}
	c, ok := m.shell.Content().(ui.ModelContent)
	return ok && c.Heading == notifCenterTitle
}

// updateNotifCenter handles the center's own keys while it is open. Only "c"
// (clear all) is consumed — reported by the handled flag; every other key falls
// through to the shell, so scrolling and Esc keep their meaning.
func (m Model) updateNotifCenter(msg tea.KeyPressMsg) (Model, bool) {
	if msg.String() != "c" {
		return m, false
	}
	m.history = nil
	m.notifUnseen = 0
	m.bindNotifCenter()
	return m, true
}

// projectRootTag is the absolute project root recorded on history entries
// (#1514) and compared against by the history view. It reads the active
// workspace's root (captured at build time), so it stays stable regardless
// of the cwd cache state.
func (m Model) projectRootTag() string {
	if ws := m.activeWS(); ws != nil {
		return ws.Root
	}
	return ""
}

// toastColor maps a severity to its palette color.
func (m Model) toastColor(sev host.Severity) color.Color {
	switch sev {
	case host.Error:
		return m.pal().Error
	case host.Warn:
		return m.pal().Warning
	}
	return m.pal().Info
}

// toastIcon maps a severity to its leading glyph (single-width, text
// presentation so it never reflows the box).
func toastIcon(sev host.Severity) string {
	switch sev {
	case host.Error:
		return "✖"
	case host.Warn:
		return "▲"
	}
	return "●"
}

// compositeToasts overlays the visible toast stack bottom-right, directly
// above the status line, newest on top. Each toast is a rounded card whose
// border and icon carry the severity color; the text stays in the theme
// foreground for legibility on the raised panel surface.
func (m Model) compositeToasts(base string) string {
	if len(m.toasts) == 0 || m.width < 8 || m.height < 4 {
		return base
	}
	visible := m.toasts
	if len(visible) > maxVisibleToasts {
		visible = visible[:maxVisibleToasts]
	}
	// Leave room for the border (2), padding (2) and icon+gap (2).
	maxW := m.width - 6
	if maxW > 54 {
		maxW = 54
	}
	if maxW < 1 {
		return base
	}
	pal := m.pal()
	y := m.height - 2 // bottom content row, above the status line
	for _, t := range visible {
		sc := m.toastColor(t.sev)
		icon := lipgloss.NewStyle().Foreground(sc).Bold(true).Render(toastIcon(t.sev))
		// MaxWidth truncates; long texts (e.g. the capability warnings, #720)
		// must wrap instead, so Width kicks in when the line would overflow.
		msgStyle := lipgloss.NewStyle().Foreground(pal.Foreground)
		if lipgloss.Width(t.text) > maxW {
			msgStyle = msgStyle.Width(maxW)
		}
		msg := msgStyle.Render(t.text)
		body := lipgloss.JoinHorizontal(lipgloss.Top, icon, " ", msg)
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(sc).
			BorderBackground(pal.Panel).
			Background(pal.Panel).
			Padding(0, 1).
			Render(body)
		h := lipgloss.Height(box)
		top := y - h + 1
		if top < 0 {
			break
		}
		w := lipgloss.Width(box)
		x := m.width - w - 1
		base = overlay.Place(base, box, x, top, m.width, m.height)
		y = top - 1 // next card one row higher, leaving a gap
	}
	return base
}
