package allfind

import (
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/codepreview"
	"ike/internal/locations"
	"ike/internal/search"
	"ike/internal/theme"
	"ike/internal/ui"
)

// results.go is the all-projects results surface (#2394, reshaped in #2413):
// the same centered results overlay Find in Path uses — the shared
// locations.List with its code-preview column beside it — one grouping level
// deeper, with the project name as the section header above the file headers
// (locations.Item.Section). Key handling is the find-in-path set: enter opens,
// cmd+g / cmd+shift+g step the matches, esc closes, alt+p/ctrl+e hands the
// excerpt column the scroll keys.
//
// While the scan runs nothing is shown here at all — the progress lives in a
// status-line segment (ProgressLabel), so the editor is never covered by a
// half-filled box. The completed result set survives closing: the show-results
// command re-opens it, and it deliberately outlives a project switch, so a hit
// opened in another project can be followed by the next one.

// OpenMatchMsg asks the root model to open a selected match: Path at the
// 1-based Line and 0-based rune Col, in the project rooted at Root. A Root
// other than the current project switches first (#2394).
type OpenMatchMsg struct {
	Root string
	Path string
	Line int
	Col  int
}

// Results is the results overlay's state. It is session state — the root
// model carries it across project switches like the notification history
// (#1514), because the results deliberately span projects (#2413).
type Results struct {
	open bool

	scanning  bool
	query     string
	roots     []string          // scan order, for the progress counter
	names     map[string]string // root → display name, seeded by Begin
	scanned   int               // roots the running scan has finished
	truncated bool
	errs      map[string]error

	// list is the find-in-path results component, sectioned by project root;
	// prev the excerpt column beside it (#2047, #2327).
	list locations.List
	prev codepreview.Cache

	// step remembers the last cmd+g outcome so the status row can show the
	// shared "3/17" counter (#2410).
	step ui.MatchStep

	// lay records, during View, where the list rows sit; Click hit-tests
	// against it, exactly like the finder's.
	lay resultsLayout

	width, height int
	pal           *theme.Palette
}

// resultsLayout maps content rows (0 = first row inside the border) to click
// targets. listW is the list column's width — a press right of it lands in
// the excerpt column.
type resultsLayout struct {
	listTop, listRows, listW int
}

// NewResults returns an empty, closed results overlay.
func NewResults() *Results { return &Results{lay: resultsLayout{listTop: -1}} }

// SetPalette threads the active theme in.
func (r *Results) SetPalette(pal *theme.Palette) { r.pal = pal }

// SetSize records the terminal size.
func (r *Results) SetSize(w, h int) { r.width, r.height = w, h }

// Begin resets the overlay for a new scan over the given projects. Nothing is
// shown while the scan runs (#2413) — the status-line segment carries the
// progress — so the previous result set is dropped only here, on the first
// message of the new one.
func (r *Results) Begin(query string, roots []Project) {
	r.list.Reset()
	r.prev.Reset()
	r.names = map[string]string{}
	r.roots = r.roots[:0]
	for _, p := range roots {
		r.names[p.Root] = p.Name
		r.roots = append(r.roots, p.Root)
	}
	r.query = query
	r.scanning = true
	r.scanned = 0
	r.truncated = false
	r.errs = nil
	r.step = ui.MatchStep{}
	r.open = false
}

// Append records one streamed batch from root. The root rides along as the
// item section, so the list groups project → file → matches and every opened
// match knows which project it belongs to.
func (r *Results) Append(root string, matches []search.Match) {
	items := make([]locations.Item, len(matches))
	for i, m := range matches {
		items[i] = locations.Item{
			Path: m.Path, Line: m.Line, StartCol: m.StartCol, EndCol: m.EndCol,
			Text: m.Text, Section: root,
		}
	}
	r.list.Append(items)
}

// Progress records how many roots the running scan has finished (#2413) — the
// number the status-line segment counts out.
func (r *Results) Progress(done int) {
	if done > r.scanned {
		r.scanned = done
	}
}

