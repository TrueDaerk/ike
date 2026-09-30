package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/crashlog"
	"ike/internal/safego"
)

// crashfallback.go is main's last line of crash reporting (#2836). The root
// model's guards (internal/app/crash.go) recover panics in Update, View, Cmds
// and IKE's own goroutines with the panic value in hand. Whatever still
// reaches bubbletea's catcher — a panic inside the program's own machinery,
// a sequence member, the message filter — ends Run with ErrProgramPanic after
// bubbletea printed the value and stack to stderr. Inside the alternate
// screen that print is gone before anyone reads it, so stderr is captured
// for the process lifetime: a pipe tees it to the real stderr and keeps the
// tail in memory, and a Run that ends in a panic writes a crash report
// holding that tail, then points at the file on the restored terminal.

// stderrTailBytes bounds the captured tail (bubbletea's dump is a few KiB).
const stderrTailBytes = 64 << 10

// stderrCapture tees os.Stderr through a pipe and keeps its tail.
type stderrCapture struct {
	orig *os.File
	w    *os.File
	mu   sync.Mutex
	tail []byte
	done chan struct{}
}

// captureStderr installs the tee; a pipe failure leaves stderr untouched and
// returns nil, which every method tolerates.
func captureStderr() *stderrCapture {
	r, w, err := os.Pipe()
	if err != nil {
		return nil
	}
	c := &stderrCapture{orig: os.Stderr, w: w, done: make(chan struct{})}
	os.Stderr = w
	safego.Go("main.stderrCapture", func() {
		defer close(c.done)
		buf := make([]byte, 4096)
		for {
			n, rerr := r.Read(buf)
			if n > 0 {
				_, _ = c.orig.Write(buf[:n])
				c.mu.Lock()
				c.tail = append(c.tail, buf[:n]...)
				if len(c.tail) > stderrTailBytes {
					c.tail = c.tail[len(c.tail)-stderrTailBytes:]
				}
				c.mu.Unlock()
			}
			if rerr != nil {
				return
			}
		}
	})
	return c
}

// origWriter is the real stderr — the terminal — for the exit line; without
// a capture it is whatever os.Stderr is.
func (c *stderrCapture) origWriter() io.Writer {
	if c == nil {
		return os.Stderr
	}
	return c.orig
}

// stop restores the real stderr, drains what was written so far and returns
// the captured tail. Safe to call more than once.
func (c *stderrCapture) stop() string {
	if c == nil {
		return ""
	}
	if os.Stderr == c.w {
		os.Stderr = c.orig
	}
	_ = c.w.Close()
	select {
	case <-c.done:
	case <-time.After(time.Second):
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.tail)
}

// reportProgramEnd turns Run's error into the exit: a panic bubbletea caught
// becomes a crash report with the stderr tail and a one-line pointer; any
// other error keeps the plain message. Both exit non-zero.
func reportProgramEnd(err error, capture *stderrCapture, stderr io.Writer) int {
	if err == nil {
		capture.stop()
		return 0
	}
	tail := capture.stop()
	if errors.Is(err, tea.ErrProgramPanic) {
		rep := crashlog.Report{Where: "program", Value: "panic caught by the program loop: " + err.Error(),
			Extra: map[string]string{"stderr": tail}}
		if last := crashlog.Last(); last != "" {
			// A guard already wrote the real report; point at it too.
			rep.Extra["earlier report"] = last
		}
		path, werr := crashlog.Write(rep)
		switch {
		case werr != nil:
			fmt.Fprintf(stderr, "ike: crashed (%v); the crash log could not be written: %v\n", err, werr)
		case path != "":
			fmt.Fprintf(stderr, "ike: crashed — crash log: %s\n", path)
		default:
			fmt.Fprintf(stderr, "ike: crashed — crash log: %s\n", crashlog.Last())
		}
		return 1
	}
	fmt.Fprintf(stderr, "ike: %v\n", err)
	return 1
}
