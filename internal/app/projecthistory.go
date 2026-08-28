package app

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/localhistory"
	"ike/internal/ui"
)

// projecthistory.go wires the project-wide local-history timeline (#2171):
// `history.projectTimeline` lists the snapshots of *every* file the store
// knows, newest first and grouped by day, so the question "what did I change
// today?" has an answer that does not start with picking a file. The per-file
// panel (#1023) and the merged Timeline (#1916) both answer "what happened to
// this file"; this view is the other axis.
//
// It is a read-only index into the existing surfaces: enter hands the
// selected row to the per-file panel with that snapshot preselected — opening
// the file first, so the panel's diff has a live buffer and its `r` restore
// works exactly as it does when the panel is opened from the file itself.
// Nothing here re-implements diffing or restoring.

// ProjectHistoryMsg runs history.projectTimeline: the project-wide timeline.
type ProjectHistoryMsg struct{}

// projectHistoryHeading prefixes the shell heading, so the open-guard
// recognizes the picker however its body was last re-bound (the
// localHistoryOpen pattern).
const projectHistoryHeading = "PROJECT HISTORY"

// projectHistoryPage is how many snapshot rows the timeline reveals at once.
// A 30-day store of a busy project holds thousands of snapshots; rendering
// and day-grouping all of them on every keystroke of the filter is work
// nobody asked for, so the list grows a page at a time — as the selection
// walks toward its end, or on `L`.
const projectHistoryPage = 100

// projectHistoryState is the open timeline's data. all is the bounded store
// scan taken at open time (the panel is modal, so it cannot change
// underneath); view is that list narrowed by the path filter, and shown caps
// how much of view is reachable.
type projectHistoryState struct {
	all       []localhistory.Snapshot // whole scan, newest first
	view      []localhistory.Snapshot // narrowed by the filter, newest first
	rows      []int                   // per rendered body line: index into view, -1 inert
	sel       int                     // selection into view (< shown)
	top       int                     // first view row the body renders
	shown     int                     // lazy-loading cap on view
	truncated bool                    // the store held more than the scan cap
	search    ui.SpeedSearch          // the filter line (path substring)
	// now freezes the grouping clock at open time: a panel left open across
	// midnight must not silently relabel "Today" under the cursor.
	now time.Time
}

// openProjectHistory scans the store and opens the timeline. An empty store
// notifies instead of opening an empty list, like the per-file panel.
func (m *Model) openProjectHistory() {
	snaps, truncated := m.lhStore.Scan(0)
	if len(snaps) == 0 {
		m.host.Notify(host.Info, "no local history yet — snapshots record on save")
		return
	}
	m.ph = projectHistoryState{
		all:       snaps,
		truncated: truncated,
		shown:     projectHistoryPage,
		now:       time.Now(),
	}
	m.phPicker = true
	m.refreshProjectHistory()
	m.shell.Open()
}

// projectHistoryOpen reports whether the shell currently shows the timeline —
// the heading check guards against another overlay having taken the shell
// over without the panel's own close path running (the pins pattern).
func (m Model) projectHistoryOpen() bool {
	if !m.phPicker || !m.shell.IsOpen() {
		return false
	}
	c, ok := m.shell.Content().(ui.ModelContent)
	return ok && strings.HasPrefix(c.Heading, projectHistoryHeading)
}

// closeProjectHistory drops the panel off the shell.
func (m *Model) closeProjectHistory() {
	m.phPicker = false
	m.ph = projectHistoryState{}
	m.shell.Close()
}

// refreshProjectHistory re-narrows the scan under the filter, clamps the
// selection and re-binds the shell body to THIS model copy (#1440): the root
// model is a value model, so a body bound once at open time would keep
// rendering the open-time state.
func (m *Model) refreshProjectHistory() {
	m.ph.view = ui.Narrow(&m.ph.search, m.ph.all,
		func(s localhistory.Snapshot) string { return displayPath(s.Path) })
	if m.ph.shown < projectHistoryPage {
		m.ph.shown = projectHistoryPage
	}
	if m.ph.sel >= m.reachableProjectHistory() {
		m.ph.sel = m.reachableProjectHistory() - 1
	}
	if m.ph.sel < 0 {
		m.ph.sel = 0
	}
	body, rows := m.renderProjectHistory()
	m.ph.rows = rows
	m.shell.SetContent(ui.ModelContent{
		Heading: projectHistoryHeading + m.ph.search.Hint(),
		Body:    func() string { return body },
	})
}

// reachableProjectHistory is how many rows the selection may walk: the
// narrowed list capped by the lazy-loading window.
func (m Model) reachableProjectHistory() int {
	return min(len(m.ph.view), m.ph.shown)
}

