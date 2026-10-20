//go:build windows

package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// conPTY is a Windows pseudo console (ConPTY, Windows 10 1809+) as the
// session's ptyMaster (#3005). The console owns two anonymous pipes: the
// parent writes keystrokes into `in` (the console's input) and reads the
// rendered VT stream from `out` (the console's output). conhost.exe sits
// between the pipes and the child, so the bytes on `out` are the same
// xterm-style stream a unix pty carries.
type conPTY struct {
	console windows.Handle
	in      *os.File // our write end of the console's input pipe
	out     *os.File // our read end of the console's output pipe
	once    sync.Once
}

func (c *conPTY) Read(p []byte) (int, error)  { return c.out.Read(p) }
func (c *conPTY) Write(p []byte) (int, error) { return c.in.Write(p) }

// Resize changes the console's size; the child learns of it the way it would
// from a resized console window (WINDOW_BUFFER_SIZE_EVENT).
func (c *conPTY) Resize(w, h int) error {
	return windows.ResizePseudoConsole(c.console, windows.Coord{X: int16(w), Y: int16(h)})
}

// Close releases the console and both pipes, once. The console goes first:
// ClosePseudoConsole ends conhost, which closes its end of the output pipe,
// so the read loop (which drains `out` until it errors, and is still running
// while release calls this) sees the end of the stream and the last output
// is read rather than discarded — ConPTY flushes before it exits, provided
// someone keeps reading, which the read loop does. The pipes are closed
// afterwards; closing `out` also cancels a read still pending on it.
func (c *conPTY) Close() error {
	c.once.Do(func() {
		windows.ClosePseudoConsole(c.console)
		_ = c.in.Close()
		_ = c.out.Close()
	})
	return nil
}

// startOnPTY starts cmd attached to a fresh pseudo console sized w×h (#3005).
// os/exec cannot do this itself — SysProcAttr has no pseudo-console slot —
// so the process is created directly through CreateProcess with the
// PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE attribute, from the command's own
// resolved Path, Args, Dir and Environ. The started process is then handed
// back to cmd as cmd.Process (os.FindProcess on its pid, while our handle
// still pins the process object), so the session's Wait, Kill, Pid and
// ProcessState work exactly as they do for a unix child. There is no slave
// end to hold: the second return is always nil.
func startOnPTY(cmd *exec.Cmd, w, h int) (ptmx ptyMaster, tty *os.File, err error) {
	if cmd.Err != nil {
		return nil, nil, cmd.Err
	}
	if cmd.Process != nil {
		return nil, nil, errors.New("exec: already started")
	}
	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, nil, fmt.Errorf("conpty: input pipe: %w", err)
	}
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		closeHandles(inR, inW)
		return nil, nil, fmt.Errorf("conpty: output pipe: %w", err)
	}
	var console windows.Handle
	size := windows.Coord{X: int16(w), Y: int16(h)}
	if err := windows.CreatePseudoConsole(size, inR, outW, 0, &console); err != nil {
		closeHandles(inR, inW, outR, outW)
		return nil, nil, fmt.Errorf("conpty: CreatePseudoConsole: %w", err)
	}
	// The console duplicated the two ends it needs; ours would only keep the
	// output pipe from reporting the end of the stream once conhost is gone.
	closeHandles(inR, outW)
	pty := &conPTY{
		console: console,
		in:      os.NewFile(uintptr(inW), "|conpty-in"),
		out:     os.NewFile(uintptr(outR), "|conpty-out"),
	}
	proc, err := createConsoleProcess(cmd, console)
	if err != nil {
		_ = pty.Close()
		return nil, nil, err
	}
	cmd.Process = proc
	return pty, nil, nil
}

// createConsoleProcess runs cmd attached to console and returns the process.
func createConsoleProcess(cmd *exec.Cmd, console windows.Handle) (*os.Process, error) {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, fmt.Errorf("conpty: attribute list: %w", err)
	}
	defer attrs.Delete()
	// The attribute's value is the HPCON itself, passed where the API
	// expects a pointer (the documented calling convention of
	// PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE), hence the handle-as-pointer
	// reinterpretation.
	consoleAsPointer := *(*unsafe.Pointer)(unsafe.Pointer(&console))
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, consoleAsPointer, unsafe.Sizeof(console)); err != nil {
		return nil, fmt.Errorf("conpty: attach console: %w", err)
	}
	si := &windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(*si))

	path, err := windows.UTF16PtrFromString(cmd.Path)
	if err != nil {
		return nil, err
	}
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(cmd.Args))
	if err != nil {
		return nil, err
	}
	var dir *uint16
	if cmd.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(cmd.Dir); err != nil {
			return nil, err
		}
	}
	// cmd.Environ() is the environment os/exec would give the child: the
	// configured Env with duplicates folded and SYSTEMROOT guaranteed.
	env, err := environmentBlock(cmd.Environ())
	if err != nil {
		return nil, err
	}
	var pi windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)
	if err := windows.CreateProcess(path, cmdline, nil, nil, false, flags, &env[0], dir, &si.StartupInfo, &pi); err != nil {
		return nil, &exec.Error{Name: cmd.Path, Err: err}
	}
	_ = windows.CloseHandle(pi.Thread)
	// FindProcess opens the pid with wait/query rights (Kill duplicates a
	// terminate-right handle on demand, as os/exec's own children do). Our
	// handle from CreateProcess keeps the pid from being recycled until then.
	proc, err := os.FindProcess(int(pi.ProcessId))
	_ = windows.CloseHandle(pi.Process)
	if err != nil {
		return nil, fmt.Errorf("conpty: adopt process %d: %w", pi.ProcessId, err)
	}
	return proc, nil
}

func closeHandles(hs ...windows.Handle) {
	for _, h := range hs {
		_ = windows.CloseHandle(h)
	}
}
