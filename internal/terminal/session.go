// Package terminal is the integrated terminal core (Roadmap 0170, #95): a
// PTY-spawned shell whose output feeds a VT emulator
// (charmbracelet/x/vt), rendered as a pane. The Session owns the process and
// the emulator; the pane-facing Model in model.go adapts it to the pane
// registry. Workspace integration (splits, persistence, scrollback paging)
// and command polish are the follow-up issues (#96, #97).
package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// OutputMsg reports that the emulator's screen changed; the root model only
// needs to repaint. Key identifies the owning pane. The app's input coalescer
// additionally folds concurrent OutputMsgs across sessions into one batch per
// flush (#803), so N busy terminals cannot multiply the render rate.
type OutputMsg struct{ Key string }

// ExitedMsg reports that the shell process ended; the root model closes the
// pane (or marks it dead when it is the last leaf).
type ExitedMsg struct{ Key string }

// notifyQuiet coalesces output notifications: heavy PTY output (yes, seq or
// a build log) must not flood the render loop, so at most one repaint request
// is in flight per interval.
const notifyQuiet = 8 * time.Millisecond

// Session is one live shell: the PTY, the process and the emulator holding
// the screen state. All methods are safe for concurrent use — the read loop
// writes into the emulator while Update/View read from it (SafeEmulator).
type Session struct {
	key   string
	shell string
	// argv is the full command line when the session runs a program instead
	// of an interactive shell (0350, #574); nil for plain shell sessions.
	argv []string
	dir  string
	em   *vt.SafeEmulator
	send func(tea.Msg)

	mu   sync.Mutex
	ptmx *os.File
	cmd  *exec.Cmd
	w, h int
	// Resize debounce (#804): lastResize stamps the leading apply; a burst
	// inside resizeQuiet parks its latest size in pendW/H behind one timer.
	lastResize    time.Time
	pendW, pendH  int
	resizePending bool
	// Resize reserve (#807): the fullest known content per screen row. The
	// upstream emulator hard-truncates the grid on shrink, so before every
	// applied resize the visible screen is snapshotted here, and after a grow
	// the clipped cells are written back — guarded by a prefix match, so a
	// row the child rewrote meanwhile is never corrupted.
	reserve  []uv.Line
	reserveW int
	// Height-shrink scroll state (#826): shrinkPushed counts the screen rows
	// a height shrink scrolled into the scrollback (real-terminal semantics —
	// the top scrolls out, the cursor line stays); a later grow pulls them
	// back. shrinkMark remembers the scrollback length right after the push:
	// child output pushing lines meanwhile buries ours, so the pull is
	// abandoned instead of resurrecting the wrong rows.
	shrinkPushed int
	shrinkMark   int
	// Reflow cache (#953): the logical lines the last width-reflow replay
	// wrote. The next reflow consumes grid rows that still match these lines
	// verbatim, so their hard breaks are known instead of guessed — the
	// exact-width soft-wrap ambiguity cannot corrupt them across repeated
	// resizes. Reset by Clear (the 3J wipes the content it describes).
	reflowCache []uv.Line
	// gridMu serializes the feed loop's emulator writes against the resize
	// snapshot/restore (#807): SafeEmulator locks each call, but CellAt
	// returns a pointer into the live buffer — copying the cell after the
	// call returns would race a concurrent feed write.
	gridMu   sync.Mutex
	title    string // last OSC 0/2 title the application set ("" until then)
	exitCode int
	exited   bool
	closed   atomic.Bool
	// mouseModes holds the DEC mouse-reporting modes the child currently has
	// enabled (?9/?1000/…); non-empty means wheel events belong to the child.
	mouseModes map[ansi.Mode]struct{}

	notifyPending atomic.Bool

	// version counts grid mutations (feed writes, resizes, clears); the View
	// render cache is keyed by it (#803), so an unchanged grid never pays a
	// second emulator render — with N terminal panes on screen, a frame
	// re-renders only the grids that actually changed.
	version   atomic.Uint64
	viewMu    sync.Mutex
	viewCache string
	viewVer   uint64
	viewValid bool

	// out decouples the PTY read loop from the emulator feed (#734): PTY
	// output is spooled immediately so the kernel TTY queue stays drained
	// even while the emulator or render loop stalls (lock/sleep/resume);
	// buffered bytes replay into the emulator in order, nothing drops.
	out *spool

	// Teardown sequencing (#748): upstream vt keeps Emulator.closed as a
	// plain bool, so Emulator.Close is not safe concurrently with Read/Write.
	// ioWG joins the loops feeding the emulator (read+feed), wlWG the write
	// loop draining it; wlStop tells the write loop a delivered byte is the
	// teardown sentinel, not child input.
	ioWG   sync.WaitGroup
	wlWG   sync.WaitGroup
	wlStop atomic.Bool
}

