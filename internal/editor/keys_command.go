package editor

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/excmd"
	"ike/internal/editor/search"
	"ike/internal/histories"
	"ike/internal/ui"
)

// SearchCommittedMsg announces that an in-file search was committed with a
// non-empty pattern (Enter on the "/" or "?" line). The app uses it to make
// f3/shift+f3 repeat the in-file search instead of retained find-in-path
// results while it is the most recent search (#376).
type SearchCommittedMsg struct{}

// beginSearch enters the command line in search mode for "/" or "?", capturing
// the cursor and viewport so an Esc restores them exactly (#255). prefill
// seeds the query line already selected (#2063), so the first typed key
// replaces it and the incremental preview jumps to it immediately; "" leaves
// the line empty as before.
func (m *Model) beginSearch(dir search.Direction, prefill string) {
	m.collapseCarets() // search is single-caret (#145)
	m.mode = Command
	m.searching = true
	m.searchDir = dir
	m.cmdline = prefill
	m.cmdCur = len([]rune(prefill))
	m.cmdSelStart, m.cmdSelEnd = 0, m.cmdCur
	m.cmdHistIdx = -1 // a fresh line starts outside history recall (#1171)
	m.preview = search.Query{}
	m.searchOrigin = m.cursor
	m.searchOrigTop, m.searchOrigLft = m.view.Top, m.view.Left
	if prefill != "" {
		m.searchPreview()
	}
}

// visualSearchPrefill returns the text an open visual selection should seed
// "/" search with (#2063), mirroring JetBrains: a single-line selection (any
// visual variant, as long as it doesn't span lines) becomes the query; a
// selection spanning multiple lines returns "" and search opens empty, since
// the query language has no line-spanning match to offer instead.
func (m *Model) visualSearchPrefill() string {
	if !m.mode.IsVisual() {
		return ""
	}
	t := m.visualSelection()
	if t.Range.Start.Line != t.Range.End.Line {
		return ""
	}
	if t.Linewise {
		return m.buf.Line(t.Range.Start.Line)
	}
	return m.buf.Slice(t.Range)
}

// searchNextRepeat repeats the active search for n/N. reverse flips the stored
// direction (N). Wrapping past a buffer end leaves a "search wrapped" hint on
// the ex line (#255).
func (m *Model) searchNextRepeat(reverse bool, count int) {
	if m.query.Empty() {
		return
	}
	dir := m.searchDir
	if reverse {
		dir = opposite(dir)
	}
	// A structural query's matches follow the document (#2363): re-evaluate
	// when edits moved the version since they were computed.
	m.query.SyncStructural(m.buf, m.docVersion)
	if p, ok := m.query.Next(m.buf, m.cursor, dir, count); ok {
		if wrapped(m.cursor, p, dir) {
			m.cmdMsg = "search wrapped"
		}
		m.hlActive = true
		m.jumpTo(p) // n/N landings are jumps (Roadmap 0220)
	}
}

// HasSearch reports whether a committed in-file search query is active, i.e.
// n/N (and f3/shift+f3 while in-file is the most recent search) have something
// to repeat.
func (m Model) HasSearch() bool { return !m.query.Empty() }

// RepeatSearch steps the committed in-file search once, like n (reverse=false)
// or N (reverse=true). It backs search.nextMatch/prevMatch when the in-file
// search is the most recent one (#376).
//
// It follows the cursor itself (#1198): n/N reach searchNextRepeat through
// Update, whose key branch ends in scroll(), but the root model calls this
// entry point directly on the model, so nothing else would move the viewport.
// scroll() is idempotent, so keeping it here rather than in searchNextRepeat
// leaves the key path untouched.
func (m *Model) RepeatSearch(reverse bool) {
	m.searchNextRepeat(reverse, 1)
	m.scroll()
}

// wrapped reports whether a search landing at p from `from` crossed a buffer
// end: a forward match behind the cursor (or on it) wrapped to the top, a
// backward match ahead of it wrapped to the bottom.
func wrapped(from, p buffer.Position, dir search.Direction) bool {
	if dir == search.Forward {
		return !from.Before(p)
	}
	return !p.Before(from)
}

func opposite(d search.Direction) search.Direction {
	if d == search.Forward {
		return search.Backward
	}
	return search.Forward
}