// projectHistoryRows is the height budget of the row block — rows plus the
// day headings between them. It is derived from the terminal, not from the
// shell's laid-out viewport: the shell sizes itself to its content, so a
// budget read back from it would shrink with what this panel last rendered
// and then shrink again. The subtraction is ui's own box arithmetic (margins,
// frame, heading) plus this panel's six fixed lines — the "↑ n newer" note,
// the counts line, the filter line, the hints and their blank separators — so
// a long history windows inside the box instead of pushing the hints out of
// sight.
func (m Model) projectHistoryRows() int {
	return max(m.height-16, 4)
}

// renderProjectHistory draws the day-grouped list plus the key hints, and
// returns the click map alongside it: one entry per body line, holding the
// view index that line selects or -1 for the inert lines (day headings, the
// counts note, the filter line, the hints).
func (m *Model) renderProjectHistory() (string, []int) {
	var lines []string
	var rows []int
	add := func(line string, row int) {
		lines = append(lines, line)
		rows = append(rows, row)
	}
	n := m.reachableProjectHistory()
	if n == 0 {
		add("  (no snapshot path matches the filter — backspace edits it, esc clears it)", -1)
	}
	budget := m.projectHistoryRows()
	top, block := m.projectHistoryWindow(n, budget)
	m.ph.top = top
	if top > 0 {
		add(fmt.Sprintf("  ↑ %d newer", top), -1)
	}
	prevDay := ""
	for _, i := range block {
		sn := m.ph.view[i]
		if day := localhistory.DayLabel(sn.Time, m.ph.now); day != prevDay {
			prevDay = day
			add(day, -1)
		}
		marker := "  "
		if i == m.ph.sel {
			marker = "▍ "
		}
		add(fmt.Sprintf("%s%s  %s  %s", marker,
			projectHistoryPathCell(displayPath(sn.Path), 52),
			sn.Time.Local().Format("15:04:05"),
			ui.RelTime(sn.Time, m.ph.now)), i)
	}
	add("", -1)
	add(m.projectHistoryCounts(n), -1)
	if m.ph.search.Active() {
		add("  filter: "+m.ph.search.Query(), -1)
	}
	add("", -1)
	add("↑↓ move · enter open in local history · type filters the path · "+
		"ctrl+l load more · esc clear filter / close", -1)
	return strings.Join(lines, "\n"), rows
}

// projectHistoryPathCell lays a path out in width columns, padded so the time
// columns align and clipped **from the left** ("…/app/localhistory.go") when
// it is too long: the file name is what identifies a row, and clipping from
// the right is exactly what would drop it.
func projectHistoryPathCell(path string, width int) string {
	r := []rune(path)
	if len(r) > width {
		return "…" + string(r[len(r)-width+1:])
	}
	return path + strings.Repeat(" ", width-len(r))
}

// projectHistoryCounts is the status line under the rows: how much of the
// history is reachable, and why the rest is not (the lazy-loading page, the
// store's scan cap). Silence would read as "this is everything".
func (m Model) projectHistoryCounts(n int) string {
	total := len(m.ph.view)
	out := fmt.Sprintf("  %d of %d snapshots", n, total)
	if len(m.ph.view) != len(m.ph.all) {
		out += fmt.Sprintf(" (filtered from %d)", len(m.ph.all))
	}
	if n < total {
		out += " — ctrl+l loads more"
	} else if m.ph.truncated {
		out += fmt.Sprintf(" — older ones beyond the %d-snapshot scan cap are not listed",
			localhistory.DefaultScanCap)
	}
	return out
}

// projectHistoryWindow picks the block of view rows the body renders: as many
// as fit into budget lines from top, counting the day headings the block
// needs, scrolled so the selection is inside it. It returns the (possibly
// corrected) top and the view indices to render.
func (m Model) projectHistoryWindow(n, budget int) (int, []int) {
	top := min(max(m.ph.top, 0), max(n-1, 0))
	if m.ph.sel < top {
		top = m.ph.sel
	}
	block := m.projectHistoryBlock(top, n, budget)
	// The selection sitting past the block's end (its day headings ate the
	// budget) scrolls down by what is missing, then re-measures.
	for len(block) > 0 && m.ph.sel > block[len(block)-1] {
		top += m.ph.sel - block[len(block)-1]
		block = m.projectHistoryBlock(top, n, budget)
	}
	// Pull the window back up while the row above it still fits, so the last
	// page of a long history renders full instead of half-empty.
	for top > 0 {
		cand := m.projectHistoryBlock(top-1, n, budget)
		if len(cand) == 0 || cand[len(cand)-1] < m.ph.sel {
			break
		}
		top, block = top-1, cand
	}
	return top, block
}

