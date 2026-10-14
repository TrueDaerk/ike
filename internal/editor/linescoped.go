package editor

import (
	"sync"

	tea "charm.land/bubbletea/v2"

	"ike/internal/highlight"
	"ike/internal/lang"
	"ike/internal/unihint"
)

// linescoped.go highlights line-oriented formats per visible line (#2996).
//
// An ndjson / jsonl file is a stream of independent JSON documents, one per
// line, and the lines are often thousands of cells long. Parsing the whole
// buffer as one document — the normal parseCmd — costs the file, not the
// viewport: 200 000 lines of 500 bytes take ~40 s of CGo and yield seven
// million spans whose index build alone stalls the loop. A language flagged
// lang.Language.LineScoped is instead parsed one line at a time, and only
// the lines in the parse window: the viewport plus a margin of one screen
// (at least lineScopeMinMargin lines) on each side. The spans of every line
// are cached by the line's text (lineSpanCache), so a scroll re-parses only
// the lines that entered the window and an edit only the line it changed.
//
// The parse result travels as a windowed highlight.SpansMsg; the editor
// records the window it covers and schedules the next parse when the
// viewport leaves it (windowParseCmd, called from Update and from the
// out-of-loop scroll entry points through ScrollCmd). Folds and sticky
// scopes are not collected: a line-long document has nothing multi-line.
//
// The same flag exempts the parse from the large-file cliff (FeatureOff):
// per-line cost is bounded by the pane, so highlighting stays on for an
// ndjson file of any size while LSP sync, the VCS gutter and the rest keep
// their gate.

// lineScopeMinMargin is the smallest margin of extra lines parsed above and
// below the viewport, so a few scroll steps never trigger a parse.
const lineScopeMinMargin = 50

// lineSpanCacheCap bounds the per-view line cache: past it the current
// generation becomes the previous one, still consulted on a miss, and a
// fresh generation starts — two maps, never more.
const lineSpanCacheCap = 4096

// lineParse is one line's cached parse: spans and notes with Line == 0,
// rebased to the buffer line on use.
type lineParse struct {
	spans []highlight.Span
	notes []lang.Note
}

// lineSpanCache memoizes per-line parses keyed by line text. A pointer field
// of the Model (like parseGate), shared by every value copy of a view and
// read from the parse goroutine, hence the lock.
type lineSpanCache struct {
	mu   sync.Mutex
	cur  map[string]lineParse
	prev map[string]lineParse
	// langPath is the path the cached parses resolved their grammar
	// through; a different one (language override, :e) empties the cache.
	langPath string
}

func newLineSpanCache() *lineSpanCache { return &lineSpanCache{cur: map[string]lineParse{}} }

// get returns the cached parse of text, promoting a previous-generation hit.
func (c *lineSpanCache) get(langPath, text string) (lineParse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.langPath != langPath {
		c.langPath = langPath
		c.cur, c.prev = map[string]lineParse{}, nil
		return lineParse{}, false
	}
	if p, ok := c.cur[text]; ok {
		return p, true
	}
	if p, ok := c.prev[text]; ok {
		c.cur[text] = p
		return p, true
	}
	return lineParse{}, false
}

// put stores one line's parse, rotating the generations at the cap.
func (c *lineSpanCache) put(langPath, text string, p lineParse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.langPath != langPath {
		c.langPath = langPath
		c.cur, c.prev = map[string]lineParse{}, nil
	}
	if len(c.cur) >= lineSpanCacheCap {
		c.cur, c.prev = map[string]lineParse{}, c.cur
	}
	c.cur[text] = p
}

// size reports the live entry count (tests).
func (c *lineSpanCache) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.cur) + len(c.prev)
}

// lineScoped reports whether the buffer's language parses per line.
func (m Model) lineScoped() bool {
	l, ok := lang.ByPath(m.langPath())
	return ok && l.LineScoped
}

