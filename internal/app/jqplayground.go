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

	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/jqplay"
	"ike/internal/pane"
	"ike/internal/scratch"
	"ike/internal/ui"
)

// jqplayground.go is the UI half of the jq playground (#1936): a floating
// query line over a JSON buffer with the program's output live underneath.
// The evaluation core — parsing, running, error and cap handling, history —
// is internal/jqplay; this file owns the query line, the result window, the
// rendering and the key routing, exactly like the regex tester (#1937) it is
// modelled on.
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
//   - The result is **read-only text**. Editing output belongs in a buffer,
//     which is exactly what "open as scratch" (ctrl+o) makes.

// jqResultRows is how many rows of the result are shown at once; the window
// scrolls with ctrl+n/ctrl+p and pgup/pgdn. The shell would otherwise grow
// past the terminal on a large result.
const jqResultRows = 14

// jqDebounce is how long the query line stays quiet before a program runs. A
// var, not a const, so tests drive the evaluation without sleeping.
var jqDebounce = 120 * time.Millisecond

// jqPlayState is the open playground. input is the parsed snapshot (nil while
// it is still being parsed, or when parsing failed — inputErr then holds the
// message); source labels where it came from. program/pos are the query line,
// result the last evaluation and top its scroll offset. hist is the
// per-session program history, histIdx the position while browsing it (-1 =
// editing a live program). gen stamps debounce ticks and runs so a stale one
// is dropped, cancel aborts the run in flight, and status carries the
// transient confirmation line.
type jqPlayState struct {
	source   string
	input    *jqplay.Input
	inputErr string
	parsing  bool

	program string
	pos     int

	result  jqplay.Result
	top     int
	pending bool

	hist    jqplay.History
	histIdx int

	gen    int
	cancel context.CancelFunc
	status string
}

// jqPlayContent implements ui.Content (not ModelContent) so the body learns
// the shell's content width budget and can clip long result rows to it.
type jqPlayContent struct{ m Model }

// Title implements ui.Content.
func (c jqPlayContent) Title() string { return "jq Playground" }

// Render implements ui.Content.
func (c jqPlayContent) Render(width int) string { return c.m.jqPlayBody(width) }

// startJQPlayground opens the playground over the JSON at hand: the focused
// HTTP response pane's body, else the focused editor's visual selection, else
// its whole buffer. The query line is prefilled with the caret's jq path
// (#1660) when the buffer has one, so "the value I was looking at" is one
// keystroke from being a program.
func (m *Model) startJQPlayground() tea.Cmd {
	src, ok := m.jqSource()
	if !ok {
		m.host.Notify(host.Info, "jq: no JSON buffer or HTTP response to query")
		return nil
	}
	s := &jqPlayState{source: src.label, histIdx: -1, hist: m.jqHistory, program: m.jqSeedProgram(src)}
	s.pos = len([]rune(s.program))
	m.jqPlay = s
	m.shell.SetContent(jqPlayContent{m: *m})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
	return m.parseJQInput(src.text)
}

// jqInputSource is one resolved snapshot: the JSON text, the label naming
// where it came from, and whether it is the *whole* focused buffer — the only
// case in which the caret's document path is a valid program against it.
type jqInputSource struct {
	text     string
	label    string
	fullFile bool
}