// updateCommandLine handles typing on the ":" / "/" / "?" line. Cursor
// movement and word/whole deletion run through the shared single-line editing
// helper (#763, #1110), so mid-line insertion works like the palette/finder
// inputs; every text change reruns the incremental preview (search) or the
// path-suggest refresh (ex line).
func (m Model) updateCommandLine(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.cmdSelStart < m.cmdSelEnd {
		// A preselected range (a search prefilled from the visual selection,
		// #2063): typing replaces it, Backspace/Delete remove it, and any
		// other key just drops the selection and falls through to the normal
		// handling below.
		switch {
		case key.Code == tea.KeyBackspace || key.Code == tea.KeyDelete:
			r := []rune(m.cmdline)
			m.cmdline = string(append(r[:m.cmdSelStart:m.cmdSelStart], r[m.cmdSelEnd:]...))
			m.cmdCur = m.cmdSelStart
			m.cmdSelStart, m.cmdSelEnd = 0, 0
			if m.searching {
				m.searchPreview()
			}
			if m.filtering {
				m.followFilterPreview()
			}
			return m, nil
		case key.Code == tea.KeyEnter || key.Code == tea.KeyEscape:
			m.cmdSelStart, m.cmdSelEnd = 0, 0 // accept/cancel act on the full text
		case key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper|tea.ModMeta) == 0:
			// Typing replaces the selection: drop it here, then fall through
			// to the shared editor below, which inserts at the new cursor.
			r := []rune(m.cmdline)
			m.cmdline = string(append(r[:m.cmdSelStart:m.cmdSelStart], r[m.cmdSelEnd:]...))
			m.cmdCur = m.cmdSelStart
			m.cmdSelStart, m.cmdSelEnd = 0, 0
		default:
			m.cmdSelStart, m.cmdSelEnd = 0, 0
		}
	}
	switch {
	case key.Code == tea.KeyEscape:
		if m.searching {
			m.cancelSearch()
		}
		if m.filtering {
			m.cancelFollowFilter() // #2255: the filter open before the line
		}
		m.mode = Normal
		m.cmdline = ""
		m.cmdCur = 0
		m.cmdSuggest = nil
		m.searching = false
	case key.Code == tea.KeyTab && !m.searching && !m.filtering:
		// Path completion for ":e <partial>" / ":w <partial>" (#543).
		m.completeCmdlinePath()
		m.cmdCur = len([]rune(m.cmdline))
	case key.Code == tea.KeyUp:
		// Recall an older query, vim-style (#1171); typing after a recall
		// edits normally (the default branch drops back to the live line).
		m.recallHistory(1)
	case key.Code == tea.KeyDown:
		m.recallHistory(-1)
	case key.Code == tea.KeyEnter:
		if m.filtering {
			// Follow filter (#2255): the pattern is already applied live, so
			// Enter only closes the line on it — after recording it in the
			// filter's own recall bucket.
			m.pushCmdHistory()
			m.commitFollowFilter()
			m.mode = Normal
			m.cmdline = ""
			m.cmdCur = 0
			return m, nil
		}
		if m.searching {
			m.pushCmdHistory()
			m.commitSearch()
			m.mode = Normal
			m.searching = false
			m.cmdline = ""
			m.cmdCur = 0
			if !m.query.Empty() {
				return m, func() tea.Msg { return SearchCommittedMsg{} }
			}
			return m, nil
		}
		return m.runExLine()
	case key.Code == 'r' && key.Mod == tea.ModCtrl && m.filtering:
		// ctrl+r toggles the regex marker on the filter line (#2255), the way
		// ctrl+c toggles the case marker on the search line: the "\v" prefix
		// is the state, so the mode is always visible in the pattern itself.
		m.toggleFilterRegex()
	case key.Code == 'c' && key.Mod == tea.ModCtrl && (m.searching || m.filtering):
		// ctrl+c toggles case sensitivity for the current query (#1111) by
		// editing the visible \c / \C marker — the marker is the state. The
		// filter line shares the markers, so it shares the toggle (#2255).
		m.toggleSearchCase()
	case key.Code == 'x' && key.Mod == tea.ModCtrl && m.searching:
		// ctrl+x toggles the structural jq mode on the search line (#2363)
		// by editing the visible \j marker, ctrl+c-style.
		m.toggleSearchStructural()
	case m.cmdline == "" && (key.Code == tea.KeyBackspace || key.Code == 'h' && key.Mod == tea.ModCtrl):
		// Backspacing an empty line leaves the command line, vim-style. An
		// emptied filter line keeps its cleared state (#2255): the user
		// deleted the pattern, which is how the whole stream comes back.
		if m.searching {
			m.cancelSearch()
		}
		if m.filtering {
			m.filtPrev = logFilter{}
			m.filtering = false
		}
		m.mode = Normal
		m.searching = false
		m.cmdSuggest = nil
	default:
		msg := key
		if key.Code == 'h' && key.Mod == tea.ModCtrl {
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace} // ctrl+h is backspace
		}
		text, cur, handled, changed := ui.EditKey(msg, m.cmdline, m.cmdCur)
		if !handled {
			break
		}
		m.cmdline, m.cmdCur = text, cur
		if changed {
			// Editing leaves history recall: the text is the live line now,
			// and up restarts the walk from the newest entry (#1171).
			m.cmdHistIdx = -1
			switch {
			case m.searching:
				m.searchPreview()
			case m.filtering:
				m.followFilterPreview() // #2255: the stream narrows as typed
			default:
				m.refreshCmdlineSuggest()
			}
		}
	}
	return m, nil
}

