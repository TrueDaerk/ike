package app

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/clipboard"
	"ike/internal/editor"
	"ike/internal/highlight"
	"ike/internal/host"
	"ike/internal/jqplay"
	"ike/internal/keymap"
	"ike/internal/pane"
	"ike/internal/scratch"
	"ike/internal/telemetry"
	"ike/internal/ui"
	"ike/internal/undostore"
)

// playground.go is the UI half of the jq and yq playgrounds (#1936, inline
// since #1970, two dialects since #2039): a query line rendered *inside the
// pane it queries* — a JSON or YAML editor pane, or the HTTP response viewer —
// with the pane's body replaced by a read-only editor holding the live result.
// The evaluation core — parsing, running, error and cap handling, history — is
// internal/jqplay; this file owns the query header, the result buffer, the
// rendering and the key routing.
//
// There is **one** mode, not two. Everything on this side is dialect-neutral
// and reads jqplay.Dialect off the open state: the label on the query line,
// the file extension the result is written under, the fold scan and the
// wording of the messages. That is why the identifiers here say "play" rather
// than "jq" — a yq playground is the same playground with another decoder
// under it, and a second copy of the hosting, the geometry or the key routing
// would be two things to fix for every later bug.
//
// Three deliberate choices:
//
//   - The **input is snapshotted and parsed once** when the playground opens,
//     not re-read per keystroke. A jq program is written against the document
//     that was on screen; re-parsing a 10 MB response on every rune would
//     make the query line stutter for no gain. The one exception is an
//     *external change* to the file the snapshot came from (#2356): the
//     watcher's event re-reads and re-parses it, because a result quietly
//     describing a document that no longer exists is worse than a parse. Not
//     per keystroke, once per detected change — the same debounced,
//     generation-stamped run a program change takes.
//   - Evaluation is **debounced and generation-stamped**. Each program change
//     schedules a tick; only the tick whose generation is still current
//     starts a run, and only the run whose generation is still current
//     installs its result. The superseded run's context is cancelled, so a
//     `repeat(0)` typed halfway through stops instead of spinning.
//   - The result is a **read-only editor buffer** (#1970), not plain text:
//     visual selection, yank, search, mouse scroll and every motion work like
//     in a normal buffer, while the read-only flag (#1762) refuses edits. The
//     original pane content is never touched — leaving the mode simply stops
//     rendering the substitute, so `esc` restores the buffer bit-identically.

// playHeaderRows is the vertical space the inline query header takes at the top
// of the hosting pane with the query on one line: the query line plus the
// input/result/status line. It is fixed — an error appearing must not resize
// the result buffer per keystroke. The expanded query view (#2032) is the one
// thing that grows it, and only on the user's key.
const playHeaderRows = 2

// playInfoRows is the header's second half: the one input/result/status row,
// which the expanded query view does not change.
const playInfoRows = 1

// playMaxQueryRows caps the expanded query view (#2032). A program long enough
// to need more than eight rows would push the result out of sight, which is
// the opposite of what expanding is for; past the cap the view windows around
// the cursor row the way the one-line view windows around the cursor cell.
const playMaxQueryRows = 8

// playMinResultRows is the result buffer the expanded query view must leave
// standing: the playground is still a playground, not a program editor.
const playMinResultRows = 3

// playQueryPrefixW is the width of the query line's `> jq: ` label.
// Continuation rows of the expanded view indent by it, so the program stays in
// one column. Both dialect names are two cells wide, so the window math never
// depends on which playground is open.
const playQueryPrefixW = 6

// playDebounce is how long the query line stays quiet before a program runs. A
// var, not a const, so tests drive the evaluation without sleeping.
var playDebounce = 120 * time.Millisecond

// playState is the open playground. paneKey names the hosting pane and
// resultEd is the substitute read-only editor showing the live result;
// bufFocus routes the keyboard into it (tab toggles). input is the parsed
// snapshot (nil while it is still being parsed, or when parsing failed —
// inputErr then holds the message); source labels where it came from.
// program/pos are the query line, result the last evaluation. hist points at
// the root model's one session-wide program history (#1977) — never a copy,
// so a program recorded here is offered by every later playground whatever
// buffer it opens over — histIdx the position while browsing it (-1 =
// editing a live program) and draft/draftPos the query line that browsing
// started from, so stepping back to -1 restores it (#1973). gen stamps
// debounce ticks and runs so a stale one is dropped, cancel aborts the run in
// flight, and status carries the transient confirmation line. dialect is which
// of the two playgrounds this is (#2039) — the one field the rendering, the
// scratch extension, the fold scan and the filter library all read.
type playState struct {
	dialect  jqplay.Dialect
	paneKey  string
	resultEd *editor.Model
	bufFocus bool

	source string
	srcKey string
	// srcEd / srcInst pin the *document* the playground queries, not just the
	// pane it mounted in (#2355): srcEd is the queried editor model for a
	// buffer source, srcInst the content instance for an HTTP response. The
	// inline mode only renders while its pane still shows that document, so
	// opening another file — or switching tabs — reveals the pane's own
	// content instead of leaving the query header parked over it.
	srcEd    *editor.Model
	srcInst  *pane.Instance
	input    *jqplay.Input
	inputErr string
	parsing  bool

	// srcPath is the file the snapshot is followed on (#2356), empty when
	// there is nothing to follow: an HTTP response, a selection, an unsaved
	// buffer. srcHash is the digest of the text last handed to the parser,
	// so an event whose content did not actually reach the buffer — a dirty
	// buffer marked stale instead of reloaded, auto-reload off, a touch that
	// changed no bytes — costs nothing. reloadedAt stamps the last refresh
	// for the info row: "is this still the file on disk?" is the question the
	// whole refresh exists to answer, and it must survive the next keystroke,
	// which clears the transient status.
	srcPath    string
	srcHash    string
	reloadedAt time.Time
	// pgen stamps input parses the way gen stamps runs: a refresh started
	// while an earlier parse of the same playground is still decoding must
	// win, whichever of the two finishes first.
	pgen int

	program string
	pos     int

	// qgoal is the column a run of vertical motions through the expanded
	// view's rows aims for (#2038), -1 when none is in flight. It is what
	// makes ↑/↓ over a short row return to the column they left, instead of
	// walking left one row at a time; every other key clears it.
	qgoal int

	// expanded is the multi-line view (#2032, edited in since #2038): the
	// query line lays itself out over several wrapped rows instead of one
	// windowed one, and the caret moves through them. Off by default — the
	// one-line header is the resting layout, and growing it costs result rows.
	expanded bool

	// result is the last *successful* evaluation — it is never replaced by a
	// failed one (#2412). A compile or runtime error lands in runErr instead,
	// so the buffer below keeps showing the last output the user could read:
	// mid-typing a query is invalid most of the time (`. | {a, b,`), and
	// blanking the result for every intermediate keystroke defeats the
	// "what was that field called again?" lookup the playground exists for.
	// haveResult records that a good result was installed at all, so the
	// stale banner is only claimed over content that really is one.
	result     jqplay.Result
	runErr     string
	haveResult bool
	pending    bool

	hist     *jqplay.History
	histIdx  int
	draft    string
	draftPos int

	comp *playCompState

	// folds are the result's foldable nodes by header line (#2029), the
	// lookup behind the collapsed placeholder's member count; the ranges
	// themselves live on the result editor.
	folds map[int]jqplay.Fold

	gen    int
	cancel context.CancelFunc
	status string
	// statusWarn renders the status line as a warning rather than a
	// confirmation (#2237): "that key does nothing here" is not good news,
	// and painting it in the same green as "copied the result" would read as
	// if something had happened.
	statusWarn bool
}

// setBufFocus moves the keyboard between the query line and the result
// buffer, keeping the substitute editor's focus flag (cursor cell) in step.
// Leaving the query line drops the completion popup (#1979) — it completes
// typing, and the keyboard just went elsewhere.
func (s *playState) setBufFocus(v bool) {
	s.bufFocus = v
	if v {
		s.comp = nil
	}
	if s.resultEd != nil {
		s.resultEd.SetFocused(v)
	}
}

// startPlayground opens the playground of dialect d over the document at
// hand: for jq the focused HTTP response pane's body, else the focused
// editor's visual selection, else its whole buffer; for yq the editor alone
// (see playSource). atPath picks the seed program: the ordinary open starts
// on `.` — or on this input's last valid program of the session (#1982) — the
// …AtPath open on the caret's document path (#1660). The mode mounts *in* the
// resolved pane (#1970): focus moves there and the pane shows the query header
// plus the read-only result buffer until esc.
//
// Opening while a playground is already up (the Tools menu is one click away
// even then) closes that one first — including a playground of the *other*
// dialect: it is one mode, one pane's content is replaced at a time, and the
// program on its query line belongs in the history like any other (#1977).
func (m *Model) startPlayground(d jqplay.Dialect, atPath bool) tea.Cmd {
	src, ok := m.playSource(d)
	if !ok {
		m.host.Notify(host.Info, playNoSourceMessage(d))
		return nil
	}
	m.closePlayground()
	s := &playState{dialect: d, paneKey: src.paneKey, source: src.label, srcKey: src.key, srcPath: src.path, srcEd: src.ed, srcInst: src.inst, histIdx: -1, qgoal: -1, hist: m.playHist(), program: m.playSeedProgram(d, src, atPath)}
	s.pos = len([]rune(s.program))
	ed := editor.New()
	ed.SetRegisters(m.regs) // app-wide registers (#1540): yanks in the result reach every buffer
	ed.SetPalette(m.themePal)
	ed.Configure(m.host.Config())
	if c := clipboard.System(); c != nil {
		ed.SetClipboard(c)
	}
	ed.ShowReadOnly(d.ResultPath(), "")
	ed.SetFoldSummary(s.playFoldSummary) // the fold placeholder names members, not lines (#2029)
	s.resultEd = &ed
	m.play = s
	m.focusContentAt(src.paneKey, src.tabIdx)
	m.sizePlayResult()
	return m.parsePlayInput(src.text)
}

