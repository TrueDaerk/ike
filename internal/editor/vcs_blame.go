package editor

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// vcs_blame.go is the editor side of inline blame (Roadmap 0320, #468): a
// dimmed "author, when · summary" annotation at the end of the cursor line,
// toggled per document. The blame map arrives via vcs.BlameMsg (editor.go);
// the app owns fetching and refreshing it.

// ToggleBlame flips the inline-blame annotation and reports the new state;
// the app fetches the blame map when it just turned on.
func (m *Model) ToggleBlame() bool {
	m.blameOn = !m.blameOn
	if !m.blameOn {
		m.blame = nil // drop the cache; a re-toggle refetches fresh data
	}
	return m.blameOn
}

// BlameOn reports whether the annotation is enabled for this document.
func (m Model) BlameOn() bool { return m.blameOn }

// blameAnnotate splices the cursor line's annotation into the row's right
// padding. Rows without blame data, or without room, render unchanged.
func (m Model) blameAnnotate(row string, line, textWidth int) string {
	info, ok := m.blame[line]
	if !ok {
		return row
	}
	ann := " ▏ " + info.Annotation(time.Now())
	annW := ansi.StringWidth(ann)
	content := strings.TrimRight(ansi.Strip(row), " ")
	// Two spaces of air between the code and the annotation.
	if ansi.StringWidth(content)+annW+2 > textWidth {
		return row
	}
	style := lipgloss.NewStyle().Foreground(m.theme().InlayHint).Italic(true)
	return ansi.Truncate(row, textWidth-annW, "") + style.Render(ann)
}