// Shell resolves the shell to spawn: the config override first, $SHELL next,
// /bin/sh as the safety net.
func Shell(override string) string {
	if override != "" {
		return override
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

// StartSession spawns shell in dir on a new PTY sized w×h and starts the read
// loop. send delivers OutputMsg/ExitedMsg into the program (host.Send).
// extraEnv entries override the inherited environment (toolchain injection,
// #98); nil leaves it untouched beyond TERM.
func StartSession(key, shell, dir string, w, h int, extraEnv []string, send func(tea.Msg)) (*Session, error) {
	return startSession(key, []string{shell}, false, dir, w, h, extraEnv, send)
}

// StartCommandSession spawns argv (a program with arguments, not a shell) on
// a PTY — the run-in-terminal seam (0350, #574). The program is interactive
// like any shell child (stdin is the PTY); its exit code is kept for the
// pane's completion line.
func StartCommandSession(key string, argv []string, dir string, w, h int, extraEnv []string, send func(tea.Msg)) (*Session, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("terminal: empty command")
	}
	return startSession(key, argv, true, dir, w, h, extraEnv, send)
}

// startSession is the shared spawn path; isCommand marks a program session.
// sessSeq makes every session routing key globally unique (#777): pane keys
// restart per registry ("terminal", "terminal:2"), so two workspaces would
// otherwise mint colliding keys and a background shell's ExitedMsg could
// close the active workspace's same-key pane. The key is opaque to every
// consumer — matching goes through SessionKey() string equality only.
var sessSeq uint64

func startSession(key string, argv []string, isCommand bool, dir string, w, h int, extraEnv []string, send func(tea.Msg)) (*Session, error) {
	key = key + "#" + strconv.FormatUint(atomic.AddUint64(&sessSeq, 1), 10)
	if w < 2 || h < 2 {
		w, h = 80, 24
	}
	// Pin the origin dir absolutely: it outlives project switches (which
	// chdir the process) as the session's marker and respawn target.
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = MergeEnv(append(os.Environ(), "TERM=xterm-256color"), extraEnv)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)})
	if err != nil {
		return nil, fmt.Errorf("terminal: start %s: %w", argv[0], err)
	}
	s := &Session{
		key:   key,
		shell: argv[0],
		dir:   dir,
		em:    vt.NewSafeEmulator(w, h),
		send:  send,
		ptmx:  ptmx,
		cmd:   cmd,
		w:     w, h: h,
	}
	if isCommand {
		s.argv = append([]string(nil), argv...)
	}
	s.mouseModes = make(map[ansi.Mode]struct{})
	// OSC 0/2 titles (the shell's running-command reporting) feed the pane
	// title; the callbacks run on the read loop, so they only store.
	s.em.SetCallbacks(vt.Callbacks{
		Title: func(t string) {
			s.mu.Lock()
			s.title = t
			s.mu.Unlock()
			s.notify()
		},
		EnableMode:  func(mode ansi.Mode) { s.trackMouseMode(mode, true) },
		DisableMode: func(mode ansi.Mode) { s.trackMouseMode(mode, false) },
	})
	s.out = newSpool()
	s.ioWG.Add(2)
	s.wlWG.Add(1)
	go s.readLoop()
	go s.feedLoop()
	go s.writeLoop()
	go s.waitExit()
	return s, nil
}