// playInputSource is one resolved snapshot: the document text, the label naming
// where it came from, the key the session's last valid program is remembered
// under (#1982 — the file path, not the label, so the same file is recognized
// whether the whole buffer or a selection in it was queried), the pane the
// inline mode mounts in (with the tab to activate for a tab-nested response
// viewer, -1 otherwise), and whether it is the *whole* focused buffer — the
// only case in which the caret's document path is a valid program against it.
//
// path is the file the snapshot is *followed* on (#2356) and is set for that
// whole-buffer case alone. A selection is deliberately not followed: after an
// external change the character range it was taken from names a different
// stretch of the file — possibly the middle of another value — so "re-read the
// selection" has no honest answer, and silently re-querying something the user
// never selected is worse than a stale result they can refresh by reopening.
// An HTTP response is not a file at all, and an unsaved buffer has no path the
// watcher reports on.
type playInputSource struct {
	text     string
	label    string
	key      string
	path     string
	paneKey  string
	tabIdx   int
	fullFile bool
	// ed / inst are the queried document itself (#2355): the editor model for
	// a buffer source, the content instance for an HTTP response. Exactly one
	// is set; playState keeps it so the mode can tell "my pane" from "my pane
	// still showing my document".
	ed   *editor.Model
	inst *pane.Instance
}

// playSource resolves what the playground of dialect d queries. For jq the
// focused HTTP response wins over the editor (the pane the user is looking at
// is the one they mean); a visual selection wins over the whole buffer, so a
// single embedded JSON blob in a log file is queryable without extracting it
// first.
//
// The yq playground (#2039) resolves the **editor only**. A response body is
// JSON in every workflow the .http client serves, and letting a focused
// response outrank the YAML file the user has open would answer "yq
// Playground" with a parse error over somebody else's pane. Selection and
// whole buffer work there exactly as they do for jq — a YAML block embedded
// in a Markdown file is queryable by selecting it.
func (m Model) playSource(d jqplay.Dialect) (playInputSource, bool) {
	httpOK := d != jqplay.DialectYQ
	if c := m.focusedContent(); httpOK && c != nil && c.Kind() == pane.KindHTTP {
		// JQInput, not BodyText (#2157): a spooled body is only partly in the
		// pane, and a program written against its head would answer questions
		// about a document that never arrived.
		if c.HTTP().HasBodyText() {
			if body := c.HTTP().JQInput(); strings.TrimSpace(body) != "" {
				paneKey := m.activeWS().Panes.Focused()
				return playInputSource{text: body, label: "HTTP response", key: "http:" + paneKey, paneKey: paneKey, tabIdx: -1, inst: c}, true
			}
		}
	}
	if ed := m.activeEditor(); ed != nil {
		name := playEditorLabel(ed)
		key := m.activeEditorKey()
		docKey := playDocKey(d, ed, key)
		if sel, has := ed.SelectionText(); has && strings.TrimSpace(sel) != "" {
			return playInputSource{text: sel, label: name + " (selection)", key: docKey, paneKey: key, tabIdx: -1, ed: ed}, true
		}
		if body := ed.Text(); strings.TrimSpace(body) != "" {
			return playInputSource{text: body, label: name, key: docKey, path: ed.Path(), paneKey: key, tabIdx: -1, fullFile: true, ed: ed}, true
		}
	}
	// The response pane may be open without being focused — an editor holding
	// the .http file is the usual focus after a dispatch. It may live as a
	// content tab of a tab host (#1778); the mode then activates that tab.
	if hostKey, tabIdx, inst, ok := m.findContent(func(c *pane.Instance) bool {
		return httpOK && c.Kind() == pane.KindHTTP
	}); ok && m.leafVisible(hostKey) {
		if inst.HTTP().HasBodyText() {
			if body := inst.HTTP().JQInput(); strings.TrimSpace(body) != "" {
				return playInputSource{text: body, label: "HTTP response", key: "http:" + hostKey, paneKey: hostKey, tabIdx: tabIdx, inst: inst}, true
			}
		}
	}
	return playInputSource{}, false
}

// playNoSourceMessage says what the dialect looked for and did not find.
func playNoSourceMessage(d jqplay.Dialect) string {
	if d == jqplay.DialectYQ {
		return "yq: no YAML buffer to query"
	}
	return "jq: no JSON buffer or HTTP response to query"
}

// playEditorLabel names the queried buffer for the header line.
func playEditorLabel(ed *editor.Model) string {
	if path := ed.Path(); path != "" {
		return baseName(path)
	}
	return "untitled buffer"
}

// playSeedProgram is the program the query line opens on.
//
// The ordinary open (json.jqPlayground / yaml.yqPlayground) starts on the
// identity program, which pretty-prints the input: most openings only want to
// check something, and a prefilled path had to be deleted before typing
// (#1982). When a valid program was already run against this input during the
// session, that one is offered instead — reopening a file resumes the look
// that was interrupted, which the session-wide history (one shared list, any
// buffer) cannot express.
//
// atPath (…PlaygroundAtPath) is the explicit form of the old default: the
// caret's document path (#1660), so "the value I was looking at" is already a
// program. The jq and yq spellings of that path differ only in how a key
// needing quotes is written (`.["a b"]` vs `."a b"`), and each dialect asks
// for its own. It needs the *whole* focused buffer as the input — against a
// response body or a selection the caret's path indexes the file and would
// name a location the input does not contain — and falls back to the identity
// when the caret has no path.
func (m Model) playSeedProgram(d jqplay.Dialect, src playInputSource, atPath bool) string {
	if atPath {
		if !src.fullFile {
			return "."
		}
		kind := editor.DocPathJQ
		if d == jqplay.DialectYQ {
			kind = editor.DocPathYQ
		}
		if ed := m.activeEditor(); ed != nil {
			if path, ok := ed.DocPath(kind); ok && path != "." {
				return path
			}
		}
		return "."
	}
	if last := m.playLastProgram[src.key]; last != "" {
		return last
	}
	return "."
}

// rememberPlayProgram records program as the input's last valid program of the
// session (#1982), so reopening the playground over the same file offers it
// again. Only a program that actually ran — it compiled and raised no runtime
// error — is worth reoffering; the identity program is the default anyway and
// is not stored, so it never displaces an earlier real program.
func (m *Model) rememberPlayProgram(key, program string) {
	program = strings.TrimSpace(program)
	if key == "" || program == "" || program == "." {
		return
	}
	if m.playLastProgram == nil {
		m.playLastProgram = map[string]string{}
	}
	m.playLastProgram[key] = program
}

// playDocKey identifies the queried document for the per-file last-program
// memory (#1982): its path, so the same file is recognized across reopens and
// across panes. An unsaved buffer has none and falls back to its editor key,
// which lives exactly as long as the buffer does. The dialect is part of the
// key (#2039): the same buffer can be opened in both playgrounds — a YAML file
// after a `to_json`, a JSON one in yq — and the program that was last valid
// there is not the one to offer here.
func playDocKey(d jqplay.Dialect, ed *editor.Model, edKey string) string {
	if path := ed.Path(); path != "" {
		return d.Name() + ":file:" + path
	}
	return d.Name() + ":buf:" + edKey
}

// playOpen reports whether the inline playground is active.
func (m Model) playOpen() bool { return m.play != nil }

// playFocused reports whether the playground's hosting pane holds the focus.
// The mode's keyboard routing is scoped to its own pane (#1980): moving the
// focus elsewhere leaves the playground mounted — query, result and history
// position intact — while the other pane takes keys normally, and returning
// the focus resumes the query line as it was.
// The mode is bound to its *document*, not to the pane alone (#2355): a pane
// that switched to another file takes keys as itself, and the mounted-but-
// hidden playground routes nothing.
func (m Model) playFocused() bool {
	return m.play != nil && m.activeWS().Panes.Focused() == m.play.paneKey && m.playSrcShown()
}

