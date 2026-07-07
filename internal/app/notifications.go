package app

import (
	"strconv"
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

// toast is one active notification.
type toast struct {
	id   int
	sev  host.Severity
	text string
}

// toastExpireMsg removes the identified toast when its timeout elapses.
type toastExpireMsg struct{ id int }

// drainNotifications moves host-queued notifications onto the toast stack
// (newest first) and returns the batched expiry ticks for non-error entries.
func (m *Model) drainNotifications() tea.Cmd {
	pending := m.host.DrainNotifications()
	if len(pending) == 0 {
		return nil
	}
	timeout := m.toastTimeout()
	var ticks []tea.Cmd
	for _, n := range pending {
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