// parseWindow is the line range [from, to) a line-scoped parse covers for
// the current viewport: the visible rows plus one screen (at least
// lineScopeMinMargin lines) above and below, clamped to the buffer.
func (m Model) parseWindow() (from, to int) {
	h := m.height
	if h < 1 {
		h = 1
	}
	margin := h
	if margin < lineScopeMinMargin {
		margin = lineScopeMinMargin
	}
	n := m.buf.LineCount()
	from = m.view.Top - margin
	if from < 0 {
		from = 0
	}
	to = m.view.Top + h + margin
	if to > n {
		to = n
	}
	if to < from {
		to = from
	}
	return from, to
}

// windowCovered reports whether the last landed line-scoped parse still
// covers the visible rows.
func (m Model) windowCovered() bool {
	if !m.hlWindowed || m.hlVersion != m.docVersion {
		return false
	}
	h := m.height
	if h < 1 {
		h = 1
	}
	top := m.view.Top
	bottom := top + h
	if n := m.buf.LineCount(); bottom > n {
		bottom = n
	}
	return top >= m.hlWinFrom && bottom <= m.hlWinTo
}

// windowParseCmd schedules a line-scoped parse when the viewport has left
// the last parsed window (or none landed yet); nil for every other buffer
// and while the window still covers the screen. Update batches it after
// every message, so a scroll by key re-highlights the new rows; the
// out-of-loop scrolls (wheel, scrollbar) hand it back through ScrollCmd.
func (m *Model) windowParseCmd() tea.Cmd {
	if m.loading != nil || !m.lineScoped() || m.windowCovered() {
		return nil
	}
	return m.parseCmd()
}

// ScrollCmd returns the command a viewport move outside Update needs —
// the line-scoped re-parse of the rows that scrolled in (#2996) — or nil.
// The app batches it after ScrollBy / ScrollbarDrag.
func (m *Model) ScrollCmd() tea.Cmd { return m.windowParseCmd() }

// lineScopedSnapshot builds the parse snapshot of the current window: the
// window's lines only, with the offset the result is rebased to.
func (m Model) lineScopedSnapshot(key, langPath string) parseSnapshot {
	from, to := m.parseWindow()
	lines := make([]string, 0, to-from)
	for i := from; i < to; i++ {
		lines = append(lines, m.buf.Line(i))
	}
	return parseSnapshot{key: key, langPath: langPath, version: m.docVersion, lines: lines, windowed: true, winFrom: from}
}

// parseLineScoped parses one windowed snapshot line by line through the
// cache and returns the rebased, windowed SpansMsg — the body of the
// off-loop command for a line-scoped language.
func parseLineScoped(s parseSnapshot, supported bool, cache *lineSpanCache) highlight.SpansMsg {
	var spans []highlight.Span
	var notes []lang.Note
	one := make([]string, 1)
	for i, text := range s.lines {
		p, ok := lineParse{}, false
		if cache != nil {
			p, ok = cache.get(s.langPath, text)
		}
		if !ok {
			p = parseOneLine(s.langPath, text, supported, one)
			if cache != nil {
				cache.put(s.langPath, text, p)
			}
		}
		line := s.winFrom + i
		for _, sp := range p.spans {
			sp.Line = line
			spans = append(spans, sp)
		}
		for _, n := range p.notes {
			n.Line = line
			notes = append(notes, n)
		}
	}
	return highlight.SpansMsg{Path: s.key, Version: s.version, Spans: spans, Notes: notes,
		Windowed: true, WinFrom: s.winFrom, WinTo: s.winFrom + len(s.lines)}
}

// parseOneLine parses text as a one-line document: the grammar's spans and
// the language's Go spans, its linter and the Unicode hygiene scan — the
// same services the whole-document pass runs, over one line. Scopes and
// folds are dropped. scratch is a reusable one-element slice.
func parseOneLine(langPath, text string, supported bool, scratch []string) lineParse {
	scratch[0] = text
	var p lineParse
	if supported {
		p.spans, _, _ = highlight.HighlightScoped(langPath, scratch)
		p.notes = highlight.Lint(langPath, scratch)
	}
	p.notes = append(p.notes, unihint.Notes(scratch)...)
	return p
}