// playSrcShown reports whether the hosting pane still shows the document the
// playground queries (#2355). Before this the mode rendered over whatever the
// pane held, so opening a file into it left the file invisible behind the
// query header and looked like a failed open.
//
// A buffer source matches when the pane's active tab still holds the very
// editor model that was queried, and that model still stands for the same
// document — an editor retargeted to another file (the empty-pane reuse path)
// keeps its pointer but changes its doc key. An HTTP source matches when the
// pane's active content is the response instance that was queried; the
// response has no file document, so it is identified by instance alone.
func (m Model) playSrcShown() bool {
	s := m.play
	if s == nil {
		return false
	}
	inst := m.activeWS().Panes.Get(s.paneKey)
	if inst == nil {
		return false
	}
	if s.srcInst != nil {
		content := inst
		if c := inst.ActiveContent(); c != nil {
			content = c
		}
		return content == s.srcInst
	}
	if s.srcEd == nil || inst.Kind() != pane.KindEditor || inst.ActiveContent() != nil {
		return false
	}
	ed := inst.Editor()
	return ed != nil && ed == s.srcEd && playDocKey(s.dialect, ed, s.paneKey) == s.srcKey
}

// playSrcAlive reports whether the queried document still exists anywhere in
// the workspace (#2355). A playground whose document was closed points at
// nothing and is dropped by syncPlaygroundSource.
func (m Model) playSrcAlive() bool {
	s := m.play
	if s == nil {
		return false
	}
	found := false
	m.contentInstances(func(_ string, _ int, c *pane.Instance) bool {
		if s.srcInst != nil {
			if c == s.srcInst {
				found = true
			}
		} else if c.Kind() == pane.KindEditor {
			for _, ed := range c.Editors() {
				if ed == s.srcEd {
					found = true
					break
				}
			}
		}
		return !found
	})
	return found
}

// syncPlaygroundSource drops a playground whose queried document left the
// workspace (#2355) — a closed tab, a closed pane, a file deleted in the
// explorer. It runs on the settled Update pass, so every close path is
// covered by one hook instead of each one remembering the mode.
func (m *Model) syncPlaygroundSource() {
	if m.play == nil {
		return
	}
	if !m.playSrcAlive() {
		m.closePlayground()
	}
}

// playHist returns the session-wide program history, allocating it on first use.
// New() installs it, but a Model assembled by hand in a test must not panic on
// the first ↑ — and the list is shared, so it may only ever be allocated once.
func (m *Model) playHist() *jqplay.History {
	if m.playHistory == nil {
		m.playHistory = &jqplay.History{}
	}
	return m.playHistory
}

// playInlineActive reports whether the inline playground owns pane key: its
// content is then the query header plus the result buffer, not the pane's own
// component.
// It is bound to the document, not to the pane (#2355): while the pane shows
// something else the playground stays mounted — query, result and history
// position intact, exactly as it survives a focus change (#1980) — but renders
// nothing, and the pane's own content is visible again. Returning to the
// source document brings it back as it was.
func (m Model) playInlineActive(key string) bool {
	return m.play != nil && m.play.paneKey == key && m.playSrcShown()
}

// playHeaderRowsFor is the vertical chrome the query header adds to pane key —
// the breadcrumbRows analogue (#1153) the mouse translation keys off. With the
// expanded query view up (#2032) it grows with the wrapped program, so the
// mouse translation and the result buffer's height follow the header instead
// of assuming the two-row default.
func (m Model) playHeaderRowsFor(key string) int {
	if m.playInlineActive(key) {
		return m.playQueryRowCount() + playInfoRows + m.playStaleRows()
	}
	return 0
}

// playQueryRowCount is how many rows the query occupies: one in the resting
// layout, and in the expanded view (#2032) as many as the wrapped program
// needs — bounded by playMaxQueryRows and by leaving playMinResultRows of result
// standing. Rendering, the mouse translation and the result buffer's height
// all read this one number, so the header can never disagree with the space
// reserved for it.
func (m Model) playQueryRowCount() int {
	s := m.play
	if s == nil {
		return 1
	}
	r, ok := m.lay.Panes[s.paneKey]
	if !ok {
		return 1
	}
	return m.playQueryRowsFor(paneInterior(r.W, paneChromeW))
}

// playQueryRowsFor is playQueryRowCount for a given interior width — the form the
// rendering uses, so the rows drawn are the rows the geometry reserved. The
// height bound still comes from the hosting pane: it is what "leave the result
// standing" is measured against.
func (m Model) playQueryRowsFor(width int) int {
	s := m.play
	if s == nil || !s.expanded {
		return 1
	}
	rows := len(jqplay.Wrap(s.program, m.playQueryWidth(width)))
	limit := playMaxQueryRows
	if r, ok := m.lay.Panes[s.paneKey]; ok {
		if fits := paneInterior(r.H, paneChromeH) - playInfoRows - m.playStaleRows() - playMinResultRows; fits < limit {
			limit = fits
		}
	}
	if rows > limit {
		rows = limit
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

// playQueryWidth is how many runes of the program a query row holds: the pane's
// interior less the label column and the two cells the `…` window markers may
// take. The floor keeps the window math sane on a pane too narrow to render
// into anyway.
func (m Model) playQueryWidth(paneWidth int) int {
	if avail := paneWidth - playQueryPrefixW - 2; avail > 10 {
		return avail
	}
	return 10
}

// togglePlayQueryView switches the expanded query view (#2032) — json.jqQueryView
// from the query line, the result buffer, the palette or the Tools menu. It is
// a pure view state: the program, the cursor and the result are untouched, only
// the header's height and the result buffer's are.
func (m *Model) togglePlayQueryView() {
	s := m.play
	if s == nil {
		return
	}
	s.expanded = !s.expanded
	m.sizePlayResult()
}

// movePlayQueryRow moves the cursor delta rows through the wrapped program of
// the multi-line view (#2038), aiming for column goal — the column the run of
// motions started in, so stepping over a short stage returns to it instead of
// dragging the cursor left one row at a time.
//
// It reports whether it moved: the one-line view, a program that wraps to a
// single row, and the row above the first / below the last all answer false,
// which is what hands ↑/↓ back to the history walk.
func (m *Model) movePlayQueryRow(delta, goal int) bool {
	s := m.play
	if s == nil || !s.expanded {
		return false
	}
	width, ok := m.playPaneQueryWidth()
	if !ok {
		return false
	}
	lines := jqplay.Wrap(s.program, width)
	row, col := jqplay.RowCol(lines, s.pos)
	next := row + delta
	if next < 0 || next >= len(lines) {
		return false
	}
	if goal > col {
		col = goal // an unset goal is -1 and never wins over a real column
	}
	s.pos, s.qgoal = jqplay.PosAt(lines, next, col), col
	s.comp = nil // the popup completes the span the cursor just left
	return true
}

// movePlayQueryEdge puts the cursor on the start or the end of its own row in
// the multi-line view (#2038), reporting whether it applied. With one row
// there is nothing row-local about home/end, so they stay the program's ends.
func (m *Model) movePlayQueryEdge(toEnd bool) bool {
	s := m.play
	if s == nil || !s.expanded {
		return false
	}
	width, ok := m.playPaneQueryWidth()
	if !ok {
		return false
	}
	lines := jqplay.Wrap(s.program, width)
	if len(lines) < 2 {
		return false
	}
	row, _ := jqplay.RowCol(lines, s.pos)
	l := lines[row]
	s.pos, s.comp = l.Start, nil
	if toEnd {
		s.pos = l.End
	}
	return true
}

// playPaneQueryWidth is how many runes of the program one query row holds in the
// hosting pane as it is laid out right now. The key handlers wrap against it,
// so a motion works in exactly the rows that are on screen.
func (m Model) playPaneQueryWidth() (int, bool) {
	s := m.play
	if s == nil {
		return 0, false
	}
	r, ok := m.lay.Panes[s.paneKey]
	if !ok {
		return 0, false
	}
	return m.playQueryWidth(paneInterior(r.W, paneChromeW)), true
}

// sizePlayResult fits the substitute result editor under the query header in
// the hosting pane's interior. Called at open and from layout(), so a resize
// or zoom keeps the buffer in step.
func (m *Model) sizePlayResult() {
	s := m.play
	if s == nil || s.resultEd == nil {
		return
	}
	r, ok := m.lay.Panes[s.paneKey]
	if !ok {
		return
	}
	s.resultEd.SetSize(paneInterior(r.W, paneChromeW), paneInterior(r.H, paneChromeH+m.playQueryRowCount()+playInfoRows+m.playStaleRows()))
}

// closePlayground records the program in the session history, aborts a run
// in flight and drops the inline mode. The hosting pane never held anything
// but its own untouched content, so leaving the mode *is* the restore. The
// history is the root model's shared list (#1977), so reopening offers the
// last programs again — over any buffer, not just the one they were run on.
func (m *Model) closePlayground() {
	if s := m.play; s != nil {
		s.cancelRun()
		s.hist.Add(s.program)
	}
	m.play = nil
}

// leavePlaygroundOnEsc is the mode's esc: it closes the playground *and* arms
// the double-esc palette detector (#2237). Before this the mode swallowed the
// chord — its own esc returned straight from the modal routing, well above the
// detector in Update, so the first press of `esc esc` left the input and the
// second one found nothing armed and the palette stayed shut. Arming here is
// the "chord recognition ahead of the input exit" the issue asks for, in the
// shape the rest of the app already uses (see Update): the single esc keeps
// its immediate meaning — no timeout latency on the common key — and the
// second esc, now landing in a normal focus, opens the palette.
func (m *Model) leavePlaygroundOnEsc() {
	m.closePlayground()
	m.lastEscAt = m.clock()
}

// cancelRun aborts the evaluation in flight, if any.
func (s *playState) cancelRun() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// playParseDoneMsg carries the parsed input snapshot back to the model. It
// names the state it was started for, so a snapshot arriving after the
// playground closed (or reopened over another buffer) is dropped, and carries
// the parse generation it was started under (#2356): with the input re-read
// on external changes, two parses of a big file can be in flight at once, and
// only the newest one may install itself.
type playParseDoneMsg struct {
	st  *playState
	gen int
	in  *jqplay.Input
	err string
}

// playDebounceMsg fires playDebounce after a program change; a stale generation
// means the user kept typing and this tick is not the one to run.
type playDebounceMsg struct {
	st  *playState
	gen int
}

// playEvalDoneMsg carries an off-loop evaluation back to the model.
type playEvalDoneMsg struct {
	st  *playState
	gen int
	res jqplay.Result
}

// parsePlayInput decodes the snapshot: inline for a screenful, off the event
// loop past jqplay.AsyncThreshold so opening the playground over a large
// response cannot stall a frame.
func (m *Model) parsePlayInput(text string) tea.Cmd {
	s := m.play
	if s == nil {
		return nil
	}
	// The digest of what the parser is about to see, recorded at dispatch:
	// the next watcher event compares against it, so a second event carrying
	// the same bytes does not start a second parse (#2356).
	s.srcHash = undostore.Hash([]byte(text))
	// Parses carry their own counter, not the run generation: typing during a
	// long parse bumps gen (the debounce), and a parse dropped over that would
	// never install the input it was started for.
	s.pgen++
	d, gen := s.dialect, s.pgen
	if len(text) <= jqplay.AsyncThreshold {
		in, err := d.Parse(text)
		return m.finishPlayParse(playParseDoneMsg{st: s, gen: gen, in: in, err: errText(err)})
	}
	s.parsing = true
	return func() tea.Msg {
		in, err := d.Parse(text)
		return playParseDoneMsg{st: s, gen: gen, in: in, err: errText(err)}
	}
}

// errText renders err as a message, "" for no error.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// finishPlayParse installs the snapshot and runs the seeded program against it.
// A parse superseded by a newer one is dropped on its generation stamp (#2356).
//
// A snapshot that failed to parse leaves the previous one in place instead of
// clearing it: the input the playground follows can turn unparsable mid-edit
// in the other editor — half a key written, a `,` short — and blanking the
// result for every intermediate save would make the mode unusable next to a
// hand-edited file. The error takes the info row (playErrorLine), so the last
// good result stays on screen *and* is visibly not current.
func (m *Model) finishPlayParse(msg playParseDoneMsg) tea.Cmd {
	s := m.play
	if s == nil || msg.st != s || msg.gen != s.pgen {
		return nil
	}
	s.parsing = false
	s.inputErr = msg.err
	if s.inputErr != "" {
		s.pending = false  // nothing will run against this text; no evaluation is in flight
		m.sizePlayResult() // the stale banner (#2412) costs a row while it is up
		return nil
	}
	m.sizePlayResult()
	s.input = msg.in
	return m.runPlayNow()
}

// schedulePlayEval debounces a program change: the run in flight is abandoned
// and a tick stamped with the new generation is scheduled. Only the tick
// still holding the current generation starts a run.
func (m *Model) schedulePlayEval() tea.Cmd {
	s := m.play
	if s == nil || s.inputErr != "" {
		return nil
	}
	s.cancelRun()
	s.gen++
	s.pending = true
	gen := s.gen
	return tea.Tick(playDebounce, func(time.Time) tea.Msg {
		return playDebounceMsg{st: s, gen: gen}
	})
}

// firePlayDebounce starts the run the tick was scheduled for, unless a newer
// keystroke already superseded it.
func (m *Model) firePlayDebounce(msg playDebounceMsg) tea.Cmd {
	s := m.play
	if s == nil || msg.st != s || msg.gen != s.gen {
		return nil
	}
	return m.runPlay()
}

// runPlayNow skips the debounce — the enter key and the initial evaluation want
// the result immediately, not a tick later.
func (m *Model) runPlayNow() tea.Cmd {
	s := m.play
	if s == nil || s.inputErr != "" || s.input == nil {
		return nil // still parsing, or the input never became one
	}
	s.cancelRun()
	s.gen++
	s.pending = true
	return m.runPlay()
}

// runPlay evaluates the current program off the event loop under a cancellable
// context carrying jqplay.EvalTimeout, so neither a huge result nor a
// non-terminating program can hold the UI.
func (m *Model) runPlay() tea.Cmd {
	s := m.play
	if s == nil || s.input == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), jqplay.EvalTimeout)
	s.cancel = cancel
	program, in, gen := s.program, s.input, s.gen
	return func() tea.Msg {
		defer cancel()
		return playEvalDoneMsg{st: s, gen: gen, res: jqplay.Run(ctx, program, in)}
	}
}