// readLoop drains PTY output into the spool as fast as the kernel delivers
// it. It deliberately does not touch the emulator (#734): a stalled emulator
// or render loop must not backpressure into the kernel TTY queue, where
// output around a suspend/resume window can be flushed and lost.
func (s *Session) readLoop() {
	defer s.ioWG.Done()
	defer s.out.close()
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			s.out.put(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// feedLoop replays spooled PTY output into the emulator in order and requests
// repaints, coalesced so a burst of output paints once per quiet interval.
func (s *Session) feedLoop() {
	defer s.ioWG.Done()
	for {
		chunk, ok := s.out.take()
		if !ok {
			return
		}
		s.gridMu.Lock()
		_, _ = s.em.Write(chunk)
		s.gridMu.Unlock()
		s.version.Add(1)
		s.notify()
	}
}

// writeLoop pumps the emulator's host-bound bytes (key encodings from
// SendKey, terminal query replies like DA1/DSR) into the PTY. It keeps
// draining after a PTY write error (teardown closed the PTY) so a query
// reply from the emulator feed can never block on the host-bound pipe, and
// exits on the teardown sentinel (#748) — only Emulator.Close would
// otherwise unblock its Read, and that call races a concurrent reader.
func (s *Session) writeLoop() {
	defer s.wlWG.Done()
	buf := make([]byte, 4096)
	for {
		n, err := s.em.Read(buf)
		if n > 0 && !s.wlStop.Load() {
			_, _ = s.ptmx.Write(buf[:n])
		}
		if err != nil || s.wlStop.Load() {
			return
		}
	}
}

// waitExit closes the session when the shell process ends and tells the app.
func (s *Session) waitExit() {
	_ = s.cmd.Wait()
	s.mu.Lock()
	if state := s.cmd.ProcessState; state != nil {
		s.exitCode, s.exited = state.ExitCode(), true
	}
	s.mu.Unlock()
	if s.closed.CompareAndSwap(false, true) {
		s.teardown()
		if s.send != nil {
			s.send(ExitedMsg{Key: s.key})
		}
	}
}

// notify schedules one OutputMsg per quiet interval.
func (s *Session) notify() {
	if s.send == nil || !s.notifyPending.CompareAndSwap(false, true) {
		return
	}
	time.AfterFunc(notifyQuiet, func() {
		s.notifyPending.Store(false)
		if !s.closed.Load() {
			s.send(OutputMsg{Key: s.key})
		}
	})
}

// resizeQuiet debounces rapid resize sequences (#804): a divider drag emits
// one resize per motion flush, and every applied resize costs a PTY SIGWINCH
// (the child — vim, htop — redraws) plus an emulator reflow. The first resize
// applies immediately (leading edge, so a lone layout change is instant);
// further ones inside the window fold into one trailing apply of the final
// size. Meanwhile the pane clips/pads the stale-size grid.
const resizeQuiet = 100 * time.Millisecond

// Resize propagates a pane size change to the PTY (SIGWINCH for the child)
// and the emulator, debounced per resizeQuiet. Same-size calls are no-ops.
func (s *Session) Resize(w, h int) {
	if w < 2 || h < 2 || s.closed.Load() {
		return
	}
	s.mu.Lock()
	if s.resizePending {
		s.pendW, s.pendH = w, h // fold into the armed trailing apply
		s.mu.Unlock()
		return
	}
	if w == s.w && h == s.h {
		s.mu.Unlock()
		return
	}
	if time.Since(s.lastResize) >= resizeQuiet {
		s.applyResizeLocked(w, h)
		s.mu.Unlock()
		return
	}
	s.pendW, s.pendH = w, h
	s.resizePending = true
	s.mu.Unlock()
	time.AfterFunc(resizeQuiet, s.flushResize)
}

// applyResizeLocked performs the actual PTY + emulator resize; s.mu held.
//
// A width change on the primary screen reflows (#935): the whole history is
// rewrapped at the new width as if the terminal had always been that size.
// Height-only changes keep the #807/#826 reserve machinery (scroll-push on
// shrink, guarded pull/restore on grow) — its semantics are exactly right
// vertically and battle-tested. The alt screen never reflows: its apps
// repaint themselves on SIGWINCH.
func (s *Session) applyResizeLocked(w, h int) {
	oldW, oldH := s.w, s.h
	// gridMu keeps the feed loop out for the whole snapshot → resize →
	// restore sequence, so the copied cells cannot race a concurrent write
	// and no child output lands between snapshot and restore.
	s.gridMu.Lock()
	if w != oldW && !s.em.IsAltScreen() {
		lines, tail := s.logicalLinesLocked(oldW, oldH)
		s.w, s.h = w, h
		s.lastResize = time.Now()
		_ = pty.Setsize(s.ptmx, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)})
		s.em.Resize(w, h)
		s.replayLocked(lines, tail, w)
		// The replay rewrote everything at the new width; stale reserve rows
		// would only poison later prefix matches.
		s.reserve, s.reserveW = nil, 0
		s.shrinkPushed, s.shrinkMark = 0, 0
		s.gridMu.Unlock()
		s.version.Add(1)
		return
	}
	// The height-restore guard compares BEFORE the snapshot folds the current
	// screen into the reserve — afterwards the overlap matches trivially and
	// stale reserve rows beyond oldH would resurrect over newer content.
	heightMatch := s.reserveMatchesLocked(min(oldW, w), min(oldH, h))
	s.snapshotReserveLocked(oldW, oldH)
	if h < oldH {
		s.scrollShrinkLocked(oldW, oldH, h)
	}
	s.w, s.h = w, h
	s.lastResize = time.Now()
	_ = pty.Setsize(s.ptmx, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)})
	s.em.Resize(w, h)
	if h > oldH {
		s.pullShrinkLocked(oldH, w, h)
	}
	s.restoreReserveLocked(oldW, oldH, w, h, heightMatch)
	s.gridMu.Unlock()
	s.version.Add(1)
}

