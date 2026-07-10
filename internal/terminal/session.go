// Package terminal is the integrated terminal core (Roadmap 0170, #95): a
// PTY-spawned shell whose output feeds a VT emulator
// (charmbracelet/x/vt), rendered as a pane. The Session owns the process and
// the emulator; the pane-facing Model in model.go adapts it to the pane
// registry. Workspace integration (splits, persistence, scrollback paging)
// and command polish are the follow-up issues (#96, #97).
package terminal

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
// needs to repaint. Key identifies the owning pane.
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
	dir   string
	em    *vt.SafeEmulator
	send  func(tea.Msg)

	mu     sync.Mutex
	ptmx   *os.File
	cmd    *exec.Cmd
	w, h   int
	title  string // last OSC 0/2 title the application set ("" until then)
	closed atomic.Bool
	// mouseModes holds the DEC mouse-reporting modes the child currently has
	// enabled (?9/?1000/…); non-empty means wheel events belong to the child.
	mouseModes map[ansi.Mode]struct{}

	notifyPending atomic.Bool
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
	if w < 2 || h < 2 {
		w, h = 80, 24
	}
	// Pin the origin dir absolutely: it outlives project switches (which
	// chdir the process) as the session's marker and respawn target.
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	cmd := exec.Command(shell)
	cmd.Dir = dir
	cmd.Env = MergeEnv(append(os.Environ(), "TERM=xterm-256color"), extraEnv)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)})
	if err != nil {
		return nil, fmt.Errorf("terminal: start %s: %w", shell, err)
	}
	s := &Session{
		key:   key,
		shell: shell,
		dir:   dir,
		em:    vt.NewSafeEmulator(w, h),
		send:  send,
		ptmx:  ptmx,
		cmd:   cmd,
		w:     w, h: h,
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
	go s.readLoop()
	go s.writeLoop()
	go s.waitExit()
	return s, nil
}

// readLoop pumps PTY output into the emulator and requests repaints,
// coalesced so a burst of output paints once per quiet interval.
func (s *Session) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			_, _ = s.em.Write(buf[:n])
			s.notify()
		}
		if err != nil {
			return
		}
	}
}

// writeLoop pumps the emulator's host-bound bytes (key encodings from
// SendKey, terminal query replies like DA1/DSR) into the PTY.
func (s *Session) writeLoop() {
	_, _ = io.Copy(s.ptmx, s.em)
}

// waitExit closes the session when the shell process ends and tells the app.
func (s *Session) waitExit() {
	_ = s.cmd.Wait()
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

// Resize propagates a pane size change to the PTY (SIGWINCH for the child)
// and the emulator. Same-size calls are no-ops.
func (s *Session) Resize(w, h int) {
	if w < 2 || h < 2 || s.closed.Load() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if w == s.w && h == s.h {
		return
	}
	s.w, s.h = w, h
	_ = pty.Setsize(s.ptmx, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)})
	s.em.Resize(w, h)
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

// View renders the current screen as ANSI-styled lines.
func (s *Session) View() string {
	return s.em.Render()
}

// CursorPosition returns the emulator's cursor cell (column, row).
func (s *Session) CursorPosition() (x, y int) {
	pos := s.em.CursorPosition()
	return pos.X, pos.Y
}

// Running reports whether the shell is still alive.
func (s *Session) Running() bool { return !s.closed.Load() }

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
	_, _ = s.em.WriteString("\x1b[2J\x1b[3J\x1b[H")
	s.em.SendKey(vt.KeyPressEvent{Code: 'l', Mod: vt.ModCtrl})
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

// Close ends the session: the child is terminated and the PTY closed. Safe to
// call more than once.
func (s *Session) Close() {
	if !s.closed.CompareAndSwap(false, true) {
		return
	}
	s.teardown()
}

// teardown releases the process and PTY.
func (s *Session) teardown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.ptmx.Close()
	_ = s.em.Close()
}
