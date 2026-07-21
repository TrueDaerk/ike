package terminal

import (
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/vt"

	"ike/internal/theme"
)

// Model is the pane-facing terminal: it owns a Session and adapts pane
// sizing, focus and key routing to it. It follows the explorer/editor value
// shape (value type, pointer-receiver mutations) so the pane registry can
// host it behind an Instance.
type Model struct {
	sess    *Session
	err     string
	focused bool
	w, h    int
	pal     *theme.Palette
	// send is remembered so StartCommand (0350, #574) can respawn a session
	// in place with the same async injector.
	send func(tea.Msg)
	// env is the spawn environment overlay, remembered so Restart (#810) can
	// rerun the command with the same injection.
	env []string
	// occupied marks that the user sent input (keys or a paste) to the
	// session — an occupied terminal is never reused for a run (#574).
	occupied bool
	// label is a caller-set display name (the run configuration's name,
	// #576); it wins over the OSC title in tab labels.
	label string
	// tool is the configured tool name when the session is a custom TUI tool
	// pane (#741); "" for plain terminals and runs.
	tool string
	// scroll is the scrollback offset in lines (0 = live view). Paging keys
	// (shift+pgup/pgdn) and the mouse wheel move it; any other key snaps back
	// to live and goes to the shell.
	scroll int
	// Mouse selection (#227), anchored in virtual coordinates — indices into
	// [scrollback ++ screen] — so it survives scrollback paging. selOn marks
	// an existing selection, dragging a drag in progress.
	selAnchor, selHead vpos
	selOn, dragging    bool
	// Command completion popup (#740): comp is the popup state, autoSuggest
	// the while-typing trigger (terminal.autosuggest), pendingSuggest a
	// recompute scheduled for the next screen update (the shell must echo
	// the keystroke before the cursor row reads current).
	comp           completion
	autoSuggest    bool
	pendingSuggest bool
}

// vpos is a cell position with a virtual line index (scrollback + screen).
type vpos struct{ line, col int }

// before orders two virtual positions.
func (p vpos) before(q vpos) bool {
	return p.line < q.line || (p.line == q.line && p.col < q.col)
}

// New starts a terminal model: shell (already resolved via Shell) spawned in
// dir with the extraEnv overlay (#98). A failed spawn yields a model
// rendering the error instead of a grid — the pane stays usable (closable)
// rather than crashing the layout.
func New(key, shell, dir string, w, h int, extraEnv []string, send func(tea.Msg)) Model {
	m := Model{w: w, h: h, send: send, env: extraEnv, autoSuggest: true}
	sess, err := StartSession(key, shell, dir, w, h, extraEnv, send)
	if err != nil {
		m.err = err.Error()
		return m
	}
	m.sess = sess
	return m
}

// NewCommand starts a terminal model running argv instead of a shell (0350,
// #574): the run-in-terminal seam. A failed spawn renders the error like New.
func NewCommand(key string, argv []string, dir string, w, h int, extraEnv []string, send func(tea.Msg)) Model {
	m := Model{w: w, h: h, send: send, env: extraEnv}
	sess, err := StartCommandSession(key, argv, dir, w, h, extraEnv, send)
	if err != nil {
		m.err = err.Error()
		return m
	}
	m.sess = sess
	return m
}

// StartCommand replaces the model's session with a fresh command session
// (#574): the reuse path when a run takes over an unoccupied terminal. Any
// previous session ends; scroll, selection and the occupied flag reset.
func (m *Model) StartCommand(key string, argv []string, dir string, extraEnv []string) {
	if m.sess != nil {
		m.sess.Close()
	}
	m.scroll = 0
	m.ClearSelection()
	m.occupied = false
	m.err = ""
	m.env = extraEnv
	w, h := m.w, m.h
	sess, err := StartCommandSession(key, argv, dir, w, h, extraEnv, m.send)
	if err != nil {
		m.sess = nil
		m.err = err.Error()
		return
	}
	m.sess = sess
}

// Restart reruns a finished command session in place (#810): same pane, same
// layout slot, same command line, directory and environment. A no-op while
// the session still runs or for plain shell sessions.
func (m *Model) Restart() {
	if m.sess == nil || m.sess.Running() || !m.sess.IsCommand() {
		return
	}
	m.StartCommand(m.sess.key, m.sess.Argv(), m.sess.Dir(), m.env)
}

