// Package finder is the find-in-path overlay (Roadmap 0150, #85): a centered
// modal with a query input, case/word/regex toggles, include/exclude glob
// fields, query history, and a live-streamed results list (the reusable
// locations component). It drives internal/search directly — each edit starts
// a new scan (the service cancels the previous one) — and consumes the
// generation-tagged result messages the root model routes back in. Selecting
// a match dispatches OpenLocationMsg; the results survive closing, so
// next/prev-match commands keep working without the overlay.
package finder

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/codepreview"
	"ike/internal/histories"
	"ike/internal/locations"
	"ike/internal/search"
	"ike/internal/theme"
	"ike/internal/ui"
)

// OpenLocationMsg asks the root model to open Path at the (1-based) Line and
// (0-based rune) Col of a selected match.
type OpenLocationMsg struct {
	Path string
	Line int
	Col  int
}

// ReplaceRequestMsg asks the root model to apply Replacement to the given
// matches (Roadmap 0150, #86): through the open dirty buffer where one holds
// the file, directly on disk otherwise. Query carries the flags capture-group
// expansion needs.
type ReplaceRequestMsg struct {
	Items       []locations.Item
	Replacement string
	Query       search.Query
	// Mtimes carries each file's modification time as first seen during the
	// scan (#2154): the disk apply path skips a file whose mtime moved on —
	// changed since the search ran — and refreshes the entry after its own
	// write, so later batches from the same scan still apply. The map is
	// shared with the finder by reference; both sides run on the update loop.
	Mtimes map[string]time.Time
}

// field enumerates the focusable inputs; tab cycles through them (the replace
// field only exists in replace mode).
type field int

const (
	fieldQuery field = iota
	fieldReplace
	fieldInclude
	fieldExclude
	fieldCount
)

const maxHistory = 50

// Model is the overlay state. The root model routes keys here while open and
// feeds search.BatchMsg/DoneMsg through Apply.
type Model struct {
	svc  *search.Service
	root string

	open  bool
	focus field
	// cur is the rune cursor within the focused field (#763); focus changes
	// and programmatic field replacement reset it to the end.
	cur int

	query   string
	replace string // replacement template (replace mode only)
	include string // comma-separated include globs
	exclude string // comma-separated exclude globs

	// preselect marks the remembered query as selected on re-open (#277):
	// the first typed character replaces it wholesale, any other key keeps
	// the text and drops the mark — JetBrains' prefill-selected behavior.
	preselect bool

	// replaceMode adds the replacement input, the before/after preview, and
	// the apply keys (project.replaceInPath); off is plain find-in-path.
	replaceMode bool
	// lastQuery is the search.Query of the current scan, retained so apply
	// requests carry the exact flags the matches were produced with.
	lastQuery search.Query

	caseSensitive bool
	wholeWord     bool
	regex         bool

	hist    []string
	histIdx int // -1 = editing live, otherwise index into hist (newest first)
	// histStore is the app-owned persistent query-history store (#1171):
	// hist seeds from its findInPath bucket on the first Open and commits
	// push back into it, so the recall list survives a restart. Nil (the
	// default, tests) keeps the history session-local.
	histStore  *histories.Store
	histLoaded bool

	gen       int // generation of the scan whose results we accept
	scanning  bool
	truncated bool
	errText   string

	// pendingCursor is the result index to restore once the running scan
	// completes (#2054): set by Open when reopening — either from the
	// in-memory list's cursor (same session) or the persisted state (a
	// fresh session) — and consumed, then reset to -1, in Apply's DoneMsg.
	// -1 means nothing to restore.
	pendingCursor int

	list locations.List

	// mtimes records each result file's mtime when its first match streams in
	// (#2154) — the freshest observation of the content the scan matched. The
	// apply path uses it as the stale-file guard; see ReplaceRequestMsg.Mtimes.
	mtimes map[string]time.Time

	// prev caches the code-preview window rendered beside the list (#2047),
	// so following the cursor re-reads the file only when the target moves.
	prev codepreview.Cache

	// lay records, during View, which content rows the mouse can hit; Click
	// hit-tests against it.
	lay layoutInfo

	width, height int
	pal           *theme.Palette

	// displayPath shortens result paths for the group headers (the root model
	// injects its project-relative formatter).
	displayPath func(string) string
}

