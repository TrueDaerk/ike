package jsonrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// ErrClosed is returned by Call/Notify after the connection has shut down.
var ErrClosed = errors.New("jsonrpc: connection closed")

// Handler receives server-initiated traffic. Notify handles notifications
// (no reply); Request handles server→client requests and must eventually be
// answered via Conn.Respond. Both run on the read-loop goroutine and must not
// block on the connection itself. Either may be nil.
type Handler struct {
	Notify  func(method string, params json.RawMessage)
	Request func(id ID, method string, params json.RawMessage)
}

// Conn is a JSON-RPC 2.0 connection over a single duplex stream (an LSP server's
// stdin/stdout). A background read goroutine reads frames and dispatches them; a
// separate write goroutine drains an unbounded outbound queue and is the sole
// writer to the stream. It is safe for concurrent use.
//
// Outbound writes are asynchronous on purpose (#594): callers marshal on their
// own goroutine (so a marshal error is still returned synchronously) and then
// enqueue the framed payload, which never blocks on the server draining its
// stdin. A busy server that stalls its stdin therefore can no longer freeze a
// caller — in particular the bubbletea Update goroutine, which sends didChange
// notifications per keystroke.
type Conn struct {
	rwc     io.ReadWriteCloser
	handler Handler

	sendMu     sync.Mutex // guards sendBuf/sendClosed and the cond
	sendCond   *sync.Cond // signalled when sendBuf gains work or the writer must stop
	sendBuf    [][]byte   // unbounded outbound queue drained by writeLoop
	sendClosed bool       // writer stops once true and the queue is empty

	mu      sync.Mutex // guards nextID, pending, closed
	nextID  int64
	pending map[int64]chan result
	closed  bool

	done chan struct{} // closed when the read loop exits
	err  error         // first read-loop error (set once)
}

// result is a settled response delivered to a waiting Call.
type result struct {
	raw json.RawMessage
	err *Error
}

// NewConn starts a connection over rwc, dispatching server traffic to handler,
// and launches the read loop. Close stops it.
func NewConn(rwc io.ReadWriteCloser, handler Handler) *Conn {
	c := &Conn{
		rwc:     rwc,
		handler: handler,
		pending: make(map[int64]chan result),
		done:    make(chan struct{}),
	}
	c.sendCond = sync.NewCond(&c.sendMu)
	go c.readLoop()
	go c.writeLoop()
	return c
}

// Done returns a channel closed when the read loop has exited (server gone or
// Close called). Err reports why after Done fires.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err returns the error that terminated the read loop, or nil while running.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Call sends a request and blocks until the matching response arrives, ctx is
// cancelled, or the connection closes. A response error is returned as *Error.
func (c *Conn) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := marshalParams(params)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	c.nextID++
	id := c.nextID
	ch := make(chan result, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	idv := NumID(id)
	if err := c.write(&message{JSONRPC: version, ID: &idv, Method: method, Params: raw}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-c.done:
		return nil, c.closeErr()
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		return res.raw, nil
	}
}

// Notify sends a notification (a request with no id, no reply).
func (c *Conn) Notify(method string, params any) error {
	raw, err := marshalParams(params)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	c.mu.Unlock()
	return c.write(&message{JSONRPC: version, Method: method, Params: raw})
}

// Respond replies to a server→client request. Pass exactly one of result/errObj.
func (c *Conn) Respond(id ID, res any, errObj *Error) error {
	msg := &message{JSONRPC: version, ID: &id}
	if errObj != nil {
		msg.Error = errObj
	} else {
		raw, err := marshalParams(res)
		if err != nil {
			return err
		}
		if raw == nil {
			// A response must carry a result or an error property; "result" has
			// omitempty on the envelope, so a nil payload must serialize as an
			// explicit JSON null — vscode-jsonrpc servers (e.g. Intelephense)
			// die on a response with neither (#991).
			raw = json.RawMessage("null")
		}
		msg.Result = raw
	}
	return c.write(msg)
}

