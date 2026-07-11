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
	"sync"
)

// Spec describes how to launch a server.
type Spec struct {
	Command string
	Args    []string
	Env     []string // extra environment, appended to the inherited environment
	Dir     string   // working directory (workspace root); "" = inherit
}

// Process is a running language server. Its ReadWriteCloser reads the server's
// stdout and writes the server's stdin; Close terminates the process.
type Process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	stderr   *ringBuffer
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
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := newRingBuffer(64 * 1024)
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	p := &Process{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
		exited: make(chan struct{}),
	}
	go p.watch()
	return p, nil
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
		close(p.exited)
	})
}