// finishPlayEval installs a result unless a newer generation superseded it, and
// refreshes the read-only buffer showing it; the returned command starts its
// highlight parse. A run that came back clean also makes its program this
// input's last valid one (#1982), which the next open over the same file
// prefills.
func (m *Model) finishPlayEval(msg playEvalDoneMsg) tea.Cmd {
	s := m.play
	if s == nil || msg.st != s || msg.gen != s.gen {
		return nil
	}
	s.pending, s.cancel = false, nil
	if msg.res.Err != "" {
		// A failed run keeps the previous result on screen (#2412): the error
		// takes the info row and the stale banner marks the buffer, but the
		// buffer itself — its text, its scroll position, its find highlights —
		// is left exactly as the last good run left it.
		s.runErr = msg.res.Err
		m.sizePlayResult() // the banner costs a row while it is up
		return nil
	}
	s.runErr = ""
	s.result, s.haveResult = msg.res, true
	m.rememberPlayProgram(s.srcKey, s.program)
	m.sizePlayResult()
	return m.syncPlayResultBuffer()
}

// playStale reports whether the result buffer is showing an output the current
// query or input no longer produces (#2412) — the state the banner names. It
// takes a good result to be stale: with nothing ever installed the buffer is
// empty, and calling that "stale" would describe content that is not there.
func (s *playState) playStale() bool {
	return s != nil && s.haveResult && (s.inputErr != "" || s.runErr != "")
}

// playStaleRows is the vertical space the stale banner takes inside the hosting
// pane. It is part of the header the same way the info row is, so the geometry,
// the mouse translation and the result editor's height all agree on it.
func (m Model) playStaleRows() int {
	if m.play.playStale() {
		return 1
	}
	return 0
}

// playStaleBanner is the one-line "the result below is not current" marker,
// rendered in Warning above the result buffer. The info row already carries the
// message; this says which *buffer* the message is about, which the row cannot.
func (m Model) playStaleBanner(width int) string {
	s := m.play
	what := "the query has an error"
	if s.inputErr != "" {
		what = "the input has an error"
	}
	line := lipgloss.NewStyle().Foreground(m.pal().Warning).Render("stale — " + what + "; showing the last good result")
	return ansi.Truncate(line, width, "…")
}

// syncPlayResultBuffer reinstalls the current result text into the substitute
// editor. ShowReadOnly resets cursor and scroll — the buffer's content just
// changed under them, so a stale position would point at nothing — and with
// them the fold state, which is why the result's own fold ranges (#2029) are
// installed right after: a new query can never leave a fold of the previous
// result behind.
func (m *Model) syncPlayResultBuffer() tea.Cmd {
	s := m.play
	if s == nil || s.resultEd == nil {
		return nil
	}
	s.resultEd.ShowReadOnly(s.dialect.ResultPath(), s.result.Text())
	s.setResultFolds(s.dialect.Folds(s.result.Text()))
	s.resultEd.SetFocused(s.bufFocus)
	return s.resultEd.Reparse()
}

// setResultFolds installs the result's foldable objects and arrays on the
// substitute editor (#2029). The playground computes them itself instead of
// waiting for the Tree-sitter parse: the result window must fold in a
// grammar-less build too, and only the structural scan knows how many members
// a node holds, which is what the placeholder says. The collapsing itself
// stays the editor's — za/zc/zo/zM/zR and every fold-aware motion (#1741)
// work in the result buffer exactly as they do in a file.
func (s *playState) setResultFolds(folds []jqplay.Fold) {
	s.folds = make(map[int]jqplay.Fold, len(folds))
	ranges := make([]highlight.Fold, 0, len(folds))
	for _, f := range folds {
		s.folds[f.HeaderLine] = f
		ranges = append(ranges, highlight.Fold{HeaderLine: f.HeaderLine, EndLine: f.EndLine})
	}
	if s.resultEd != nil {
		s.resultEd.SetHostFolds(ranges)
	}
}