// snapshotReserveLocked folds the current screen into the reserve: a row
// whose visible cells still prefix-match its reserve row keeps the longer
// reserved content, anything else is replaced by what is on screen now.
// Rows beyond the current height are kept for a later height grow.
func (s *Session) snapshotReserveLocked(w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	if len(s.reserve) < h {
		s.reserve = append(s.reserve, make([]uv.Line, h-len(s.reserve))...)
	}
	for y := 0; y < h; y++ {
		row := make(uv.Line, w)
		for x := 0; x < w; x++ {
			if c := s.em.CellAt(x, y); c != nil {
				row[x] = *c
			} else {
				row[x] = uv.EmptyCell
			}
		}
		if !rowPrefixEqual(s.reserve[y], row, w) {
			s.reserve[y] = row
		}
	}
	if w > s.reserveW {
		s.reserveW = w
	}
}

// reserveMatchesLocked reports whether every screen row up to h still
// prefix-matches its reserve row over the first w columns. Must run before
// snapshotReserveLocked in the same apply — the snapshot syncs the overlap,
// after which the comparison is vacuously true.
func (s *Session) reserveMatchesLocked(w, h int) bool {
	if len(s.reserve) == 0 {
		return false
	}
	for y := 0; y < h && y < len(s.reserve); y++ {
		if !rowPrefixEqual(s.reserve[y], s.screenRowLocked(w, y), w) {
			return false
		}
	}
	return true
}

// restoreReserveLocked writes reserved cells back after a grow. Width: each
// row that still prefix-matches its reserve row gets the clipped columns
// back. Height: the rows a shrink dropped come back only when the whole
// pre-snapshot overlap matched (heightMatch) — content that scrolled or was
// rewritten meanwhile shifts row indexes, and restoring then would resurrect
// stale lines over newer content.
func (s *Session) restoreReserveLocked(oldW, oldH, w, h int, heightMatch bool) {
	if len(s.reserve) == 0 {
		return
	}
	overlap := min(min(oldH, h), len(s.reserve))
	for y := 0; y < overlap; y++ {
		cur := s.screenRowLocked(min(oldW, w), y)
		if !rowPrefixEqual(s.reserve[y], cur, min(oldW, w)) {
			continue
		}
		if w > oldW { // width grow: fill the clipped columns
			for x := oldW; x < w && x < len(s.reserve[y]); x++ {
				c := s.reserve[y][x]
				s.em.SetCell(x, y, &c)
			}
		}
	}
	if h > oldH && heightMatch { // height grow: bring the dropped rows back
		for y := oldH; y < h && y < len(s.reserve); y++ {
			for x := 0; x < w && x < len(s.reserve[y]); x++ {
				c := s.reserve[y][x]
				s.em.SetCell(x, y, &c)
			}
		}
	}
}

// scrollShrinkLocked applies real-terminal height-shrink semantics (#826)
// before the emulator resize truncates: when the cursor would fall below the
// new height, the top rows scroll into the scrollback and the screen slides
// up so the cursor line (the prompt, the newest output) survives — upstream
// alone hard-truncates the bottom rows, eating everything below the shrink
// point. Runs before em.Resize, under gridMu; the subsequent Resize clamps
// the emulator cursor to h-1, which is exactly where the slide put its line.
// The alt screen has no scrollback and its apps redraw on SIGWINCH — skip.
func (s *Session) scrollShrinkLocked(w, oldH, h int) {
	if s.em.IsAltScreen() {
		return
	}
	sb := s.em.Scrollback()
	if sb == nil || sb.MaxLines() <= 0 {
		return
	}
	cy := s.em.CursorPosition().Y
	shift := cy - (h - 1)
	if shift <= 0 {
		return
	}
	if shift > oldH {
		shift = oldH
	}
	for y := 0; y < shift; y++ {
		sb.Push(s.screenRowLocked(w, y))
	}
	for y := 0; y < oldH; y++ {
		src := y + shift
		for x := 0; x < w; x++ {
			c := uv.EmptyCell
			if src < oldH {
				if cell := s.em.CellAt(x, src); cell != nil {
					c = *cell
				}
			}
			s.em.SetCell(x, y, &c)
		}
	}
	s.shrinkPushed += shift
	s.shrinkMark = sb.Len()
}