// New returns a closed finder driving svc.
func New(svc *search.Service) *Model {
	return &Model{svc: svc, histIdx: -1, pendingCursor: -1}
}

// SetPalette threads the active theme in.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetHistories injects the persistent query-history store (#1171).
func (m *Model) SetHistories(h *histories.Store) { m.histStore = h }

// SetDisplayPath injects the header path formatter.
func (m *Model) SetDisplayPath(f func(string) string) { m.displayPath = f }

// SetSize records the terminal size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// Open shows the finder rooted at root, keeping the previous query and
// toggles (JetBrains re-opens with the last search) but clearing results.
//
// The result cursor is restored too (#2054): on a fresh session (histStore
// injected, first Open) it comes from the persisted find state, which also
// seeds the query, toggles and globs; on any later reopen within the same
// session it comes from wherever the in-memory list's cursor already sat.
// Either way the target lands once the reopened scan's results are in (see
// Apply's DoneMsg case).
func (m *Model) Open(root string) { m.openWith(root, "") }

// OpenPrefilled shows the finder rooted at root with the query prefilled from
// sel — the active text selection at the moment Find in Path was invoked
// (#2165). JetBrains' behavior: the prefill replaces the remembered query and
// arrives fully selected, so the first typed character overwrites it.
//
// A selection that spans multiple lines, or is blank, prefills nothing and
// the remembered query survives untouched (see prefillQuery). In regex mode
// the prefill is escaped with regexp.QuoteMeta so the selected text is
// matched literally.
func (m *Model) OpenPrefilled(root, sel string) { m.openWith(root, sel) }

func (m *Model) openWith(root, sel string) {
	restoreCursor := -1
	if m.histStore != nil && !m.histLoaded {
		// Seed the recall list and last search state from the persisted
		// store once (#1171, #2054); afterwards the in-memory state is
		// authoritative for this session.
		m.hist = m.histStore.All(histories.FindInPath)
		if st, ok := m.histStore.LoadFindState(); ok {
			m.query = st.Query
			m.include = st.Include
			m.exclude = st.Exclude
			m.caseSensitive = st.CaseSensitive
			m.wholeWord = st.WholeWord
			m.regex = st.Regex
			restoreCursor = st.Cursor
		}
		m.histLoaded = true
	} else if m.list.Total() > 0 {
		restoreCursor = m.list.Cursor()
	}
	if q, ok := prefillQuery(sel, m.regex); ok {
		// A prefill outranks both the remembered query and the restored
		// result cursor: the reopened scan is a different search.
		m.query = q
		restoreCursor = -1
	}
	m.open = true
	m.replaceMode = false
	m.root = root
	m.focus = fieldQuery
	m.cur = len([]rune(m.query))
	m.histIdx = -1
	m.errText = ""
	m.preselect = m.query != ""
	m.rescan()
	m.pendingCursor = restoreCursor
}

// OpenReplace shows the finder in replace mode (#86): find-in-path plus the
// replacement input, preview, and apply keys.
func (m *Model) OpenReplace(root string) { m.OpenReplacePrefilled(root, "") }

// OpenReplacePrefilled is OpenReplace with the query prefilled from the active
// selection (#2165), on the same terms as OpenPrefilled.
func (m *Model) OpenReplacePrefilled(root, sel string) {
	m.openWith(root, sel)
	m.replaceMode = true
}

// prefillQuery turns a selection into a query prefill. ok is false — no
// prefill, the caller keeps whatever query it already had — when the
// selection is blank or spans more than one line: a line-spanning selection
// has no equivalent in the query language, so offering a truncated one would
// search for something the user never selected. That is the rule the editor's
// "/" search (#2063) and the HTTP viewer's (#2122) already follow. A single
// trailing newline (a linewise selection of one line) is not a span and is
// dropped. In regex mode the text is escaped so it matches literally.
func prefillQuery(sel string, regex bool) (string, bool) {
	line := strings.TrimSuffix(strings.TrimSuffix(sel, "\n"), "\r")
	if strings.TrimSpace(line) == "" || strings.ContainsAny(line, "\n\r") {
		return "", false
	}
	if regex {
		line = regexp.QuoteMeta(line)
	}
	return line, true
}

