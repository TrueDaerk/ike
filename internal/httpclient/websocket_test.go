package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"ike/internal/httpfile"
)

// wsEchoServer starts an in-process websocket server that echoes every
// received message back, prefixed with "echo: " for text frames and verbatim
// for binary ones.
func wsEchoServer(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			kind, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if kind == websocket.TextMessage {
				payload = append([]byte("echo: "), payload...)
			}
			if err := conn.WriteMessage(kind, payload); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// wsRequest parses a single WEBSOCKET block against the test server's URL.
func wsRequest(t *testing.T, srv *httptest.Server, body string) *httpfile.Request {
	t.Helper()
	target := "ws" + strings.TrimPrefix(srv.URL, "http")
	src := "WEBSOCKET " + target + "\n"
	if body != "" {
		src += "\n" + body + "\n"
	}
	f := httpfile.Parse(src)
	if len(f.Errors) != 0 || len(f.Requests) != 1 {
		t.Fatalf("parse: %+v %+v", f.Errors, f.Requests)
	}
	return f.Requests[0]
}

// collectChunks gathers transcript lines and lets tests wait for them.
type collectChunks struct {
	mu    sync.Mutex
	lines []string
	tail  string
}

func (c *collectChunks) add(chunk []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tail += string(chunk)
	for {
		nl := strings.IndexByte(c.tail, '\n')
		if nl < 0 {
			return
		}
		c.lines = append(c.lines, c.tail[:nl])
		c.tail = c.tail[nl+1:]
	}
}

// waitFor blocks until pred over the collected lines holds, or fails the test.
func (c *collectChunks) waitFor(t *testing.T, what string, pred func([]string) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		ok := pred(append([]string(nil), c.lines...))
		c.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t.Fatalf("timed out waiting for %s; lines: %q", what, c.lines)
}

func linesWith(lines []string, marker, substr string) int {
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, marker) && strings.Contains(l, substr) {
			n++
		}
	}
	return n
}

func TestDispatchWSSendsAndReceives(t *testing.T) {
	srv := wsEchoServer(t)
	req := wsRequest(t, srv, "hello\n===\nworld")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	chunks := &collectChunks{}
	var headerStatus string
	done := make(chan *Response, 1)
	go func() {
		resp, err := DispatchWS(ctx, req, Options{DisableConfig: true}, WSCallbacks{
			StreamCallbacks: StreamCallbacks{
				OnHeaders: func(status string, _ int, _ string, _ http.Header) { headerStatus = status },
				OnChunk:   chunks.add,
			},
		})
		if err != nil {
			t.Error(err)
			done <- nil
			return
		}
		done <- resp
	}()

	chunks.waitFor(t, "both echoes", func(lines []string) bool {
		return linesWith(lines, "←", "echo: hello") == 1 && linesWith(lines, "←", "echo: world") == 1
	})
	cancel()
	resp := <-done
	if resp == nil {
		t.Fatal("no response")
	}
	if resp.StatusCode != 101 {
		t.Fatalf("status: %s", resp.Status)
	}
	if !strings.Contains(headerStatus, "101") {
		t.Fatalf("OnHeaders status: %q", headerStatus)
	}
	body := string(resp.Body)
	if linesWith(strings.Split(body, "\n"), "→", "hello") != 1 || linesWith(strings.Split(body, "\n"), "←", "echo: world") != 1 {
		t.Fatalf("transcript: %q", body)
	}
	if resp.Frames != 4 {
		t.Fatalf("frames: %d", resp.Frames)
	}
	if resp.Request == nil || resp.Request.Method != "WEBSOCKET" {
		t.Fatalf("snapshot: %+v", resp.Request)
	}
}

func TestDispatchWSWaitForServer(t *testing.T) {
	// The server only speaks when spoken to, so "second" can only ever go out
	// after the echo of "first" arrived — which is exactly what the transcript
	// order proves.
	srv := wsEchoServer(t)
	req := wsRequest(t, srv, "first\n=== wait-for-server\nsecond")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	chunks := &collectChunks{}
	done := make(chan *Response, 1)
	go func() {
		resp, _ := DispatchWS(ctx, req, Options{DisableConfig: true}, WSCallbacks{
			StreamCallbacks: StreamCallbacks{OnChunk: chunks.add},
		})
		done <- resp
	}()
	chunks.waitFor(t, "second echo", func(lines []string) bool {
		return linesWith(lines, "←", "echo: second") == 1
	})
	cancel()
	resp := <-done
	var sentSecond, gotFirst int
	for i, l := range strings.Split(string(resp.Body), "\n") {
		if strings.HasPrefix(l, "→") && strings.Contains(l, "second") {
			sentSecond = i
		}
		if strings.HasPrefix(l, "←") && strings.Contains(l, "echo: first") {
			gotFirst = i
		}
	}
	if sentSecond < gotFirst {
		t.Fatalf("second sent before first echo arrived: %q", resp.Body)
	}
}