// toggleSearchCase flips the effective case mode of the open search line
// (#1111) by rewriting its leading marker, so the mode is always visible in
// the query itself: forced-insensitive shows \c, forced-exact shows \C.
// From the unmarked state the toggle forces the opposite of what the
// editor.search_ignore_case setting yields — with the setting off that is
// \c (smartcase → insensitive), with it on \C (insensitive → exact).
func (m *Model) toggleSearchCase() {
	switch {
	case strings.HasPrefix(m.cmdline, `\c`):
		m.cmdline = m.cmdline[2:]
		m.cmdCur = max(0, m.cmdCur-2)
		if m.searchIgnoreCase {
			// Removing \c alone would fall back to the insensitive default;
			// the toggle must land on the sensitive side.
			m.cmdline = `\C` + m.cmdline
			m.cmdCur += 2
		}
	case strings.HasPrefix(m.cmdline, `\C`):
		m.cmdline = `\c` + m.cmdline[2:]
	case m.searchIgnoreCase:
		m.cmdline = `\C` + m.cmdline
		m.cmdCur += 2
	default:
		m.cmdline = `\c` + m.cmdline
		m.cmdCur += 2
	}
	m.cmdPreview()
}

// toggleFilterRegex flips the "\v" regex marker on the open follow-filter
// line (#2255), leaving the case markers alone.
func (m *Model) toggleFilterRegex() {
	if strings.HasPrefix(m.cmdline, `\v`) {
		m.cmdline = m.cmdline[2:]
		m.cmdCur = max(0, m.cmdCur-2)
	} else {
		m.cmdline = `\v` + m.cmdline
		m.cmdCur += 2
	}
	m.cmdPreview()
}

// cmdPreview re-runs the open command line's live preview — the search
// incsearch, or the follow filter (#2255).
func (m *Model) cmdPreview() {
	if m.filtering {
		m.followFilterPreview()
		return
	}
	m.searchPreview()
}

// compileSearchLine compiles the open search line: a leading "\j" marker
// selects the structural jq mode (#2363), whose pattern is a jq query over
// the buffer's JSON/YAML document; anything else is the text search. The
// structural query evaluates immediately against the current document
// version, so its matches (or its error) are ready for the preview.
func (m Model) compileSearchLine() search.Query {
	if prog, ok := strings.CutPrefix(m.cmdline, `\j`); ok {
		q := search.CompileStructural(m.docPathLang(), prog)
		q.SyncStructural(m.buf, m.docVersion)
		return q
	}
	return search.Compile(m.parseSearchPattern(m.cmdline))
}

// toggleSearchStructural flips the structural-mode marker "\j" on the open
// search line (#2363), the way ctrl+c flips the case marker: the marker is
// the state. Toggling on in a buffer without a JSON/YAML document is allowed —
// the inline hint reports why the query cannot run.
func (m *Model) toggleSearchStructural() {
	if strings.HasPrefix(m.cmdline, `\j`) {
		m.cmdline = m.cmdline[2:]
		m.cmdCur = max(0, m.cmdCur-2)
	} else {
		m.cmdline = `\j` + m.cmdline
		m.cmdCur += 2
	}
	m.cmdPreview()
}

// parseSearchPattern splits the typed line into pattern, regex flag and case
// mode. Leading markers, in any order: "\v" enables regex (very-magic
// toggle), "\c" forces case-insensitive matching, "\C" forces exact matching
// (#1111). Without a case marker the editor.search_ignore_case setting picks
// insensitive, else smartcase (#257) applies.
func (m Model) parseSearchPattern(line string) (string, bool, search.Case) {
	regex := false
	cs := search.CaseSmart
	if m.searchIgnoreCase {
		cs = search.CaseFold
	}
	for {
		switch {
		case strings.HasPrefix(line, `\v`):
			regex = true
		case strings.HasPrefix(line, `\c`):
			cs = search.CaseFold
		case strings.HasPrefix(line, `\C`):
			cs = search.CaseExact
		default:
			return line, regex, cs
		}
		line = line[2:]
	}
}