// playFoldSummary is the placeholder a collapsed node renders as: its member
// count in its own unit, `{ ⋯ 3 keys }` rather than the file buffer's
// "⋯ 3 lines" — how big the value is, which is what a reader skimming a
// result wants to know. An unknown header falls back to the editor's default.
func (s *playState) playFoldSummary(header, end int) string {
	f, ok := s.folds[header]
	if !ok || f.EndLine != end {
		return ""
	}
	return f.Label()
}

// updatePlayground consumes every key while the playground's pane is
// focused: the query line owns them by default, the result buffer after tab
// (#1970). The result buffer is refit afterwards because a key may have
// changed the program, and with the expanded query view up (#2032) the header
// then holds a different number of rows than the buffer was sized under.
func (m Model) updatePlayground(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	out, cmd := m.updatePlaygroundKey(msg)
	if mm, ok := out.(Model); ok {
		mm.sizePlayResult()
		return mm, cmd
	}
	return out, cmd
}

// updatePlaygroundKey is the routing itself; updatePlayground wraps it.
func (m Model) updatePlaygroundKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.play
	if s == nil {
		return m, nil
	}
	s.status, s.statusWarn = "", false
	// A goal column survives a *run* of vertical motions only (#2038): it is
	// stashed here and cleared, so every other key re-anchors it on the
	// column the cursor is actually in.
	goal := s.qgoal
	s.qgoal = -1
	// The spatial focus moves (default ctrl+arrows) leave the pane with the
	// playground still mounted (#1980), the way they escape a focused
	// terminal: the mode is scoped to its pane, not to the whole keyboard.
	if dir, ok := m.focusKeys[msg.String()]; ok {
		m.FocusDir(dir)
		return m, nil
	}
	if s.bufFocus {
		return m.updatePlayBufferKey(msg)
	}
	// The copy chord reaches the result buffer's selection even while the
	// query line owns the keyboard (#2062): a drag in the result selects and
	// tab (or a click on the query header) moves the focus back with the
	// selection still on screen, so swallowing the chord here would strand a
	// visible selection — the same shape as the response pane's search prompt
	// in #2051. The chord is no valid query input, and without a selection it
	// falls through to the query line unchanged.
	if m.playCopyChord(msg) {
		if _, has := s.resultEd.SelectionText(); has {
			var cmd tea.Cmd
			*s.resultEd, cmd = s.resultEd.Update(editor.ActionMsg{Action: "copy"})
			return m, cmd
		}
	}
	// The find chord (editor.find, default cmd+f) opens the search in the
	// result buffer straight from the query line (#2383): searching a result
	// used to cost a tab there and a tab back, while cmd+f is the search
	// everywhere else in the IDE. It moves the keyboard into the result
	// buffer with it — the search prompt has to receive the typing, and
	// n/N afterwards belong to the same focus, so the shortcut lands where
	// the searching continues instead of bouncing back after one match.
	if m.playFindChord(msg) {
		return m.beginPlayResultSearch()
	}
	// The open completion popup (#1979) owns its keys first: arrows step it,
	// enter/tab accept, esc dismisses — the query line's own meaning of those
	// keys comes back the moment it closes. Typing falls through and
	// re-filters below.
	if s.comp != nil {
		if handled, cmd := m.playCompletionKey(msg); handled {
			return m, cmd
		}
	}
	switch msg.String() {
	case "esc":
		m.leavePlaygroundOnEsc()
		return m, nil
	case "tab":
		s.setBufFocus(true)
		return m, nil
	case "ctrl+space":
		// The editor's manual completion request: open the popup without a
		// trigger rune, the full builtin list on an empty partial.
		m.refreshPlayCompletion("", true)
		return m, nil
	case "enter":
		s.hist.Add(s.program)
		s.histIdx = -1
		return m, m.runPlayNow()
	case "up":
		// In the multi-line view the arrows are cursor motion first (#2038);
		// they fall through to the history walk at the program's own top and
		// bottom, the way a multi-line shell prompt hands ↑ over once there is
		// no line above. alt+↑/↓ reach the history from anywhere.
		if m.movePlayQueryRow(-1, goal) {
			return m, nil
		}
		return m, m.stepPlayHistory(1)
	case "down":
		if m.movePlayQueryRow(1, goal) {
			return m, nil
		}
		return m, m.stepPlayHistory(-1)
	case "alt+up":
		return m, m.stepPlayHistory(1)
	case "alt+down":
		return m, m.stepPlayHistory(-1)
	case "home", "end":
		// "this line" once there are lines (#2038); in the one-line view
		// ui.EditKey's whole-program ends below still apply.
		if m.movePlayQueryEdge(msg.String() == "end") {
			return m, nil
		}
	case "ctrl+home":
		s.pos, s.comp = 0, nil
		return m, nil
	case "ctrl+end":
		s.pos, s.comp = len([]rune(s.program)), nil
		return m, nil
	case "pgup", "pgdown":
		// Paging works from the query line without moving the focus.
		var cmd tea.Cmd
		*s.resultEd, cmd = s.resultEd.Update(msg)
		return m, cmd
	case "ctrl+y":
		m.copyPlayResult()
		return m, nil
	case "ctrl+o":
		return m.openPlayResultAsScratch()
	case "ctrl+s":
		// Name the program and keep it (#1995) — json.jqSaveFilter's chord.
		m.startPlaySavePrompt()
		return m, nil
	case "ctrl+l":
		// The saved-filter picker (#1995) — json.jqFilters' chord, over this
		// playground's own library (#2039).
		m.openPlayFilterPicker(s.dialect, false)
		return m, nil
	case "ctrl+g":
		// The language cheatsheet (#2382) — json.jqCheatsheet's chord, the
		// library's sibling: ctrl+l is where *your* programs live, ctrl+g
		// where the language does. Opening it leaves the playground mounted,
		// so esc comes back to the query line untouched.
		m.openPlayCheatsheet(s.dialect)
		return m, nil
	}
	out, pos, handled, changed := ui.EditKey(msg, s.program, s.pos)
	if !handled {
		// The code-action chord (alt+enter) is answered before the Global
		// lookup can drop it (#2237): it is bound in the editor context, so
		// nothing here or below claims it and the key used to do a silent
		// nothing. There is no LSP behind a jq program — the honest answer is
		// to say the key is not available here and name what is.
		if m.playCodeActionChord(msg) {
			m.playNoCodeActions()
			return m, nil
		}
		// A key neither the mode nor the query line claims resolves against
		// the Global keymap scope (#1983), so cmd+shift+a (Search Everywhere),
		// cmd+e (Recent Files) and the other IDE-level chords keep working
		// like in any pane. Local keys already returned above, so they keep
		// priority where they collide.
		if ok, cmd := m.playGlobalChord(msg); ok {
			return m, cmd
		}
		return m, nil
	}
	s.program, s.pos = out, pos
	if !changed {
		// A cursor motion under the popup moves away from the span it would
		// replace; it closes rather than accepting at a stale position.
		s.comp = nil
		return m, nil
	}
	s.histIdx = -1
	m.refreshPlayCompletion(msg.Text, false)
	return m, m.schedulePlayEval()
}

// updatePlayBufferKey routes a key into the result buffer: the full editor
// keymap applies (motions, search, visual selection, yank — mutations are
// refused by the read-only flag, #1762). tab returns to the query line; esc
// closes the mode only from resting normal mode, so it first quits a visual
// selection, a search prompt or a pending command like it would in any
// buffer. The result actions (ctrl+y / ctrl+o) shadow the editor's scroll
// and jumplist keys — a throwaway result buffer has no jumplist worth keeping.
func (m Model) updatePlayBufferKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.play
	// The app keymap's copy chord (editor.copy, default cmd+c) copies the
	// visual selection like in any read-only buffer (#1980). Resolved here
	// against the editor context directly: the playground owns the keyboard,
	// so the chord never reaches the keymap layer on its own — and the
	// ActionMsg it dispatches would route to the pane's hidden document
	// editor, not the substitute result buffer.
	if m.playCopyChord(msg) {
		var cmd tea.Cmd
		*s.resultEd, cmd = s.resultEd.Update(editor.ActionMsg{Action: "copy"})
		return m, cmd
	}
	// The find chord does here what "/" does (#2383): the same search, so the
	// chord works from either focus without the user having to remember which
	// one they are in.
	if m.playFindChord(msg) {
		return m.beginPlayResultSearch()
	}
	switch msg.String() {
	case "tab":
		s.setBufFocus(false)
		return m, nil
	case "ctrl+y":
		m.copyPlayResult()
		return m, nil
	case "ctrl+o":
		return m.openPlayResultAsScratch()
	case "ctrl+g":
		// The cheatsheet is reachable from both focuses (#2382): looking the
		// language up is not something to have to tab back for.
		m.openPlayCheatsheet(s.dialect)
		return m, nil
	case "esc":
		if s.resultEd.ModeName() == editor.Normal {
			m.leavePlaygroundOnEsc()
			return m, nil
		}
	}
	// The code-action chord gets the same honest answer here as on the query
	// line (#2237): the result is a read-only throwaway buffer with no
	// language server behind it, so alt+enter has nothing to offer — and
	// saying nothing at all is what made the key feel broken.
	if m.playCodeActionChord(msg) {
		m.playNoCodeActions()
		return m, nil
	}
	// Modified chords the result actions leave over resolve against the
	// Global keymap scope (#1983) before the buffer sees them — the same
	// eligibility rule the main dispatch applies to a capturing editor.
	// Plain and shift-only keys stay with the buffer: they are motions,
	// search input and typed prompt text.
	if k, ok := keymap.FromKeyMsg(msg); ok &&
		(k.Has(keymap.ModCtrl) || k.Has(keymap.ModAlt) || k.Has(keymap.ModMeta)) {
		if handled, cmd := m.playGlobalChord(msg); handled {
			return m, cmd
		}
	}
	var cmd tea.Cmd
	*s.resultEd, cmd = s.resultEd.Update(msg)
	return m, cmd
}

