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
	"ike/internal/host"
	"ike/internal/jqplay"
	"ike/internal/keymap"
	"ike/internal/pane"
	"ike/internal/scratch"
	"ike/internal/ui"
)

// jqplayground.go is the UI half of the jq playground (#1936, inline since
// #1970): a query line rendered *inside the pane it queries* — a JSON editor
// pane or the HTTP response viewer — with the pane's body replaced by a
// read-only editor holding the live jq result. The evaluation core — parsing,
// running, error and cap handling, history — is internal/jqplay; this file
// owns the query header, the result buffer, the rendering and the key routing.
//
// Three deliberate choices:
//
//   - The **input is snapshotted and parsed once** when the playground opens,
//     not re-read per keystroke. A jq program is written against the document
//     that was on screen; re-parsing a 10 MB response on every rune would
//     make the query line stutter for no gain.
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

// jqHeaderRows is the vertical space the inline query header takes at the top
// of the hosting pane with the query on one line: the query line plus the
// input/result/status line. It is fixed — an error appearing must not resize
// the result buffer per keystroke. The expanded query view (#2032) is the one
// thing that grows it, and only on the user's key.
const jqHeaderRows = 2

// jqInfoRows is the header's second half: the one input/result/status row,
// which the expanded query view does not change.
const jqInfoRows = 1

// jqMaxQueryRows caps the expanded query view (#2032). A program long enough
// to need more than eight rows would push the result out of sight, which is
// the opposite of what expanding is for; past the cap the view windows around
// the cursor row the way the one-line view windows around the cursor cell.
const jqMaxQueryRows = 8

// jqMinResultRows is the result buffer the expanded query view must leave
// standing: the playground is still a playground, not a program editor.
const jqMinResultRows = 3

// jqQueryPrefixW is the width of the query line's `> jq: ` label. Continuation
// rows of the expanded view indent by it, so the program stays in one column.
const jqQueryPrefixW = 6

// jqResultPath is the display path of the substitute result editor. Like an
// archive entry's virtual path (#1762) it is never written to; it exists so
// the language sniff resolves JSON highlighting for the result.
const jqResultPath = "jq result.json"

// jqDebounce is how long the query line stays quiet before a program runs. A
// var, not a const, so tests drive the evaluation without sleeping.
var jqDebounce = 120 * time.Millisecond

// jqPlayState is the open playground. paneKey names the hosting pane and
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
// flight, and status carries the transient confirmation line.
type jqPlayState struct {
	paneKey  string
	resultEd *editor.Model
	bufFocus bool

	source   string
	srcKey   string
	input    *jqplay.Input
	inputErr string
	parsing  bool

	program string
	pos     int

	// expanded is the full-query view (#2032): the query line lays itself out
	// over several wrapped rows instead of one windowed one. Off by default —
	// the one-line header is the resting layout, and growing it costs result
	// rows.
	expanded bool

	result  jqplay.Result
	pending bool

	hist     *jqplay.History
	histIdx  int
	draft    string
	draftPos int

	comp *jqCompState

	gen    int
	cancel context.CancelFunc
	status string
}

// setBufFocus moves the keyboard between the query line and the result
// buffer, keeping the substitute editor's focus flag (cursor cell) in step.
// Leaving the query line drops the completion popup (#1979) — it completes
// typing, and the keyboard just went elsewhere.
func (s *jqPlayState) setBufFocus(v bool) {
	s.bufFocus = v
	if v {
		s.comp = nil
	}
	if s.resultEd != nil {
		s.resultEd.SetFocused(v)
	}
}

// startJQPlayground opens the playground over the JSON at hand: the focused
// HTTP response pane's body, else the focused editor's visual selection, else
// its whole buffer. atPath picks the seed program: the ordinary open starts
// on `.` — or on this input's last valid program of the session (#1982) — the
// json.jqPlaygroundAtPath open on the caret's jq path (#1660). The mode
// mounts *in* the resolved pane (#1970): focus moves there and the pane shows
// the query header plus the read-only result buffer until esc.
//
// Opening while a playground is already up (the Tools menu is one click away
// even then) closes that one first: it is one mode, and the program on its
// query line belongs in the history like any other (#1977).
func (m *Model) startJQPlayground(atPath bool) tea.Cmd {
	src, ok := m.jqSource()
	if !ok {
		m.host.Notify(host.Info, "jq: no JSON buffer or HTTP response to query")
		return nil
	}
	m.closeJQPlayground()
	s := &jqPlayState{paneKey: src.paneKey, source: src.label, srcKey: src.key, histIdx: -1, hist: m.jqHist(), program: m.jqSeedProgram(src, atPath)}
	s.pos = len([]rune(s.program))
	ed := editor.New()
	ed.SetRegisters(m.regs) // app-wide registers (#1540): yanks in the result reach every buffer
	ed.SetPalette(m.themePal)
	ed.Configure(m.host.Config())
	if c := clipboard.System(); c != nil {
		ed.SetClipboard(c)
	}
	ed.ShowReadOnly(jqResultPath, "")
	s.resultEd = &ed
	m.jqPlay = s
	m.focusContentAt(src.paneKey, src.tabIdx)
	m.sizeJQResult()
	return m.parseJQInput(src.text)
}