// Finish ends the scan and opens the results overlay, focused like the
// find-in-path one. A scan that matched nothing does not take the keyboard —
// there is nothing to walk; the caller says so with a toast and the
// show-results command still brings the empty result up.
func (r *Results) Finish(truncated bool, errs map[string]error) {
	r.scanning = false
	r.scanned = len(r.roots)
	r.truncated = truncated
	r.errs = errs
	if r.list.Total() > 0 {
		r.Open()
	}
}

// Open shows the overlay with the keyboard — the show-results command
// (project.findInAllProjectsResults) and the end of a scan that found
// something.
func (r *Results) Open() {
	r.open = true
	r.prev.SetFocus(false)
}

// Close hides the overlay; the results stay for the show-results command,
// cmd+g stepping and the next project switch.
func (r *Results) Close() {
	r.open = false
	r.prev.SetFocus(false)
}

// IsOpen reports whether the overlay is on screen and owns the keyboard.
func (r *Results) IsOpen() bool { return r.open }

// Scanning reports whether a scan is still streaming in.
func (r *Results) Scanning() bool { return r.scanning }

// HasResults reports whether a finished scan left something to show — matches,
// or at least a header worth re-opening (errors, an empty result).
func (r *Results) HasResults() bool { return !r.scanning && len(r.roots) > 0 }

// Total returns the merged match count.
func (r *Results) Total() int { return r.list.Total() }

// Query returns the pattern the current result set was produced with.
func (r *Results) Query() string { return r.query }

// ProgressLabel is the running scan's status-line segment (#2413): projects
// scanned out of the total, plus the hits so far. Empty while no scan runs.
func (r *Results) ProgressLabel() string {
	if !r.scanning || len(r.roots) == 0 {
		return ""
	}
	s := "⌕ all projects " + strconv.Itoa(r.scanned) + "/" + strconv.Itoa(len(r.roots))
	if n := r.list.Total(); n > 0 {
		s += " · " + plural(n, "hit", "hits")
	}
	return s
}

// StepMatch moves the selection by delta with wrap-around — the shared
// match-step chord's outcome (#2410), reported so the status row can show
// "3/17" and the root model can hint when there is nothing to step to.
func (r *Results) StepMatch(delta int) ui.MatchStep {
	st := r.list.StepMatch(delta)
	r.step = st
	return st
}

// Advance steps the retained result cursor by delta (wrapping) and returns
// the match it landed on together with its project root — the seam that keeps
// cmd+g walking the all-projects hits with the overlay closed, exactly as the
// finder's Advance does for find-in-path.
func (r *Results) Advance(delta int) (root string, it locations.Item, ok bool) {
	it, ok = r.list.Advance(delta)
	if !ok {
		return "", locations.Item{}, false
	}
	return it.Section, it, true
}

// Current returns the selected match and its project root.
func (r *Results) Current() (root string, it locations.Item, ok bool) {
	it, ok = r.list.Current()
	if !ok {
		return "", locations.Item{}, false
	}
	return it.Section, it, true
}

// Update handles one key while the overlay is open. The set is find-in-path's
// (#2413): enter opens, cmd+g / cmd+shift+g step, esc closes, and the excerpt
// column takes the scroll keys while focused.
func (r *Results) Update(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	// The excerpt column is a focusable read-only viewport (#2327), as in the
	// finder: alt+p/ctrl+e toggles it, esc hands the keyboard back.
	if codepreview.IsFocusKey(key) {
		r.prev.SetFocus(!r.prev.Focused())
		return nil
	}
	if r.prev.Focused() {
		switch {
		case key == "esc":
			r.prev.SetFocus(false)
			return nil
		case r.prev.Key(key):
			return nil
		}
	}
	// The overlay owns the keyboard ahead of the keymap layer, so it answers
	// the match-step chord itself (#2410) — cmd+g, ctrl+g and f3 alike.
	if delta, ok := ui.MatchStepChord(key); ok {
		r.StepMatch(delta)
		return nil
	}
	switch key {
	case "esc":
		r.Close()
	case "enter", "alt+enter", "ctrl+enter":
		return r.openCurrent()
	case "down", "j":
		r.list.Step(1) // wraps past the last hit, like the finder's (#1666)
	case "up", "k":
		r.list.Step(-1)
	case "pgdown":
		r.list.Page(1)
	case "pgup":
		r.list.Page(-1)
	case "home":
		r.list.Home()
	case "end":
		r.list.End()
	}
	return nil
}