// Close hides the overlay. Results are kept for next/prev-match, and the
// current search state persists (#2054) so the next Open — even in a later
// session — resumes it.
func (m *Model) Close() {
	m.open = false
	m.svc.Cancel()
	m.scanning = false
	m.prev.SetFocus(false)
	if m.histStore != nil {
		m.histStore.SaveFindState(histories.FindState{
			Query:         m.query,
			Include:       m.include,
			Exclude:       m.exclude,
			CaseSensitive: m.caseSensitive,
			WholeWord:     m.wholeWord,
			Regex:         m.regex,
			Cursor:        m.list.Cursor(),
		})
	}
}

// IsOpen reports whether the overlay is shown.
func (m *Model) IsOpen() bool { return m.open }

// HasResults reports whether a past scan left navigable matches.
func (m *Model) HasResults() bool { return m.list.Total() > 0 }

// Advance moves the retained result cursor by delta (wrapping) and returns
// the match — the next/prev-match seam that works with the overlay closed.
func (m *Model) Advance(delta int) (locations.Item, bool) { return m.list.Advance(delta) }

// Query returns the current query text (replace-in-path builds on it, #86).
func (m *Model) Query() string { return m.query }

// theme returns the active palette, defaulting when none was threaded in.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// Apply consumes one streamed scan message, dropping stale generations.
func (m *Model) Apply(msg tea.Msg) {
	switch msg := msg.(type) {
	case search.BatchMsg:
		if msg.Gen != m.gen {
			return
		}
		if m.mtimes == nil {
			m.mtimes = map[string]time.Time{}
		}
		items := make([]locations.Item, len(msg.Matches))
		for i, hit := range msg.Matches {
			if _, seen := m.mtimes[hit.Path]; !seen {
				// First match of this file: record its mtime as the
				// stale-file baseline (#2154). A stat failure simply leaves
				// the file unguarded by mtime (the line guard still applies).
				if fi, err := os.Stat(hit.Path); err == nil {
					m.mtimes[hit.Path] = fi.ModTime()
				}
			}
			items[i] = locations.Item{
				Path:     hit.Path,
				Line:     hit.Line,
				StartCol: hit.StartCol,
				EndCol:   hit.EndCol,
				Text:     hit.Text,
			}
		}
		m.list.Append(items)
	case search.DoneMsg:
		if msg.Gen != m.gen {
			return
		}
		m.scanning = false
		m.truncated = msg.Truncated
		if msg.Err != nil {
			m.errText = msg.Err.Error()
		}
		if m.pendingCursor >= 0 {
			m.list.SetCursor(m.pendingCursor)
			m.pendingCursor = -1
		}
	}
}

// rescan restarts the scan for the current inputs (or clears on an empty
// query). It always drops any pending cursor restore — Open re-arms it right
// after calling rescan (see there); every other caller is a user-driven edit
// that invalidates whatever restore target was still pending.
func (m *Model) rescan() {
	m.list.Reset()
	m.pendingCursor = -1
	m.truncated = false
	m.errText = ""
	m.mtimes = map[string]time.Time{}
	if strings.TrimSpace(m.query) == "" {
		m.svc.Cancel()
		m.scanning = false
		return
	}
	m.scanning = true
	m.lastQuery = search.Query{
		Pattern:       m.query,
		Root:          m.root,
		CaseSensitive: m.caseSensitive,
		WholeWord:     m.wholeWord,
		Regex:         m.regex,
		Include:       splitGlobs(m.include),
		Exclude:       splitGlobs(m.exclude),
	}
	m.gen = m.svc.Scan(m.lastQuery)
}