// pullShrinkLocked reverses scrollShrinkLocked after a height grow: the rows
// the shrink pushed come back out of the scrollback onto the top of the
// screen (round-trip identical), the on-screen content slides down and the
// cursor follows via an injected CUP. Only the session's own pushes are
// pulled, and only while they are still the newest scrollback lines — child
// output that scrolled meanwhile buried them, so the pull is abandoned (the
// screen already reflects the newer content). Runs after em.Resize, under
// gridMu.
func (s *Session) pullShrinkLocked(oldH, w, h int) {
	if s.shrinkPushed <= 0 || s.em.IsAltScreen() {
		return
	}
	sb := s.em.Scrollback()
	if sb == nil {
		return
	}
	if sb.Len() != s.shrinkMark {
		s.shrinkPushed = 0
		return
	}
	pull := min(s.shrinkPushed, h-oldH, sb.Len())
	if pull <= 0 {
		return
	}
	// Pop the newest pull lines: the scrollback API only pushes, so the kept
	// prefix is re-pushed after a clear. Line headers are copied first —
	// Push clones into the same backing array Clear truncated.
	all := sb.Lines()
	popped := make([]uv.Line, pull)
	copy(popped, all[len(all)-pull:])
	keep := make([]uv.Line, len(all)-pull)
	copy(keep, all[:len(all)-pull])
	sb.Clear()
	for _, l := range keep {
		sb.Push(l)
	}
	// Slide the screen down (bottom-up: every write lands above-read rows),
	// then lay the popped rows back on top, oldest first.
	for y := oldH - 1; y >= 0; y-- {
		row := s.screenRowLocked(w, y)
		for x := 0; x < w; x++ {
			c := row[x]
			s.em.SetCell(x, y+pull, &c)
		}
	}
	for y := 0; y < pull; y++ {
		for x := 0; x < w; x++ {
			c := uv.EmptyCell
			if x < len(popped[y]) {
				c = popped[y][x]
			}
			s.em.SetCell(x, y, &c)
		}
	}
	// The cursor rides the slide. Injected as CUP through the emulator's
	// input path — the only cursor mutator the safe wrapper exposes.
	pos := s.em.CursorPosition()
	_, _ = s.em.Write([]byte(fmt.Sprintf("\x1b[%d;%dH", pos.Y+pull+1, pos.X+1)))
	s.shrinkPushed -= pull
	s.shrinkMark = sb.Len()
}

// screenRowLocked reads the first n cells of screen row y.
func (s *Session) screenRowLocked(n, y int) uv.Line {
	row := make(uv.Line, n)
	for x := 0; x < n; x++ {
		if c := s.em.CellAt(x, y); c != nil {
			row[x] = *c
		} else {
			row[x] = uv.EmptyCell
		}
	}
	return row
}

// rowPrefixEqual reports whether the first n cells of a and b hold the same
// content. Style differences are ignored — the guard only needs to know the
// text is still the text the reserve captured.
func rowPrefixEqual(a, b uv.Line, n int) bool {
	if len(a) < n || len(b) < n {
		return false
	}
	for i := 0; i < n; i++ {
		if a[i].Content != b[i].Content {
			return false
		}
	}
	return true
}

// flushResize applies the last size a debounced burst settled on.
func (s *Session) flushResize() {
	if s.closed.Load() {
		return
	}
	s.mu.Lock()
	s.resizePending = false
	changed := s.pendW != s.w || s.pendH != s.h
	if changed {
		s.applyResizeLocked(s.pendW, s.pendH)
	}
	s.mu.Unlock()
	if changed {
		s.notify() // repaint at the settled size
	}
}

// SendKey encodes one key press for the child, honouring the emulator's
// input modes (application cursor keys, etc.); the write loop delivers it.
func (s *Session) SendKey(k vt.KeyPressEvent) {
	if !s.closed.Load() {
		s.em.SendKey(k)
	}
}

// trackMouseMode records the child flipping a DEC mouse-reporting mode; other
// modes are none of our business here.
func (s *Session) trackMouseMode(mode ansi.Mode, on bool) {
	switch mode {
	case ansi.ModeMouseX10, ansi.ModeMouseNormal, ansi.ModeMouseHighlight,
		ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent:
		s.mu.Lock()
		if on {
			s.mouseModes[mode] = struct{}{}
		} else {
			delete(s.mouseModes, mode)
		}
		s.mu.Unlock()
	}
}

// WantsMouse reports whether the child currently has any mouse-reporting mode
// enabled — wheel events then belong to it, not to the pane's scrollback.
func (s *Session) WantsMouse() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.mouseModes) > 0
}

// AltScreen reports whether the child is on the alternate screen.
func (s *Session) AltScreen() bool { return s.em.IsAltScreen() }

// SendMouse forwards a mouse event to the child; the emulator encodes it per
// the child's enabled modes (and drops it when none is set).
func (s *Session) SendMouse(m vt.Mouse) {
	if !s.closed.Load() {
		s.em.SendMouse(m)
	}
}

// Paste sends text through the emulator's paste path (bracketed paste when
// the application enabled it).
func (s *Session) Paste(text string) {
	if !s.closed.Load() {
		s.em.Paste(text)
	}
}

// View renders the current screen as ANSI-styled lines, cached per grid
// version (#803): an unchanged grid returns the previous render without
// touching the emulator. A version bump racing the render at worst stamps a
// newer grid under an older version — the next call recomputes; a stale grid
// can never be served for a newer version.
func (s *Session) View() string {
	v := s.version.Load()
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	if s.viewValid && s.viewVer == v {
		return s.viewCache
	}
	s.viewCache, s.viewVer, s.viewValid = s.em.Render(), v, true
	return s.viewCache
}

