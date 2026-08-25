// Package ghissues is the GitHub Issues tool window (#1934): a singleton
// pane listing the open issues of the current repository — number, title,
// colored label chips, assignee, linked-PR state — with fuzzy filtering, a
// label filter, a rendered detail view for the selected issue, and the
// start-work action creating the issue/<number>-<slug> branch of the change
// workflow. It is a pure consumer of internal/forge messages: the app injects
// the refresh command and routes forge.IssuesMsg results in; the pane itself
// never runs a subprocess.
package ghissues

import (
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
	"ike/internal/fuzzy"
	"ike/internal/theme"
	"ike/internal/ui"
)

// OpenURLMsg asks the app to open url in the platform browser ('o').
type OpenURLMsg struct {
	URL string
}

// StartWorkRequestMsg asks the app to run the start-work flow for the
// selected issue ('s'); the app answers with forge.StartWorkCmd.
type StartWorkRequestMsg struct {
	Number int
	Title  string
}

// Model is the tool window state. Value type with pointer-receiver mutators,
// embedded in a pane.Instance like the Usages panel (#1155).
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette

	// refresh is the app-injected fetch command (forge.RefreshCmd at the
	// workspace root); 'r' re-runs it, the pane stays subprocess-free.
	refresh tea.Cmd

	loading bool
	loaded  bool
	setup   string // unavailable state: no gh, no GitHub remote
	errMsg  string // transient fetch error; prior content stays

	issues []forge.Issue
	prs    []forge.PR

	// visible indexes the issues surviving the filters, in display order:
	// fuzzy score order under a filter, listing order otherwise.
	visible []int
	cursor  int
	top     int

	// The '/' filter line, the dataview pattern (#1777): while open it owns
	// the keyboard; the match set narrows live as the pattern changes.
	fEditing bool
	fInput   string
	fCur     int

	// labels are the distinct label names across the listing; labelSel picks
	// one as a hard filter ('l' cycles through them and back to none = -1).
	labels   []string
	labelSel int

	// Detail view: the selected issue's body rendered through glamour,
	// re-rendered lazily when the width changes.
	detail      bool
	detailFor   int // issue number the lines were rendered for
	detailW     int // width they were rendered at
	detailLines []string
	detailTop   int

	// Double-click detection mirrors the Usages panel (#514).
	lastClickRow int
	lastClickAt  time.Time
	now          func() time.Time
}

// New returns an empty pane; content arrives via SetResult.
func New(pal *theme.Palette) Model {
	return Model{pal: pal, labelSel: -1, lastClickRow: -1, now: time.Now}
}

// SetSize records the interior content size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetFocused marks the pane focused (header + selection highlight).
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the active theme.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetRefresh injects the fetch command 'r' re-runs.
func (m *Model) SetRefresh(cmd tea.Cmd) { m.refresh = cmd }

// MarkLoading flips the pane into its fetching state (the app calls it when
// it dispatches the refresh command itself, on open).
func (m *Model) MarkLoading() { m.loading = true }

// SetResult applies one finished fetch.
//
// A background poll (#2085) lands here exactly like a manual refresh, minus
// the parts that would fight the user: it does not clear a pending loading
// state a fresh 'r' set, and the selection is restored by issue number after
// the swap, so a newer issue appearing above the cursor leaves it on the
// issue it was on. The filter line, the label filter and the detail view are
// untouched either way — they are derived from state SetResult never writes.
func (m *Model) SetResult(msg forge.IssuesMsg) {
	if !msg.Poll {
		m.loading = false
	}
	m.setup = msg.Setup
	if msg.Setup != "" {
		return
	}
	if msg.Err != nil {
		m.errMsg = msg.Err.Error()
		return
	}
	m.errMsg = ""
	m.loaded = true
	selected, selectedBody := 0, ""
	if is := m.Selected(); is != nil {
		selected, selectedBody = is.Number, is.Body
	}
	m.issues = msg.Issues
	if msg.PRErr == nil {
		// A failed PR listing (PRErr) is a partial result: keeping the last
		// known PR states beats blanking the linked-PR column over a blip.
		m.prs = msg.PRs
	}
	m.rebuildLabels()
	m.applyFilter()
	m.restoreCursor(selected)
	if m.detail && m.detailFor == selected {
		// The body may have been edited on the forge since it was rendered.
		// Only an actual edit drops the cache — re-rendering an unchanged
		// body every poll would be pure work — and the cache is dropped by
		// clearing the lines rather than the issue number, so the re-render
		// keeps the offset the user scrolled to (ensureDetail only rewinds
		// when the issue itself changes).
		if is := m.Selected(); is != nil && is.Body != selectedBody {
			m.detailLines = nil
		}
	}
}