// splitGlobs turns a comma-separated field into a glob slice.
func splitGlobs(s string) []string {
	var out []string
	for _, g := range strings.Split(s, ",") {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}

// Paste inserts a pasted block into the focused field at the cursor (#1273):
// query, replacement, include or exclude globs alike. A pending prefill
// selection (#277) is replaced wholesale, the same way a typed character
// replaces it. handled is false when the block flattened to nothing.
func (m *Model) Paste(text string) (handled bool) {
	pre := m.preselect
	f := m.focused()
	cur := m.cur
	if pre && m.focus == fieldQuery {
		// The remembered query is selected: a paste replaces it, not appends.
		*f, cur = "", 0
	}
	out, ncur, changed := ui.PasteText(*f, cur, text)
	if !changed {
		return false
	}
	m.preselect = false
	*f, m.cur = out, ncur
	m.editedField()
	return true
}

// Update handles one key while the overlay is open.
func (m *Model) Update(msg tea.KeyPressMsg) tea.Cmd {
	// Selection semantics for the remembered query (#277): the first typed
	// character replaces it; any other key keeps the text, drops the mark.
	pre := m.preselect
	m.preselect = false
	// The excerpt column is a focusable read-only viewport (#2327): while it
	// holds the focus the editor-like motions scroll it and esc hands the
	// keyboard back to the query field.
	if codepreview.IsFocusKey(msg.String()) {
		m.prev.SetFocus(!m.prev.Focused())
		return nil
	}
	if m.prev.Focused() {
		switch {
		case msg.String() == "esc":
			m.prev.SetFocus(false)
			return nil
		case m.prev.Key(msg.String()):
			return nil
		case ui.Typing(msg) || msg.String() == "tab" || msg.String() == "shift+tab":
			// A key the excerpt has no motion for, but the fields do: typing
			// or cycling means the user is back at the inputs, so the focus
			// follows before the key is handled below.
			m.prev.SetFocus(false)
		}
	}
	switch msg.String() {
	case "esc":
		m.Close()
		return nil
	case "enter":
		// Replace mode: enter applies the selected match and steps on;
		// find mode (and alt+enter below): open the file at the match.
		if m.replaceMode {
			if it, ok := m.list.RemoveCurrent(); ok {
				m.commitHistory()
				return m.replaceCmd([]locations.Item{it})
			}
			return nil
		}
		return m.openCurrent()
	// The ctrl chords double every alt binding below: on macOS Option is a
	// composition key, so alt chords never reach the terminal (#422, same
	// story as the tab keys in #248). ctrl is the delivered primary; alt
	// stays for terminals where it works.
	case "alt+enter", "ctrl+enter":
		return m.openCurrent()
	case "alt+f", "ctrl+f":
		// Replace the selected file's matches — the excluded ones stay (#2154).
		if m.replaceMode {
			if batch := m.list.RemoveIncludedGroup(); len(batch) > 0 {
				m.commitHistory()
				return m.replaceCmd(batch)
			}
		}
		return nil
	case "alt+a", "ctrl+a":
		// Replace all non-excluded matches (#2154): apply-selected when some
		// are toggled off, apply-all otherwise. Excluded rows stay listed.
		if m.replaceMode {
			if batch := m.list.RemoveIncluded(); len(batch) > 0 {
				m.commitHistory()
				return m.replaceCmd(batch)
			}
		}
		return nil
	case "alt+t", "ctrl+t":
		// Toggle the selected match out of (or back into) the apply set
		// (#2154), stepping on so repeated presses triage the list.
		if m.replaceMode && m.list.ToggleExcluded() {
			m.list.Step(1)
		}
		return nil
	case "alt+g", "ctrl+g":
		// Toggle the selected file's matches as a group (#2154).
		if m.replaceMode {
			m.list.ToggleExcludedGroup()
		}
		return nil
	case "tab":
		m.setFocus(m.nextField(1))
		return nil
	case "shift+tab":
		m.setFocus(m.nextField(-1))
		return nil
	case "down":
		if m.list.Total() > 0 {
			m.list.Step(1) // wraps to the first hit past the last (#1666)
		} else {
			m.history(-1) // toward older is up; down walks back to newer
		}
		return nil
	case "up":
		if m.list.Total() > 0 {
			m.list.Step(-1)
		} else {
			m.history(1)
		}
		return nil
	case "pgdown":
		m.list.Page(1) // one visible page, clamped at the ends (#1666)
		return nil
	case "pgup":
		m.list.Page(-1)
		return nil
	case "alt+c", "ctrl+c":
		m.caseSensitive = !m.caseSensitive
		m.rescan()
		return nil
	case "alt+w", "ctrl+w":
		m.wholeWord = !m.wholeWord
		m.rescan()
		return nil
	case "alt+x", "ctrl+x":
		m.regex = !m.regex
		m.rescan()
		return nil
	case "alt+up", "ctrl+up":
		m.history(1)
		return nil
	case "alt+down", "ctrl+down":
		m.history(-1)
		return nil
	}
	// Everything else is single-line editing on the focused field (#763):
	// cursor motions, word ops, insertion. The chords above keep priority.
	f := m.focused()
	if out, ncur, handled, changed := ui.EditKey(msg, *f, m.cur); handled {
		if pre && m.focus == fieldQuery && len(out) > len(*f) {
			// The key inserted text: it replaces the selected prefill (#277),
			// so re-apply it to an empty field.
			out, ncur, _, _ = ui.EditKey(msg, "", 0)
		}
		*f, m.cur = out, ncur
		if changed {
			m.editedField()
		}
	}
	return nil
}

// setFocus moves the input focus and parks the cursor at the field's end.
func (m *Model) setFocus(f field) {
	m.focus = f
	m.cur = len([]rune(*m.focused()))
}

// layoutInfo maps content rows (0 = first row inside the border) to click
// targets; View fills it in each render, -1 marks an absent row.
type layoutInfo struct {
	query, replace, toggles, include, exclude int
	listTop, listRows                         int
	// listW is the result column's width (#2047): a press right of it lands
	// in the code preview, which is not clickable.
	listW int
}

// maxBoxWidth caps the centered overlay. It went from 100 to 120 with the
// code-preview column (#2047): two columns want more room, and on a narrower
// terminal the width-12 margin still decides.
const maxBoxWidth = 120

// boxWidth is the overlay's outer width: the terminal minus a margin, capped
// and floored to stay readable.
func (m *Model) boxWidth() int {
	w := m.width - 12
	if w > maxBoxWidth {
		w = maxBoxWidth
	}
	if w < 40 {
		w = min(40, m.width-2)
	}
	return w
}

// resultsBody renders the fixed-height results block: the match list on the
// left, padded to listH rows so the overlay keeps its size with few or no
// matches (#2047), and — when there is room — the selected match's file
// excerpt on the right, separated by a vertical rule. Both halves come from
// the shared preview component (#2053).
func (m *Model) resultsBody(listW, previewW, listH int, pal *theme.Palette) string {
	var left []string
	if body := m.list.Render(listW, listH, pal, m.displayPath); body != "" {
		left = strings.Split(body, "\n")
	}
	return m.prev.Columns(left, listW, previewW, listH, m.previewTarget(), pal)
}

// previewTarget is the selected match as the preview column's target: its
// file, line and — so the hit keeps its match emphasis inside the excerpt
// (#2327) — every match range on that line (#1121). The zero Target (no
// selection) renders an empty column.
func (m *Model) previewTarget() codepreview.Target {
	it, ok := m.list.Current()
	if !ok {
		return codepreview.Target{}
	}
	return codepreview.TargetFrom(it.Path, it.Line, it.Ranges(), func(rg locations.Range) (int, int) {
		return rg.Start, rg.End
	})
}

// previewSplit is the overlay's column geometry: the preview column adapts to
// the code around the selected hit (#2327). View and Click both read it, so
// the clickable region and the rendered rule agree.
func (m *Model) previewSplit(innerW, listH int) (listW, previewW int) {
	return m.prev.SplitFor(innerW, listH, m.previewTarget())
}

// toggleSpans mirrors togglesRow's layout: the half-open x range of each
// toggle ("[x] " + label) within the content row.
func toggleSpans() [3][2]int {
	labels := [3]string{caseLabel, wordLabel, regexLabel}
	var spans [3][2]int
	x := len(togglesIndent)
	for i, l := range labels {
		w := 4 + len(l) // "[x] " + label
		spans[i] = [2]int{x, x + w}
		x += w + 2
	}
	return spans
}

// Click handles a left press at panel-local coordinates (0,0 = the box's
// top-left border cell, mirroring the settings panel, #127): an input row
// takes focus, a toggle flips and rescans, a result row selects its match,
// and a press on the already-selected match opens it — the same
// press-again-to-activate semantics as the settings panel.
func (m *Model) Click(x, y int) tea.Cmd {
	if !m.open || m.lay.query <= 0 {
		return nil
	}
	cx, cy := x-2, y-1 // border + horizontal padding
	if cx < 0 || cy < 0 {
		return nil
	}
	switch cy {
	case m.lay.query:
		m.setFocus(fieldQuery)
		m.preselect = false
	case m.lay.replace:
		m.setFocus(fieldReplace)
	case m.lay.include:
		m.setFocus(fieldInclude)
	case m.lay.exclude:
		m.setFocus(fieldExclude)
	case m.lay.toggles:
		for i, sp := range toggleSpans() {
			if cx < sp[0] || cx >= sp[1] {
				continue
			}
			switch i {
			case 0:
				m.caseSensitive = !m.caseSensitive
			case 1:
				m.wholeWord = !m.wholeWord
			case 2:
				m.regex = !m.regex
			}
			m.rescan()
			break
		}
	}
	inList := m.lay.listW <= 0 || cx < m.lay.listW // right of it lies the preview (#2047)
	inRows := m.lay.listTop >= 0 && cy >= m.lay.listTop && cy < m.lay.listTop+m.lay.listRows
	if !inList && inRows {
		// A press in the excerpt column hands it the scroll keys (#2327)
		// instead of activating the row behind it.
		m.prev.SetFocus(true)
		return nil
	}
	if inList {
		m.prev.SetFocus(false)
	}
	if inList && inRows {
		if idx, ok := m.list.ItemAt(cy - m.lay.listTop); ok {
			if idx == m.list.Cursor() {
				return m.openCurrent()
			}
			m.list.SetCursor(idx)
		}
	}
	return nil
}

// Wheel scrolls the results list by delta items — or, with the preview
// focused, the excerpt (#2327).
func (m *Model) Wheel(delta int) {
	if m.prev.Focused() {
		m.prev.Scroll(delta)
		return
	}
	m.list.Move(delta)
}

// openCurrent dispatches the selected match as a navigation and closes.
func (m *Model) openCurrent() tea.Cmd {
	it, ok := m.list.Current()
	if !ok {
		return nil
	}
	m.commitHistory()
	m.Close()
	return func() tea.Msg {
		return OpenLocationMsg{Path: it.Path, Line: it.Line, Col: it.StartCol}
	}
}

// replaceCmd dispatches an apply request for the given matches.
func (m *Model) replaceCmd(items []locations.Item) tea.Cmd {
	req := ReplaceRequestMsg{Items: items, Replacement: m.replace, Query: m.lastQuery, Mtimes: m.mtimes}
	return func() tea.Msg { return req }
}

// nextField cycles input focus, skipping the replace field in find mode.
func (m *Model) nextField(dir int) field {
	f := m.focus
	for {
		f = (f + field(dir) + fieldCount) % fieldCount
		if f != fieldReplace || m.replaceMode {
			return f
		}
	}
}

// focused returns the field the cursor edits.
func (m *Model) focused() *string {
	switch m.focus {
	case fieldReplace:
		return &m.replace
	case fieldInclude:
		return &m.include
	case fieldExclude:
		return &m.exclude
	}
	return &m.query
}

// editedField reacts to a text change: query/glob changes restart the scan
// (editing the query also leaves history-recall mode); the replacement
// template only changes the preview, never the match set.
func (m *Model) editedField() {
	if m.focus == fieldReplace {
		return
	}
	if m.focus == fieldQuery {
		m.histIdx = -1
	}
	m.rescan()
}

// history walks the recall list: dir>0 moves to older entries, dir<0 back
// toward the live query.
func (m *Model) history(dir int) {
	if len(m.hist) == 0 || m.focus != fieldQuery {
		return
	}
	next := m.histIdx + dir
	if next < -1 {
		next = -1
	}
	if next >= len(m.hist) {
		next = len(m.hist) - 1
	}
	if next == m.histIdx {
		return
	}
	m.histIdx = next
	if next == -1 {
		m.query = ""
	} else {
		m.query = m.hist[next]
	}
	m.cur = len([]rune(m.query))
	m.rescan()
}

// commitHistory records the current query at the front of the recall list.
func (m *Model) commitHistory() {
	q := strings.TrimSpace(m.query)
	if q == "" {
		return
	}
	out := []string{q}
	for _, h := range m.hist {
		if h != q {
			out = append(out, h)
		}
	}
	if len(out) > maxHistory {
		out = out[:maxHistory]
	}
	m.hist = out
	if m.histStore != nil {
		// Persist on push (#1171): commits are rare, so no debounce. The
		// store applies the same dedupe and cap.
		m.histStore.Push(histories.FindInPath, q)
	}
}

// View renders the centered overlay box.
func (m *Model) View() string {
	if !m.open || m.width <= 0 {
		return ""
	}
	pal := m.theme()
	boxW := m.boxWidth()
	// The box renders boxW-2 TOTAL cells including its border (lipgloss
	// counts the border inside Width), minus 2 border and 2 padding cells:
	// rows wider than boxW-6 soft-wrap onto a second line (#971).
	innerW := boxW - 6 // border + padding

	heading := "Find in Path"
	if m.replaceMode {
		heading = "Replace in Path"
	}
	title := lipgloss.NewStyle().Bold(true).Underline(true).Render(heading)
	lay := layoutInfo{replace: -1, listTop: -1}
	rows := []string{title, ""}
	lay.query = len(rows)
	rows = append(rows, m.inputRow("Search ", m.query, fieldQuery, innerW))
	if m.replaceMode {
		lay.replace = len(rows)
		rows = append(rows, m.inputRow("Replace", m.replace, fieldReplace, innerW))
	}
	lay.toggles = len(rows)
	rows = append(rows, m.togglesRow(innerW))
	lay.include = len(rows)
	rows = append(rows, m.inputRow("Include", m.include, fieldInclude, innerW))
	lay.exclude = len(rows)
	rows = append(rows, m.inputRow("Exclude", m.exclude, fieldExclude, innerW))
	rows = append(rows, "")

	// In replace mode every row previews its rewrite inline (#2154): the
	// match struck through, the replacement beside it. The hook re-reads the
	// live template, so editing the replacement redraws without a rescan.
	if m.replaceMode {
		m.list.Rewrite = func(it locations.Item, r locations.Range) (string, bool) {
			return search.RewriteSegment(it.Text, r.Start, r.End, m.lastQuery, m.replace)
		}
	} else {
		m.list.Rewrite = nil
	}

	listH := ui.ClampResultRows(m.height/2 - 9)
	listW, previewW := m.previewSplit(innerW, listH)
	lay.listTop = len(rows)
	lay.listRows = listH
	lay.listW = listW
	rows = append(rows, m.resultsBody(listW, previewW, listH, pal))
	m.lay = lay
	if pre := m.previewRows(innerW); len(pre) > 0 {
		rows = append(rows, "")
		rows = append(rows, pre...)
	}
	rows = append(rows, "", m.statusRow(innerW))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.BorderFocus).
		Padding(0, 1).
		Width(boxW - 2)
	return box.Render(strings.Join(rows, "\n"))
}

