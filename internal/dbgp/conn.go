package dbgp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// ErrClosed reports a call on (or interrupted by) a closed connection.
var ErrClosed = errors.New("dbgp: connection closed")

// ErrBadInit reports that the engine's first packet arrived but could not be
// parsed as DBGp XML (#1991). Distinguishing it from a silent handshake lets
// the listener doctor name the concrete cause instead of a generic timeout.
var ErrBadInit = errors.New("dbgp: malformed init packet")

// Conn is one DBGp connection to a debug engine. Commands are correlated by
// transaction id; the read loop runs until the stream ends. Continuation
// commands (run, step_*) block in Call until the engine breaks or finishes —
// callers that must not block (the DAP bridge answering a continue request)
// issue them on their own goroutine. There is deliberately no call timeout:
// a program may legitimately run for minutes between breaks.
type Conn struct {
	rwc io.ReadWriteCloser

	writeMu sync.Mutex
	mu      sync.Mutex
	tid     int
	pending map[int]chan *Response
	closed  bool

	initCh chan *Init
	// initFail carries the parse error of a malformed pre-init packet so
	// WaitInit can fail fast with the concrete cause (#1991); initSeen stops
	// later garbage from being mistaken for a broken handshake.
	initFail chan error
	initSeen bool

	// onStream receives engine stream packets (stdout/stderr when
	// redirected) on the read-loop goroutine — hand off, don't block.
	onStream func(Stream)
}

// NewConn starts a connection over rwc; onStream may be nil.
func NewConn(rwc io.ReadWriteCloser, onStream func(Stream)) *Conn {
	c := &Conn{
		rwc:      rwc,
		pending:  map[int]chan *Response{},
		initCh:   make(chan *Init, 1),
		initFail: make(chan error, 1),
		onStream: onStream,
	}
	go c.readLoop()
	return c
}

// WaitInit waits for the engine's init packet (its first message after
// connecting).
func (c *Conn) WaitInit(timeout time.Duration) (*Init, error) {
	select {
	case init, ok := <-c.initCh:
		if !ok || init == nil {
			return nil, ErrClosed
		}
		return init, nil
	case err := <-c.initFail:
		return nil, fmt.Errorf("%w: %v", ErrBadInit, err)
	case <-time.After(timeout):
		return nil, errors.New("dbgp: timeout waiting for init")
	}
}

// Call sends command with args (already flag-formed, e.g. "-f", uri) and
// waits for the correlated response. data, when non-empty, is sent as the
// base64 payload after "--". A protocol-level error element comes back as a
// *Error; the response is returned alongside so callers can inspect status.
func (c *Conn) Call(command string, args []string, data string) (*Response, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	c.tid++
	tid := c.tid
	ch := make(chan *Response, 1)
	c.pending[tid] = ch
	c.mu.Unlock()

	line := command + " -i " + fmt.Sprint(tid)
	for _, a := range args {
		line += " " + quoteArg(a)
	}
	if data != "" {
		line += " -- " + data
	}

	c.writeMu.Lock()
	err := writeCommand(c.rwc, line)
	c.writeMu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, tid)
		c.mu.Unlock()
		return nil, err
	}

	resp, ok := <-ch
	if !ok || resp == nil {
		return nil, ErrClosed
	}
	if resp.Err != nil {
		return resp, resp.Err
	}
	return resp, nil
}

// readLoop dispatches engine packets until the stream ends, then fails
// every pending call.
func (c *Conn) readLoop() {
	r := bufio.NewReader(c.rwc)
	for {
		data, err := readPacket(r)
		if err != nil {
			c.teardown()
			return
		}
		pkt, err := parsePacket(data)
		if pkt == nil {
			// A malformed or unknown packet is skipped, not fatal — but before
			// the init arrived it means the handshake itself is broken (#1991),
			// which WaitInit reports as ErrBadInit instead of timing out.
			// (err is nil for well-formed XML of an unknown kind.)
			c.mu.Lock()
			seen := c.initSeen
			c.mu.Unlock()
			if !seen {
				if err == nil {
					err = errors.New("the first packet is not a DBGp init element")
				}
				select {
				case c.initFail <- err:
				default:
				}
			}
			continue
		}
		switch p := pkt.(type) {
		case *Init:
			c.mu.Lock()
			c.initSeen = true
			c.mu.Unlock()
			select {
			case c.initCh <- p:
			default:
			}
		case *Response:
			c.mu.Lock()
			ch := c.pending[p.TID]
			delete(c.pending, p.TID)
			c.mu.Unlock()
			if ch != nil {
				ch <- p
			}
		case *Stream:
			if c.onStream != nil {
				c.onStream(*p)
			}
		}
	}
}

// teardown fails pending calls and marks the connection closed.
func (c *Conn) teardown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.initCh)
	for tid, ch := range c.pending {
		close(ch)
		delete(c.pending, tid)
	}
}

// Close ends the connection; pending calls fail with ErrClosed.
func (c *Conn) Close() error {
	err := c.rwc.Close()
	c.teardown()
	return err
}

// Closed reports whether the connection has torn down.
func (c *Conn) Closed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// StatusEnded reports whether a continuation status means the session is
// over ("stopping": end of run, "stopped": session gone).
func StatusEnded(status string) bool {
	return status == "stopping" || status == "stopped"
}

// joinFlags builds a flag list, skipping empty values.
func joinFlags(pairs ...string) []string {
	if len(pairs)%2 != 0 {
		panic("dbgp: joinFlags needs flag/value pairs")
	}
	out := make([]string, 0, len(pairs))
	for i := 0; i < len(pairs); i += 2 {
		if strings.TrimSpace(pairs[i+1]) == "" {
			continue
		}
		out = append(out, pairs[i], pairs[i+1])
	}
	return out
}
