package editor

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// httpflight.go is the editor side of the in-flight request indicator (#1746):
// a "⟳ 1.2s" annotation at the request line of every .http request that is
// currently running. The statusline segment and the response pane already say
// that *a* dispatch runs (#1272), but neither says *which* line in the file it
// belongs to — and with the response pane closed the file itself looked idle.
//
// The editor only renders; the app owns the flight set and pushes a fresh map
// on every flight tick (internal/app/http.go), which is also what keeps the
// elapsed time moving.

// SetHTTPFlight replaces the in-flight annotations of this buffer: 0-based
// request line to indicator text. A nil or empty map clears them. It reports
// nothing but invalidates the render cache when the annotations changed, so
// the moving duration actually repaints (#615).
func (m *Model) SetHTTPFlight(marks map[int]string) {
	if sameFlightMarks(m.httpFlight, marks) {
		return
	}
	if len(marks) == 0 {
		m.httpFlight = nil
		m.bumpRender()
		return
	}
	m.httpFlight = make(map[int]string, len(marks))
	for line, text := range marks {
		m.httpFlight[line] = text
	}
	m.bumpRender()
}

// HTTPFlightAt returns the indicator text of a line, if it carries one.
func (m Model) HTTPFlightAt(line int) (string, bool) {
	text, ok := m.httpFlight[line]
	return text, ok
}

// sameFlightMarks reports whether two annotation sets render identically.
func sameFlightMarks(a, b map[int]string) bool {
	if len(a) != len(b) {
		return false
	}
	for line, text := range a {
		if other, ok := b[line]; !ok || other != text {
			return false
		}
	}
	return true
}

// httpFlightAnnotate places a running request's indicator at the right edge of
// its request line. Unlike the per-line log hint it is worth truncating the
// line for: the indicator is transient and answers "is this request still
// running?", so it must not silently vanish on a long request target.
func (m Model) httpFlightAnnotate(row string, line, textWidth int) (string, bool) {
	text, ok := m.httpFlight[line]
	if !ok || text == "" {
		return row, false
	}
	ann := " " + text
	annW := ansi.StringWidth(ann)
	// Without room for the annotation plus a column of text there is nothing
	// sensible to render.
	if annW+1 > textWidth {
		return row, false
	}
	style := lipgloss.NewStyle().Foreground(m.theme().Accent).Bold(true)
	body := ansi.Truncate(row, textWidth-annW, "")
	if pad := textWidth - annW - ansi.StringWidth(body); pad > 0 {
		body += strings.Repeat(" ", pad)
	}
	return body + style.Render(ann), true
}