func TestDispatchWSInteractiveSendAndBinary(t *testing.T) {
	srv := wsEchoServer(t)
	req := wsRequest(t, srv, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	chunks := &collectChunks{}
	sessions := make(chan *WSSession, 1)
	done := make(chan *Response, 1)
	go func() {
		resp, _ := DispatchWS(ctx, req, Options{DisableConfig: true}, WSCallbacks{
			StreamCallbacks: StreamCallbacks{OnChunk: chunks.add},
			OnSession:       func(s *WSSession) { sessions <- s },
		})
		done <- resp
	}()
	session := <-sessions
	if err := session.Send("interactive"); err != nil {
		t.Fatal(err)
	}
	chunks.waitFor(t, "interactive echo", func(lines []string) bool {
		return linesWith(lines, "←", "echo: interactive") == 1
	})

	// A binary frame renders as hex, not as garbage text.
	session.writeMu.Lock()
	err := session.conn.WriteMessage(websocket.BinaryMessage, []byte{0x00, 0x01, 0xfe})
	session.writeMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	chunks.waitFor(t, "binary echo", func(lines []string) bool {
		return linesWith(lines, "←", "(binary 3 bytes) 0001fe") == 1
	})
	cancel()
	resp := <-done
	if session.Closed() != true {
		t.Fatal("session not marked closed")
	}
	if err := session.Send("late"); err == nil {
		t.Fatal("send after close succeeded")
	}
	if !strings.Contains(strings.Join(resp.Warnings, " "), "session closed") {
		t.Fatalf("warnings: %q", resp.Warnings)
	}
}

func TestResendWSReplaysInitialMessages(t *testing.T) {
	srv := wsEchoServer(t)
	req := wsRequest(t, srv, "replayed")
	ctx1, cancel1 := context.WithCancel(context.Background())
	chunks1 := &collectChunks{}
	done1 := make(chan *Response, 1)
	go func() {
		resp, _ := DispatchWS(ctx1, req, Options{DisableConfig: true}, WSCallbacks{
			StreamCallbacks: StreamCallbacks{OnChunk: chunks1.add},
		})
		done1 <- resp
	}()
	chunks1.waitFor(t, "first echo", func(lines []string) bool {
		return linesWith(lines, "←", "echo: replayed") == 1
	})
	cancel1()
	first := <-done1

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	chunks2 := &collectChunks{}
	done2 := make(chan *Response, 1)
	go func() {
		resp, err := ResendWS(ctx2, "0", first.Request, Options{DisableConfig: true}, WSCallbacks{
			StreamCallbacks: StreamCallbacks{OnChunk: chunks2.add},
		})
		if err != nil {
			t.Error(err)
		}
		done2 <- resp
	}()
	chunks2.waitFor(t, "replayed echo", func(lines []string) bool {
		return linesWith(lines, "←", "echo: replayed") == 1
	})
	cancel2()
	if resp := <-done2; resp == nil || resp.Frames < 2 {
		t.Fatalf("resend response: %+v", resp)
	}
}

func TestDispatchWSVariables(t *testing.T) {
	srv := wsEchoServer(t)
	target := "ws" + strings.TrimPrefix(srv.URL, "http")
	f := httpfile.Parse("@greeting=hi\nWEBSOCKET " + target + "\n\n{{greeting}} there\n")
	if len(f.Requests) != 1 {
		t.Fatalf("parse: %+v", f.Errors)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chunks := &collectChunks{}
	done := make(chan *Response, 1)
	go func() {
		resp, err := DispatchWS(ctx, f.Requests[0],
			Options{DisableConfig: true, Vars: &httpfile.Vars{File: f.VarMap()}},
			WSCallbacks{StreamCallbacks: StreamCallbacks{OnChunk: chunks.add}})
		if err != nil {
			t.Error(err)
		}
		done <- resp
	}()
	chunks.waitFor(t, "resolved echo", func(lines []string) bool {
		return linesWith(lines, "←", "echo: hi there") == 1
	})
	cancel()
	<-done
}

func TestDispatchWSRefusedUpgrade(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusForbidden)
	}))
	defer srv.Close()
	req := wsRequest(t, srv, "")
	_, err := DispatchWS(context.Background(), req, Options{DisableConfig: true}, WSCallbacks{})
	if err == nil || !strings.Contains(err.Error(), "handshake failed") {
		t.Fatalf("err: %v", err)
	}
}
