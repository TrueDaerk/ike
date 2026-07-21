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

	mu       sync.Mutex
	ptmx     *os.File
	cmd      *exec.Cmd
	w, h     int
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
	// gridMu serializes the feed loop's emulator writes against the resize
	// snapshot/restore (#807): SafeEmulator locks each call, but CellAt
	// returns a pointer into the live buffer — copying the cell after the
	// call returns would race a concurrent feed write.
	gridMu sync.Mutex
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
// Around the emulator resize it maintains the content reserve (#807): the
// upstream emulator destroys clipped cells on shrink, so the screen is
// snapshotted before and the clipped region restored after a grow.
func (s *Session) applyResizeLocked(w, h int) {
	oldW, oldH := s.w, s.h
	// gridMu keeps the feed loop out for the whole snapshot → resize →
	// restore sequence, so the copied cells cannot race a concurrent write
	// and no child output lands between snapshot and restore.
	s.gridMu.Lock()
	s.snapshotReserveLocked(oldW, oldH)
	s.w, s.h = w, h
	s.lastResize = time.Now()
	_ = pty.Setsize(s.ptmx, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)})
	s.em.Resize(w, h)
	s.restoreReserveLocked(oldW, oldH, w, h)
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

// restoreReserveLocked writes reserved cells back after a grow. Width: each
// row that still prefix-matches its reserve row gets the clipped columns
// back. Height: the rows a shrink dropped come back only when every
// overlapping row matched — content that scrolled meanwhile shifts row
// indexes, and restoring then would resurrect stale lines.
func (s *Session) restoreReserveLocked(oldW, oldH, w, h int) {
	if len(s.reserve) == 0 {
		return
	}
	allMatch := true
	overlap := min(min(oldH, h), len(s.reserve))
	for y := 0; y < overlap; y++ {
		cur := s.screenRowLocked(min(oldW, w), y)
		if !rowPrefixEqual(s.reserve[y], cur, min(oldW, w)) {
			allMatch = false
			continue
		}
		if w > oldW { // width grow: fill the clipped columns
			for x := oldW; x < w && x < len(s.reserve[y]); x++ {
				c := s.reserve[y][x]
				s.em.SetCell(x, y, &c)
			}
		}
	}
	if h > oldH && allMatch { // height grow: bring the dropped rows back
		for y := oldH; y < h && y < len(s.reserve); y++ {
			for x := 0; x < w && x < len(s.reserve[y]); x++ {
				c := s.reserve[y][x]
				s.em.SetCell(x, y, &c)
			}
		}
	}
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