// Close shuts the connection: it closes the underlying stream, which unblocks the
// read loop, and fails any in-flight Calls.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	// Stop the writer: closing the stream unblocks a write stalled on a full
	// pipe, and stopWriter wakes an idle writer waiting on the queue.
	err := c.rwc.Close()
	c.stopWriter()
	return err
}

// write marshals one message and hands the framed payload to the writer
// goroutine. It never touches the stream, so it never blocks on the server
// draining stdin — the enqueue is a slice append under a short-held mutex.
func (c *Conn) write(msg *message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.sendMu.Lock()
	if c.sendClosed {
		c.sendMu.Unlock()
		return ErrClosed
	}
	c.sendBuf = append(c.sendBuf, data)
	c.sendMu.Unlock()
	c.sendCond.Signal()
	return nil
}

// writeLoop is the sole writer to the stream. It drains the outbound queue in
// FIFO order (preserving message ordering) and blocks only itself when the
// server stalls its stdin; producers enqueue without blocking. It exits once the
// connection is stopping and the queue is empty, or on the first write error.
func (c *Conn) writeLoop() {
	for {
		c.sendMu.Lock()
		for len(c.sendBuf) == 0 && !c.sendClosed {
			c.sendCond.Wait()
		}
		if len(c.sendBuf) == 0 && c.sendClosed {
			c.sendMu.Unlock()
			return
		}
		batch := c.sendBuf
		c.sendBuf = nil
		c.sendMu.Unlock()

		for _, data := range batch {
			if err := writeFrame(c.rwc, data); err != nil {
				// The stream is gone; drop the rest. The read loop's shutdown
				// surfaces the terminating error and fails pending calls.
				c.stopWriter()
				return
			}
		}
	}
}

// stopWriter wakes writeLoop and tells it to drain-and-exit. Idempotent.
func (c *Conn) stopWriter() {
	c.sendMu.Lock()
	c.sendClosed = true
	c.sendMu.Unlock()
	c.sendCond.Broadcast()
}

// readLoop reads frames until the stream ends, dispatching each message.
func (c *Conn) readLoop() {
	r := bufio.NewReader(c.rwc)
	var loopErr error
	for {
		payload, err := readFrame(r)
		if err != nil {
			loopErr = err
			break
		}
		var msg message
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue // skip malformed frames rather than tearing down
		}
		c.dispatch(&msg)
	}
	c.shutdown(loopErr)
}

// dispatch routes one decoded message to a waiting Call or the handler.
func (c *Conn) dispatch(msg *message) {
	switch {
	case msg.isResponse():
		c.mu.Lock()
		ch, ok := c.pending[msg.ID.Num]
		if ok {
			delete(c.pending, msg.ID.Num)
		}
		c.mu.Unlock()
		if ok {
			ch <- result{raw: msg.Result, err: msg.Error}
		}
	case msg.isRequest():
		if c.handler.Request != nil {
			c.handler.Request(*msg.ID, msg.Method, msg.Params)
		}
	case msg.Method != "": // notification
		if c.handler.Notify != nil {
			c.handler.Notify(msg.Method, msg.Params)
		}
	}
}

// shutdown records the terminating error, fails pending calls, and signals Done.
func (c *Conn) shutdown(err error) {
	c.mu.Lock()
	if c.err == nil {
		if err == nil || errors.Is(err, io.EOF) {
			c.err = ErrClosed
		} else {
			c.err = err
		}
	}
	c.closed = true
	pending := c.pending
	c.pending = make(map[int64]chan result)
	c.mu.Unlock()

	for _, ch := range pending {
		ch <- result{err: &Error{Code: CodeInternalError, Message: c.err.Error()}}
	}
	// The stream is gone (server exited or Close): stop the writer so a queued
	// or in-flight write does not linger, and enqueues return ErrClosed.
	c.stopWriter()
	close(c.done)
}

func (c *Conn) closeErr() error {
	if e := c.Err(); e != nil {
		return e
	}
	return ErrClosed
}

// marshalParams encodes params, treating nil as absent.
func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	if raw, ok := params.(json.RawMessage); ok {
		return raw, nil
	}
	return json.Marshal(params)
}