// playCopyChord reports whether msg is the app keymap's editor.copy binding in
// the editor context — the chord that must reach the result buffer's selection
// (#1980) even though the playground's modal routing keeps it from the keymap
// layer. The lookup is against the editor context, not the hosting pane's: the
// substitute buffer is an editor regardless of what pane it is mounted in.
func (m Model) playCopyChord(msg tea.KeyPressMsg) bool {
	return m.playEditorChord(msg, "editor.copy")
}

// playCodeActionChord reports whether msg is the chord bound to lsp.codeAction
// (default alt+enter) — the key the issue found doing a silent nothing in the
// playground (#2237). Like editor.copy it is an editor-context binding, so the
// mode's routing keeps it from the keymap layer and the Global fallback never
// matches it; recognizing it here is what lets the mode answer at all.
func (m Model) playCodeActionChord(msg tea.KeyPressMsg) bool {
	return m.playEditorChord(msg, "lsp.codeAction")
}

// playFindChord reports whether msg is the chord bound to editor.find
// (default cmd+f) — the search key everywhere else in the IDE, which the
// playground's modal routing kept from the keymap layer (#2383). Reading it
// from the live table rather than hard-coding cmd+f means a rebound
// editor.find is the chord that works here too.
func (m Model) playFindChord(msg tea.KeyPressMsg) bool {
	return m.playEditorChord(msg, "editor.find")
}

// beginPlayResultSearch opens the result buffer's search prompt and leaves the
// keyboard there (#2383). Called from both focuses: from the query line it
// moves the focus first — the prompt needs the keys, and so do n/N after it —
// and from the result buffer it is exactly what "/" does. The buffer is
// read-only (#1762), so a search never touches it, and neither the program,
// the history nor the result are involved at all.
func (m Model) beginPlayResultSearch() (tea.Model, tea.Cmd) {
	s := m.play
	if s == nil || s.resultEd == nil {
		return m, nil
	}
	s.setBufFocus(true)
	var cmd tea.Cmd
	*s.resultEd, cmd = s.resultEd.Update(editor.ActionMsg{Action: "find"})
	return m, cmd
}

// playEditorChord reports whether msg is the single-step chord bound to
// command in the editor context. The lookup is against the editor context, not
// the hosting pane's: the playground's buffers are editors regardless of what
// pane the mode is mounted in.
func (m Model) playEditorChord(msg tea.KeyPressMsg, command string) bool {
	if m.bindings == nil || m.bindings.Table() == nil {
		return false
	}
	k, ok := keymap.FromKeyMsg(msg)
	if !ok {
		return false
	}
	chord := keymap.Chord{Steps: []keymap.Key{k}}
	b, found := m.bindings.Table().Lookup(chord, keymap.Context(editor.ContextID))
	return found && b.Command == command
}

// playNoCodeActions is the playground's answer to the code-action chord
// (#2237). Intention actions are an LSP offer over a real document; a jq
// program in a query line and a throwaway read-only result have no server, no
// document and no diagnostics behind them, so there is nothing to offer and
// nothing worth inventing. What the key gets instead is a plain statement that
// it does not apply here plus the two keys that do the closest thing the
// playground actually has — completion and the saved-filter library — which is
// the whole difference between "not available" and "apparently broken".
func (m *Model) playNoCodeActions() {
	s := m.play
	if s == nil {
		return
	}
	s.status = "no code actions in the playground — ctrl+space completes, ctrl+g is the cheatsheet, ctrl+l opens saved filters"
	s.statusWarn = true
}

// playGlobalChord resolves a single-step chord against the Global scope of the
// live binding table and dispatches its command (#1983). The playground owns
// the keyboard while its pane is focused, so without this a chord like
// cmd+shift+a would never reach the keymap layer. Only keys the playground
// leaves over get here — its own keys keep priority — and only Global-scope
// bindings fire: a pane-scoped binding belongs to the pane the mode replaces,
// not to the playground. Multi-step chords cannot resolve without buffering
// query input and are left alone, the same trade the terminal makes (#805).
func (m Model) playGlobalChord(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.bindings == nil || m.bindings.Table() == nil {
		return false, nil
	}
	k, ok := keymap.FromKeyMsg(msg)
	if !ok {
		return false, nil
	}
	chord := keymap.Chord{Steps: []keymap.Key{k}}
	b, found := m.bindings.Table().Lookup(chord, keymap.Global)
	if !found {
		return false, nil
	}
	if c, okc := m.reg.Command(b.Command); okc {
		return true, m.dispatchCommandFrom(b.Command, c, telemetry.SourceKeybind)
	}
	return false, nil
}

// pastePlayground inserts a bracketed paste into the query line, flattened:
// the query line is one line, and a pasted multi-line program would otherwise
// smuggle newlines into it. The result buffer never takes a paste — it is
// read-only.
func (m *Model) pastePlayground(text string) tea.Cmd {
	s := m.play
	if s == nil || text == "" {
		return nil
	}
	out, pos, changed := ui.PasteText(s.program, s.pos, text)
	if !changed {
		return nil
	}
	s.program, s.pos, s.histIdx = out, pos, -1
	s.comp = nil         // a paste is not typing: no popup over it (the editor's rule)
	s.setBufFocus(false) // pasting a program is query-line work
	m.sizePlayResult()   // a longer program is a taller expanded header (#2032)
	return m.schedulePlayEval()
}

// stepPlayHistory walks the session program history (delta +1 = older).
//
// Two details make it feel like a history instead of a dead key (#1973):
//
//   - The query line browsing started from is kept as the draft, so stepping
//     back to -1 restores the half-written program instead of clearing the
//     line. Any edit resets histIdx, so the next walk captures a fresh draft.
//   - A newest entry that merely repeats the query line is skipped on the way
//     out. Both commit points leave exactly that state — enter keeps the
//     program it just recorded, and reopening over the same caret seeds the
//     path that was last run — so without the skip the first ↑ would change
//     nothing at all.
func (m *Model) stepPlayHistory(delta int) tea.Cmd {
	s := m.play
	next := s.histIdx + delta
	if s.histIdx == -1 && delta > 0 {
		s.draft, s.draftPos = s.program, s.pos
		if p, ok := s.hist.At(next); ok && p == strings.TrimSpace(s.program) {
			next++
		}
	}
	if next < -1 {
		next = -1
	}
	if next == s.histIdx {
		return nil // already live / at the oldest entry: nothing to restore
	}
	if next >= s.hist.Len() {
		return nil
	}
	if next == -1 {
		s.histIdx, s.program, s.pos = -1, s.draft, s.draftPos
		return m.runPlayNow()
	}
	program, ok := s.hist.At(next)
	if !ok {
		return nil
	}
	s.histIdx, s.program, s.pos = next, program, len([]rune(program))
	return m.runPlayNow()
}

// playPaneClick handles a mouse press inside the hosting pane: a press on the
// query header returns the focus to the query line, a press in the body moves
// the caret in the result buffer and arms the selection drag, and the
// scrollbar column outranks the content like in any editor pane (#1022).
// x/y are content-local, so the header rows sit at negative y.
func (m Model) playPaneClick(key string, msg mouseEvent, x, y int) (tea.Model, tea.Cmd) {
	s := m.play
	ed := s.resultEd
	if y < 0 {
		if msg.Button == tea.MouseLeft {
			s.setBufFocus(false)
			m.clickPlayQueryRow(x, y)
		}
		return m, nil
	}
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	if ed.ScrollbarHit(x, y) {
		if ed.ScrollbarPress(y) {
			m.drag = &dragState{kind: dragEditScroll, srcPane: key, curX: msg.X, curY: msg.Y}
		}
		return m, nil
	}
	s.setBufFocus(true)
	ed.MouseClick(x, y)
	m.drag = &dragState{kind: dragEditSelect, srcPane: key, curX: msg.X, curY: msg.Y}
	return m, nil
}

// clickPlayQueryRow puts the caret on the clicked cell of the query header — in
// the multi-line view the way to reach a stage far down a long pipeline
// without walking there (#2038), and in the one-line view a click into the
// visible window. x/y are content-local, so the header sits at negative y with
// the query rows first and the info row last; a click on the info row or left
// of the `jq:` label only returns the focus to the query line.
func (m *Model) clickPlayQueryRow(x, y int) {
	s := m.play
	r, ok := m.lay.Panes[s.paneKey]
	if !ok {
		return
	}
	width := paneInterior(r.W, paneChromeW)
	lines, rows, start := m.playQueryWindow(width)
	idx := y + rows + playInfoRows // the clicked query row, 0-based
	col := x - playQueryPrefixW
	if idx < 0 || idx >= rows || col < 0 {
		return
	}
	s.comp = nil
	if rows <= 1 {
		s.pos = playOneLinePos(s.program, s.pos, m.playQueryWidth(width), col)
		return
	}
	if idx == 0 && start > 0 {
		col-- // the row's leading `…` marker is not program text
	}
	if line := start + idx; line < len(lines) {
		s.pos = jqplay.PosAt(lines, line, col)
		return
	}
	s.pos = lines[len(lines)-1].End // a blank row below a short program
}