// inputRow renders one labelled input line with a block cursor on the focused
// field.
func (m *Model) inputRow(label, value string, f field, width int) string {
	// The remembered query is "selected" (#277): rendered inverted so it
	// reads as replace-on-type. Hard truncate: lipgloss MaxWidth WRAPS
	// overlong content (#971) — both handled by ui.InputRow.
	return ui.InputRow(m.theme(), label, value, f == fieldQuery, m.preselect, m.focus == f, m.cur, width)
}

// The toggle row's fixed pieces; toggleSpans derives the click ranges from
// them, so keep the render and the constants in sync.
const (
	togglesIndent = "        "
	caseLabel     = "Case (ctrl+c)"
	wordLabel     = "Word (ctrl+w)"
	regexLabel    = "Regex (ctrl+x)"
)

// togglesRow renders the three match-mode toggles with their key hints
// (ctrl is the delivered primary on macOS, #422; alt still works elsewhere).
func (m *Model) togglesRow(width int) string {
	return ui.TogglesRow(m.theme(), togglesIndent, width,
		ui.Toggle{Label: caseLabel, Active: m.caseSensitive},
		ui.Toggle{Label: wordLabel, Active: m.wholeWord},
		ui.Toggle{Label: regexLabel, Active: m.regex},
	)
}