// Occupied reports whether the user has sent any input to the session; a run
// never takes over an occupied terminal (#574).
func (m Model) Occupied() bool { return m.occupied }

// SetLabel names the terminal for chrome (tab labels, pane titles): run
// terminals carry their configuration's name (#576).
func (m *Model) SetLabel(l string) { m.label = l }

// Label returns the caller-set display name, "" when none.
func (m Model) Label() string { return m.label }

// SetTool marks the session as a custom TUI tool pane (#741) carrying the
// configured tool name; chrome and persistence treat it as a tool, not a
// terminal, and its exit closes the pane.
func (m *Model) SetTool(name string) { m.tool = name }

// Tool returns the configured tool name, "" for plain terminals and runs.
func (m Model) Tool() string { return m.tool }

// Pid returns the running child's process id, or 0 when there is none (#625).
func (m Model) Pid() int {
	if m.sess == nil {
		return 0
	}
	return m.sess.Pid()
}

// SessionKey returns the underlying session's routing key ("" for a failed
// spawn) — output/exit messages carry it.
func (m Model) SessionKey() string {
	if m.sess == nil {
		return ""
	}
	return m.sess.key
}

// IsCommand reports whether the session runs a program rather than a shell.
func (m Model) IsCommand() bool { return m.sess != nil && m.sess.IsCommand() }

// ExitCode proxies the session's exit status (ok=false while running).
func (m Model) ExitCode() (int, bool) {
	if m.sess == nil {
		return 0, false
	}
	return m.sess.ExitCode()
}

// SetPalette threads the active theme palette (chrome only; the grid's colors
// come from the application's own escape codes).
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetSize resizes the grid and the PTY.
func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	if m.sess != nil {
		m.sess.Resize(w, h)
	}
}

// SetFocused records focus; the cursor cell renders only while focused.
func (m *Model) SetFocused(on bool) { m.focused = on }

// Size reports the current grid size (#676): hosts embedding the model
// (the debug panel's Output column) assert their sizing through it.
func (m Model) Size() (w, h int) { return m.w, m.h }

// Running reports whether the shell is alive.
func (m Model) Running() bool { return m.sess != nil && m.sess.Running() }

// ScrollbackLen reports the history length (0 for a failed spawn).
func (m Model) ScrollbackLen() int {
	if m.sess == nil {
		return 0
	}
	return m.sess.ScrollbackLen()
}

// Title returns the application-set OSC title ("" when none).
func (m Model) Title() string {
	if m.sess == nil {
		return ""
	}
	return m.sess.Title()
}

// Clear empties the scrollback and repaints (terminal.clear).
func (m *Model) Clear() {
	m.scroll = 0
	m.ClearSelection()
	if m.sess != nil {
		m.sess.Clear()
	}
}

// Dir returns the session's origin directory ("" for a failed spawn).
func (m Model) Dir() string {
	if m.sess == nil {
		return ""
	}
	return m.sess.Dir()
}

// ShellPath returns the spawned shell binary ("" for a failed spawn).
func (m Model) ShellPath() string {
	if m.sess == nil {
		return ""
	}
	return m.sess.ShellPath()
}

// Close ends the underlying session.
func (m *Model) Close() {
	if m.sess != nil {
		m.sess.Close()
	}
}

// Update routes a key press: the scrollback paging keys move the view,
// everything else goes raw to the PTY (snapping the view back to live). The
// reserved set the root model never forwards is documented there
// (terminalReservedKeys in internal/app).
func (m *Model) Update(msg tea.KeyPressMsg) tea.Cmd {
	if m.sess == nil {
		return nil
	}
	switch msg.String() {
	case "shift+pgup":
		m.ScrollBy(m.pageSize())
		return nil
	case "shift+pgdown":
		m.ScrollBy(-m.pageSize())
		return nil
	}
	// A finished tool pane (#810) stays open showing its last output; r
	// reruns the configured command in place. Everything else is inert —
	// there is no child to type into (ctrl+w closes the pane app-side).
	if m.tool != "" && !m.sess.Running() {
		if msg.String() == "r" {
			m.Restart()
		}
		return nil
	}
	m.scroll = 0
	// The completion popup (#740) intercepts its own keys (navigation,
	// accept, dismiss, ctrl+space) before the raw route.
	if m.completionKey(msg.String()) {
		return nil
	}
	m.ClearSelection()
	m.occupied = true // input reached the session: never reuse it for a run (#574)
	if ev, ok := motionKey(msg); ok {
		m.completionTyped(msg.String(), "")
		m.sess.SendKey(ev)
		return nil
	}
	for _, ev := range toVTKeys(msg) {
		m.sess.SendKey(ev)
	}
	m.completionTyped(msg.String(), msg.Text)
	return nil
}

