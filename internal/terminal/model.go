package terminal

import (
	"image/color"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"

	"ike/internal/overlay"
	"ike/internal/theme"
	"ike/internal/ui"
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
	// session — the routing marker key handling and paste share.
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
	// Auto-scroll while a selection drag leaves the pane (#1821): dragX/dragY
	// is the last pointer cell, autoDir the running scroll direction
	// (+1 = into history, -1 = back towards live, 0 = not scrolling) and
	// autoGen retires the repeat ticks of a drag that has ended.
	dragX, dragY     int
	autoDir, autoGen int
	// Multi-click selection (#936): consecutive presses on the same cell
	// within multiClickWindow cycle click → word → line → click … After a
	// double/triple click the drag extends unit-wise (#951): selMode carries
	// the granularity and selOriginA/B the clicked word/line, which always
	// stays fully selected while the head grows in either direction.
	lastClickAt            time.Time
	lastClickPos           vpos
	clickStreak            int
	selMode                int
	selOriginA, selOriginB vpos
	// Command completion popup (#740): comp is the popup state, autoSuggest
	// the while-typing trigger (terminal.autosuggest), pendingSuggest a
	// recompute scheduled for the next screen update (the shell must echo
	// the keystroke before the cursor row reads current).
	comp           completion
	autoSuggest    bool
	pendingSuggest bool
	// Scrollback search (#1169): open while non-nil; it owns the keyboard.
	// Lives behind a pointer so value-receiver View copies share it.
	search *termSearch
	// sbGrab is the in-thumb grab offset of a scrollbar drag (#1368), see
	// scrollbar.go.
	sbGrab int
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
	sess, err := StartSession(key, shell, dir, m.gridW(), h, extraEnv, send)
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
	sess, err := StartCommandSession(key, argv, dir, m.gridW(), h, extraEnv, send)
	if err != nil {
		m.err = err.Error()
		return m
	}
	m.sess = sess
	return m
}

// NewPipe starts a process-less terminal model (#1370): a pipe session whose
// content arrives through FeedText. The debug integration renders DAP output
// events through it so debuggee output gets the real terminal pane's reflow,
// scrollback and search.
func NewPipe(key string, w, h int, send func(tea.Msg)) Model {
	m := Model{w: w, h: h, send: send}
	m.sess = NewPipeSession(key, m.gridW(), h, send)
	return m
}

// IsPipe reports whether the model wraps a process-less pipe session (#1370).
func (m Model) IsPipe() bool { return m.sess != nil && m.sess.IsPipe() }