// jqSource resolves what the playground queries. The focused HTTP response
// wins over the editor (the pane the user is looking at is the one they mean);
// a visual selection wins over the whole buffer, so a single embedded JSON
// blob in a log file is queryable without extracting it first.
func (m Model) jqSource() (jqInputSource, bool) {
	if c := m.focusedContent(); c != nil && c.Kind() == pane.KindHTTP {
		if body := c.HTTP().BodyText(); strings.TrimSpace(body) != "" {
			return jqInputSource{text: body, label: "HTTP response"}, true
		}
	}
	if ed := m.activeEditor(); ed != nil {
		name := jqEditorLabel(ed)
		if sel, has := ed.SelectionText(); has && strings.TrimSpace(sel) != "" {
			return jqInputSource{text: sel, label: name + " (selection)"}, true
		}
		if body := ed.Text(); strings.TrimSpace(body) != "" {
			return jqInputSource{text: body, label: name, fullFile: true}, true
		}
	}
	// The response pane may be open without being focused — an editor holding
	// the .http file is the usual focus after a dispatch.
	if p := m.httpPanel(); p != nil {
		if body := p.BodyText(); strings.TrimSpace(body) != "" {
			return jqInputSource{text: body, label: "HTTP response"}, true
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

// jqPlayOpen reports whether the shell currently shows the playground.
func (m Model) jqPlayOpen() bool { return m.jqPlay != nil && m.shell.IsOpen() }

// closeJQPlayground records the program in the session history, aborts a run
// in flight and clears the dialog. The history outlives the dialog, so
// reopening offers the last programs again.
func (m *Model) closeJQPlayground() {
	if s := m.jqPlay; s != nil {
		s.cancelRun()
		s.hist.Add(s.program)
		m.jqHistory = s.hist
	}
	m.jqPlay = nil
	m.shell.Close()
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

// finishJQEval installs a result unless a newer generation superseded it.
func (m *Model) finishJQEval(msg jqEvalDoneMsg) {
	s := m.jqPlay
	if s == nil || msg.st != s || msg.gen != s.gen {
		return
	}
	s.pending, s.cancel = false, nil
	s.result, s.top = msg.res, 0
}

// updateJQPlayground consumes every key while the playground is open.
func (m Model) updateJQPlayground(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.jqPlay
	if s == nil {
		return m, nil
	}
	s.status = ""
	switch msg.String() {
	case "esc":
		m.closeJQPlayground()
		return m, nil
	case "enter":
		s.hist.Add(s.program)
		s.histIdx = -1
		return m, m.runJQNow()
	case "up":
		return m, m.stepJQHistory(1)
	case "down":
		return m, m.stepJQHistory(-1)
	case "ctrl+n":
		m.scrollJQResult(1)
		return m, nil
	case "ctrl+p":
		m.scrollJQResult(-1)
		return m, nil
	case "pgdown":
		m.scrollJQResult(jqResultRows)
		return m, nil
	case "pgup":
		m.scrollJQResult(-jqResultRows)
		return m, nil
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

// pasteJQPlayground inserts a bracketed paste into the query line, flattened:
// the query line is one line, and a pasted multi-line program would otherwise
// smuggle newlines into it.
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

// scrollJQResult moves the result window, clamped to the result's length.
func (m *Model) scrollJQResult(delta int) {
	s := m.jqPlay
	lines := len(s.result.Lines())
	s.top += delta
	if s.top > lines-jqResultRows {
		s.top = lines - jqResultRows
	}
	if s.top < 0 {
		s.top = 0
	}
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

// jqPlayBody renders the whole dialog into the shell's width budget.
func (m Model) jqPlayBody(width int) string {
	s := m.jqPlay
	if s == nil {
		return ""
	}
	if width < 20 {
		width = 20
	}
	pal := m.pal()
	hint := lipgloss.NewStyle().Foreground(pal.Hint)
	var b strings.Builder

	b.WriteString(m.jqQueryRow(width))
	b.WriteString("\n")
	if err := m.jqErrorLine(); err != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Error).Render(clip("E: "+err, width)) + "\n")
	}
	b.WriteString(hint.Render(clip(m.jqInputLine(), width)) + "\n\n")

	b.WriteString(hint.Render(m.jqResultHeader()) + "\n")
	b.WriteString(m.jqResultWindow(width))
	if s.status != "" {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(pal.Success).Render(clip(s.status, width)))
	}
	b.WriteString("\n" + hint.Render(clip("enter run · ↑/↓ program history · ctrl+n/p scroll result · pgup/pgdn page", width)))
	b.WriteString("\n" + hint.Render(clip("ctrl+y copy result · ctrl+o open result as scratch · esc close", width)))
	return b.String()
}

// jqErrorLine is the message shown right under the query: a bad input beats a
// bad program, because with no parsed input no program could have run.
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
// by the jq scanner.
func (m Model) jqQueryRow(width int) string {
	s := m.jqPlay
	avail := width - 8
	if avail < 10 {
		avail = 10
	}
	return "> jq: " + m.jqHighlighted(s.program, s.pos, avail)
}

// jqHighlighted renders the program clipped to width runes around the cursor,
// each rune in its token's color, with the cursor cell reversed. It is the
// windowedInput of clone_prompt.go plus the colors: the cursor has to be
// drawn per rune anyway, so coloring per rune costs nothing extra.
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
	if pos >= end {
		b.WriteString(cursor.Render(" "))
	}
	if end < len(r) {
		b.WriteString("…")
	}
	return b.String()
}

// jqKindStyles maps the scanner's kinds onto the theme, once per render
// rather than once per rune. The playground borrows the **chrome** palette
// rather than the editor's capture colors: it is a dialog over the shell
// surface, not a buffer, and the chrome slots are the ones every theme
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

// jqResultWindow renders the visible rows of the result plus the "more rows"
// marker, so a long result reads as scrollable rather than as complete.
func (m Model) jqResultWindow(width int) string {
	s := m.jqPlay
	lines := s.result.Lines()
	hint := lipgloss.NewStyle().Foreground(m.pal().Hint)
	if len(lines) == 0 {
		return hint.Render("  —") + "\n"
	}
	if s.top > len(lines)-1 {
		s.top = len(lines) - 1
	}
	if s.top < 0 {
		s.top = 0
	}
	var b strings.Builder
	end := min(s.top+jqResultRows, len(lines))
	for i := s.top; i < end; i++ {
		b.WriteString("  " + clip(lines[i], width-2) + "\n")
	}
	if more := len(lines) - end; more > 0 {
		b.WriteString(hint.Render("  … "+strconv.Itoa(more)+" more row(s)") + "\n")
	}
	return b.String()
}