// jqInputSource is one resolved snapshot: the JSON text, the label naming
// where it came from, the key the session's last valid program is remembered
// under (#1982 — the file path, not the label, so the same file is recognized
// whether the whole buffer or a selection in it was queried), the pane the
// inline mode mounts in (with the tab to activate for a tab-nested response
// viewer, -1 otherwise), and whether it is the *whole* focused buffer — the
// only case in which the caret's document path is a valid program against it.
type jqInputSource struct {
	text     string
	label    string
	key      string
	paneKey  string
	tabIdx   int
	fullFile bool
}

// jqSource resolves what the playground queries. The focused HTTP response
// wins over the editor (the pane the user is looking at is the one they mean);
// a visual selection wins over the whole buffer, so a single embedded JSON
// blob in a log file is queryable without extracting it first.
func (m Model) jqSource() (jqInputSource, bool) {
	if c := m.focusedContent(); c != nil && c.Kind() == pane.KindHTTP {
		if body := c.HTTP().BodyText(); strings.TrimSpace(body) != "" {
			paneKey := m.activeWS().Panes.Focused()
			return jqInputSource{text: body, label: "HTTP response", key: "http:" + paneKey, paneKey: paneKey, tabIdx: -1}, true
		}
	}
	if ed := m.activeEditor(); ed != nil {
		name := jqEditorLabel(ed)
		key := m.activeEditorKey()
		docKey := jqDocKey(ed, key)
		if sel, has := ed.SelectionText(); has && strings.TrimSpace(sel) != "" {
			return jqInputSource{text: sel, label: name + " (selection)", key: docKey, paneKey: key, tabIdx: -1}, true
		}
		if body := ed.Text(); strings.TrimSpace(body) != "" {
			return jqInputSource{text: body, label: name, key: docKey, paneKey: key, tabIdx: -1, fullFile: true}, true
		}
	}
	// The response pane may be open without being focused — an editor holding
	// the .http file is the usual focus after a dispatch. It may live as a
	// content tab of a tab host (#1778); the mode then activates that tab.
	if hostKey, tabIdx, inst, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindHTTP
	}); ok && m.leafVisible(hostKey) {
		if body := inst.HTTP().BodyText(); strings.TrimSpace(body) != "" {
			return jqInputSource{text: body, label: "HTTP response", key: "http:" + hostKey, paneKey: hostKey, tabIdx: tabIdx}, true
		}
	}
	return jqInputSource{}, false
}

// jqEditorLabel names the queried buffer for the header line.
func jqEditorLabel(ed *editor.Model) string {
	if path := ed.Path(); path != "" {
		return baseName(path)
	}
	return "untitled buffer"
}

// jqSeedProgram is the program the query line opens on.
//
// The ordinary open (json.jqPlayground) starts on the identity program, which
// pretty-prints the input: most openings only want to check something, and a
// prefilled path had to be deleted before typing (#1982). When a valid
// program was already run against this input during the session, that one is
// offered instead — reopening a file resumes the look that was interrupted,
// which the session-wide history (one shared list, any buffer) cannot express.
//
// atPath (json.jqPlaygroundAtPath) is the explicit form of the old default:
// the caret's jq path (#1660), so "the value I was looking at" is already a
// program. It needs the *whole* focused buffer as the input — against a
// response body or a selection the caret's path indexes the file and would
// name a location the input does not contain — and falls back to the identity
// when the caret has no path.
func (m Model) jqSeedProgram(src jqInputSource, atPath bool) string {
	if atPath {
		if !src.fullFile {
			return "."
		}
		if ed := m.activeEditor(); ed != nil {
			if path, ok := ed.DocPath(editor.DocPathJQ); ok && path != "." {
				return path
			}
		}
		return "."
	}
	if last := m.jqLastProgram[src.key]; last != "" {
		return last
	}
	return "."
}

// rememberJQProgram records program as the input's last valid program of the
// session (#1982), so reopening the playground over the same file offers it
// again. Only a program that actually ran — it compiled and raised no runtime
// error — is worth reoffering; the identity program is the default anyway and
// is not stored, so it never displaces an earlier real program.
func (m *Model) rememberJQProgram(key, program string) {
	program = strings.TrimSpace(program)
	if key == "" || program == "" || program == "." {
		return
	}
	if m.jqLastProgram == nil {
		m.jqLastProgram = map[string]string{}
	}
	m.jqLastProgram[key] = program
}

// jqDocKey identifies the queried document for the per-file last-program
// memory (#1982): its path, so the same file is recognized across reopens and
// across panes. An unsaved buffer has none and falls back to its editor key,
// which lives exactly as long as the buffer does.
func jqDocKey(ed *editor.Model, edKey string) string {
	if path := ed.Path(); path != "" {
		return "file:" + path
	}
	return "buf:" + edKey
}