// searchPreview recompiles the half-typed pattern and moves to the nearest
// match from the search origin, vim's incsearch (#255). No match (or an empty
// pattern) parks the cursor back at the origin; nothing lands on the nav
// stack — only the committed jump does.
func (m *Model) searchPreview() {
	m.preview = m.compileSearchLine()
	if !m.preview.Empty() {
		if p, ok := m.preview.Next(m.buf, m.searchOrigin, m.searchDir, 1); ok {
			m.cursor = p
			m.desiredCol = p.Col
			m.scroll()
			return
		}
	}
	m.restoreSearchOrigin()
}

// cancelSearch abandons the search line: cursor and viewport return exactly to
// where they were when the search opened; the previously committed query (and
// its n/N state) is untouched.
func (m *Model) cancelSearch() {
	m.preview = search.Query{}
	m.restoreSearchOrigin()
}

// restoreSearchOrigin puts cursor and viewport back at their captured state.
func (m *Model) restoreSearchOrigin() {
	m.cursor = m.buf.ClampCursor(m.searchOrigin)
	m.desiredCol = m.cursor.Col
	m.view.Top, m.view.Left = m.searchOrigTop, m.searchOrigLft
	m.scroll()
}

// commitSearch installs the previewed pattern as the active query and jumps to
// the first match from the origin. Zero matches leave a "no matches" report on
// the ex line and restore the origin.
func (m *Model) commitSearch() {
	m.preview = m.compileSearchLine()
	m.query = m.preview
	m.preview = search.Query{}
	if m.query.Empty() {
		m.restoreSearchOrigin()
		return
	}
	if err := m.query.StructuralErr(); err != "" {
		// A structural query that cannot run reports why (#2363) instead of
		// committing zero silent matches; nothing is left for n/N to repeat.
		m.query = search.Query{}
		m.hlActive = false
		m.cmdMsg = "E: " + err
		m.restoreSearchOrigin()
		return
	}
	p, ok := m.query.Next(m.buf, m.searchOrigin, m.searchDir, 1)
	if !ok {
		m.hlActive = false
		m.cmdMsg = "no matches: " + m.query.Pattern
		m.restoreSearchOrigin()
		return
	}
	m.hlActive = true
	if wrapped(m.searchOrigin, p, m.searchDir) {
		m.cmdMsg = "search wrapped"
	}
	m.cursor = m.searchOrigin // the jump departs from the origin, not the preview
	m.jumpTo(p)               // the initial /-search landing is a jump (Roadmap 0220)
}

// SetHistories injects the app-owned persistent query-history store (#1171):
// the "/" "?" line and the ":" line each recall from (and push to) their own
// named bucket. Nil (the default) disables recall, so the editor runs
// standalone in tests without touching the state store.
func (m *Model) SetHistories(h *histories.Store) { m.histStore = h }

// cmdHistBucket names the open command line's history bucket: the search
// line ("/" and "?") shares one bucket, the ex line has its own — separate
// recall lists like vim's / and : histories.
func (m Model) cmdHistBucket() string {
	switch {
	case m.filtering:
		return histories.FollowFilter // #2255
	case m.searching:
		return histories.Search
	}
	return histories.Ex
}

// recallHistory walks the command line through the bucket's entries (#1171):
// dir>0 moves to older queries (up), dir<0 back toward the live line (down).
// A recall replaces the input with the cursor at the end; the stashed live
// line comes back when the walk returns past the newest entry. Recalled text
// re-runs the incremental search preview / ex-line suggestions, so a recall
// behaves exactly like having typed the query.
func (m *Model) recallHistory(dir int) {
	if m.histStore == nil {
		return
	}
	entries := m.histStore.All(m.cmdHistBucket())
	if len(entries) == 0 {
		return
	}
	next := m.cmdHistIdx + dir
	if next < -1 {
		next = -1
	}
	if next >= len(entries) {
		next = len(entries) - 1
	}
	if next == m.cmdHistIdx {
		return
	}
	if m.cmdHistIdx == -1 {
		m.cmdHistLive = m.cmdline
	}
	m.cmdHistIdx = next
	if next == -1 {
		m.cmdline = m.cmdHistLive
	} else {
		m.cmdline = entries[next]
	}
	m.cmdCur = len([]rune(m.cmdline))
	switch {
	case m.searching || m.filtering:
		m.cmdPreview()
	default:
		m.refreshCmdlineSuggest()
	}
}