// motionKey translates the macOS-conventional editing chords into the
// readline/ZLE emacs-mode defaults — the iTerm "natural text editing"
// convention (#225, #240): option+arrows jump words (ESC b / ESC f),
// cmd+arrows go to line start/end (ctrl+a / ctrl+e), option+backspace kills
// the previous word (ESC DEL), option+forward-delete kills the next word
// (ESC d, #733), cmd+backspace kills to line start (ctrl+u).
// Shift-augmented variants behave the same; a PTY has no selection to extend.
func motionKey(k tea.KeyPressMsg) (vt.KeyPressEvent, bool) {
	mod := k.Mod &^ textMods
	isCmd := mod == tea.ModSuper || mod == tea.ModMeta
	switch {
	case mod == tea.ModAlt && k.Code == tea.KeyLeft:
		return vt.KeyPressEvent{Code: 'b', Mod: vt.ModAlt}, true
	case mod == tea.ModAlt && k.Code == tea.KeyRight:
		return vt.KeyPressEvent{Code: 'f', Mod: vt.ModAlt}, true
	case mod == tea.ModAlt && k.Code == tea.KeyBackspace:
		return vt.KeyPressEvent{Code: vt.KeyBackspace, Mod: vt.ModAlt}, true
	case mod == tea.ModAlt && k.Code == tea.KeyDelete:
		return vt.KeyPressEvent{Code: 'd', Mod: vt.ModAlt}, true
	case isCmd && k.Code == tea.KeyLeft:
		return vt.KeyPressEvent{Code: 'a', Mod: vt.ModCtrl}, true
	case isCmd && k.Code == tea.KeyRight:
		return vt.KeyPressEvent{Code: 'e', Mod: vt.ModCtrl}, true
	case isCmd && k.Code == tea.KeyBackspace:
		return vt.KeyPressEvent{Code: 'u', Mod: vt.ModCtrl}, true
	}
	return vt.KeyPressEvent{}, false
}

// pageSize is one paging step: half the grid, at least one line.
func (m Model) pageSize() int {
	if m.h > 1 {
		return m.h / 2
	}
	return 1
}

// ScrollBy moves the scrollback view by delta lines (positive = older),
// clamped to the available history; 0 is the live view.
func (m *Model) ScrollBy(delta int) {
	if m.sess == nil {
		return
	}
	m.scroll += delta
	if m.scroll < 0 {
		m.scroll = 0
	}
	if max := m.sess.ScrollbackLen(); m.scroll > max {
		m.scroll = max
	}
}

// Scroll reports the current scrollback offset (0 = live).
func (m Model) Scroll() int { return m.scroll }

// MousePress routes a left press at the pane-local cell (x, y): a child that
// enabled mouse reporting gets the click (like the wheel, #226); otherwise it
// anchors a text selection (#227).
func (m *Model) MousePress(x, y int) {
	if m.sess == nil {
		return
	}
	m.ClearSelection()
	if m.sess.WantsMouse() {
		m.sess.SendMouse(vt.MouseClick{X: x, Y: y, Button: vt.MouseLeft})
		return
	}
	m.dragging = true
	m.selAnchor = m.virtualAt(x, y)
	m.selHead = m.selAnchor
}

// MouseDrag extends the selection to (x, y) — or forwards the drag motion to
// a mouse-reporting child.
func (m *Model) MouseDrag(x, y int) {
	if m.sess == nil {
		return
	}
	if m.sess.WantsMouse() {
		m.sess.SendMouse(vt.MouseMotion{X: x, Y: y, Button: vt.MouseLeft})
		return
	}
	if !m.dragging {
		return
	}
	m.selHead = m.virtualAt(x, y)
	m.selOn = m.selHead != m.selAnchor
}