// jqPlayOpen reports whether the inline playground is active.
func (m Model) jqPlayOpen() bool { return m.jqPlay != nil }

// jqPlayFocused reports whether the playground's hosting pane holds the focus.
// The mode's keyboard routing is scoped to its own pane (#1980): moving the
// focus elsewhere leaves the playground mounted — query, result and history
// position intact — while the other pane takes keys normally, and returning
// the focus resumes the query line as it was.
func (m Model) jqPlayFocused() bool {
	return m.jqPlay != nil && m.activeWS().Panes.Focused() == m.jqPlay.paneKey
}

// jqHist returns the session-wide program history, allocating it on first use.
// New() installs it, but a Model assembled by hand in a test must not panic on
// the first ↑ — and the list is shared, so it may only ever be allocated once.
func (m *Model) jqHist() *jqplay.History {
	if m.jqHistory == nil {
		m.jqHistory = &jqplay.History{}
	}
	return m.jqHistory
}

// jqInlineActive reports whether the inline playground owns pane key: its
// content is then the query header plus the result buffer, not the pane's own
// component.
func (m Model) jqInlineActive(key string) bool {
	return m.jqPlay != nil && m.jqPlay.paneKey == key
}

// jqHeaderRowsFor is the vertical chrome the query header adds to pane key —
// the breadcrumbRows analogue (#1153) the mouse translation keys off. With the
// expanded query view up (#2032) it grows with the wrapped program, so the
// mouse translation and the result buffer's height follow the header instead
// of assuming the two-row default.
func (m Model) jqHeaderRowsFor(key string) int {
	if m.jqInlineActive(key) {
		return m.jqQueryRowCount() + jqInfoRows
	}
	return 0
}

// jqQueryRowCount is how many rows the query occupies: one in the resting
// layout, and in the expanded view (#2032) as many as the wrapped program
// needs — bounded by jqMaxQueryRows and by leaving jqMinResultRows of result
// standing. Rendering, the mouse translation and the result buffer's height
// all read this one number, so the header can never disagree with the space
// reserved for it.
func (m Model) jqQueryRowCount() int {
	s := m.jqPlay
	if s == nil {
		return 1
	}
	r, ok := m.lay.Panes[s.paneKey]
	if !ok {
		return 1
	}
	return m.jqQueryRowsFor(paneInterior(r.W, paneChromeW))
}

