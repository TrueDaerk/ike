package dap

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"ike/internal/lsp/jsonrpc"
)

// Conn is one DAP connection: it frames requests out and dispatches
// responses to their callers and events to the handler. All methods are safe
// for concurrent use; the read loop runs until the stream ends.
type Conn struct {
	rwc io.ReadWriteCloser

	writeMu sync.Mutex
	mu      sync.Mutex
	seq     int
	pending map[int]chan envelope
	closed  bool

	// onEvent receives every adapter event (stopped, terminated, output, …)
	// on the read-loop goroutine — handlers must hand off, not block.
	onEvent func(event string, body json.RawMessage)

	// onReverse handles adapter-initiated requests (runInTerminal, #625) on the
	// read-loop goroutine. It must hand off and reply asynchronously via Respond
	// — it MUST NOT block. When nil, or when it returns false, the request is
	// refused. Guarded by mu.
	onReverse func(seq int, command string, args json.RawMessage) bool
}

// SetReverseHandler installs the adapter-initiated request handler (#625).
func (c *Conn) SetReverseHandler(fn func(seq int, command string, args json.RawMessage) bool) {
	c.mu.Lock()
	c.onReverse = fn
	c.mu.Unlock()
}

// Respond replies to an adapter-initiated request with a success body.
func (c *Conn) Respond(seq int, command string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.write(envelope{Type: typeResponse, RequestSeq: seq, Command: command, Success: true, Body: data})
}

// RefuseRequest replies to an adapter-initiated request with a failure.
func (c *Conn) RefuseRequest(seq int, command, message string) error {
	return c.write(envelope{Type: typeResponse, RequestSeq: seq, Command: command, Success: false, Message: message})
}

// callTimeout bounds a single request/response round trip; a hung adapter
// must never freeze the UI-facing caller forever.
const callTimeout = 15 * time.Second

// ErrClosed reports a call on (or interrupted by) a closed connection.
var ErrClosed = errors.New("dap: connection closed")

// NewConn starts a connection over rwc; onEvent may be nil.
func NewConn(rwc io.ReadWriteCloser, onEvent func(event string, body json.RawMessage)) *Conn {
	c := &Conn{rwc: rwc, pending: map[int]chan envelope{}, onEvent: onEvent}
	go c.readLoop()
	return c
}

// Call sends command with args and waits for its response body.
func (c *Conn) Call(command string, args any) (json.RawMessage, error) {
	var payload json.RawMessage
	if args != nil {
		data, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		payload = data
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	c.seq++
	seq := c.seq
	ch := make(chan envelope, 1)
	c.pending[seq] = ch
	c.mu.Unlock()

	req := envelope{Seq: seq, Type: typeRequest, Command: command, Arguments: payload}
	if err := c.write(req); err != nil {
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
		return nil, err
	}
	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, ErrClosed
		}
		if !resp.Success {
			msg := resp.Message
			if msg == "" {
				msg = "request failed"
			}
			return nil, fmt.Errorf("dap: %s: %s", command, msg)
		}
		return resp.Body, nil
	case <-time.After(callTimeout):
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
		return nil, fmt.Errorf("dap: %s: timeout", command)
	}
}

// write frames one message onto the stream.
func (c *Conn) write(msg envelope) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return jsonrpc.WriteFrame(c.rwc, data)
}

// readLoop dispatches incoming messages until the stream ends, then fails
// every pending call.
func (c *Conn) readLoop() {
	r := bufio.NewReader(c.rwc)
	for {
		data, err := jsonrpc.ReadFrame(r)
		if err != nil {
			c.teardown()
			return
		}
		var msg envelope
		if json.Unmarshal(data, &msg) != nil {
			continue // a malformed frame is skipped, not fatal
		}
		switch msg.Type {
		case typeResponse:
			c.mu.Lock()
			ch := c.pending[msg.RequestSeq]
			delete(c.pending, msg.RequestSeq)
			c.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
		case typeEvent:
			if c.onEvent != nil {
				c.onEvent(msg.Event, msg.Body)
			}
		case typeRequest:
			// Adapter-initiated requests: route to the reverse handler when one
			// is registered and willing (runInTerminal, #625); otherwise refuse
			// politely so the adapter falls back.
			c.mu.Lock()
			h := c.onReverse
			c.mu.Unlock()
			if h == nil || !h(msg.Seq, msg.Command, msg.Arguments) {
				// Refuse off the read loop: a synchronous write here can
				// deadlock against an adapter that is itself mid-write (#638).
				go func(seq int, command string) {
					_ = c.RefuseRequest(seq, command, "unsupported")
				}(msg.Seq, msg.Command)
			}
		}
	}
}

// teardown fails pending calls and marks the connection closed.
func (c *Conn) teardown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for seq, ch := range c.pending {
		close(ch)
		delete(c.pending, seq)
	}
}

// Close ends the connection; pending calls fail with ErrClosed.
func (c *Conn) Close() error {
	err := c.rwc.Close()
	c.teardown()
	return err
}