// MouseRelease ends a drag (or forwards the release); the selection, if any,
// stays visible until a key goes to the shell or a new press lands.
func (m *Model) MouseRelease(x, y int) {
	if m.sess == nil {
		return
	}
	if m.sess.WantsMouse() {
		m.sess.SendMouse(vt.MouseRelease{X: x, Y: y, Button: vt.MouseLeft})
		return
	}
	m.dragging = false
}

// HasSelection reports whether a mouse selection exists.
func (m Model) HasSelection() bool { return m.selOn }

// ClearSelection drops the selection and any drag in progress.
func (m *Model) ClearSelection() { m.selOn, m.dragging = false, false }

// SelectionText extracts the selected text: the span runs from the earlier
// endpoint (inclusive) to the later one (exclusive), lines right-trimmed and
// newline-joined — the stream selection every terminal implements.
func (m Model) SelectionText() string {
	if !m.selOn || m.sess == nil {
		return ""
	}
	start, end := m.selAnchor, m.selHead
	if end.before(start) {
		start, end = end, start
	}
	var lines []string
	for v := start.line; v <= end.line; v++ {
		text := []rune(m.sess.LineText(v))
		from, to := 0, len(text)
		if v == start.line && start.col < to {
			from = start.col
		} else if v == start.line {
			from = to
		}
		if v == end.line && end.col < to {
			to = end.col
		}
		if from > to {
			from = to
		}
		lines = append(lines, strings.TrimRight(string(text[from:to]), " "))
	}
	return strings.Join(lines, "\n")
}