// restoreCursor puts the cursor back on the issue numbered n after a listing
// swap, keeping the scroll window around it. An issue that left the listing
// (closed since the last poll) leaves the cursor at its old row, clamped —
// the same place a manual refresh would have left it.
func (m *Model) restoreCursor(n int) {
	if n == 0 {
		return
	}
	for i, idx := range m.visible {
		if m.issues[idx].Number == n {
			m.cursor = i
			m.clampScroll()
			return
		}
	}
	m.clampScroll()
}

// Loaded reports whether a listing ever arrived (tests).
func (m *Model) Loaded() bool { return m.loaded }

// Visible reports how many issues survive the filters (tests).
func (m *Model) Visible() int { return len(m.visible) }

// Cursor reports the selected visible row (tests).
func (m *Model) Cursor() int { return m.cursor }

// Filter returns the fuzzy pattern, "" when unfiltered (tests).
func (m *Model) Filter() string { return m.fInput }

// Filtering reports whether the filter line is open (tests).
func (m *Model) Filtering() bool { return m.fEditing }

// LabelFilter returns the active label filter, "" when none (tests).
func (m *Model) LabelFilter() string {
	if m.labelSel < 0 || m.labelSel >= len(m.labels) {
		return ""
	}
	return m.labels[m.labelSel]
}

// DetailOpen reports whether the detail view is showing (tests).
func (m *Model) DetailOpen() bool { return m.detail }

// Selected returns the issue under the cursor, nil when the list is empty.
func (m *Model) Selected() *forge.Issue {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return nil
	}
	return &m.issues[m.visible[m.cursor]]
}

// rebuildLabels recomputes the distinct label names, keeping the active
// label filter when it still exists.
func (m *Model) rebuildLabels() {
	keep := m.LabelFilter()
	seen := map[string]bool{}
	m.labels = nil
	for _, is := range m.issues {
		for _, l := range is.Labels {
			if !seen[l.Name] {
				seen[l.Name] = true
				m.labels = append(m.labels, l.Name)
			}
		}
	}
	sort.Strings(m.labels)
	m.labelSel = -1
	for i, name := range m.labels {
		if name == keep {
			m.labelSel = i
		}
	}
}

// hasLabel reports whether the issue carries the named label.
func hasLabel(is *forge.Issue, name string) bool {
	for _, l := range is.Labels {
		if l.Name == name {
			return true
		}
	}
	return false
}

// matchText is the haystack one issue exposes to the fuzzy filter: number,
// title, labels and assignees, so "#19 fable" or a label name both narrow.
func matchText(is *forge.Issue) string {
	var b strings.Builder
	b.WriteString("#" + strconv.Itoa(is.Number) + " " + is.Title)
	for _, l := range is.Labels {
		b.WriteString(" " + l.Name)
	}
	for _, a := range is.Assignees {
		b.WriteString(" " + a)
	}
	return b.String()
}

// applyFilter recomputes the visible set: the label filter is a hard gate,
// the fuzzy pattern ranks (listing order when empty).
func (m *Model) applyFilter() {
	label := m.LabelFilter()
	m.visible = m.visible[:0]
	scores := map[int]int{}
	for i := range m.issues {
		if label != "" && !hasLabel(&m.issues[i], label) {
			continue
		}
		if m.fInput != "" {
			res, ok := fuzzy.Match(m.fInput, matchText(&m.issues[i]))
			if !ok {
				continue
			}
			scores[i] = res.Score
		}
		m.visible = append(m.visible, i)
	}
	if m.fInput != "" {
		sort.SliceStable(m.visible, func(a, b int) bool {
			return scores[m.visible[a]] > scores[m.visible[b]]
		})
	}
	m.clampScroll()
}

// Update handles one message while the pane exists; only key presses reach
// it, focus-filtered by the pane layer.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		return m.handleKey(k)
	}
	return nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.fEditing {
		m.filterKey(msg)
		return nil
	}
	if m.detail {
		return m.detailKey(msg)
	}
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if ui.ListNav(msg.String(), &m.cursor, len(m.visible), m.bodyHeight(), ui.NavFull) {
		m.clampScroll()
		return nil
	}
	switch msg.String() {
	case "r":
		return m.startRefresh()
	case "/":
		m.fEditing = true
		m.fCur = len([]rune(m.fInput))
	case "l":
		m.cycleLabel()
	case "enter":
		m.openDetail()
	case "s":
		return m.startWork()
	case "o":
		return m.openInBrowser()
	}
	return nil
}