// jqQueryRowsFor is jqQueryRowCount for a given interior width — the form the
// rendering uses, so the rows drawn are the rows the geometry reserved. The
// height bound still comes from the hosting pane: it is what "leave the result
// standing" is measured against.
func (m Model) jqQueryRowsFor(width int) int {
	s := m.jqPlay
	if s == nil || !s.expanded {
		return 1
	}
	rows := len(jqplay.Wrap(s.program, m.jqQueryWidth(width)))
	limit := jqMaxQueryRows
	if r, ok := m.lay.Panes[s.paneKey]; ok {
		if fits := paneInterior(r.H, paneChromeH) - jqInfoRows - jqMinResultRows; fits < limit {
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

// jqQueryWidth is how many runes of the program a query row holds: the pane's
// interior less the label column and the two cells the `…` window markers may
// take. The floor keeps the window math sane on a pane too narrow to render
// into anyway.
func (m Model) jqQueryWidth(paneWidth int) int {
	if avail := paneWidth - jqQueryPrefixW - 2; avail > 10 {
		return avail
	}
	return 10
}

// toggleJQQueryView switches the expanded query view (#2032) — json.jqQueryView
// from the query line, the result buffer, the palette or the Tools menu. It is
// a pure view state: the program, the cursor and the result are untouched, only
// the header's height and the result buffer's are.
func (m *Model) toggleJQQueryView() {
	s := m.jqPlay
	if s == nil {
		return
	}
	s.expanded = !s.expanded
	m.sizeJQResult()
}

// sizeJQResult fits the substitute result editor under the query header in
// the hosting pane's interior. Called at open and from layout(), so a resize
// or zoom keeps the buffer in step.
func (m *Model) sizeJQResult() {
	s := m.jqPlay
	if s == nil || s.resultEd == nil {
		return
	}
	r, ok := m.lay.Panes[s.paneKey]
	if !ok {
		return
	}
	s.resultEd.SetSize(paneInterior(r.W, paneChromeW), paneInterior(r.H, paneChromeH+m.jqQueryRowCount()+jqInfoRows))
}

// closeJQPlayground records the program in the session history, aborts a run
// in flight and drops the inline mode. The hosting pane never held anything
// but its own untouched content, so leaving the mode *is* the restore. The
// history is the root model's shared list (#1977), so reopening offers the
// last programs again — over any buffer, not just the one they were run on.
func (m *Model) closeJQPlayground() {
	if s := m.jqPlay; s != nil {
		s.cancelRun()
		s.hist.Add(s.program)
	}
	m.jqPlay = nil
}

// cancelRun aborts the evaluation in flight, if any.
func (s *jqPlayState) cancelRun() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// jqParseDoneMsg carries the parsed input snapshot back to the model. It
// names the state it was started for, so a snapshot arriving after the
// playground closed (or reopened over another buffer) is dropped.
type jqParseDoneMsg struct {
	st  *jqPlayState
	in  *jqplay.Input
	err string
}

// jqDebounceMsg fires jqDebounce after a program change; a stale generation
// means the user kept typing and this tick is not the one to run.
type jqDebounceMsg struct {
	st  *jqPlayState
	gen int
}

// jqEvalDoneMsg carries an off-loop evaluation back to the model.
type jqEvalDoneMsg struct {
	st  *jqPlayState
	gen int
	res jqplay.Result
}

// parseJQInput decodes the snapshot: inline for a screenful, off the event
// loop past jqplay.AsyncThreshold so opening the playground over a large
// response cannot stall a frame.
func (m *Model) parseJQInput(text string) tea.Cmd {
	s := m.jqPlay
	if s == nil {
		return nil
	}
	if len(text) <= jqplay.AsyncThreshold {
		in, err := jqplay.Parse(text)
		return m.finishJQParse(jqParseDoneMsg{st: s, in: in, err: errText(err)})
	}
	s.parsing = true
	return func() tea.Msg {
		in, err := jqplay.Parse(text)
		return jqParseDoneMsg{st: s, in: in, err: errText(err)}
	}
}

// errText renders err as a message, "" for no error.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// finishJQParse installs the snapshot and runs the seeded program against it.
func (m *Model) finishJQParse(msg jqParseDoneMsg) tea.Cmd {
	s := m.jqPlay
	if s == nil || msg.st != s {
		return nil
	}
	s.parsing = false
	s.input, s.inputErr = msg.in, msg.err
	if s.inputErr != "" {
		return nil
	}
	return m.runJQNow()
}

// scheduleJQEval debounces a program change: the run in flight is abandoned
// and a tick stamped with the new generation is scheduled. Only the tick
// still holding the current generation starts a run.
func (m *Model) scheduleJQEval() tea.Cmd {
	s := m.jqPlay
	if s == nil || s.inputErr != "" {
		return nil
	}
	s.cancelRun()
	s.gen++
	s.pending = true
	gen := s.gen
	return tea.Tick(jqDebounce, func(time.Time) tea.Msg {
		return jqDebounceMsg{st: s, gen: gen}
	})
}

// fireJQDebounce starts the run the tick was scheduled for, unless a newer
// keystroke already superseded it.
func (m *Model) fireJQDebounce(msg jqDebounceMsg) tea.Cmd {
	s := m.jqPlay
	if s == nil || msg.st != s || msg.gen != s.gen {
		return nil
	}
	return m.runJQ()
}

// runJQNow skips the debounce — the enter key and the initial evaluation want
// the result immediately, not a tick later.
func (m *Model) runJQNow() tea.Cmd {
	s := m.jqPlay
	if s == nil || s.inputErr != "" || s.input == nil {
		return nil // still parsing, or the input never became one
	}
	s.cancelRun()
	s.gen++
	s.pending = true
	return m.runJQ()
}

// runJQ evaluates the current program off the event loop under a cancellable
// context carrying jqplay.EvalTimeout, so neither a huge result nor a
// non-terminating program can hold the UI.
func (m *Model) runJQ() tea.Cmd {
	s := m.jqPlay
	if s == nil || s.input == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), jqplay.EvalTimeout)
	s.cancel = cancel
	program, in, gen := s.program, s.input, s.gen
	return func() tea.Msg {
		defer cancel()
		return jqEvalDoneMsg{st: s, gen: gen, res: jqplay.Run(ctx, program, in)}
	}
}

// finishJQEval installs a result unless a newer generation superseded it, and
// refreshes the read-only buffer showing it; the returned command starts its
// highlight parse. A run that came back clean also makes its program this
// input's last valid one (#1982), which the next open over the same file
// prefills.
func (m *Model) finishJQEval(msg jqEvalDoneMsg) tea.Cmd {
	s := m.jqPlay
	if s == nil || msg.st != s || msg.gen != s.gen {
		return nil
	}
	s.pending, s.cancel = false, nil
	s.result = msg.res
	if msg.res.Err == "" {
		m.rememberJQProgram(s.srcKey, s.program)
	}
	return m.syncJQResultBuffer()
}

// syncJQResultBuffer reinstalls the current result text into the substitute
// editor. ShowReadOnly resets cursor and scroll — the buffer's content just
// changed under them, so a stale position would point at nothing.
func (m *Model) syncJQResultBuffer() tea.Cmd {
	s := m.jqPlay
	if s == nil || s.resultEd == nil {
		return nil
	}
	s.resultEd.ShowReadOnly(jqResultPath, s.result.Text())
	s.resultEd.SetFocused(s.bufFocus)
	return s.resultEd.Reparse()
}

// updateJQPlayground consumes every key while the playground's pane is
// focused: the query line owns them by default, the result buffer after tab
// (#1970). The result buffer is refit afterwards because a key may have
// changed the program, and with the expanded query view up (#2032) the header
// then holds a different number of rows than the buffer was sized under.
func (m Model) updateJQPlayground(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	out, cmd := m.updateJQPlaygroundKey(msg)
	if mm, ok := out.(Model); ok {
		mm.sizeJQResult()
		return mm, cmd
	}
	return out, cmd
}

// updateJQPlaygroundKey is the routing itself; updateJQPlayground wraps it.
func (m Model) updateJQPlaygroundKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.jqPlay
	if s == nil {
		return m, nil
	}
	s.status = ""
	// The spatial focus moves (default ctrl+arrows) leave the pane with the
	// playground still mounted (#1980), the way they escape a focused
	// terminal: the mode is scoped to its pane, not to the whole keyboard.
	if dir, ok := m.focusKeys[msg.String()]; ok {
		m.FocusDir(dir)
		return m, nil
	}
	if s.bufFocus {
		return m.updateJQBufferKey(msg)
	}
	// The open completion popup (#1979) owns its keys first: arrows step it,
	// enter/tab accept, esc dismisses — the query line's own meaning of those
	// keys comes back the moment it closes. Typing falls through and
	// re-filters below.
	if s.comp != nil {
		if handled, cmd := m.jqCompletionKey(msg); handled {
			return m, cmd
		}
	}
	switch msg.String() {
	case "esc":
		m.closeJQPlayground()
		return m, nil
	case "tab":
		s.setBufFocus(true)
		return m, nil
	case "ctrl+space":
		// The editor's manual completion request: open the popup without a
		// trigger rune, the full builtin list on an empty partial.
		m.refreshJQCompletion("", true)
		return m, nil
	case "enter":
		s.hist.Add(s.program)
		s.histIdx = -1
		return m, m.runJQNow()
	case "up":
		return m, m.stepJQHistory(1)
	case "down":
		return m, m.stepJQHistory(-1)
	case "pgup", "pgdown":
		// Paging works from the query line without moving the focus.
		var cmd tea.Cmd
		*s.resultEd, cmd = s.resultEd.Update(msg)
		return m, cmd
	case "ctrl+y":
		m.copyJQResult()
		return m, nil
	case "ctrl+o":
		return m.openJQResultAsScratch()
	case "ctrl+s":
		// Name the program and keep it (#1995) — json.jqSaveFilter's chord.
		m.startJQSavePrompt()
		return m, nil
	case "ctrl+l":
		// The saved-filter picker (#1995) — json.jqFilters' chord.
		m.openJQFilterPicker(false)
		return m, nil
	}
	out, pos, handled, changed := ui.EditKey(msg, s.program, s.pos)
	if !handled {
		// A key neither the mode nor the query line claims resolves against
		// the Global keymap scope (#1983), so cmd+shift+a (Search Everywhere),
		// cmd+e (Recent Files) and the other IDE-level chords keep working
		// like in any pane. Local keys already returned above, so they keep
		// priority where they collide.
		if ok, cmd := m.jqGlobalChord(msg); ok {
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
	m.refreshJQCompletion(msg.Text, false)
	return m, m.scheduleJQEval()
}

// updateJQBufferKey routes a key into the result buffer: the full editor
// keymap applies (motions, search, visual selection, yank — mutations are
// refused by the read-only flag, #1762). tab returns to the query line; esc
// closes the mode only from resting normal mode, so it first quits a visual
// selection, a search prompt or a pending command like it would in any
// buffer. The result actions (ctrl+y / ctrl+o) shadow the editor's scroll
// and jumplist keys — a throwaway result buffer has no jumplist worth keeping.
func (m Model) updateJQBufferKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.jqPlay
	// The app keymap's copy chord (editor.copy, default cmd+c) copies the
	// visual selection like in any read-only buffer (#1980). Resolved here
	// against the editor context directly: the playground owns the keyboard,
	// so the chord never reaches the keymap layer on its own — and the
	// ActionMsg it dispatches would route to the pane's hidden document
	// editor, not the substitute result buffer.
	if m.jqCopyChord(msg) {
		var cmd tea.Cmd
		*s.resultEd, cmd = s.resultEd.Update(editor.ActionMsg{Action: "copy"})
		return m, cmd
	}
	switch msg.String() {
	case "tab":
		s.setBufFocus(false)
		return m, nil
	case "ctrl+y":
		m.copyJQResult()
		return m, nil
	case "ctrl+o":
		return m.openJQResultAsScratch()
	case "esc":
		if s.resultEd.ModeName() == editor.Normal {
			m.closeJQPlayground()
			return m, nil
		}
	}
	// Modified chords the result actions leave over resolve against the
	// Global keymap scope (#1983) before the buffer sees them — the same
	// eligibility rule the main dispatch applies to a capturing editor.
	// Plain and shift-only keys stay with the buffer: they are motions,
	// search input and typed prompt text.
	if k, ok := keymap.FromKeyMsg(msg); ok &&
		(k.Has(keymap.ModCtrl) || k.Has(keymap.ModAlt) || k.Has(keymap.ModMeta)) {
		if handled, cmd := m.jqGlobalChord(msg); handled {
			return m, cmd
		}
	}
	var cmd tea.Cmd
	*s.resultEd, cmd = s.resultEd.Update(msg)
	return m, cmd
}

// jqCopyChord reports whether msg is the app keymap's editor.copy binding in
// the editor context — the chord that must reach the result buffer's selection
// (#1980) even though the playground's modal routing keeps it from the keymap
// layer. The lookup is against the editor context, not the hosting pane's: the
// substitute buffer is an editor regardless of what pane it is mounted in.
func (m Model) jqCopyChord(msg tea.KeyPressMsg) bool {
	if m.bindings == nil || m.bindings.Table() == nil {
		return false
	}
	k, ok := keymap.FromKeyMsg(msg)
	if !ok {
		return false
	}
	chord := keymap.Chord{Steps: []keymap.Key{k}}
	b, found := m.bindings.Table().Lookup(chord, keymap.Context(editor.ContextID))
	return found && b.Command == "editor.copy"
}

// jqGlobalChord resolves a single-step chord against the Global scope of the
// live binding table and dispatches its command (#1983). The playground owns
// the keyboard while its pane is focused, so without this a chord like
// cmd+shift+a would never reach the keymap layer. Only keys the playground
// leaves over get here — its own keys keep priority — and only Global-scope
// bindings fire: a pane-scoped binding belongs to the pane the mode replaces,
// not to the playground. Multi-step chords cannot resolve without buffering
// query input and are left alone, the same trade the terminal makes (#805).
func (m Model) jqGlobalChord(msg tea.KeyPressMsg) (bool, tea.Cmd) {
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
		return true, m.dispatchCommand(b.Command, c)
	}
	return false, nil
}