// openCurrent dispatches the selected match and closes the overlay, like the
// finder's enter. The result set survives — the jump may switch projects, and
// the user continues through the hits afterwards (#2413).
func (r *Results) openCurrent() tea.Cmd {
	root, it, ok := r.Current()
	if !ok {
		return nil
	}
	r.Close()
	msg := OpenMatchMsg{Root: root, Path: it.Path, Line: it.Line, Col: it.StartCol}
	return func() tea.Msg { return msg }
}

// Click handles a left press at panel-local coordinates (0,0 = the box's
// top-left border cell), like the finder's: a result row selects, a press on
// the already-selected row opens it, a press in the excerpt column hands it
// the scroll keys.
func (r *Results) Click(x, y int) tea.Cmd {
	if !r.open || r.lay.listTop < 0 {
		return nil
	}
	cx, cy := x-2, y-1 // border + horizontal padding
	if cx < 0 || cy < 0 {
		return nil
	}
	inList := r.lay.listW <= 0 || cx < r.lay.listW
	inRows := cy >= r.lay.listTop && cy < r.lay.listTop+r.lay.listRows
	if !inList && inRows {
		r.prev.SetFocus(true)
		return nil
	}
	if inList {
		r.prev.SetFocus(false)
	}
	if inList && inRows {
		if idx, ok := r.list.ItemAt(cy - r.lay.listTop); ok {
			if idx == r.list.Cursor() {
				return r.openCurrent()
			}
			r.list.SetCursor(idx)
		}
	}
	return nil
}

// Wheel scrolls the results list by delta items — or, with the excerpt column
// focused, the excerpt (#2327).
func (r *Results) Wheel(delta int) {
	if r.prev.Focused() {
		r.prev.Scroll(delta)
		return
	}
	r.list.Move(delta)
}

// theme returns the active palette, defaulting when none was threaded in.
func (r *Results) theme() *theme.Palette {
	if r.pal != nil {
		return r.pal
	}
	return theme.DefaultPalette()
}

// boxWidth is the overlay's outer width — the finder's geometry (#2047), so
// the two results surfaces sit in the same frame.
func (r *Results) boxWidth() int {
	w := r.width - 12
	if w > maxResultsWidth {
		w = maxResultsWidth
	}
	if w < 40 {
		w = min(40, r.width-2)
	}
	return w
}

// maxResultsWidth caps the centered overlay, like the finder's.
const maxResultsWidth = 120

// View renders the centered overlay box; the root model composites it with
// overlay.Center, like every other modal.
func (r *Results) View() string {
	if !r.open || r.width <= 0 {
		return ""
	}
	pal := r.theme()
	boxW := r.boxWidth()
	innerW := boxW - 6 // border + padding

	title := lipgloss.NewStyle().Bold(true).Underline(true).Render("Find in All Projects")
	if r.query != "" {
		title += lipgloss.NewStyle().Faint(true).Render("  " + r.query)
	}
	rows := []string{ansi.Truncate(title, innerW, "…"), r.summaryRow(innerW), ""}

	// The section headers name the project the run of files below belongs to
	// (#2413); the file headers below them are project-relative.
	r.list.SectionLabel = r.sectionLabel
	listH := ui.ClampResultRows(r.height/2 - 5)
	listW, previewW := r.prev.SplitFor(innerW, listH, r.previewTarget())
	r.lay = resultsLayout{listTop: len(rows), listRows: listH, listW: listW}
	rows = append(rows, r.resultsBody(listW, previewW, listH, pal))
	rows = append(rows, "", r.statusRow(innerW))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.BorderFocus).
		Padding(0, 1).
		Width(boxW - 2).
		Render(strings.Join(rows, "\n"))
}

