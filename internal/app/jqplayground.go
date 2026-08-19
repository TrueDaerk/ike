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

	"ike/internal/clipboard"
	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/jqplay"
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
// of the hosting pane: the query line plus the input/result/status line. It is
// fixed — an error appearing must not resize the result buffer per keystroke.
const jqHeaderRows = 2

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
// program/pos are the query line, result the last evaluation. hist is the
// per-session program history, histIdx the position while browsing it (-1 =
// editing a live program). gen stamps debounce ticks and runs so a stale one
// is dropped, cancel aborts the run in flight, and status carries the
// transient confirmation line.
type jqPlayState struct {
	paneKey  string
	resultEd *editor.Model
	bufFocus bool

	source   string
	input    *jqplay.Input
	inputErr string
	parsing  bool

	program string
	pos     int

	result  jqplay.Result
	pending bool

	hist    jqplay.History
	histIdx int

	gen    int
	cancel context.CancelFunc
	status string
}

// setBufFocus moves the keyboard between the query line and the result
// buffer, keeping the substitute editor's focus flag (cursor cell) in step.
func (s *jqPlayState) setBufFocus(v bool) {
	s.bufFocus = v
	if s.resultEd != nil {
		s.resultEd.SetFocused(v)
	}
}

// startJQPlayground opens the playground over the JSON at hand: the focused
// HTTP response pane's body, else the focused editor's visual selection, else
// its whole buffer. The query line is prefilled with the caret's jq path
// (#1660) when the buffer has one, so "the value I was looking at" is one
// keystroke from being a program. The mode mounts *in* the resolved pane
// (#1970): focus moves there and the pane shows the query header plus the
// read-only result buffer until esc.
func (m *Model) startJQPlayground() tea.Cmd {
	src, ok := m.jqSource()
	if !ok {
		m.host.Notify(host.Info, "jq: no JSON buffer or HTTP response to query")
		return nil
	}
	s := &jqPlayState{paneKey: src.paneKey, source: src.label, histIdx: -1, hist: m.jqHistory, program: m.jqSeedProgram(src)}
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
// where it came from, the pane the inline mode mounts in (with the tab to
// activate for a tab-nested response viewer, -1 otherwise), and whether it is
// the *whole* focused buffer — the only case in which the caret's document
// path is a valid program against it.
type jqInputSource struct {
	text     string
	label    string
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
			return jqInputSource{text: body, label: "HTTP response", paneKey: m.activeWS().Panes.Focused(), tabIdx: -1}, true
		}
	}
	if ed := m.activeEditor(); ed != nil {
		name := jqEditorLabel(ed)
		key := m.activeEditorKey()
		if sel, has := ed.SelectionText(); has && strings.TrimSpace(sel) != "" {
			return jqInputSource{text: sel, label: name + " (selection)", paneKey: key, tabIdx: -1}, true
		}
		if body := ed.Text(); strings.TrimSpace(body) != "" {
			return jqInputSource{text: body, label: name, paneKey: key, tabIdx: -1, fullFile: true}, true
		}
	}
	// The response pane may be open without being focused — an editor holding
	// the .http file is the usual focus after a dispatch. It may live as a
	// content tab of a tab host (#1778); the mode then activates that tab.
	if hostKey, tabIdx, inst, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindHTTP
	}); ok && m.leafVisible(hostKey) {
		if body := inst.HTTP().BodyText(); strings.TrimSpace(body) != "" {
			return jqInputSource{text: body, label: "HTTP response", paneKey: hostKey, tabIdx: tabIdx}, true
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

// jqSeedProgram is the program the query line opens on: the caret's jq path
// (#1660) when the whole focused buffer is the input, else the identity
// program, which pretty-prints it. A response body or a selection gets the
// identity — the caret's path indexes the *file*, and against a sub-document
// it would name a location the input does not contain.
func (m Model) jqSeedProgram(src jqInputSource) string {
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

// jqPlayOpen reports whether the inline playground is active.
func (m Model) jqPlayOpen() bool { return m.jqPlay != nil }

// jqInlineActive reports whether the inline playground owns pane key: its
// content is then the query header plus the result buffer, not the pane's own
// component.
func (m Model) jqInlineActive(key string) bool {
	return m.jqPlay != nil && m.jqPlay.paneKey == key
}

// jqHeaderRowsFor is the vertical chrome the query header adds to pane key —
// the breadcrumbRows analogue (#1153) the mouse translation keys off.
func (m Model) jqHeaderRowsFor(key string) int {
	if m.jqInlineActive(key) {
		return jqHeaderRows
	}
	return 0
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
	s.resultEd.SetSize(paneInterior(r.W, paneChromeW), paneInterior(r.H, paneChromeH+jqHeaderRows))
}

// closeJQPlayground records the program in the session history, aborts a run
// in flight and drops the inline mode. The hosting pane never held anything
// but its own untouched content, so leaving the mode *is* the restore. The
// history outlives the mode, so reopening offers the last programs again.
func (m *Model) closeJQPlayground() {
	if s := m.jqPlay; s != nil {
		s.cancelRun()
		s.hist.Add(s.program)
		m.jqHistory = s.hist
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
// highlight parse.
func (m *Model) finishJQEval(msg jqEvalDoneMsg) tea.Cmd {
	s := m.jqPlay
	if s == nil || msg.st != s || msg.gen != s.gen {
		return nil
	}
	s.pending, s.cancel = false, nil
	s.result = msg.res
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

// updateJQPlayground consumes every key while the playground is open: the
// query line owns them by default, the result buffer after tab (#1970).
func (m Model) updateJQPlayground(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.jqPlay
	if s == nil {
		return m, nil
	}
	s.status = ""
	if s.bufFocus {
		return m.updateJQBufferKey(msg)
	}
	switch msg.String() {
	case "esc":
		m.closeJQPlayground()
		return m, nil
	case "tab":
		s.setBufFocus(true)
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
	}
	out, pos, handled, changed := ui.EditKey(msg, s.program, s.pos)
	if !handled {
		return m, nil
	}
	s.program, s.pos = out, pos
	if !changed {
		return m, nil
	}
	s.histIdx = -1
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
	var cmd tea.Cmd
	*s.resultEd, cmd = s.resultEd.Update(msg)
	return m, cmd
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
	s.setBufFocus(false) // pasting a program is query-line work
	return m.scheduleJQEval()
}

// stepJQHistory walks the session program history (delta +1 = older).
func (m *Model) stepJQHistory(delta int) tea.Cmd {
	s := m.jqPlay
	next := s.histIdx + delta
	if next < -1 {
		next = -1
	}
	if next >= s.hist.Len() {
		return nil
	}
	if next == -1 {
		s.histIdx, s.program, s.pos = -1, "", 0
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

// jqInlineBody renders the hosting pane's content: the two header rows and
// the result buffer underneath, filling the pane's interior exactly.
func (m Model) jqInlineBody(width int) string {
	s := m.jqPlay
	if s == nil {
		return ""
	}
	if width < 20 {
		width = 20
	}
	return m.jqQueryRow(width) + "\n" + m.jqInfoRow(width) + "\n" + s.resultEd.View()
}

// jqInfoRow is the header's second line: an error beats a transient status
// beats the input/result summary with the key hints. One fixed row — the
// buffer below must not resize when an error appears mid-keystroke.
func (m Model) jqInfoRow(width int) string {
	s := m.jqPlay
	pal := m.pal()
	if err := m.jqErrorLine(); err != "" {
		return lipgloss.NewStyle().Foreground(pal.Error).Render(clip("E: "+err, width))
	}
	if s.status != "" {
		return lipgloss.NewStyle().Foreground(pal.Success).Render(clip(s.status, width))
	}
	hint := "tab: result · enter: run · ↑/↓ history · ctrl+y copy · ctrl+o scratch · esc close"
	if s.bufFocus {
		hint = "tab: query line · ctrl+y copy · ctrl+o scratch · esc close"
	}
	line := m.jqInputLine() + " · " + m.jqResultHeader() + " · " + hint
	return lipgloss.NewStyle().Foreground(pal.Hint).Render(clip(line, width))
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

// jqInputLine names the queried snapshot: where it came from, how big it is
// and how many top-level values it holds (a `.jsonl` body holds many).
func (m Model) jqInputLine() string {
	s := m.jqPlay
	if s.parsing {
		return "Input: " + s.source + " — parsing…"
	}
	if s.input == nil {
		return "Input: " + s.source
	}
	line := fmt.Sprintf("Input: %s — %s", s.source, humanBytes(int64(s.input.Size())))
	if n := s.input.Len(); n > 1 {
		line += fmt.Sprintf(", %d values", n)
	}
	if s.input.Truncated {
		line += fmt.Sprintf(" (first %d only)", jqplay.MaxInputValues)
	}
	return line
}

// jqQueryRow renders the query line, windowed around its cursor and colored
// by the jq scanner. While the result buffer holds the keyboard the cursor
// cell is not drawn — the caret lives in the buffer then.
func (m Model) jqQueryRow(width int) string {
	s := m.jqPlay
	avail := width - 8
	if avail < 10 {
		avail = 10
	}
	pos := s.pos
	if s.bufFocus {
		pos = -1
	}
	return "> jq: " + m.jqHighlighted(s.program, pos, avail)
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

// jqResultHeader summarises the evaluation: how many values, the caveats
// (evaluation in flight, capped output), or why there is nothing to show.
func (m Model) jqResultHeader() string {
	s := m.jqPlay
	switch {
	case s.parsing:
		return "Result — parsing the input…"
	case s.inputErr != "":
		return "Result — the input is not JSON"
	case s.pending:
		return "Result — evaluating…"
	case strings.TrimSpace(s.program) == "":
		return "Result — no program yet"
	}
	out := fmt.Sprintf("Result — %d value(s)", len(s.result.Outputs))
	if s.result.Truncated {
		out += fmt.Sprintf(" (stopped at %d)", jqplay.MaxOutputs)
	}
	if s.result.Err != "" && len(s.result.Outputs) > 0 {
		out += " before the error"
	}
	return out
}