// playOneLinePos maps a column of the one-line query row back onto a rune
// position, mirroring playHighlighted's cursor window — its start offset and the
// leading `…` cell that stands for the runes scrolled off to the left.
func playOneLinePos(program string, pos, width, col int) int {
	r := []rune(program)
	start := 0
	if pos >= width {
		start = pos - width + 1
	}
	if start > 0 {
		col--
	}
	if col < 0 {
		col = 0
	}
	if p := start + col; p < len(r) {
		return p
	}
	return len(r)
}

// dragEditor resolves the editor a selection or scrollbar drag targets in
// pane key: the inline playground result buffer when the mode owns that pane (#1970)
// — which may be an HTTP pane with no editor of its own — else the pane's
// active document editor.
func (m Model) dragEditor(key string) *editor.Model {
	if s := m.play; s != nil && s.paneKey == key {
		return s.resultEd
	}
	if inst := m.activeWS().Panes.Get(key); inst != nil {
		return inst.Editor()
	}
	return nil
}

// copyPlayResult writes the whole result — not just the visible window — to the
// system clipboard.
func (m *Model) copyPlayResult() {
	s := m.play
	text := s.result.Text()
	if text == "" {
		s.status = "nothing to copy — the result is empty"
		return
	}
	m.copyToClipboard(text)
	s.status = "copied the result (" + strconv.Itoa(len(s.result.Outputs)) + " value(s))"
	m.host.Notify(host.Info, "copied the "+s.dialect.Name()+" result")
}

// openPlayResultAsScratch writes the result into a fresh scratch — `.json` for
// jq, `.yaml` for yq (#2039) — and opens it through the standard funnel, so
// highlighting, folding and the path breadcrumb all apply, and the playground
// can be run again over the result, which is how a multi-step session actually
// goes.
func (m Model) openPlayResultAsScratch() (tea.Model, tea.Cmd) {
	s := m.play
	text := s.result.Text()
	if text == "" {
		s.status = "nothing to open — the result is empty"
		return m, nil
	}
	path, err := scratch.Create(s.dialect.Ext())
	if err != nil {
		s.status = "scratch: " + err.Error()
		return m, nil
	}
	if err := os.WriteFile(path, []byte(text+"\n"), 0o644); err != nil {
		s.status = "scratch: " + err.Error()
		return m, nil
	}
	m.closePlayground()
	return m.openPath(path, false)
}

// playInlineBody renders the hosting pane's content: the query rows, the info
// row and the result buffer underneath, filling the pane's interior exactly.
// The query row count comes from playQueryRowCount, the same number the pane
// geometry reserved.
func (m Model) playInlineBody(width int) string {
	s := m.play
	if s == nil {
		return ""
	}
	if width < 20 {
		width = 20
	}
	body := strings.Join(m.playQueryRows(width), "\n") + "\n" + m.playInfoRow(width) + "\n"
	if s.playStale() {
		body += m.playStaleBanner(width) + "\n"
	}
	return body + s.resultEd.View()
}

// playInfoRow is the header's second line: an error beats a transient status
// beats the input/result summary with the key hints. One fixed row — the
// buffer below must not resize when an error appears mid-keystroke.
//
// The row is composed of styled segments (#1978): caps render in Warning so
// a capped run is not the dimmest thing on the row, a runtime error keeps
// the count of values produced before it, and the key hints are dropped as
// whole `·`-separated segments on a narrow pane instead of being cut
// mid-word. Truncation is cell-aware (ansi.Truncate), so a wide glyph in the
// source label cannot overflow the row.
func (m Model) playInfoRow(width int) string {
	s := m.play
	pal := m.pal()
	hint := lipgloss.NewStyle().Foreground(pal.Hint)
	if err := m.playErrorLine(); err != "" {
		line := lipgloss.NewStyle().Foreground(pal.Error).Render("E: " + err)
		// The buffer below is the last good result, not this run's partial
		// output (#2412) — say which one the reader is looking at rather than
		// counting values the failed run never installed.
		if s.playStale() {
			line += hint.Render(fmt.Sprintf(" · showing the last good result (%d value(s))", len(s.result.Outputs)))
		}
		return ansi.Truncate(line, width, "…")
	}
	if s.status != "" {
		col := pal.Success
		if s.statusWarn {
			col = pal.Warning
		}
		return ansi.Truncate(lipgloss.NewStyle().Foreground(col).Render(s.status), width, "…")
	}
	line := m.playInputSegment()
	if res := m.playResultSegment(); res != "" {
		line += hint.Render(" · ") + res
	}
	if m.playQueryCut(width) {
		// The `…` at the row's edge alone is too easy to miss (#2032): say
		// that the program on screen is not all of it, in the same Warning the
		// other "you are seeing less than there is" markers use.
		line += lipgloss.NewStyle().Foreground(pal.Warning).Render(" · query cut")
	}
	for _, h := range m.playHints() {
		seg := hint.Render(" · " + h)
		if ansi.StringWidth(line)+ansi.StringWidth(seg) > width {
			break
		}
		line += seg
	}
	return ansi.Truncate(line, width, "…")
}

// playHints is the key-hint tail of the info row, one segment per hint so a
// narrow pane drops trailing hints whole rather than clipping one mid-word.
// The query-view toggle (#2032) is named with the chord actually bound to
// json.jqQueryView, so a rebind renames the hint with it, and it sits early in
// the list: a program that does not fit the row is exactly the situation in
// which the hint has to survive a narrow pane.
func (m Model) playHints() []string {
	s := m.play
	view := m.playQueryViewChord() + " full query"
	if s.expanded {
		view = m.playQueryViewChord() + " one-line query"
	}
	if s.bufFocus {
		// za/zM/zR are the editor's own fold keys (#1741), listed here
		// because folding a big result (#2029) is the reason to be in the
		// buffer at all — and nothing else on the row advertises them.
		return []string{"tab query line", "za fold", "zM/zR fold all", view, "ctrl+g cheatsheet", "ctrl+y copy", "ctrl+o scratch", "esc close", playHelpHint}
	}
	// The arrows change meaning with the view (#2038), so the hints say which
	// one is in front of the user: rows to walk, or the history.
	hist := []string{"↑/↓ history"}
	if s.expanded {
		hist = []string{"↑/↓ lines", "alt+↑/↓ history"}
	}
	out := []string{"tab result", "enter run", view}
	out = append(out, hist...)
	return append(out, "ctrl+s save filter", "ctrl+l filters", "ctrl+g cheatsheet", "ctrl+y copy", "ctrl+o scratch", "esc close", playHelpHint)
}

// playHelpHint is the hint tail's last segment (#2237): the one key that lists
// every other one. It sits last on purpose — the hints drop from the right on
// a narrow pane, and this is the segment a user needs least once they know it.
// f1 is Global-scope (palette.keymapHelp), so it already reaches the cheatsheet
// through playGlobalChord from both the query line and the result buffer; the
// mode simply never said so.
const playHelpHint = "f1 keys"

// playQueryViewChord names the chord bound to json.jqQueryView (#2032) for the
// hints, resolved against the live binding table so a rebind is reflected
// rather than a hard-coded default being advertised. With the command unbound
// — a user may unbind it — the palette is what is left to name.
func (m Model) playQueryViewChord() string {
	if m.bindings != nil && m.bindings.Table() != nil {
		for _, b := range m.bindings.Table().Bindings() {
			if b.Command == "json.jqQueryView" {
				return b.Chord.String()
			}
		}
	}
	return "palette"
}

// playQueryCut reports whether the query rows are showing less than the program
// holds: the one-line view windowed around the cursor, or an expanded view
// capped at playMaxQueryRows. It drives the info row's marker and is the reason
// the toggle is worth pressing.
func (m Model) playQueryCut(width int) bool {
	s := m.play
	avail := m.playQueryWidth(width)
	if !s.expanded {
		return len([]rune(s.program)) > avail
	}
	return len(jqplay.Wrap(s.program, avail)) > m.playQueryRowsFor(width)
}

// playErrorLine is the message shown on the info row: a bad input beats a bad
// program, because with no parsed input no program could have run.
func (m Model) playErrorLine() string {
	s := m.play
	if s.inputErr != "" {
		return s.inputErr
	}
	return s.runErr
}