// pushCmdHistory records the committed line in its bucket (#1171). Committing
// always pushes — even a search without matches is worth recalling — and the
// store dedupes and caps. The recall pointer resets so the next up starts at
// this newest entry.
func (m *Model) pushCmdHistory() {
	m.cmdHistIdx = -1
	if m.histStore != nil && m.cmdline != "" {
		m.histStore.Push(m.cmdHistBucket(), m.cmdline)
	}
}

// runExLine parses and executes a ":" command, returning any resulting tea.Cmd.
func (m Model) runExLine() (Model, tea.Cmd) {
	m.pushCmdHistory()
	cmd := excmd.Parse(m.cmdline)
	m.mode = Normal
	m.cmdline = ""
	m.cmdSuggest = nil
	m.cmdMsg = ""
	if cmd.Err != "" {
		m.cmdMsg = "E: " + cmd.Err
		return m, nil
	}

	// A bare range with no command name jumps to the last line of the range.
	if cmd.Name == "" {
		if cmd.Range.Count == 0 {
			return m, nil
		}
		_, end, err := cmd.Range.Resolve(m.exResolver(), m.cursor.Line)
		if err != "" {
			m.cmdMsg = "E: " + err
			return m, nil
		}
		m.moveTo(buffer.Position{Line: end, Col: 0})
		return m, nil
	}

	switch cmd.Name {
	case "w", "write":
		if c, _ := m.saveGuarded(orDefault(cmd.Args, m.path), false); c != nil {
			return m, c
		}
	case "q", "quit":
		return m, func() tea.Msg { return CloseMsg{Force: cmd.Bang} }
	case "wq", "x", "xit":
		c, ok := m.saveGuarded(orDefault(cmd.Args, m.path), true)
		if c != nil {
			return m, c // conflict: prompt first, keep the pane open
		}
		if !ok {
			return m, nil // write failed: stay open, the ex line has the error
		}
		return m, func() tea.Msg { return CloseMsg{} }
	case "e", "edit":
		if cmd.Args != "" {
			if err := m.Load(cmd.Args); err != nil {
				if os.IsNotExist(err) {
					// vim-style: a new path opens as an unsaved buffer, seeded
					// with the language's file template (#170); :w creates it.
					m.NewFile(cmd.Args)
				} else {
					m.cmdMsg = "E: " + err.Error()
				}
			}
		}
	case "g", "global", "v", "vglobal":
		m.cmdMsg = "E: :" + cmd.Name + " is not implemented yet"
	case "s", "substitute":
		return m.substitute(cmd), nil
	case "d", "delete":
		return m.exDelete(cmd), nil
	case "y", "yank":
		return m.exYank(cmd), nil
	case "sor", "sort":
		return m.exSort(cmd), nil
	default:
		switch {
		case isRun(cmd.Name, '>'):
			return m.exIndent(cmd, len(cmd.Name)), nil
		case isRun(cmd.Name, '<'):
			return m.exIndent(cmd, -len(cmd.Name)), nil
		}
		m.cmdMsg = "E: not an editor command: " + cmd.Name
	}
	return m, nil
}

// exResolver captures the editor state the ex range resolver consults: the
// cursor line, the last visual selection bounds, and a line-search hook for
// pattern addresses.
func (m Model) exResolver() excmd.Resolver {
	return excmd.Resolver{
		Buf:         m.buf,
		Current:     m.cursor.Line,
		VisualStart: m.visualStart,
		VisualEnd:   m.visualEnd,
		Search:      m.exSearchLine,
	}
}

// exSearchLine finds the next line (0-based) matching pat as a regex, searching
// from the line after/before `from` and wrapping around the buffer ends. It
// backs the "/pat/" and "?pat?" ex addresses.
func (m Model) exSearchLine(pat string, from int, forward bool) (int, bool) {
	q := search.Compile(pat, true, search.CaseSmart)
	if q.Empty() {
		return 0, false
	}
	n := m.buf.LineCount()
	for i := 1; i <= n; i++ {
		var l int
		if forward {
			l = (from + i) % n
		} else {
			l = ((from-i)%n + n) % n
		}
		if len(q.LineMatches(m.buf, l)) > 0 {
			return l, true
		}
	}
	return 0, false
}

// orDefault returns s when non-empty, else def.
func orDefault(s, def string) string {
	if s != "" {
		return s
	}
	return def
}
