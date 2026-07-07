package app

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"image/color"

	"ike/internal/host"
	"ike/internal/overlay"
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

// histEntry is one recorded notification in the history ring.
type histEntry struct {
	at   time.Time
	sev  host.Severity
	text string
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
	var ticks []tea.Cmd
	for _, n := range pending {
		m.history = append([]histEntry{{at: time.Now(), sev: n.Severity, text: n.Text}}, m.history...)
		if len(m.history) > historyCap {
			m.history = m.history[:historyCap]
		}
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

// historyView renders the history ring for the floating shell: newest first,
// severity-colored, timestamped.
func (m Model) historyView() string {
	if len(m.history) == 0 {
		return "no notifications yet"
	}
	var b strings.Builder
	for i, e := range m.history {
		if i > 0 {
			b.WriteByte('\n')
		}
		line := e.at.Format("15:04:05") + " ● " + e.text
		b.WriteString(lipgloss.NewStyle().Foreground(m.toastColor(e.sev)).Render(line))
	}
	return b.String()
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

// compositeToasts overlays the visible toast stack bottom-right, directly
// above the status line, newest on top.
func (m Model) compositeToasts(base string) string {
	if len(m.toasts) == 0 || m.width < 8 || m.height < 4 {
		return base
	}
	visible := m.toasts
	if len(visible) > maxVisibleToasts {
		visible = visible[:maxVisibleToasts]
	}
	maxW := m.width - 4
	if maxW > 60 {
		maxW = 60
	}
	for i, t := range visible {
		text := " ● " + t.text + " "
		box := lipgloss.NewStyle().
			MaxWidth(maxW).
			Background(m.pal().Surface).
			Foreground(m.toastColor(t.sev)).
			Render(text)
		w := lipgloss.Width(box)
		x := m.width - w - 1
		y := m.height - 2 - i // row above the status line, stacking upward
		if y < 0 {
			break
		}
		base = overlay.Place(base, box, x, y, m.width, m.height)
	}
	return base
}
