package httppane

// highlight.go moves the body's syntax pass off the update loop (#2353). A
// response used to be highlighted synchronously inside compose — one large
// minified body froze the whole IDE for as long as the parse ran. Now compose
// only *schedules* the pass: the body shows immediately as plain rows, the
// host runs HighlightCmd on a goroutine (the editor's parseCmd arrangement),
// and the resulting HighlightedMsg colours the rows after the fact. A
// generation counter guards the round trip — every recompose invalidates
// whatever pass is still in flight, so a newer response can never be painted
// with an older response's spans.
//
// Above a configurable size (http.highlight_limit_kb) the pass is skipped
// entirely, with a visible notice row: even off-loop, parsing a huge body
// burns CPU and memory for colours nobody asked to wait for.

import (
	"fmt"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"ike/internal/highlight"
)

// DefaultHighlightLimitKB mirrors the http.highlight_limit_kb default (#2353):
// the largest body the viewer schedules a syntax pass for, in KiB.
const DefaultHighlightLimitKB = 2048

// hlLimitBytes is the active highlight cap in bytes — a package global set
// from the config (like idcolor's enable flag): the pane itself has no config
// plumbing.
var hlLimitBytes atomic.Int64

func init() { hlLimitBytes.Store(DefaultHighlightLimitKB << 10) }

// SetHighlightLimit pushes http.highlight_limit_kb into the pane (#2353);
// values below 1 KiB fall back to the default rather than turning the pass
// off silently — config validation keeps them out anyway.
func SetHighlightLimit(kb int) {
	if kb < 1 {
		kb = DefaultHighlightLimitKB
	}
	hlLimitBytes.Store(int64(kb) << 10)
}

// HighlightLimit is the active cap in bytes.
func HighlightLimit() int { return int(hlLimitBytes.Load()) }

// pendingHighlight is one scheduled syntax pass: the composed body lines, the
// fence tag to parse them under, where the body starts in row coordinates and
// the compose generation the result must still match.
type pendingHighlight struct {
	gen       int
	tag       string
	lines     []string
	bodyStart int
}

// HighlightedMsg carries one finished syntax pass back into the update loop
// (#2353). Gen names the compose it belongs to; the host routes it to the
// pane, which drops a stale one.
type HighlightedMsg struct {
	Gen       int
	Spans     []highlight.Span
	Folds     []highlight.Fold
	BodyStart int
}

// scheduleHighlight records the body's syntax pass for HighlightCmd to run
// off-loop, or appends the cap notice instead when the body is past the
// configured limit. It runs right before the caller appends the body rows, so
// len(m.rows) — notice included — is where the body will start.
func (m *Model) scheduleHighlight(tag string, lines []string) {
	if tag == "" || !highlight.FencedSupported(tag) {
		return
	}
	total := 0
	for _, l := range lines {
		total += len(l) + 1
	}
	if limit := HighlightLimit(); total > limit {
		// The cap is surfaced, never silent (#2353) — same rule as the
		// pretty-print cap: an uncoloured megabyte should say why.
		m.rows = append(m.rows, row{kind: kindWarn,
			text: fmt.Sprintf("(body larger than the %s highlight limit — showing plain text; raise http.highlight_limit_kb in the settings)", byteSize(limit))})
		return
	}
	m.hlPending = &pendingHighlight{gen: m.hlGen, tag: tag, lines: lines, bodyStart: len(m.rows)}
}

// HighlightCmd hands the scheduled syntax pass to the host as a command that
// parses on a goroutine (#2353), nil when nothing is scheduled. The host
// batches it after every call that may have recomposed; the pane's own key
// handler does the same for its local recompose triggers.
func (m *Model) HighlightCmd() tea.Cmd {
	p := m.hlPending
	if p == nil {
		return nil
	}
	m.hlPending = nil
	return func() tea.Msg {
		spans := highlight.HighlightFenced(p.tag, p.lines)
		folds := highlight.FencedFolds(p.tag, p.lines)
		return HighlightedMsg{Gen: p.gen, Spans: spans, Folds: folds, BodyStart: p.bodyStart}
	}
}

// ApplyHighlight paints one finished pass onto the composed rows and reports
// whether it landed. A stale generation — the rows were recomposed while the
// parse ran — is dropped: the newer response wins, never the older colours.
func (m *Model) ApplyHighlight(msg HighlightedMsg) bool {
	if msg.Gen != m.hlGen {
		return false
	}
	m.bodyIx = highlight.NewIndex(msg.Spans)
	m.setFolds(msg.Folds, msg.BodyStart)
	return true
}

// FinishHighlight runs the scheduled pass synchronously and applies it —
// for tests, which want the coloured rows without pumping the command through
// an update loop. Reports whether a pass ran and landed.
func (m *Model) FinishHighlight() bool {
	cmd := m.HighlightCmd()
	if cmd == nil {
		return false
	}
	msg, ok := cmd().(HighlightedMsg)
	return ok && m.ApplyHighlight(msg)
}