// pasteJQPlayground inserts a bracketed paste into the query line, flattened:
// the query line is one line, and a pasted multi-line program would otherwise
// smuggle newlines into it. The result buffer never takes a paste — it is
// read-only.
func (m *Model) pasteJQPlayground(text string) tea.Cmd {
	s := m.jqPlay
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
	m.sizeJQResult()     // a longer program is a taller expanded header (#2032)
	return m.scheduleJQEval()
}

// stepJQHistory walks the session program history (delta +1 = older).
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
func (m *Model) stepJQHistory(delta int) tea.Cmd {
	s := m.jqPlay
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
		return m.runJQNow()
	}
	program, ok := s.hist.At(next)
	if !ok {
		return nil
	}
	s.histIdx, s.program, s.pos = next, program, len([]rune(program))
	return m.runJQNow()
}

// jqPaneClick handles a mouse press inside the hosting pane: a press on the
// query header returns the focus to the query line, a press in the body moves
// the caret in the result buffer and arms the selection drag, and the
// scrollbar column outranks the content like in any editor pane (#1022).
// x/y are content-local, so the header rows sit at negative y.
func (m Model) jqPaneClick(key string, msg mouseEvent, x, y int) (tea.Model, tea.Cmd) {
	s := m.jqPlay
	ed := s.resultEd
	if y < 0 {
		if msg.Button == tea.MouseLeft {
			s.setBufFocus(false)
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

// dragEditor resolves the editor a selection or scrollbar drag targets in
// pane key: the inline jq result buffer when the mode owns that pane (#1970)
// — which may be an HTTP pane with no editor of its own — else the pane's
// active document editor.
func (m Model) dragEditor(key string) *editor.Model {
	if s := m.jqPlay; s != nil && s.paneKey == key {
		return s.resultEd
	}
	if inst := m.activeWS().Panes.Get(key); inst != nil {
		return inst.Editor()
	}
	return nil
}

// copyJQResult writes the whole result — not just the visible window — to the
// system clipboard.
func (m *Model) copyJQResult() {
	s := m.jqPlay
	text := s.result.Text()
	if text == "" {
		s.status = "nothing to copy — the result is empty"
		return
	}
	clipboardWrite(text)
	s.status = "copied the result (" + strconv.Itoa(len(s.result.Outputs)) + " value(s))"
	m.host.Notify(host.Info, "copied the jq result")
}

// openJQResultAsScratch writes the result into a fresh .json scratch and
// opens it through the standard funnel, so highlighting, folding and the
// path breadcrumb all apply — and the playground can be run again over the
// result, which is how a multi-step jq session actually goes.
func (m Model) openJQResultAsScratch() (tea.Model, tea.Cmd) {
	s := m.jqPlay
	text := s.result.Text()
	if text == "" {
		s.status = "nothing to open — the result is empty"
		return m, nil
	}
	path, err := scratch.Create("json")
	if err != nil {
		s.status = "scratch: " + err.Error()
		return m, nil
	}
	if err := os.WriteFile(path, []byte(text+"\n"), 0o644); err != nil {
		s.status = "scratch: " + err.Error()
		return m, nil
	}
	m.closeJQPlayground()
	return m.openPath(path, false)
}

// jqInlineBody renders the hosting pane's content: the query rows, the info
// row and the result buffer underneath, filling the pane's interior exactly.
// The query row count comes from jqQueryRowCount, the same number the pane
// geometry reserved.
func (m Model) jqInlineBody(width int) string {
	s := m.jqPlay
	if s == nil {
		return ""
	}
	if width < 20 {
		width = 20
	}
	return strings.Join(m.jqQueryRows(width), "\n") + "\n" + m.jqInfoRow(width) + "\n" + s.resultEd.View()
}

// jqInfoRow is the header's second line: an error beats a transient status
// beats the input/result summary with the key hints. One fixed row — the
// buffer below must not resize when an error appears mid-keystroke.
//
// The row is composed of styled segments (#1978): caps render in Warning so
// a capped run is not the dimmest thing on the row, a runtime error keeps
// the count of values produced before it, and the key hints are dropped as
// whole `·`-separated segments on a narrow pane instead of being cut
// mid-word. Truncation is cell-aware (ansi.Truncate), so a wide glyph in the
// source label cannot overflow the row.
func (m Model) jqInfoRow(width int) string {
	s := m.jqPlay
	pal := m.pal()
	hint := lipgloss.NewStyle().Foreground(pal.Hint)
	if err := m.jqErrorLine(); err != "" {
		line := lipgloss.NewStyle().Foreground(pal.Error).Render("E: " + err)
		// A runtime error can arrive after values were produced; they are
		// sitting in the buffer below, so say so instead of hiding the count.
		if s.inputErr == "" && len(s.result.Outputs) > 0 {
			line += hint.Render(fmt.Sprintf(" · %d value(s) before the error", len(s.result.Outputs)))
		}
		return ansi.Truncate(line, width, "…")
	}
	if s.status != "" {
		return ansi.Truncate(lipgloss.NewStyle().Foreground(pal.Success).Render(s.status), width, "…")
	}
	line := m.jqInputSegment()
	if res := m.jqResultSegment(); res != "" {
		line += hint.Render(" · ") + res
	}
	if m.jqQueryCut(width) {
		// The `…` at the row's edge alone is too easy to miss (#2032): say
		// that the program on screen is not all of it, in the same Warning the
		// other "you are seeing less than there is" markers use.
		line += lipgloss.NewStyle().Foreground(pal.Warning).Render(" · query cut")
	}
	for _, h := range m.jqHints() {
		seg := hint.Render(" · " + h)
		if ansi.StringWidth(line)+ansi.StringWidth(seg) > width {
			break
		}
		line += seg
	}
	return ansi.Truncate(line, width, "…")
}

// jqHints is the key-hint tail of the info row, one segment per hint so a
// narrow pane drops trailing hints whole rather than clipping one mid-word.
// The query-view toggle (#2032) is named with the chord actually bound to
// json.jqQueryView, so a rebind renames the hint with it, and it sits early in
// the list: a program that does not fit the row is exactly the situation in
// which the hint has to survive a narrow pane.
func (m Model) jqHints() []string {
	s := m.jqPlay
	view := m.jqQueryViewChord() + " full query"
	if s.expanded {
		view = m.jqQueryViewChord() + " one-line query"
	}
	if s.bufFocus {
		return []string{"tab query line", view, "ctrl+y copy", "ctrl+o scratch", "esc close"}
	}
	return []string{"tab result", "enter run", view, "↑/↓ history", "ctrl+s save filter", "ctrl+l filters", "ctrl+y copy", "ctrl+o scratch", "esc close"}
}

// jqQueryViewChord names the chord bound to json.jqQueryView (#2032) for the
// hints, resolved against the live binding table so a rebind is reflected
// rather than a hard-coded default being advertised. With the command unbound
// — a user may unbind it — the palette is what is left to name.
func (m Model) jqQueryViewChord() string {
	if m.bindings != nil && m.bindings.Table() != nil {
		for _, b := range m.bindings.Table().Bindings() {
			if b.Command == "json.jqQueryView" {
				return b.Chord.String()
			}
		}
	}
	return "palette"
}

// jqQueryCut reports whether the query rows are showing less than the program
// holds: the one-line view windowed around the cursor, or an expanded view
// capped at jqMaxQueryRows. It drives the info row's marker and is the reason
// the toggle is worth pressing.
func (m Model) jqQueryCut(width int) bool {
	s := m.jqPlay
	avail := m.jqQueryWidth(width)
	if !s.expanded {
		return len([]rune(s.program)) > avail
	}
	return len(jqplay.Wrap(s.program, avail)) > m.jqQueryRowsFor(width)
}

// jqErrorLine is the message shown on the info row: a bad input beats a bad
// program, because with no parsed input no program could have run.
func (m Model) jqErrorLine() string {
	s := m.jqPlay
	if s.inputErr != "" {
		return s.inputErr
	}
	return s.result.Err
}

// jqInputSegment names the queried snapshot: where it came from, how big it
// is and how many top-level values it holds (a `.jsonl` body holds many). A
// truncated read renders its cap in Warning — the user is seeing less than
// the buffer holds, which the row's dim Hint color would undersell.
func (m Model) jqInputSegment() string {
	s := m.jqPlay
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
	return out
}

// jqQueryRow renders the query line, windowed around its cursor and colored
// by the jq scanner. While the result buffer holds the keyboard — or the
// focus is on another pane entirely (#1980) — the cursor cell is not drawn
// and the `>` marker is blanked, the same inactive affordance the regex
// tester's and clone prompt's field labels use: an absent cursor alone is
// too subtle a cue that typing goes elsewhere. The label renders in the
// chrome's Secondary either way, marking the row as chrome over the pane
// surface rather than buffer text. Both prefixes are 6 cells, so the window
// math never changes with focus.
func (m Model) jqQueryRow(width int) string {
	s := m.jqPlay
	pos := s.pos
	prefix := "> jq: "
	if s.bufFocus || !m.jqPlayFocused() {
		pos = -1
		prefix = "  jq: "
	}
	label := lipgloss.NewStyle().Foreground(m.pal().Secondary).Render(prefix)
	return label + m.jqHighlighted(s.program, pos, m.jqQueryWidth(width))
}

// jqQueryRows is the query half of the header: the one windowed row by
// default, the wrapped program over jqQueryRowCount rows in the expanded view
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
func (m Model) jqQueryRows(width int) []string {
	s := m.jqPlay
	rows := m.jqQueryRowsFor(width)
	if rows <= 1 {
		return []string{m.jqQueryRow(width)}
	}
	pal := m.pal()
	label := lipgloss.NewStyle().Foreground(pal.Secondary)
	prefix, pos := "> jq: ", s.pos
	if s.bufFocus || !m.jqPlayFocused() {
		prefix, pos = "  jq: ", -1
	}
	lines := jqplay.Wrap(s.program, m.jqQueryWidth(width))
	start := 0
	if pos >= 0 {
		if cur := jqplay.LineAt(lines, pos); cur >= rows {
			start = cur - rows + 1
		}
	}
	r := []rune(s.program)
	tokens := jqplay.Tokens(s.program)
	styles := m.jqKindStyles()
	cursor := lipgloss.NewStyle().Reverse(true)
	out := make([]string, 0, rows)
	for i := 0; i < rows; i++ {
		row := label.Render(prefix)
		if i > 0 {
			row = strings.Repeat(" ", jqQueryPrefixW)
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
		for j := l.Start; j < l.End; j++ {
			if j == pos {
				row += cursor.Render(string(r[j]))
				continue
			}
			row += styles[jqplay.KindAt(tokens, j)].Render(string(r[j]))
		}
		if pos == l.End && idx == jqplay.LineAt(lines, pos) {
			row += cursor.Render(" ")
		}
		if idx == start+rows-1 && idx < len(lines)-1 {
			row += "…"
		}
		out = append(out, row)
	}
	return out
}

// jqHighlighted renders the program clipped to width runes around the cursor,
// each rune in its token's color, with the cursor cell reversed; pos < 0
// draws no cursor. It is the windowedInput of clone_prompt.go plus the
// colors: the cursor has to be drawn per rune anyway, so coloring per rune
// costs nothing extra.
func (m Model) jqHighlighted(program string, pos, width int) string {
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
	styles := m.jqKindStyles()
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

// jqKindStyles maps the scanner's kinds onto the theme, once per render
// rather than once per rune. The query line borrows the **chrome** palette
// rather than the editor's capture colors: it is a header row over the pane
// surface, not buffer text, and the chrome slots are the ones every theme
// guarantees to contrast against it.
func (m Model) jqKindStyles() map[jqplay.Kind]lipgloss.Style {
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

// jqResultSegment summarises the evaluation: how many values, the caveats
// (capped output, evaluation in flight), or why there is nothing to show.
// While a re-run is pending the previous count stays up with an
// "evaluating…" suffix — replacing it outright made the row shimmer on
// every keystroke, since pending is set the moment the debounce tick is
// scheduled. A zero-value result renders in Warning: the buffer below is
// blank then, and this summary is the only signal that nothing matched. The
// input-error and after-error cases never reach here — jqInfoRow's error
// branch owns them.
func (m Model) jqResultSegment() string {
	s := m.jqPlay
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