// projectHistoryBlock fills budget lines from view index top with rows and
// the day headings between them, and returns the row indices.
func (m Model) projectHistoryBlock(top, n, budget int) []int {
	var out []int
	used, prevDay := 0, ""
	for i := top; i < n && used < budget; i++ {
		if day := localhistory.DayLabel(m.ph.view[i].Time, m.ph.now); day != prevDay {
			prevDay = day
			used++
			if used >= budget {
				break // a heading with no row under it would be a dangling title
			}
		}
		out = append(out, i)
		used++
	}
	return out
}

// projectHistorySelection returns the snapshot under the cursor, false when
// the filter matches nothing.
func (m Model) projectHistorySelection() (localhistory.Snapshot, bool) {
	if m.ph.sel < 0 || m.ph.sel >= m.reachableProjectHistory() {
		return localhistory.Snapshot{}, false
	}
	return m.ph.view[m.ph.sel], true
}

// updateProjectHistory consumes every key while the timeline is open:
// printable keys narrow the path filter (which is why loading more is the
// chord `ctrl+l` and not the Timeline's bare `L` — here a letter belongs to
// the filter), the navigation keys move the selection, enter hands off to the
// per-file panel.
func (m Model) updateProjectHistory(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// esc clears a running filter first and only closes on the second
		// press — the shared type-ahead contract (#2111).
		if m.ph.search.EscClears() {
			m.ph.sel, m.ph.top = 0, 0
			m.refreshProjectHistory()
			return m, nil
		}
		m.closeProjectHistory()
		return m, nil
	case "enter":
		return m.projectHistoryOpenSelected()
	case "ctrl+l":
		if !m.growProjectHistory() {
			m.host.Notify(host.Info, "the whole scanned history is listed")
			return m, nil
		}
		m.refreshProjectHistory()
		return m, nil
	}
	if handled, changed := m.ph.search.Key(msg); handled {
		if changed {
			m.ph.sel, m.ph.top = 0, 0
		}
		m.refreshProjectHistory()
		return m, nil
	}
	sel := m.ph.sel
	// NavDefault, not NavFull: j/k/g/G belong to the path filter here, so the
	// selection walks on the arrows, the page keys, ctrl+n/p and home/end.
	if ui.ListNav(msg.String(), &sel, m.reachableProjectHistory(), m.projectHistoryRows(), ui.NavDefault) {
		m.ph.sel = sel
		// Lazy loading (#2171): walking toward the end of the list reveals
		// the next page before the user hits the bottom.
		if m.ph.sel >= m.reachableProjectHistory()-3 {
			m.growProjectHistory()
		}
		m.refreshProjectHistory()
	}
	return m, nil
}

// growProjectHistory reveals the next page of the narrowed list, reporting
// whether anything was left to reveal.
func (m *Model) growProjectHistory() bool {
	if m.ph.shown >= len(m.ph.view) {
		return false
	}
	m.ph.shown += projectHistoryPage
	return true
}

// projectHistoryOpenSelected is the handoff (#2171): close the timeline, open
// the row's file, and raise the per-file local-history panel with that
// snapshot preselected. Opening the file first is what makes the panel's
// restore work from here — it edits a buffer, and the timeline's whole point
// is that its rows are files the user does not have open.
func (m Model) projectHistoryOpenSelected() (tea.Model, tea.Cmd) {
	sn, ok := m.projectHistorySelection()
	if !ok {
		return m, nil
	}
	m.closeProjectHistory()
	out, cmd := m.openPathInEditor(sn.Path)
	next, ok := out.(Model)
	if !ok {
		return out, cmd
	}
	// A file deleted since the snapshot was taken has no buffer to open; the
	// panel still opens and diffs the snapshot against nothing, which is the
	// honest picture of a removed file.
	if !next.openLocalHistoryFor(sn.Path, sn.Entry) {
		next.host.Notify(host.Info, "no local history for "+baseName(sn.Path)+" anymore")
	}
	return next, cmd
}

// projectHistoryClickRow maps a body row of the timeline onto a snapshot
// (#2275) through the click map the last render built: a click selects, a
// click on the already-selected row opens it, and the day headings, the
// counts note and the hints are inert.
func (m Model) projectHistoryClickRow(row int) (tea.Model, tea.Cmd) {
	if row < 0 || row >= len(m.ph.rows) || m.ph.rows[row] < 0 {
		return m, nil
	}
	if idx := m.ph.rows[row]; idx != m.ph.sel {
		m.ph.sel = idx
		if m.ph.sel >= m.reachableProjectHistory()-3 {
			m.growProjectHistory()
		}
		m.refreshProjectHistory()
		return m, nil
	}
	return m.projectHistoryOpenSelected()
}