// FeedText appends debuggee output to a pipe session (#1370). DAP output uses
// bare newlines; the emulator needs carriage returns, so line endings are
// normalized to \r\n. A no-op on PTY-backed sessions.
func (m *Model) FeedText(text string) {
	if m.sess == nil || text == "" {
		return
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n", "\r\n")
	m.sess.FeedBytes([]byte(text))
}

// SetOutputTap tees the session's raw output through fn (#1915): the
// problem-matcher seam for run sessions. fn runs on the session's feed
// goroutine; nil removes the tap. A no-op without a live session.
func (m *Model) SetOutputTap(fn func([]byte)) {
	if m.sess != nil {
		m.sess.SetTap(fn)
	}
}

// FinishPipe ends a pipe session with the debuggee's exit status (#1370); the
// pane then renders the usual dead view with the output still reviewable.
func (m *Model) FinishPipe(exitCode int, hasCode bool) {
	if m.sess != nil {
		m.sess.FinishPipe(exitCode, hasCode)
	}
}

// StartCommand replaces the model's session with a fresh command session
// (#574): the Run tool (#1905) launching the next configuration in place. Any
// previous session ends; scroll, selection and the input marker reset.
func (m *Model) StartCommand(key string, argv []string, dir string, extraEnv []string) {
	if m.sess != nil {
		m.sess.Close()
	}
	m.scroll = 0
	m.search = nil
	m.ClearSelection()
	m.occupied = false
	m.err = ""
	m.env = extraEnv
	w, h := m.gridW(), m.h
	sess, err := StartCommandSession(key, argv, dir, w, h, extraEnv, m.send)
	if err != nil {
		m.sess = nil
		m.err = err.Error()
		return
	}
	m.sess = sess
	m.sess.SetPalette(m.pal)
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

// SetScrollbackLines re-bounds the live session's scrollback (#1545), the
// config-reload path; n <= 0 and dead sessions are no-ops. New sessions get
// the process-wide default at creation instead.
func (m *Model) SetScrollbackLines(n int) {
	if m.sess != nil {
		m.sess.SetScrollbackSize(n)
	}
}

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

// Busy reports whether the session currently runs foreground work closing
// would kill (#986); false for a failed or dead session.
func (m Model) Busy() bool { return m.sess != nil && m.sess.Busy() }

// SendEOF forwards a ctrl+d to the child (#986): on an idle shell prompt it
// exits the shell, which closes the pane/tab through the regular exit path.
func (m *Model) SendEOF() {
	if m.sess != nil && m.sess.Running() {
		m.sess.SendKey(vt.KeyPressEvent{Code: 'd', Mod: vt.ModCtrl})
	}
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

// SetPalette threads the active theme palette: the pane chrome plus the
// grid's indexed colours, which resolve against the theme's terminal palette
// instead of the outer terminal's (#1363).
func (m *Model) SetPalette(p *theme.Palette) {
	m.pal = p
	if m.sess != nil {
		m.sess.SetPalette(p)
	}
}

// SetSize resizes the grid and the PTY. The PTY is one column narrower than
// the pane (gridW): the rightmost column is the scrollbar gutter (#1500), so
// the bar never covers content and its appearance never reflows the child.
func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	if m.sess != nil {
		m.sess.Resize(m.gridW(), h)
	}
}

// gridW is the child grid's width: the pane width minus the reserved
// scrollbar gutter column (#1500). Panes too narrow to split keep full width.
func (m Model) gridW() int {
	if m.w >= 2 {
		return m.w - 1
	}
	return m.w
}

// SetFocused records focus; the cursor cell renders only while focused.
func (m *Model) SetFocused(on bool) { m.focused = on }

// Size reports the current grid size (#676): hosts embedding the model
// (the debug panel's Output column) assert their sizing through it.
func (m Model) Size() (w, h int) { return m.w, m.h }

// Running reports whether the shell is alive.
func (m Model) Running() bool { return m.sess != nil && m.sess.Running() }

// SetParked forwards the parked flag (#1522) to the live session; a no-op for
// a failed spawn.
func (m Model) SetParked(parked bool) {
	if m.sess != nil {
		m.sess.SetParked(parked)
	}
}

// Parked reports the session's parked flag (#1522); false for a failed spawn.
func (m Model) Parked() bool { return m.sess != nil && m.sess.Parked() }

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
	m.search = nil // the history it searched is gone (#1169)
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

// Cwd returns the shell's live working directory (OSC 7, #770), the origin
// directory when the shell never reported one, "" for a failed spawn.
func (m Model) Cwd() string {
	if m.sess == nil {
		return ""
	}
	return m.sess.Cwd()
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
	// The scrollback search (#1169) owns the keyboard while open; `/` opens
	// it only in the scrolled plain-shell state (searchCaptures) — at the live
	// view or under an alt-screen/mouse-reporting child the key stays with
	// the child.
	if m.search != nil {
		m.searchKey(msg)
		return nil
	}
	if msg.String() == "/" && m.searchCaptures() {
		m.startSearch()
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
	m.occupied = true // input reached the session
	m.scroll = 0
	// The completion popup (#740) intercepts its own keys (navigation,
	// accept, dismiss, ctrl+space) before the raw route.
	if m.completionKey(msg.String()) {
		return nil
	}
	m.ClearSelection()
	if ev, ok := motionKey(msg); ok {
		m.completionTyped(msg.String(), "")
		m.sess.SendKey(ev)
		return nil
	}
	if seq, ok := modifiedCursorSeq(msg); ok {
		m.completionTyped(msg.String(), "")
		m.sess.SendSeq(seq)
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

// modifiedCursorSeq encodes a modifier+cursor-key chord (arrows, home, end)
// the xterm way: CSI 1 ; <mod> <final>, with <mod> = 1 + the modifier bits
// (shift 1, alt 2, ctrl 4, meta 8) — shift+up is ESC [ 1;2 A (#1841). The
// emulator's key encoder only knows unmodified cursor keys and silently drops
// anything else, so these chords never reached the child at all. Application
// cursor-key mode (DECCKM) has no modified form; xterm sends CSI here too.
// Chords ike claims itself (focus moves, shift+pgup/pgdn paging, the
// natural-editing translations in motionKey) are handled before this point.
func modifiedCursorSeq(k tea.KeyPressMsg) (string, bool) {
	var final byte
	switch k.Code {
	case tea.KeyUp:
		final = 'A'
	case tea.KeyDown:
		final = 'B'
	case tea.KeyRight:
		final = 'C'
	case tea.KeyLeft:
		final = 'D'
	case tea.KeyHome:
		final = 'H'
	case tea.KeyEnd:
		final = 'F'
	default:
		return "", false
	}
	// Lock/typing modifiers say nothing about a cursor key; anything outside
	// the xterm-encodable set (super, hyper) stays on the old path.
	mod := k.Mod &^ (tea.ModCapsLock | tea.ModNumLock)
	if mod == 0 || mod&^(tea.ModShift|tea.ModAlt|tea.ModCtrl|tea.ModMeta) != 0 {
		return "", false
	}
	param := 1
	for _, m := range []struct {
		bit    tea.KeyMod
		weight int
	}{{tea.ModShift, 1}, {tea.ModAlt, 2}, {tea.ModCtrl, 4}, {tea.ModMeta, 8}} {
		if mod&m.bit != 0 {
			param += m.weight
		}
	}
	return "\x1b[1;" + strconv.Itoa(param) + string(final), true
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

// multiClickWindow is how quickly a follow-up press on the same cell must
// land to count as a double/triple click (#936).
const multiClickWindow = 500 * time.Millisecond

// Selection granularities (#951): what one drag step extends by.
const (
	selChar = iota // plain drag: cell-wise
	selWord        // after a double click: word-wise
	selLine        // after a triple click: logical-line-wise
)

// MousePress routes a left press at the pane-local cell (x, y): a child that
// enabled mouse reporting gets the click (like the wheel, #226); otherwise it
// anchors a text selection (#227). Repeated presses on the same cell within
// multiClickWindow select the word (double) or the whole logical line across
// its soft-wrapped rows (triple), then cycle (#936).
func (m *Model) MousePress(x, y int) {
	if m.sess == nil {
		return
	}
	m.ClearSelection()
	if m.sess.WantsMouse() {
		m.sess.SendMouse(vt.MouseClick{X: x, Y: y, Button: vt.MouseLeft})
		m.clickStreak = 0
		return
	}
	v := m.virtualAt(x, y)
	now := time.Now()
	if v == m.lastClickPos && now.Sub(m.lastClickAt) <= multiClickWindow {
		m.clickStreak++
	} else {
		m.clickStreak = 1
	}
	m.lastClickAt, m.lastClickPos = now, v

	m.dragging = true
	switch (m.clickStreak-1)%3 + 1 {
	case 2:
		m.selMode = selWord
		m.selectWordAt(v)
	case 3:
		m.selMode = selLine
		m.selectLineAt(v.line)
	default:
		m.selMode = selChar
		m.selAnchor = v
		m.selHead = m.selAnchor
	}
}

// logicalLineSpan walks the soft-wrap chain around virtual line v and returns
// the first and last row of the logical line it belongs to.
func (m Model) logicalLineSpan(v int) (first, last int) {
	first, last = v, v
	for first > 0 && m.sess.SoftWrapped(first-1) {
		first--
	}
	for m.sess.SoftWrapped(last) {
		last++
	}
	return first, last
}

// selectWordAt selects the word under v and remembers it as the drag-extend
// origin (#936/#951). A press on a blank cell falls back to a plain
// character selection anchored there.
func (m *Model) selectWordAt(v vpos) {
	a, b, ok := m.wordSpanAt(v)
	if !ok {
		m.selMode = selChar
		m.selAnchor, m.selHead = v, v
		return
	}
	m.selOriginA, m.selOriginB = a, b
	m.selAnchor, m.selHead = a, b
	m.selOn = true
}

// wordSpanAt returns the [a, b) span of the word under v, shell-friendly
// boundaries, across the whole logical line — a word spanning a soft-wrap
// break stays one word (#936). A non-word cell spans just itself; ok=false on
// blank space.
func (m Model) wordSpanAt(v vpos) (a, b vpos, ok bool) {
	w := m.sess.Width()
	if w <= 0 {
		return a, b, false
	}
	first, last := m.logicalLineSpan(v.line)
	// The logical line as one rune string: wrapped rows are full-width by
	// definition of the heuristic, only the final row is right-trimmed, so
	// index math is linear (padded defensively regardless).
	var text []rune
	for l := first; l <= last; l++ {
		seg := []rune(m.sess.LineText(l))
		if l < last {
			for len(seg) < w {
				seg = append(seg, ' ')
			}
		}
		text = append(text, seg...)
	}
	idx := (v.line-first)*w + v.col
	if idx >= len(text) || text[idx] == ' ' {
		return a, b, false // blank space: no word here
	}
	ia, ib := idx, idx+1
	if isWordRune(text[idx]) {
		for ia > 0 && isWordRune(text[ia-1]) {
			ia--
		}
		for ib < len(text) && isWordRune(text[ib]) {
			ib++
		}
	}
	a = vpos{line: first + ia/w, col: ia % w}
	b = vpos{line: first + (ib-1)/w, col: (ib-1)%w + 1}
	return a, b, true
}

// selectLineAt selects the whole logical line through virtual line v — every
// row of its soft-wrap chain, hard-newline bounded — and remembers it as the
// drag-extend origin (#936/#951).
func (m *Model) selectLineAt(v int) {
	a, b := m.lineSpanAt(v)
	m.selOriginA, m.selOriginB = a, b
	m.selAnchor, m.selHead = a, b
	m.selOn = a != b // an empty line has nothing to select yet
}

// lineSpanAt returns the [a, b) span of the logical line through virtual
// line v.
func (m Model) lineSpanAt(v int) (a, b vpos) {
	first, last := m.logicalLineSpan(v)
	end := len([]rune(m.sess.LineText(last)))
	return vpos{line: first, col: 0}, vpos{line: last, col: end}
}

// isWordRune classifies shell-friendly word characters (#936): alphanumerics
// plus the path/flag/URL punctuation smart selection needs, so a double-click
// grabs /usr/local/bin, --flag=value or user@host:path whole.
func isWordRune(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return true
	}
	return strings.ContainsRune("/.-_~+@$%=:", r)
}

// MouseDrag extends the selection to (x, y) — or forwards the drag motion to
// a mouse-reporting child. Coordinates are pane-local and may leave the pane:
// dragging past the top or bottom edge auto-scrolls the view (#1821), and the
// returned command is the tick that keeps it scrolling while the pointer rests
// there.
func (m *Model) MouseDrag(x, y int) tea.Cmd {
	if m.sess == nil {
		return nil
	}
	if m.sess.WantsMouse() {
		m.sess.SendMouse(vt.MouseMotion{X: x, Y: y, Button: vt.MouseLeft})
		return nil
	}
	if !m.dragging {
		return nil
	}
	return m.dragTo(x, y)
}

// autoScrollInterval is one auto-scroll step of an edge-held drag (#1821):
// constant speed, one line per tick — the usual terminal/editor behaviour.
const autoScrollInterval = 60 * time.Millisecond

// AutoScrollMsg is the repeat tick of a selection drag resting past a pane
// edge (#1821). Key routes it to the session it started on, Gen retires the
// ticks of a drag that has since ended.
type AutoScrollMsg struct {
	Key string
	Gen int
}

// AutoScroll continues an edge-held drag from the last known pointer cell
// (#1821): one more scroll step plus the matching selection extension. Stale
// ticks (wrong session, ended drag, pointer back inside the pane) are dropped
// and stop the repeat.
func (m *Model) AutoScroll(msg AutoScrollMsg) tea.Cmd {
	if m.sess == nil || !m.dragging || m.autoDir == 0 {
		return nil
	}
	if msg.Key != m.SessionKey() || msg.Gen != m.autoGen {
		return nil
	}
	return m.dragTo(m.dragX, m.dragY)
}

// dragTo applies one drag step at the pane-local cell (x, y): a pointer past
// an edge scrolls the view one line first (#1821), then the selection extends
// to the — clamped — cell under it. Because the selection is anchored in
// virtual coordinates, scrolling alone moves nothing; only the head follows.
func (m *Model) dragTo(x, y int) tea.Cmd {
	m.dragX, m.dragY = x, y
	m.autoDir = m.autoScrollStep(y)
	v := m.virtualAt(x, y)
	switch m.selMode {
	case selWord:
		// Word-wise extension (#951): the selection is the union of the
		// origin word and the word under the pointer; a blank cell extends
		// cell-wise so the drag never feels stuck.
		a, b, ok := m.wordSpanAt(v)
		if !ok {
			a, b = v, vpos{line: v.line, col: v.col + 1}
		}
		m.extendUnit(a, b)
	case selLine:
		a, b := m.lineSpanAt(v.line)
		m.extendUnit(a, b)
	default:
		m.selHead = v
		m.selOn = m.selHead != m.selAnchor
	}
	if m.autoDir == 0 {
		return nil
	}
	key, gen := m.SessionKey(), m.autoGen
	return tea.Tick(autoScrollInterval, func(time.Time) tea.Msg {
		return AutoScrollMsg{Key: key, Gen: gen}
	})
}

// autoScrollStep scrolls one line when the pane-local row y lies outside the
// pane: above it goes into the scrollback, below it back towards the live view
// (#1821). It reports the direction actually applied — 0 inside the pane and
// at the ends of the history, where the repeat tick stops.
func (m *Model) autoScrollStep(y int) int {
	dir := 0
	switch {
	case y < 0:
		dir = 1
	case m.h > 0 && y >= m.h:
		dir = -1
	default:
		return 0
	}
	before := m.scroll
	m.ScrollBy(dir)
	if m.scroll == before {
		return 0 // top of the history or the live view: nothing left to scroll
	}
	return dir
}

// extendUnit grows the selection from the multi-click origin unit to cover
// the dragged unit [a, b): backwards drags anchor at the origin's end,
// forward drags at its start — the originally clicked word/line always stays
// fully selected (#951).
func (m *Model) extendUnit(a, b vpos) {
	switch {
	case a.before(m.selOriginA):
		m.selAnchor, m.selHead = m.selOriginB, a
	case m.selOriginB.before(b):
		m.selAnchor, m.selHead = m.selOriginA, b
	default:
		m.selAnchor, m.selHead = m.selOriginA, m.selOriginB
	}
	m.selOn = m.selAnchor != m.selHead
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
	m.stopAutoScroll()
}

// HasSelection reports whether a mouse selection exists.
func (m Model) HasSelection() bool { return m.selOn }

// ClearSelection drops the selection and any drag in progress.
func (m *Model) ClearSelection() {
	m.selOn, m.dragging = false, false
	m.stopAutoScroll()
}

// stopAutoScroll ends the auto-scroll repeat (#1821); the generation bump
// makes every tick already in flight a no-op.
func (m *Model) stopAutoScroll() {
	m.autoDir = 0
	m.autoGen++
}

// SelectionText extracts the selected text: the span runs from the earlier
// endpoint (inclusive) to the later one (exclusive), lines right-trimmed —
// the stream selection every terminal implements. Rows joined by a soft wrap
// concatenate without a newline (#936): only hard newlines put `\n` in the
// clipboard, so a wrapped command pastes back as the one line the program
// printed.
func (m Model) SelectionText() string {
	if !m.selOn || m.sess == nil {
		return ""
	}
	start, end := m.selAnchor, m.selHead
	if end.before(start) {
		start, end = end, start
	}
	var b strings.Builder
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
		b.WriteString(strings.TrimRight(string(text[from:to]), " "))
		if v < end.line && !m.sess.SoftWrapped(v) {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// virtualAt maps a pane-local cell to virtual coordinates, honouring the
// current scrollback offset and clamping to the grid.
func (m Model) virtualAt(x, y int) vpos {
	x = clamp(x, 0, m.gridW()-1)
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

// PasteText routes a pasted block to the open scrollback search's query
// (#1882) when one is up, otherwise forwards it through the bracketed-paste
// path to the shell.
func (m *Model) PasteText(text string) {
	if m.search != nil {
		out, ncur, changed := ui.PasteText(m.search.query, m.search.pos, text)
		if changed {
			m.search.query, m.search.pos = out, ncur
			m.searchJump()
		}
		return
	}
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

// View renders the grid, with the cursor cell reversed while focused and the
// child has not hidden the hardware cursor (#1858); a
// scrolled view windows over [scrollback ++ screen] instead. A dead or failed
// session renders its state. The scrollback scrollbar (#1368) overlays the
// rightmost column on top of whatever the base view rendered.
func (m Model) View() string {
	return m.overlayScrollbar(m.baseView())
}

// baseView is the view without the scrollbar overlay.
func (m Model) baseView() string {
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
	// An open scrollback search (#1169) can land on the live screen (scroll
	// back at 0): matches highlight and the field replaces the bottom row,
	// standing in for the cursor/completion chrome while it owns the keys.
	if m.search != nil {
		lines := strings.Split(view, "\n")
		m.searchHighlight(lines, m.sess.ScrollbackLen())
		for len(lines) < m.h {
			lines = append(lines, "")
		}
		lines[len(lines)-1] = m.searchLine()
		return strings.Join(lines, "\n")
	}
	if !m.sess.Running() || m.sess.PipeDone() {
		return m.deadView(view)
	}
	if !m.focused {
		return view
	}
	// A child that hid the hardware cursor (DECTCEM, #1858) paints its own
	// into the grid — overlaying ours on top would show a second one.
	if m.sess.CursorHidden() {
		return m.completionView(view)
	}
	cx, cy := m.sess.CursorPosition()
	return m.completionView(overlayCursor(view, cx, cy))
}

// deadView renders a finished session. A tool pane (#810) composites a
// centered exit dialog over the last output — prominent even in fullscreen;
// a pane too small for the dialog (and every non-tool command session) falls
// back to the footer line, truncating the grid by one row when it fills the
// pane so the footer stays visible inside the fixed pane height.
func (m Model) deadView(view string) string {
	if g, ok := m.deadDialogGeom(); ok {
		return overlay.Place(view, m.renderDeadDialog(g), g.x, g.y, m.w, m.h)
	}
	lines := strings.Split(view, "\n")
	if m.h > 0 && len(lines) >= m.h {
		lines = lines[:m.h-1]
	}
	return strings.Join(append(lines, m.exitLine()), "\n")
}

// Dead-dialog button labels (#810). ASCII: the click mapping equates visual
// columns with byte offsets.
const (
	deadRestartBtn = "[ Restart (r) ]"
	deadCloseBtn   = "[ Close (ctrl+w) ]"
	deadBtnGap     = "  "
	deadDialogPad  = 2 // horizontal padding inside the border
)

// deadDialog is the exit dialog geometry, shared by the renderer and the
// click mapping so both always agree.
type deadDialog struct {
	title, buttons   string
	x, y, w, h       int // outer box rect, pane-local
	btnRow           int // absolute row of the buttons line
	restartX, closeX int // absolute start columns of the buttons
}

// deadDialogGeom computes the centered dialog geometry for a finished tool
// session; ok=false when the session is alive, not a tool, or the pane is
// too small for the box (the footer fallback then applies).
func (m Model) deadDialogGeom() (deadDialog, bool) {
	if m.sess == nil || m.sess.Running() || m.tool == "" {
		return deadDialog{}, false
	}
	title := m.tool + " exited"
	if c, ok := m.sess.ExitCode(); ok {
		title += " (code " + strconv.Itoa(c) + ")"
	}
	buttons := deadRestartBtn + deadBtnGap + deadCloseBtn
	inner := len(buttons)
	if len(title) > inner {
		inner = len(title)
	}
	g := deadDialog{
		title:   title,
		buttons: buttons,
		w:       inner + 2*deadDialogPad + 2, // padding + border
		h:       5,                           // border + title + blank + buttons + border
	}
	if g.w > m.w || g.h > m.h {
		return deadDialog{}, false
	}
	g.x = (m.w - g.w) / 2
	g.y = (m.h - g.h) / 2
	g.btnRow = g.y + 3 // border + title + blank
	g.restartX = g.x + 1 + deadDialogPad
	g.closeX = g.restartX + len(deadRestartBtn) + len(deadBtnGap)
	return g, true
}

// renderDeadDialog draws the dialog box for geometry g.
func (m Model) renderDeadDialog(g deadDialog) string {
	var accent, borderCol color.Color = lipgloss.White, lipgloss.White
	if m.pal != nil {
		accent, borderCol = m.pal.Accent, m.pal.Border
	}
	btnStyle := lipgloss.NewStyle().Bold(true).Reverse(true)
	buttons := btnStyle.Foreground(accent).Render(deadRestartBtn) +
		deadBtnGap + btnStyle.Render(deadCloseBtn)
	title := lipgloss.NewStyle().Bold(true).Render(g.title)
	inner := g.w - 2*deadDialogPad - 2
	body := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Width(inner).Render(title),
		"",
		buttons,
	)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderCol).
		Padding(0, deadDialogPad).
		Render(body)
}

// exitLine renders the completion marker: command sessions (#574) report the
// exit code so a run's outcome is visible at a glance; a tool pane too small
// for the dialog (#810) falls back to the footer actions here. Kept ASCII —
// DeadActionHit maps click columns onto byte offsets of this string.
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

// DeadActionHit maps a click in a finished tool pane onto an exit-dialog
// button (#810) — "restart", "close", or "" for anywhere else. x/y are
// pane-local content coordinates. Falls back to the footer-line spans when
// the pane is too small for the dialog.
func (m Model) DeadActionHit(x, y int) string {
	if m.sess == nil || m.sess.Running() || m.tool == "" {
		return ""
	}
	if g, ok := m.deadDialogGeom(); ok {
		if y != g.btnRow {
			return ""
		}
		switch {
		case x >= g.restartX && x < g.restartX+len(deadRestartBtn):
			return "restart"
		case x >= g.closeX && x < g.closeX+len(deadCloseBtn):
			return "close"
		}
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
			// Scrollback rows decorate as they are windowed in (#1168) — the
			// same underline affordance the cached live render carries.
			rows = append(rows, decorateLinkLine(m.sess.HistoryLine(virtual)))
		case virtual-sbLen < len(screen):
			rows = append(rows, screen[virtual-sbLen])
		}
	}
	m.highlightSelection(rows, sbLen-off)
	marker := "[scrollback -" + strconv.Itoa(off) + "  shift+pgdn to return]"
	// An open scrollback search (#1169) highlights its matches and takes the
	// marker row for its input field.
	if m.search != nil {
		m.searchHighlight(rows, sbLen-off)
		marker = m.searchLine()
	}
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
		from, to := 0, m.gridW()
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

// forEachGlyph walks an ANSI-styled line grapheme-cluster-aware, invoking
// visit once per visible glyph with its starting grid cell and cell width
// (2 for wide glyphs such as emoji, 0 for combining marks that couldn't
// attach to a preceding cluster). Escape sequences are copied verbatim.
// It returns the rendered result and the total number of cells the line
// occupies, so callers can pad past the rendered content.
func forEachGlyph(line string, visit func(cluster string, col, width int) string) (string, int) {
	var b strings.Builder
	col := 0
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
			// CSI sequences end on a final byte in @-~; the two-byte forms
			// (ESC + single char) end immediately.
			if line[i] >= 0x40 && line[i] <= 0x7e && line[i] != '[' {
				inEsc = false
			}
			i++
			continue
		}
		cluster, w := ansi.FirstGraphemeCluster(line[i:], ansi.GraphemeWidth)
		b.WriteString(visit(cluster, col, w))
		col += w
		i += len(cluster)
	}
	return b.String(), col
}

// reverseSpan reverse-videos the cells [from, to) of an ANSI-styled line,
// padding past the rendered content so a selection reads full-width. A glyph
// that only partially overlaps the range (e.g. a wide emoji straddling the
// boundary) is reversed as a whole unit.
func reverseSpan(line string, from, to int) string {
	out, visible := forEachGlyph(line, func(cluster string, col, w int) string {
		if col+w > from && col < to {
			return cursorStyle.Render(cluster)
		}
		return cluster
	})
	if visible >= to {
		return out
	}
	var b strings.Builder
	b.WriteString(out)
	if pad := from - visible; pad > 0 {
		b.WriteString(strings.Repeat(" ", pad))
		visible = from
	}
	b.WriteString(cursorStyle.Render(strings.Repeat(" ", to-visible)))
	return b.String()
}

// reverseCell restyles the cell at column col of an ANSI-styled line. A
// cursor sitting on top of a wide glyph reverses the whole glyph, not just
// its left half.
func reverseCell(line string, col int) string {
	done := false
	out, visible := forEachGlyph(line, func(cluster string, c, w int) string {
		if !done && col >= c && col < c+w {
			done = true
			return cursorStyle.Render(cluster)
		}
		return cluster
	})
	if done {
		return out
	}
	// Cursor past the rendered content: pad with spaces up to the column.
	var b strings.Builder
	b.WriteString(out)
	if pad := col - visible; pad > 0 {
		b.WriteString(strings.Repeat(" ", pad))
	}
	b.WriteString(cursorStyle.Render(" "))
	return b.String()
}
