package htmlpreview

// links.go is the HTML preview's link layer (0530/3, #2741), the sibling of
// the markdown preview's (#2180, internal/preview/links.go): tab/shift+tab
// walk the rendered document's links, enter follows the selected one and y
// copies its destination, a click on a link follows it. Unlike the markdown
// preview nothing is recovered by scanning the output — the render core hands
// back the link index with every label piece's byte and cell span
// (htmlrender.Document.LinkSpans) and the id/name anchors.
//
// The pane only says where a link points; the policy — what a destination
// means and where it opens — lives in the root model, which also handles the
// reverse cursor sync: enter with no link selected asks the source editor's
// caret to move to the source line of the rendered line at the sync row.

import (
	"net/url"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// LinkMsg is the user acting on a link of an HTML preview: enter or a click
// follows it, y copies it (Copy). Path is the previewed document, the base a
// relative target resolves against; Key routes an "#anchor" scroll back to
// the pane.
type LinkMsg struct {
	Key    string
	Path   string
	Target string
	Copy   bool
}

// SourceLineMsg is the reverse cursor sync: enter on no selected link asks
// for the editor of Path to move its caret to Line (0-based), the source line
// of the rendered line at the preview's sync row.
type SourceLineMsg struct {
	Path string
	Line int
}

// HasLinks reports whether the rendered document holds a followable link —
// the condition under which the pane claims the tab key, as the markdown
// preview does (#2180).
func (m Model) HasLinks() bool { return len(m.doc.Links) > 0 }

// selected returns the index of the selected link. sel counts from 1 so the
// zero-value model selects nothing.
func (m Model) selected() (int, bool) {
	i := m.sel - 1
	return i, i >= 0 && i < len(m.doc.Links)
}

// SelectedTarget returns the destination of the selected link, if any — the
// seam the status line and the tests read.
func (m Model) SelectedTarget() (string, bool) {
	i, ok := m.selected()
	if !ok {
		return "", false
	}
	return m.doc.Links[i].Href, true
}

// selectLink moves the selection delta links along, wrapping, and scrolls the
// chosen link into view. With no link in the document it is a no-op.
func (m *Model) selectLink(delta int) {
	n := len(m.doc.Links)
	if n == 0 {
		m.sel = 0
		return
	}
	i, ok := m.selected()
	switch {
	case !ok && delta > 0:
		i = 0
	case !ok:
		i = n - 1
	default:
		i = ((i+delta)%n + n) % n
	}
	m.sel = i + 1
	m.reveal(m.doc.Links[i].FirstLine)
}

// clearSelection drops the link selection (esc), so enter reverts to the
// reverse cursor sync.
func (m *Model) clearSelection() { m.sel = 0 }

// enter follows the selected link, or — nothing selected — emits the reverse
// cursor sync for the rendered line at the sync row.
func (m Model) enter() tea.Cmd {
	if target, ok := m.SelectedTarget(); ok {
		return m.linkCmd(target, false)
	}
	return m.sourceLineCmd(m.syncRow())
}

// linkCmd wraps a link action as the LinkMsg the root model acts on.
func (m Model) linkCmd(target string, copy bool) tea.Cmd {
	msg := LinkMsg{Key: m.key, Path: m.path, Target: target, Copy: copy}
	return func() tea.Msg { return msg }
}

// copySelected copies the selected link's destination (y); inert without a
// selection rather than guessing at a link.
func (m Model) copySelected() tea.Cmd {
	target, ok := m.SelectedTarget()
	if !ok {
		return nil
	}
	return m.linkCmd(target, true)
}

// syncRow is the rendered line the reverse sync reads: the row the forward
// sync aims the caret's line at (a third down the viewport), clamped to the
// document, so a forward and a reverse sync round-trip.
func (m Model) syncRow() int {
	return min(m.top+m.h/3, len(m.doc.Lines)-1)
}

// sourceLineCmd maps rendered line row back to its source line through the
// source map; a row that maps nowhere (an empty document) yields nothing.
func (m Model) sourceLineCmd(row int) tea.Cmd {
	if row < 0 {
		return nil
	}
	line, ok := m.doc.SourceLine(row)
	if !ok {
		return nil
	}
	msg := SourceLineMsg{Path: m.path, Line: line}
	return func() tea.Msg { return msg }
}

// LinkAt returns the index of the link under content-local cell (x, y), if
// any — the click hit-test.
func (m Model) LinkAt(x, y int) (int, bool) {
	row := m.top + y
	if y < 0 || y >= m.viewHeight() || row >= len(m.doc.Lines) {
		return 0, false
	}
	for _, s := range m.doc.LinkSpans {
		if s.Line == row && x >= s.Col && x < s.EndCol {
			return s.Link, true
		}
	}
	return 0, false
}

// Click is a left press at content-local (x, y): on a link it selects and
// follows it; anywhere else it does nothing — the press has already focused
// the pane, and a stray click must not move the editor's caret.
func (m *Model) Click(x, y int) tea.Cmd {
	i, ok := m.LinkAt(x, y)
	if !ok {
		return nil
	}
	m.sel = i + 1
	return m.linkCmd(m.doc.Links[i].Href, false)
}

// ScrollToAnchor scrolls to the element whose id (or <a name>) is name and
// reports whether one exists — how an in-document "#anchor" link lands. The
// fragment is matched as written and percent-decoded, so "#caf%C3%A9" finds
// id="café".
func (m *Model) ScrollToAnchor(name string) bool {
	line, ok := m.doc.Anchors[name]
	if !ok {
		dec, err := url.PathUnescape(name)
		if err != nil {
			return false
		}
		if line, ok = m.doc.Anchors[dec]; !ok {
			return false
		}
	}
	m.scrollTo(line)
	return true
}

// reveal scrolls the minimum amount that brings rendered line row into the
// viewport, keeping a line of context on the side it entered from.
func (m *Model) reveal(row int) {
	switch h := m.viewHeight(); {
	case row < m.top:
		m.scrollTo(row - 1)
	case row >= m.top+h:
		m.scrollTo(row - h + 2)
	}
}

// highlightLink returns rendered line i with the selected link's label pieces
// in reverse video. The spans are exact byte ranges of the render, so the
// label is re-emitted plain inside the reverse run (its own resets would end
// the reverse early) and everything around it survives untouched.
func (m Model) highlightLink(i int) string {
	line := m.doc.Lines[i]
	sel, ok := m.selected()
	if !ok {
		return line
	}
	// Spans come in rendered order; splice from the right so earlier byte
	// offsets stay valid.
	spans := m.doc.LinkSpans
	for k := len(spans) - 1; k >= 0; k-- {
		s := spans[k]
		if s.Link != sel || s.Line != i || s.End > len(line) || s.Start > s.End {
			continue
		}
		line = line[:s.Start] + "\x1b[7m" + ansi.Strip(line[s.Start:s.End]) + "\x1b[0m" + line[s.End:]
	}
	return line
}