// CursorPosition returns the emulator's cursor cell (column, row).
func (s *Session) CursorPosition() (x, y int) {
	pos := s.em.CursorPosition()
	return pos.X, pos.Y
}

// Running reports whether the shell is still alive.
func (s *Session) Running() bool { return !s.closed.Load() }

// IsCommand reports whether the session runs a program (0350, #574) rather
// than an interactive shell.
func (s *Session) IsCommand() bool { return s.argv != nil }

// Argv returns the command session's full command line, nil for shells.
func (s *Session) Argv() []string { return s.argv }

// Pid returns the child process id, or 0 before it starts / after it is torn
// down. Used to answer a DAP runInTerminal reverse request with the debuggee's
// process id (#625).
func (s *Session) Pid() int {
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

// ExitCode returns the child's exit status once it ended; ok is false while
// it still runs (or when the session was torn down before Wait observed it).
func (s *Session) ExitCode() (code int, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode, s.exited
}

// Shell returns the spawned shell binary; Dir the directory it started in
// (the session's origin root — the live cwd is the shell's business).
func (s *Session) ShellPath() string { return s.shell }
func (s *Session) Dir() string       { return s.dir }

// Title returns the last OSC 0/2 title the running application set, "" when
// none arrived yet.
func (s *Session) Title() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.title
}

// Clear empties the scrollback and the visible screen (the JetBrains "clear
// buffer" behaviour), then asks the shell to repaint its prompt via the
// ctrl+l convention. The screen wipe happens emulator-side so the buffer is
// clean even when the running program ignores ^L.
func (s *Session) Clear() {
	if s.closed.Load() {
		return
	}
	// 2J pushes the visible lines into the scrollback (the xterm behaviour),
	// 3J then erases the scrollback — the canonical clear-everything pair.
	// Via Write, not WriteString: SafeEmulator wraps only Write, and the
	// promoted Emulator.WriteString would bypass the lock and race the feed
	// loop (#803).
	_, _ = s.em.Write([]byte("\x1b[2J\x1b[3J\x1b[H"))
	s.em.SendKey(vt.KeyPressEvent{Code: 'l', Mod: vt.ModCtrl})
	s.mu.Lock()
	s.reflowCache = nil // the content it described is gone (#953)
	s.mu.Unlock()
	s.version.Add(1)
	s.notify()
}

// ScrollbackLen reports how many lines have scrolled off the screen.
func (s *Session) ScrollbackLen() int { return s.em.ScrollbackLen() }

// HistoryLine renders scrollback line y (0 = oldest) with its styles.
func (s *Session) HistoryLine(y int) string {
	w := s.em.Width()
	line := uv.NewLine(w)
	for x := 0; x < w; x++ {
		if c := s.em.ScrollbackCellAt(x, y); c != nil {
			line.Set(x, c)
		}
	}
	return line.Render()
}