// playInputSegment names the queried snapshot: where it came from, how big it
// is and how many top-level values it holds (a `.jsonl` body holds many). A
// truncated read renders its cap in Warning — the user is seeing less than
// the buffer holds, which the row's dim Hint color would undersell.
func (m Model) playInputSegment() string {
	s := m.play
	pal := m.pal()
	hint := lipgloss.NewStyle().Foreground(pal.Hint)
	if s.parsing {
		return hint.Render("Input: " + s.source + " — parsing…")
	}
	if s.input == nil {
		return hint.Render("Input: " + s.source)
	}
	line := fmt.Sprintf("Input: %s — %s", s.source, humanBytes(int64(s.input.Size())))
	if n := s.input.Len(); n > 1 {
		line += fmt.Sprintf(", %d values", n)
	}
	out := hint.Render(line)
	if s.input.Truncated {
		out += lipgloss.NewStyle().Foreground(pal.Warning).Render(fmt.Sprintf(" (first %d only)", jqplay.MaxInputValues))
	}
	// The refresh stamp (#2356) outlives the transient status line, which the
	// next keystroke clears: "is this still the file on disk?" is answered by
	// a time, not by a message that was on screen a minute ago.
	if !s.reloadedAt.IsZero() {
		out += hint.Render(" · reloaded " + s.reloadedAt.Format("15:04:05"))
	}
	return out
}

// playQueryRow renders the query line, windowed around its cursor and colored
// by the jq scanner. While the result buffer holds the keyboard — or the
// focus is on another pane entirely (#1980) — the cursor cell is not drawn
// and the `>` marker is blanked, the same inactive affordance the regex
// tester's and clone prompt's field labels use: an absent cursor alone is
// too subtle a cue that typing goes elsewhere. The label renders in the
// chrome's Secondary either way, marking the row as chrome over the pane
// surface rather than buffer text. Both prefixes are 6 cells, so the window
// math never changes with focus.
func (m Model) playQueryRow(width int) string {
	s := m.play
	pos := s.pos
	prefix := "> " + s.dialect.Name() + ": "
	if s.bufFocus || !m.playFocused() {
		pos = -1
		prefix = "  " + s.dialect.Name() + ": "
	}
	label := lipgloss.NewStyle().Foreground(m.pal().Secondary).Render(prefix)
	return label + m.playHighlighted(s.program, pos, m.playQueryWidth(width))
}

// playQueryRows is the query half of the header: the one windowed row by
// default, the wrapped program over playQueryRowCount rows in the expanded view
// (#2032). Exactly that many rows are always returned — a short program pads
// with blank rows rather than letting the pane's content shrink under the
// geometry the header reserved.
//
// The wrap breaks at `|` boundaries (jqplay.Wrap), so a row holds whole
// pipeline stages wherever the width allows, and every row is highlighted by
// the same scanner the one-line view uses. Rows past the cap are windowed
// around the cursor's row and marked with the `…` the one-line view marks a
// cut with, so the cursor is always on screen and there is always a sign that
// more program exists.
func (m Model) playQueryRows(width int) []string {
	s := m.play
	lines, rows, start := m.playQueryWindow(width)
	if rows <= 1 {
		return []string{m.playQueryRow(width)}
	}
	pal := m.pal()
	label := lipgloss.NewStyle().Foreground(pal.Secondary)
	prefix, pos := "> "+s.dialect.Name()+": ", s.pos
	if s.bufFocus || !m.playFocused() {
		prefix, pos = "  "+s.dialect.Name()+": ", -1
	}
	curRow := -1
	if pos >= 0 {
		curRow, _ = jqplay.RowCol(lines, pos)
	}
	r := []rune(s.program)
	tokens := jqplay.Tokens(s.program)
	styles := m.playKindStyles()
	cursor := lipgloss.NewStyle().Reverse(true)
	out := make([]string, 0, rows)
	for i := 0; i < rows; i++ {
		row := label.Render(prefix)
		if i > 0 {
			row = strings.Repeat(" ", playQueryPrefixW)
		}
		idx := start + i
		if idx >= len(lines) {
			out = append(out, row)
			continue
		}
		if idx == start && start > 0 {
			row += "…"
		}
		l := lines[idx]
		// The cursor may stand in the blanks a stage break dropped, which are
		// on no row at all; it is drawn on this row's first cell then (#2038),
		// which is also where a vertical motion into the row lands.
		cur := pos
		if idx == curRow && cur < l.Start {
			cur = l.Start
		}
		for j := l.Start; j < l.End; j++ {
			if j == cur {
				row += cursor.Render(string(r[j]))
				continue
			}
			row += styles[jqplay.KindAt(tokens, j)].Render(string(r[j]))
		}
		if cur == l.End && idx == curRow {
			row += cursor.Render(" ")
		}
		if idx == start+rows-1 && idx < len(lines)-1 {
			row += "…"
		}
		out = append(out, row)
	}
	return out
}

// playQueryWindow is the multi-line view's layout at a pane interior of width
// cells: the wrapped program, how many of its rows are on screen and the row
// the window starts at. The rendering, the click mapping (#2038) and the
// completion anchor all read it, so the row drawn, the row clicked and the row
// the popup hangs under can never disagree.
//
// The window follows the cursor's row whether or not the cursor is *drawn* —
// the focus moving into the result buffer must not scroll the program away
// under it.
func (m Model) playQueryWindow(width int) (lines []jqplay.Line, rows, start int) {
	s := m.play
	lines = jqplay.Wrap(s.program, m.playQueryWidth(width))
	rows = m.playQueryRowsFor(width)
	if cur, _ := jqplay.RowCol(lines, s.pos); cur >= rows {
		start = cur - rows + 1
	}
	return lines, rows, start
}

// playHighlighted renders the program clipped to width runes around the cursor,
// each rune in its token's color, with the cursor cell reversed; pos < 0
// draws no cursor. It is the windowedInput of clone_prompt.go plus the
// colors: the cursor has to be drawn per rune anyway, so coloring per rune
// costs nothing extra.
func (m Model) playHighlighted(program string, pos, width int) string {
	r := []rune(program)
	if pos > len(r) {
		pos = len(r)
	}
	start := 0
	if pos >= width {
		start = pos - width + 1
	}
	end := min(start+width, len(r))
	tokens := jqplay.Tokens(program)
	styles := m.playKindStyles()
	cursor := lipgloss.NewStyle().Reverse(true)
	var b strings.Builder
	if start > 0 {
		b.WriteString("…")
	}
	for i := start; i < end; i++ {
		cell := string(r[i])
		if i == pos {
			b.WriteString(cursor.Render(cell))
			continue
		}
		b.WriteString(styles[jqplay.KindAt(tokens, i)].Render(cell))
	}
	if pos >= 0 && pos >= end {
		b.WriteString(cursor.Render(" "))
	}
	if end < len(r) {
		b.WriteString("…")
	}
	return b.String()
}

// playKindStyles maps the scanner's kinds onto the theme, once per render
// rather than once per rune. The query line borrows the **chrome** palette
// rather than the editor's capture colors: it is a header row over the pane
// surface, not buffer text, and the chrome slots are the ones every theme
// guarantees to contrast against it.
func (m Model) playKindStyles() map[jqplay.Kind]lipgloss.Style {
	pal := m.pal()
	style := lipgloss.NewStyle()
	return map[jqplay.Kind]lipgloss.Style{
		jqplay.KindPlain:    style.Foreground(pal.Foreground),
		jqplay.KindPath:     style.Foreground(pal.Accent),
		jqplay.KindString:   style.Foreground(pal.Success),
		jqplay.KindNumber:   style.Foreground(pal.Info),
		jqplay.KindKeyword:  style.Foreground(pal.Secondary).Bold(true),
		jqplay.KindFunc:     style.Foreground(pal.Secondary),
		jqplay.KindVariable: style.Foreground(pal.Warning),
		jqplay.KindFormat:   style.Foreground(pal.Warning),
		jqplay.KindOperator: style.Foreground(pal.Hint),
		jqplay.KindComment:  style.Foreground(pal.Hint),
	}
}

// playResultSegment summarises the evaluation: how many values, the caveats
// (capped output, evaluation in flight), or why there is nothing to show.
// While a re-run is pending the previous count stays up with an
// "evaluating…" suffix — replacing it outright made the row shimmer on
// every keystroke, since pending is set the moment the debounce tick is
// scheduled. A zero-value result renders in Warning: the buffer below is
// blank then, and this summary is the only signal that nothing matched. The
// input-error and after-error cases never reach here — playInfoRow's error
// branch owns them.
func (m Model) playResultSegment() string {
	s := m.play
	pal := m.pal()
	hint := lipgloss.NewStyle().Foreground(pal.Hint)
	warn := lipgloss.NewStyle().Foreground(pal.Warning)
	switch {
	case s.parsing:
		return "" // the input segment already says so
	case strings.TrimSpace(s.program) == "":
		return hint.Render("Result — no program yet")
	}
	n := len(s.result.Outputs)
	if s.pending && n == 0 {
		return hint.Render("Result — evaluating…")
	}
	st := hint
	if n == 0 {
		st = warn
	}
	out := st.Render(fmt.Sprintf("Result — %d value(s)", n))
	if s.result.Truncated {
		out += warn.Render(fmt.Sprintf(" (stopped at %d)", jqplay.MaxOutputs))
	}
	if s.pending {
		out += hint.Render(" · evaluating…")
	}
	return out
}