// resultsBody is the finder's two-column body: the sectioned match list on
// the left, the selected hit's file excerpt on the right (#2047, #2053).
func (r *Results) resultsBody(listW, previewW, listH int, pal *theme.Palette) string {
	var left []string
	if body := r.list.Render(listW, listH, pal, r.displayPath); body != "" {
		left = strings.Split(body, "\n")
	}
	return r.prev.Columns(left, listW, previewW, listH, r.previewTarget(), pal)
}

// previewTarget is the selected match as the excerpt column's target — its
// file, line and every match range on that line (#1121, #2327).
func (r *Results) previewTarget() codepreview.Target {
	it, ok := r.list.Current()
	if !ok {
		return codepreview.Target{}
	}
	src := it.Ranges()
	ranges := make([]codepreview.Range, 0, len(src))
	for _, rg := range src {
		if rg.End > rg.Start {
			ranges = append(ranges, codepreview.Range{Start: rg.Start, End: rg.End})
		}
	}
	return codepreview.Target{Path: it.Path, Line: it.Line, Ranges: ranges}
}

// sectionLabel renders one project header: the display name, its root, the
// match count and, when the root failed to scan, the error beside it. The
// marker sets it apart from the file headers under it, which wear the same
// accent colour.
func (r *Results) sectionLabel(root string, items int) string {
	pal := r.theme()
	dim := lipgloss.NewStyle().Faint(true)
	head := lipgloss.NewStyle().Bold(true).Foreground(pal.BorderFocus).Render("▸ "+r.name(root)) +
		dim.Render("  "+root+"  ("+plural(items, "match", "matches")+")")
	if err, ok := r.errs[root]; ok && err != nil {
		head += lipgloss.NewStyle().Foreground(pal.Error).Render("  ✖ " + err.Error())
	}
	return head
}

// name is a root's display name from the history, falling back to its
// directory name for a root the form never listed.
func (r *Results) name(root string) string {
	if n := r.names[root]; n != "" {
		return n
	}
	return filepath.Base(root)
}

// displayPath shortens a result path against the project it came from, so the
// file headers read like find-in-path's project-relative ones.
func (r *Results) displayPath(p string) string {
	for _, root := range r.roots {
		if rel, err := filepath.Rel(root, p); err == nil &&
			rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rel
		}
	}
	return p
}

// summaryRow is the header line: total count, projects searched, truncation
// and the number of roots that failed.
func (r *Results) summaryRow(width int) string {
	pal := r.theme()
	s := plural(r.list.Total(), "match", "matches") + " in " + plural(len(r.roots), "project", "projects")
	if r.truncated {
		s += " (truncated)"
	}
	row := lipgloss.NewStyle().Faint(true).Render(s)
	if n := len(r.errs); n > 0 {
		row += lipgloss.NewStyle().Foreground(pal.Error).Render("  " + plural(n, "root failed", "roots failed"))
	}
	return ansi.Truncate(row, width, "…")
}

// statusRow spells the keys out, or — right after a cmd+g step — the shared
// match counter (#2410).
func (r *Results) statusRow(width int) string {
	dim := lipgloss.NewStyle().Faint(true)
	switch {
	case r.prev.Focused():
		return dim.Render(ansi.Truncate(
			"preview — j/k ctrl+d/u scroll, h/l scrolls right, z back to the match, esc leaves",
			width, "…"))
	case r.list.Total() == 0:
		return dim.Render("no matches — esc closes")
	case r.step.Handled && r.step.Total > 0:
		return dim.Render(ansi.Truncate(
			ui.MatchCounter(r.step.Index, r.step.Total, r.step.Wrapped)+
				" — enter opens (switches project if needed), esc closes", width, "…"))
	}
	return dim.Render(ansi.Truncate(
		"enter opens (switches project if needed) — cmd+g steps, alt+p/ctrl+e the preview, esc closes",
		width, "…"))
}

// plural renders "1 match" / "3 matches" style counts.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