// previewRows renders the before/after preview for the selected match in
// replace mode: the current line and what applying the template makes of it.
func (m *Model) previewRows(width int) []string {
	if !m.replaceMode {
		return nil
	}
	it, ok := m.list.Current()
	if !ok {
		return nil
	}
	// Every occurrence on the line is part of the row (#1121), so the preview
	// rewrites them all — right to left, so earlier columns stay valid.
	after := it.Text
	ranges := it.Ranges()
	for i := len(ranges) - 1; i >= 0; i-- {
		out, valid := search.RewriteRange(after, ranges[i].Start, ranges[i].End, m.lastQuery, m.replace)
		if !valid {
			return nil
		}
		after = out
	}
	pal := m.theme()
	del := lipgloss.NewStyle().Foreground(pal.Error)
	add := lipgloss.NewStyle().Foreground(pal.Success)
	// Hard truncate (#971): MaxWidth would wrap an overlong preview line.
	return []string{
		ansi.Truncate(del.Render("- "+strings.TrimRight(it.Text, " \t")), width, "…"),
		ansi.Truncate(add.Render("+ "+strings.TrimRight(after, " \t")), width, "…"),
	}
}

// statusRow summarizes the scan: live progress, counts, truncation, errors.
func (m *Model) statusRow(width int) string {
	pal := m.theme()
	dim := lipgloss.NewStyle().Faint(true)
	switch {
	case m.prev.Focused():
		// The excerpt owns the keyboard (#2327): spell its motions out
		// instead of the scan counts, which the row shows again on blur.
		return dim.Render(ansi.Truncate(
			"preview — j/k ctrl+d/u scroll, h/l scrolls right, z back to the match, esc leaves",
			width, "…"))
	case m.errText != "":
		return lipgloss.NewStyle().Foreground(pal.Error).Render(
			ansi.Truncate("error: "+m.errText, width, "…"))
	case strings.TrimSpace(m.query) == "":
		if m.replaceMode {
			return dim.Render("type to search — enter replaces match, ctrl+f file, ctrl+a all, ctrl+t/g excludes, ctrl+enter opens")
		}
		return dim.Render(ansi.Truncate(
			"type to search — enter opens, esc closes, tab cycles fields, alt+p/ctrl+e the preview",
			width, "…"))
	case m.scanning && m.list.Total() == 0:
		return dim.Render("searching…")
	case m.list.Total() == 0:
		return dim.Render("no matches")
	}
	s := plural(m.list.Total(), "match", "matches") + " in " + plural(m.list.Files(), "file", "files")
	if n := m.list.ExcludedCount(); n > 0 {
		s += ", " + strconv.Itoa(n) + " excluded"
	}
	if m.truncated {
		s += " (truncated)"
	} else if m.scanning {
		s += "…"
	}
	return dim.Render(s)
}

// plural renders "1 match" / "3 matches" style counts.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