// LineText returns the plain text of virtual line v — an index into
// [scrollback ++ screen] — without styles, right-trimmed. Out-of-range lines
// are empty.
func (s *Session) LineText(v int) string {
	sb := s.em.ScrollbackLen()
	w := s.em.Width()
	var b strings.Builder
	for x := 0; x < w; x++ {
		var c *uv.Cell
		if v < sb {
			c = s.em.ScrollbackCellAt(x, v)
		} else {
			c = s.em.CellAt(x, v-sb)
		}
		switch {
		case c == nil:
			b.WriteByte(' ')
		case c.Width == 0:
			// Continuation cell of a wide rune: already covered by its head.
		case c.Content == "":
			b.WriteByte(' ')
		default:
			b.WriteString(c.Content)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// Width reports the terminal grid width in columns.
func (s *Session) Width() int { return s.em.Width() }

// SoftWrapped reports whether virtual line v — an index into
// [scrollback ++ screen] — continues into v+1 because the renderer ran out of
// columns, rather than because the program printed a newline (#936). The
// emulator keeps no per-row wrap metadata, so this is the heuristic every
// terminal without shell integration uses: a row whose final column is
// occupied wrapped into the next one. The one ambiguity — a hard-newline line
// that exactly fills the width — reads as wrapped and joins on copy.
//
// Width changes reflow the whole history (#935), so clipped lines no longer
// arise from resizes; the #947 guards below stay for content that predates a
// reflow (an alt-screen phase, a legacy session): lines wider than the
// viewport, or screen rows still prefix-matching a wider resize reserve, are
// clips — never wraps. Better a missed join than chaining unrelated lines.
func (s *Session) SoftWrapped(v int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.softWrappedLocked(v)
}

// softWrappedLocked is SoftWrapped's body; s.mu held.
func (s *Session) softWrappedLocked(v int) bool {
	sb := s.em.ScrollbackLen()
	w := s.em.Width()
	if w <= 0 || v < 0 || v >= sb+s.em.Height()-1 {
		return false // the last virtual line has nothing to continue into
	}
	if v < sb {
		// Stored wider than the viewport: a clipped long line, not a wrap.
		if s.em.ScrollbackCellAt(w, v) != nil {
			return false
		}
		return cellOccupied(s.em.ScrollbackCellAt(w-1, v))
	}
	y := v - sb
	if !cellOccupied(s.em.CellAt(w-1, y)) {
		return false
	}
	// Full row: a prefix-matching reserve row wider than the viewport means
	// the row was written on a wider grid and merely got cut (or ended
	// exactly at w) — not a wrap. Rows rewritten since fail the prefix match
	// and take the plain heuristic.
	if s.reserveW > w && y < len(s.reserve) && len(s.reserve[y]) > w {
		s.gridMu.Lock()
		match := rowPrefixEqual(s.reserve[y], s.screenRowLocked(w, y), w)
		s.gridMu.Unlock()
		if match {
			return false
		}
	}
	return true
}

// --- width reflow (#935) ---------------------------------------------------

// logicalLinesLocked reconstructs the logical lines of [scrollback ++ screen]
// at the current (pre-resize) width. The reflow cache (#953) is consulted
// first: it holds the logical lines the last replay wrote, so hard breaks in
// the matched prefix are KNOWN — the exact-width heuristic ambiguity cannot
// misjoin them, and repeated shrink/grow cycles stay lossless. Rows the cache
// does not cover (output since the last replay: prompt redraws, new commands)
// fall back to the softWrappedLocked heuristic. Rows below both the cursor
// and the last content row are dropped — the cursor's own (possibly blank)
// row survives, so the replay puts the cursor back where the shell left it.
// s.mu and s.gridMu held.
func (s *Session) logicalLinesLocked(w, h int) (lines, tail []uv.Line) {
	sb := s.em.ScrollbackLen()
	last := s.em.CursorPosition().Y
	for y := h - 1; y > last; y-- {
		if len(trimTrailingBlank(s.screenRowLocked(w, y))) > 0 {
			last = y
			break
		}
	}
	total := sb + last + 1

	// The tail is the last logical content line (and anything below it): the
	// shell's live edit line. It is NOT reflowed — its physical rows replay
	// verbatim (clipped on shrink) so an interactive shell's own SIGWINCH
	// redraw finds the row geometry it remembers and repaints cleanly,
	// instead of walking up over relaid-out history rows (#953). It anchors
	// on the last content row, NOT the cursor: a resize can catch the shell
	// mid-redraw with the cursor parked high in the grid, and trusting it
	// would clip whole history rows as "tail".
	tailStart := total - 1
	if tailStart < 0 {
		tailStart = 0
	}
	for tailStart > 0 && s.softWrappedLocked(tailStart-1) {
		tailStart--
	}

	rows := make([]uv.Line, 0, total)
	for v := 0; v < total; v++ {
		var row uv.Line
		if v < sb {
			for x := 0; ; x++ {
				c := s.em.ScrollbackCellAt(x, v)
				if c == nil {
					break
				}
				row = append(row, *c)
			}
		} else {
			row = s.screenRowLocked(w, v-sb)
		}
		rows = append(rows, row)
	}

	var consumed int
	lines, consumed = reconcileCache(s.reflowCache, rows[:tailStart], w)

	var pending uv.Line
	for v := consumed; v < tailStart; v++ {
		wrapped := v < tailStart-1 && s.softWrappedLocked(v)
		if wrapped {
			// A wrapped row is full by definition; keep it verbatim so the
			// continuation glues seamlessly.
			pending = append(pending, rows[v]...)
			continue
		}
		pending = append(pending, trimTrailingBlank(rows[v])...)
		lines = append(lines, trimTrailingBlank(pending))
		pending = nil
	}
	for v := tailStart; v < total; v++ {
		tail = append(tail, trimTrailingBlank(rows[v]))
	}
	return lines, tail
}

// reconcileCache consumes grid rows from the top while they still are the
// rewrap of the cached logical lines at width w (#953), returning those lines
// verbatim — their hard breaks are authoritative. The first mismatching line
// (rewritten or new content) stops the walk; when even the first cached line
// no longer matches (e.g. the scrollback cap trimmed it), whole leading cache
// lines are skipped until one aligns with row 0 again.
func reconcileCache(cache []uv.Line, rows []uv.Line, w int) (lines []uv.Line, consumed int) {
	for skip := 0; skip < len(cache); skip++ {
		r := 0
		for _, cl := range cache[skip:] {
			seg := rewrapLine(cl, w)
			if r+len(seg) > len(rows) || !segMatches(seg, rows[r:r+len(seg)]) {
				break
			}
			lines = append(lines, cl)
			r += len(seg)
		}
		if r > 0 {
			return lines, r
		}
	}
	return nil, 0
}

// rewrapLine chunks a logical line into the physical rows the emulator would
// produce at width w: display cells accumulate up to w, zero-width
// continuation cells stay with their head, and a wide cell that would
// straddle the edge moves to the next row whole. An empty line is one empty
// row.
func rewrapLine(l uv.Line, w int) []uv.Line {
	if w <= 0 || len(l) == 0 {
		return []uv.Line{nil}
	}
	var segs []uv.Line
	var cur uv.Line
	used := 0
	for i := 0; i < len(l); i++ {
		cw := l[i].Width
		if cw > 0 && used+cw > w {
			segs = append(segs, cur)
			cur, used = nil, 0
		}
		cur = append(cur, l[i])
		used += cw
	}
	return append(segs, cur)
}

// segMatches reports whether the expected rewrap rows equal the actual grid
// rows by trimmed cell content (styles may drift through render/re-parse and
// do not affect wrap structure).
func segMatches(expected, actual []uv.Line) bool {
	for i := range expected {
		e, a := trimTrailingBlank(expected[i]), trimTrailingBlank(actual[i])
		if len(e) != len(a) {
			return false
		}
		for x := range e {
			if e[x].Content != a[x].Content {
				return false
			}
		}
	}
	return true
}

// replayLocked rewrites the emulator's content from scratch: clear screen
// (2J first — it pushes the stale rows into the scrollback — then 3J to wipe
// that scrollback wholesale), home, then every logical line hard-newline
// separated with no trailing newline (SGR/links preserved via
// uv.Line.Render). The emulator re-wraps each line at the current width
// itself, so wrap state, cursor and scrollback come out exactly as if the
// terminal had always been this size. The written lines become the next
// reflow cache (#953). s.mu and s.gridMu held.
func (s *Session) replayLocked(lines, tail []uv.Line, w int) {
	var b strings.Builder
	b.WriteString("\x1b[0m\x1b[2J\x1b[3J\x1b[H")
	for i, l := range lines {
		if i > 0 {
			b.WriteString("\r\n")
		}
		b.WriteString(l.Render())
	}
	// The tail (the shell's live edit line) keeps its physical rows verbatim,
	// clipped to the new width: an interactive shell repaints it on SIGWINCH
	// and must find the geometry it remembers (#953).
	for i, row := range tail {
		if i > 0 || len(lines) > 0 {
			b.WriteString("\r\n")
		}
		if segs := rewrapLine(row, w); len(segs) > 0 {
			b.WriteString(uv.Line(segs[0]).Render())
		}
	}
	_, _ = s.em.Write([]byte(b.String()))
	// Only the reflowed prefix is cacheable — the tail belongs to the shell.
	s.reflowCache = lines
}

// trimTrailingBlank drops the trailing run of blank cells (the same
// definition the scrollback push uses), keeping styled spaces.
func trimTrailingBlank(l uv.Line) uv.Line {
	end := len(l)
	for end > 0 {
		c := &l[end-1]
		if !c.IsZero() && !c.Equal(&uv.EmptyCell) {
			break
		}
		end--
	}
	return l[:end]
}

// cellOccupied reports whether a cell holds visible content — the soft-wrap
// heuristic's "did the line reach the final column" test. Width-0
// continuation cells count: a wide rune reaches the edge.
func cellOccupied(c *uv.Cell) bool {
	if c == nil {
		return false
	}
	if c.Width == 0 {
		return true
	}
	return c.Content != "" && c.Content != " "
}

// Close ends the session: the child is terminated and the PTY closed. Safe to
// call more than once.
func (s *Session) Close() {
	if !s.closed.CompareAndSwap(false, true) {
		return
	}
	s.teardown()
}

// teardown releases the process, PTY and emulator. The join order matters
// (#748): the read loop ends when the closed PTY errors its Read, the feed
// loop once the spool drains (exit output still reaches the emulator), and
// only then is the write loop stopped — woken by a sentinel byte through the
// host-bound pipe, since nothing else unblocks its Read. Emulator.Close runs
// last, once no goroutine is inside the emulator: upstream vt keeps its
// closed flag as a plain bool, so Close concurrent with Read/Write is a data
// race. The mutex is released before the joins — the feed loop's title
// callback takes it.
func (s *Session) teardown() {
	s.mu.Lock()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.ptmx.Close()
	if s.out != nil {
		s.out.close()
	}
	s.mu.Unlock()
	s.ioWG.Wait()
	s.wlStop.Store(true)
	// The sentinel write blocks until the write loop reads it; if the loop
	// already exited, Close's pipe error below releases the goroutine.
	go func() { _, _ = s.em.InputPipe().Write([]byte{0}) }()
	s.wlWG.Wait()
	_ = s.em.Close()
}
