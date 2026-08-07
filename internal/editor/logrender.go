package editor

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/highlight"
	"ike/internal/logline"
	"ike/internal/theme"
)

// logrender.go is the editor half of the log-file rendering (#1621): the log
// language plugin emits captures the ordinary highlight pipeline carries —
// log.time / log.error / log.warn / log.info / log.debug for header fields,
// log.rainbow.N for thread/logger names, log.key / log.message for the logfmt
// pairs past the header (#1633), ansi.<spec> for SGR-styled runs and
// conceal for the escape bytes — and this file resolves them into styles the
// theme table does not know: severities from the palette's semantic colors,
// timestamps, keys and debug lines as terminal faint, the message bold,
// ANSI colors from the theme's
// terminal palette (#1363). A `theme.captures.log.*` config key still wins,
// because styleAt consults the capture table first. Everything is gated by
// logRender (editor.log_rendering / view.toggleLogRendering), which also
// gates the parse-produced conceal ranges for log buffers, so toggling off
// shows the raw bytes.

// logCapture reports whether capture belongs to the log-rendering layer.
func logCapture(capture string) bool {
	return strings.HasPrefix(capture, "log.") || strings.HasPrefix(capture, "ansi.")
}

// logBuffer reports whether the buffer renders as a log file.
func (m Model) logBuffer() bool { return highlight.Lang(m.path) == "log" }

// parseConcealOn reports whether the parse-produced conceal ranges apply
// right now: for log buffers the log toggle gates them (concealed ANSI escape
// bytes), for everything else the markdown toggle does (#1599).
func (m Model) parseConcealOn() bool {
	if m.logBuffer() {
		return m.logRenderOn()
	}
	return m.mdRenderOn()
}

// toggleLogRendering flips log rich rendering for this view
// (view.toggleLogRendering): severity/timestamp/rainbow styling and the
// escape-byte conceal all key on logRender. The override sticks like the #64
// view toggles.
func (m *Model) toggleLogRendering() {
	m.logRender = !m.logRender
	m.logRenderSet = true
}

// logStyle resolves a log-layer capture. An explicit theme.captures entry
// wins; otherwise severities draw from the palette's semantic colors,
// timestamps and debug noise render faint, rainbow slots reuse the #1589
// palette mechanic and ansi specs resolve against the terminal palette.
func (m Model) logStyle(capture string) (lipgloss.Style, bool) {
	if st, ok := m.hlTheme.Style(capture); ok {
		return st, true
	}
	pal := m.theme()
	switch capture {
	case "log.error":
		return lipgloss.NewStyle().Foreground(pal.Error), true
	case "log.warn":
		return lipgloss.NewStyle().Foreground(pal.Warning), true
	case "log.time", "log.debug", "log.key":
		return lipgloss.NewStyle().Faint(true), true
	case "log.message":
		return lipgloss.NewStyle().Bold(true), true
	}
	if n, ok := strings.CutPrefix(capture, "log.rainbow."); ok {
		return m.hlTheme.Style("rainbow." + n)
	}
	if spec, ok := strings.CutPrefix(capture, "ansi."); ok {
		if s, ok := logline.ParseSpec(spec); ok {
			return ansiStyle(s, pal), true
		}
	}
	return lipgloss.Style{}, false
}

// ansiStyle turns a decoded SGR state into a lipgloss style.
func ansiStyle(s logline.SGRStyle, pal *theme.Palette) lipgloss.Style {
	st := lipgloss.NewStyle()
	if c, ok := ansiColor(s.Fg, pal); ok {
		st = st.Foreground(c)
	}
	if c, ok := ansiColor(s.Bg, pal); ok {
		st = st.Background(c)
	}
	if s.Bold {
		st = st.Bold(true)
	}
	if s.Faint {
		st = st.Faint(true)
	}
	if s.Italic {
		st = st.Italic(true)
	}
	if s.Underline {
		st = st.Underline(true)
	}
	return st
}

// ansiColor resolves an SGR color token: indexes 0–15 map onto the theme's
// terminal palette (#1363), higher ones stay 256-color indexes and "#rrggbb"
// literals pass through.
func ansiColor(tok string, pal *theme.Palette) (color.Color, bool) {
	if tok == "" {
		return nil, false
	}
	if strings.HasPrefix(tok, "#") {
		return theme.Resolve(tok), true
	}
	n, err := strconv.Atoi(tok)
	if err != nil {
		return nil, false
	}
	if n >= 0 && n < theme.ANSICount {
		return pal.ANSI[n], true
	}
	return theme.Resolve(tok), true
}