// filterKey feeds one key to the open filter line, which owns the keyboard:
// esc clears and closes, enter keeps the filter and closes, everything else
// edits and re-narrows live.
func (m *Model) filterKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "esc":
		m.fEditing, m.fInput, m.fCur = false, "", 0
		m.applyFilter()
	case "enter":
		m.fEditing = false
	default:
		if out, ncur, handled, changed := ui.EditKey(msg, m.fInput, m.fCur); handled {
			m.fInput, m.fCur = out, ncur
			if changed {
				m.cursor, m.top = 0, 0
				m.applyFilter()
			}
		}
	}
}

// detailKey handles the detail view: scroll, back, and the actions that stay
// meaningful with an issue on screen.
func (m *Model) detailKey(msg tea.KeyPressMsg) tea.Cmd {
	page := m.bodyHeight()
	switch msg.String() {
	case "esc", "q", "backspace":
		m.detail = false
	case "j", "down":
		m.detailTop++
	case "k", "up":
		m.detailTop--
	case "pgdown", "ctrl+d", "space":
		m.detailTop += page
	case "pgup", "ctrl+u":
		m.detailTop -= page
	case "g", "home":
		m.detailTop = 0
	case "G", "end":
		m.detailTop = len(m.detailLines) - page
	case "r":
		return m.startRefresh()
	case "s":
		return m.startWork()
	case "o":
		return m.openInBrowser()
	}
	m.clampDetail()
	return nil
}

// startRefresh re-runs the injected fetch.
func (m *Model) startRefresh() tea.Cmd {
	if m.refresh == nil {
		return nil
	}
	m.loading = true
	return m.refresh
}

// cycleLabel advances the label filter: none → each label in order → none.
func (m *Model) cycleLabel() {
	if len(m.labels) == 0 {
		return
	}
	m.labelSel++
	if m.labelSel >= len(m.labels) {
		m.labelSel = -1
	}
	m.cursor, m.top = 0, 0
	m.applyFilter()
}

// openDetail flips into the detail view for the selected issue.
func (m *Model) openDetail() {
	if m.Selected() == nil {
		return
	}
	m.detail = true
	m.detailTop = 0
}

// startWork asks the app to branch off for the selected issue.
func (m *Model) startWork() tea.Cmd {
	is := m.Selected()
	if is == nil {
		return nil
	}
	msg := StartWorkRequestMsg{Number: is.Number, Title: is.Title}
	return func() tea.Msg { return msg }
}

// openInBrowser asks the app to open the selected issue's page.
func (m *Model) openInBrowser() tea.Cmd {
	is := m.Selected()
	if is == nil || is.URL == "" {
		return nil
	}
	msg := OpenURLMsg{URL: is.URL}
	return func() tea.Msg { return msg }
}

// Title is the pane header text: "Issues — N open" once loaded.
func (m *Model) Title() string {
	t := "Issues"
	if m.loaded {
		t += " — " + strconv.Itoa(len(m.issues)) + " open"
	}
	return t
}

// bodyHeight is the room between the header and footer lines.
func (m *Model) bodyHeight() int {
	h := m.height - 2
	if h < 1 {
		h = 1
	}
	return h
}

// clampScroll keeps the cursor valid and inside the visible window.
func (m *Model) clampScroll() {
	if m.cursor > len(m.visible)-1 {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.top > m.cursor {
		m.top = m.cursor
	}
	if h := m.bodyHeight(); m.cursor >= m.top+h {
		m.top = m.cursor - h + 1
	}
	if m.top < 0 {
		m.top = 0
	}
}

// clampDetail bounds the detail scroll.
func (m *Model) clampDetail() {
	max := len(m.detailLines) - m.bodyHeight()
	if max < 0 {
		max = 0
	}
	if m.detailTop > max {
		m.detailTop = max
	}
	if m.detailTop < 0 {
		m.detailTop = 0
	}
}

// clip bounds one rendered line to the pane width (plain text only).
func (m *Model) clip(s string) string {
	if m.width > 0 && len([]rune(s)) > m.width {
		return string([]rune(s)[:m.width-1]) + "…"
	}
	return s
}

// theme resolves the palette with the shared default fallback.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// PasteText inserts a pasted block into the open filter line at its cursor
// (#2002) and re-narrows the list, exactly like typing there does.
func (m *Model) PasteText(text string) bool {
	if !m.fEditing {
		return false
	}
	out, ncur, changed := ui.PasteText(m.fInput, m.fCur, text)
	if !changed {
		return false
	}
	m.fInput, m.fCur = out, ncur
	m.cursor, m.top = 0, 0
	m.applyFilter()
	return true
}