// virtualAt maps a pane-local cell to virtual coordinates, honouring the
// current scrollback offset and clamping to the grid.
func (m Model) virtualAt(x, y int) vpos {
	x = clamp(x, 0, m.w-1)
	y = clamp(y, 0, m.h-1)
	sb := 0
	if m.sess != nil {
		sb = m.sess.ScrollbackLen()
	}
	return vpos{line: clamp(sb-m.scroll+y, 0, sb+m.h), col: x}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// wheelChildCap bounds what one wheel call forwards to the CHILD (#669): a
// coalesced trackpad burst can stand for hundreds of lines, and a child that
// receives one PTY sequence per line keeps scrolling for seconds after the
// user stopped — the exact backlog the batch was meant to kill. Roughly one
// screenful; the pane's own scrollback path is cheap and never capped.
const wheelChildCap = 40

// wheelEventLines is how many lines one wheel notch stands for when
// translating a line delta back into discrete wheel events for a
// mouse-reporting child (mirrors the app's per-notch scroll of 3 rows).
const wheelEventLines = 3

// MouseWheel routes one wheel movement at the pane-local cell (x, y); delta
// is in lines, positive = up/towards history (#226) — a coalesced burst
// arrives as one call with the whole distance (#669). The convention every
// terminal emulator implements: a child that enabled mouse reporting gets
// wheel events (one per notch, capped); an alt-screen child without it gets
// arrow keys (the xterm "alternate scroll" behaviour, capped); a plain shell
// pages the pane's scrollback by the full delta.
func (m *Model) MouseWheel(x, y, delta int) {
	if m.sess == nil || delta == 0 {
		return
	}
	up := delta > 0
	lines, events := wheelChildBudget(delta)
	switch {
	case m.sess.WantsMouse():
		m.scroll = 0
		btn := vt.MouseWheelDown
		if up {
			btn = vt.MouseWheelUp
		}
		for i := 0; i < events; i++ {
			m.sess.SendMouse(vt.MouseWheel{X: x, Y: y, Button: btn})
		}
	case m.sess.AltScreen():
		m.scroll = 0
		code := vt.KeyDown
		if up {
			code = vt.KeyUp
		}
		for i := 0; i < lines; i++ {
			m.sess.SendKey(vt.KeyPressEvent{Code: code})
		}
	default:
		m.ScrollBy(delta)
	}
}

// wheelChildBudget converts a (possibly coalesced) line delta into what may
// be forwarded to the child: the capped line count for alt-screen arrow keys
// and the number of discrete wheel events for a mouse-reporting child.
func wheelChildBudget(delta int) (lines, events int) {
	lines = delta
	if lines < 0 {
		lines = -lines
	}
	if lines > wheelChildCap {
		lines = wheelChildCap
	}
	events = (lines + wheelEventLines - 1) / wheelEventLines
	return lines, events
}

// PasteText forwards pasted text through the bracketed-paste path.
func (m *Model) PasteText(text string) {
	if m.sess != nil {
		m.occupied = true
		m.sess.Paste(text)
	}
}

// textMods are the modifiers that only transform which text a key produces;
// a key carrying nothing beyond these is plain typed input, not a chord.
const textMods = tea.ModShift | tea.ModCapsLock | tea.ModNumLock

// toVTKeys converts a bubbletea key press into the emulator's key events; the
// two structs share the same shape (code, shifted code, modifiers, text).
// The emulator's encoder writes a plain key only when no modifier is set, so
// shifted or caps-locked characters (shift+a → "A") would be dropped (#224);
// such presses are replayed as their produced text instead.
func toVTKeys(k tea.KeyPressMsg) []vt.KeyPressEvent {
	if k.Text != "" && k.Mod != 0 && k.Mod&^textMods == 0 {
		evs := make([]vt.KeyPressEvent, 0, 1)
		for _, r := range k.Text {
			evs = append(evs, vt.KeyPressEvent{Code: r, Text: string(r)})
		}
		return evs
	}
	return []vt.KeyPressEvent{{
		Code:        k.Code,
		ShiftedCode: k.ShiftedCode,
		Mod:         vt.KeyMod(k.Mod),
		Text:        k.Text,
	}}
}

// View renders the grid, with the cursor cell reversed while focused; a
// scrolled view windows over [scrollback ++ screen] instead. A dead or failed
// session renders its state.
func (m Model) View() string {
	if m.sess == nil {
		return "terminal failed: " + m.err
	}
	if m.scroll > 0 {
		return m.scrolledView()
	}
	view := m.sess.View()
	if m.selOn {
		lines := strings.Split(view, "\n")
		m.highlightSelection(lines, m.sess.ScrollbackLen())
		view = strings.Join(lines, "\n")
	}
	if !m.sess.Running() {
		return m.deadView(view)
	}
	if !m.focused {
		return view
	}
	cx, cy := m.sess.CursorPosition()
	return m.completionView(overlayCursor(view, cx, cy))
}

// deadView renders a finished session: the grid with the exit footer as the
// last row, truncating the grid by one row when it fills the pane so the
// footer stays visible inside the fixed pane height (#810).
func (m Model) deadView(view string) string {
	lines := strings.Split(view, "\n")
	if m.h > 0 && len(lines) >= m.h {
		lines = lines[:m.h-1]
	}
	return strings.Join(append(lines, m.exitLine()), "\n")
}

// exitLine renders the completion marker: command sessions (#574) report the
// exit code so a run's outcome is visible at a glance; tool panes (#810)
// additionally offer their footer actions. Kept ASCII — DeadActionHit maps
// click columns onto byte offsets of this string.
func (m Model) exitLine() string {
	if m.tool != "" {
		code := ""
		if c, ok := m.sess.ExitCode(); ok {
			code = " (code " + strconv.Itoa(c) + ")"
		}
		return "[" + m.tool + " exited" + code + "]  [restart (r)]  [close (ctrl+w)]"
	}
	if code, ok := m.sess.ExitCode(); ok && m.sess.IsCommand() {
		return "[process exited with code " + strconv.Itoa(code) + "]"
	}
	return "[process exited]"
}

// DeadActionHit maps a click in a finished tool pane onto a footer action
// (#810): "restart", "close", or "" for anywhere else. x/y are pane-local
// content coordinates.
func (m Model) DeadActionHit(x, y int) string {
	if m.sess == nil || m.sess.Running() || m.tool == "" {
		return ""
	}
	row := len(strings.Split(m.sess.View(), "\n"))
	if m.h > 0 && row >= m.h {
		row = m.h - 1
	}
	if y != row {
		return ""
	}
	line := m.exitLine()
	for _, a := range []struct{ span, action string }{
		{"[restart (r)]", "restart"},
		{"[close (ctrl+w)]", "close"},
	} {
		if i := strings.Index(line, a.span); i >= 0 && x >= i && x < i+len(a.span) {
			return a.action
		}
	}
	return ""
}

// scrolledView renders the paging window: scroll lines above the live screen,
// filled from the scrollback, the remainder from the screen's top. The last
// line carries a position marker instead of the cursor.
func (m Model) scrolledView() string {
	sbLen := m.sess.ScrollbackLen()
	off := m.scroll
	if off > sbLen {
		off = sbLen
	}
	screen := strings.Split(m.sess.View(), "\n")
	rows := make([]string, 0, m.h)
	for i := 0; i < m.h; i++ {
		virtual := sbLen - off + i // index into [scrollback ++ screen]
		switch {
		case virtual < sbLen:
			rows = append(rows, m.sess.HistoryLine(virtual))
		case virtual-sbLen < len(screen):
			rows = append(rows, screen[virtual-sbLen])
		}
	}
	m.highlightSelection(rows, sbLen-off)
	marker := "[scrollback -" + strconv.Itoa(off) + "  shift+pgdn to return]"
	if len(rows) > 0 {
		rows[len(rows)-1] = marker
	}
	return strings.Join(rows, "\n")
}

// highlightSelection reverse-videos the selected span on the visible rows;
// firstVirtual is the virtual line index rendered at rows[0].
func (m Model) highlightSelection(rows []string, firstVirtual int) {
	if !m.selOn {
		return
	}
	start, end := m.selAnchor, m.selHead
	if end.before(start) {
		start, end = end, start
	}
	for i := range rows {
		v := firstVirtual + i
		if v < start.line || v > end.line {
			continue
		}
		from, to := 0, m.w
		if v == start.line {
			from = start.col
		}
		if v == end.line {
			to = end.col
		}
		if from < to {
			rows[i] = reverseSpan(rows[i], from, to)
		}
	}
}

// overlayCursor reverse-videos the cursor cell inside the rendered grid. The
// grid is ANSI-styled, so the splice walks the target line rune-aware while
// skipping escape sequences.
func overlayCursor(view string, x, y int) string {
	lines := strings.Split(view, "\n")
	if y < 0 || y >= len(lines) {
		return view
	}
	lines[y] = reverseCell(lines[y], x)
	return strings.Join(lines, "\n")
}

var cursorStyle = lipgloss.NewStyle().Reverse(true)

// reverseSpan reverse-videos the visible cells [from, to) of an ANSI-styled
// line, padding past the rendered content so a selection reads full-width.
func reverseSpan(line string, from, to int) string {
	var b strings.Builder
	visible := 0
	inEsc := false
	for i := 0; i < len(line); {
		if !inEsc && line[i] == 0x1b {
			inEsc = true
			b.WriteByte(line[i])
			i++
			continue
		}
		if inEsc {
			b.WriteByte(line[i])
			if line[i] >= 0x40 && line[i] <= 0x7e && line[i] != '[' {
				inEsc = false
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		if visible >= from && visible < to {
			b.WriteString(cursorStyle.Render(string(r)))
		} else {
			b.WriteString(line[i : i+size])
		}
		visible++
		i += size
	}
	if visible < to {
		if pad := from - visible; pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
			visible = from
		}
		b.WriteString(cursorStyle.Render(strings.Repeat(" ", to-visible)))
	}
	return b.String()
}

// reverseCell restyles the visible cell at column col of an ANSI-styled line.
func reverseCell(line string, col int) string {
	var b strings.Builder
	visible := 0
	inEsc := false
	done := false
	for i := 0; i < len(line); {
		if !inEsc && line[i] == 0x1b {
			inEsc = true
			b.WriteByte(line[i])
			i++
			continue
		}
		if inEsc {
			b.WriteByte(line[i])
			// CSI sequences end on a final byte in @-~; the two-byte forms
			// (ESC + single char) end immediately.
			if line[i] >= 0x40 && line[i] <= 0x7e && line[i] != '[' {
				inEsc = false
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		if visible == col && !done {
			b.WriteString(cursorStyle.Render(string(r)))
			done = true
		} else {
			b.WriteString(line[i : i+size])
		}
		visible++
		i += size
	}
	if !done {
		// Cursor past the rendered content: pad with spaces up to the column.
		if pad := col - visible; pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		b.WriteString(cursorStyle.Render(" "))
	}
	return b.String()
}
