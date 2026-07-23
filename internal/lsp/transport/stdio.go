// Package transport spawns a language-server process and exposes its stdio as an
// io.ReadWriteCloser the jsonrpc package frames messages over (Roadmap 0100). It
// captures stderr for diagnostics and watches for process exit so the client can
// trigger crash recovery. Pure Go — no CGo.
package transport

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Spec describes how to launch a server.
type Spec struct {
	Command string
	Args    []string
	Env     []string // extra environment, appended to the inherited environment
	Dir     string   // working directory (workspace root); "" = inherit
	// Detached starts the process in a new session (setsid), detached from the
	// controlling terminal. DAP adapters set this: debugpy's launcher otherwise
	// calls tcsetpgrp on the inherited tty to hand the debuggee terminal
	// foreground, which steals the terminal from the TUI — the TUI's next tty
	// read then takes SIGTTIN and the whole process group stops (#620). With no
	// controlling terminal there is nothing for debugpy to grab; DAP is pure
	// stdio, so nothing is lost.
	Detached bool
	// LogPath, when set, tees the server's stderr into this file (append; the
	// parent directory is created, an existing file above logRotateBytes is
	// rotated to "<path>.old" first). Each start writes a header line and each
	// exit a footer with the exit error, so crash reasons survive the process
	// (#715). Log failures are silent — logging must never block the server.
	LogPath string
}

// Process is a running language server. Its ReadWriteCloser reads the server's
// stdout and writes the server's stdin; Close terminates the process.
type Process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	stderr   *ringBuffer
	log      *os.File // nil without Spec.LogPath
	exited   chan struct{}
	waitErr  error
	waitOnce sync.Once
}

// rwc adapts the process's stdout (read) and stdin (write) into one duplex stream.
type rwc struct {
	r io.ReadCloser
	w io.WriteCloser
}

func (p rwc) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p rwc) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p rwc) Close() error {
	werr := p.w.Close()
	rerr := p.r.Close()
	if werr != nil {
		return werr
	}
	return rerr
}

// ErrNotFound wraps the case where the server binary is not on PATH (nor in a
// known toolchain install directory, see Resolve), so callers can degrade
// gracefully (a missing server is a no-op, never a crash).
var ErrNotFound = errors.New("transport: server binary not found")

// Start launches the server described by spec. The command is resolved via
// Resolve — PATH first, then well-known toolchain install directories (#370:
// `go install` targets GOBIN, which plain sessions rarely have on PATH) — and
// launched by absolute path. It returns ErrNotFound (wrapped) when the binary
// cannot be located, so the manager can disable that language with a status
// message instead of failing hard.
func Start(spec Spec) (*Process, error) {
	bin, err := Resolve(spec.Command)
	if err != nil {
		return nil, errors.Join(ErrNotFound, err)
	}
	cmd := exec.Command(bin, spec.Args...)
	cmd.Dir = spec.Dir
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	if spec.Detached {
		detach(cmd)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := newRingBuffer(64 * 1024)
	logFile := openLog(spec.LogPath)
	if logFile != nil {
		writeLogLine(logFile, "--- started: "+bin+" "+strings.Join(spec.Args, " "))
		cmd.Stderr = io.MultiWriter(stderr, logFile)
	} else {
		cmd.Stderr = stderr
	}

	if err := cmd.Start(); err != nil {
		if logFile != nil {
			writeLogLine(logFile, "--- failed to start: "+err.Error())
			_ = logFile.Close()
		}
		return nil, err
	}

	p := &Process{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
		log:    logFile,
		exited: make(chan struct{}),
	}
	go p.watch()
	return p, nil
}

// logRotateBytes caps a server log; a bigger file rotates to .old on start.
const logRotateBytes = 1 << 20

// openLog opens (and rotates) the stderr log file; nil when path is "" or on
// any error — logging is strictly best-effort.
func openLog(path string) *os.File {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil
	}
	if st, err := os.Stat(path); err == nil && st.Size() > logRotateBytes {
		_ = os.Rename(path, path+".old")
	}
	// Read-capable so FreshLine can inspect the last byte before a marker.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o644)
	if err != nil {
		return nil
	}
	return f
}

// writeLogLine appends one timestamped marker line, always starting on a
// fresh line even when the server's last stderr write had no trailing
// newline (#990).
func writeLogLine(f *os.File, line string) {
	FreshLine(f)
	_, _ = f.WriteString(time.Now().Format("2006-01-02 15:04:05") + " " + line + "\n")
}

// Conn returns the duplex stream over the server's stdio.
func (p *Process) Conn() io.ReadWriteCloser { return rwc{r: p.stdout, w: p.stdin} }

// Exited returns a channel closed when the process exits.
func (p *Process) Exited() <-chan struct{} { return p.exited }

// WaitErr returns the process exit error (nil for a clean exit) once it has
// exited; it blocks until then.
func (p *Process) WaitErr() error {
	<-p.exited
	return p.waitErr
}

// Stderr returns the most recently captured stderr output (bounded), useful for
// surfacing why a server crashed.
func (p *Process) Stderr() string { return p.stderr.String() }

// Stop closes stdin (asking the server to exit) and kills it if still alive.
func (p *Process) Stop() error {
	_ = p.stdin.Close()
	select {
	case <-p.exited:
		return nil
	default:
		return p.cmd.Process.Kill()
	}
}

// watch reaps the process and signals exit exactly once.
func (p *Process) watch() {
	err := p.cmd.Wait()
	p.waitOnce.Do(func() {
		p.waitErr = err
		if p.log != nil {
			exit := "clean"
			if err != nil {
				exit = err.Error()
			}
			writeLogLine(p.log, "--- exited: "+exit)
			_ = p.log.Close()
		}
		close(p.exited)
	})
}
