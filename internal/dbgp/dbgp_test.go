package dbgp

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeEngine is a scripted DBGp engine on the far end of a net.Pipe: it
// records incoming command lines and answers each from its script.
type fakeEngine struct {
	conn net.Conn
	r    *bufio.Reader

	t *testing.T
}

func newFakeEngine(t *testing.T) (*fakeEngine, *Conn, chan Stream) {
	t.Helper()
	client, server := net.Pipe()
	streams := make(chan Stream, 8)
	c := NewConn(client, func(s Stream) { streams <- s })
	t.Cleanup(func() { _ = c.Close(); _ = server.Close() })
	return &fakeEngine{conn: server, r: bufio.NewReader(server), t: t}, c, streams
}

// send frames one XML packet to the client.
func (e *fakeEngine) send(xmlBody string) {
	e.t.Helper()
	payload := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + xmlBody
	msg := fmt.Sprintf("%d\x00%s\x00", len(payload), payload)
	if _, err := e.conn.Write([]byte(msg)); err != nil {
		e.t.Fatalf("engine write: %v", err)
	}
}

// read returns the next NUL-terminated command line from the client.
func (e *fakeEngine) read() string {
	e.t.Helper()
	line, err := e.r.ReadString(0)
	if err != nil {
		e.t.Fatalf("engine read: %v", err)
	}
	return strings.TrimSuffix(line, "\x00")
}

// tid extracts the transaction id from a command line.
func tid(t *testing.T, line string) string {
	t.Helper()
	fields := strings.Fields(line)
	for i, f := range fields {
		if f == "-i" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	t.Fatalf("no -i in %q", line)
	return ""
}

func TestInitAndBreakpointSet(t *testing.T) {
	engine, c, _ := newFakeEngine(t)

	engine.send(`<init xmlns="urn:debugger_protocol_v1" fileuri="file:///proj/test.php" language="PHP" protocol_version="1.0" appid="123" idekey="ike"/>`)
	init, err := c.WaitInit(time.Second)
	if err != nil {
		t.Fatalf("WaitInit: %v", err)
	}
	if init.FileURI != "file:///proj/test.php" || init.Language != "PHP" {
		t.Fatalf("unexpected init: %+v", init)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		id, err := c.BreakpointSet("file:///proj/test.php", 12)
		if err != nil {
			t.Errorf("BreakpointSet: %v", err)
		}
		if id != "42" {
			t.Errorf("breakpoint id = %q, want 42", id)
		}
	}()
	line := engine.read()
	if !strings.HasPrefix(line, "breakpoint_set ") ||
		!strings.Contains(line, "-t line") ||
		!strings.Contains(line, "-f file:///proj/test.php") ||
		!strings.Contains(line, "-n 12") {
		t.Fatalf("unexpected command line: %q", line)
	}
	engine.send(`<response xmlns="urn:debugger_protocol_v1" command="breakpoint_set" transaction_id="` + tid(t, line) + `" id="42"/>`)
	<-done
}

func TestContinuationBreakResponse(t *testing.T) {
	engine, c, _ := newFakeEngine(t)

	type result struct {
		resp *Response
		err  error
	}
	res := make(chan result, 1)
	go func() {
		r, err := c.Run()
		res <- result{r, err}
	}()
	line := engine.read()
	engine.send(`<response xmlns="urn:debugger_protocol_v1" xmlns:xdebug="https://xdebug.org/dbgp/xdebug" command="run" transaction_id="` + tid(t, line) + `" status="break" reason="ok"><xdebug:message filename="file:///proj/test.php" lineno="7"/></response>`)
	r := <-res
	if r.err != nil {
		t.Fatalf("Run: %v", r.err)
	}
	if r.resp.Status != "break" || r.resp.Message == nil || r.resp.Message.Lineno != 7 {
		t.Fatalf("unexpected break response: %+v", r.resp)
	}
	if FromURI(r.resp.Message.Filename) != "/proj/test.php" {
		t.Fatalf("FromURI = %q", FromURI(r.resp.Message.Filename))
	}
}

func TestStackAndContexts(t *testing.T) {
	engine, c, _ := newFakeEngine(t)

	go func() {
		line := engine.read()
		engine.send(`<response xmlns="urn:debugger_protocol_v1" command="stack_get" transaction_id="` + tid(t, line) + `">` +
			`<stack where="foo" level="0" type="file" filename="file:///proj/lib.php" lineno="3"/>` +
			`<stack where="{main}" level="1" type="file" filename="file:///proj/test.php" lineno="9"/>` +
			`</response>`)
	}()
	frames, err := c.StackGet()
	if err != nil {
		t.Fatalf("StackGet: %v", err)
	}
	if len(frames) != 2 || frames[0].Where != "foo" || frames[1].Lineno != 9 {
		t.Fatalf("unexpected frames: %+v", frames)
	}

	go func() {
		line := engine.read()
		engine.send(`<response xmlns="urn:debugger_protocol_v1" command="context_names" transaction_id="` + tid(t, line) + `">` +
			`<context name="Locals" id="0"/><context name="Superglobals" id="1"/></response>`)
	}()
	ctxs, err := c.ContextNames(0)
	if err != nil {
		t.Fatalf("ContextNames: %v", err)
	}
	if len(ctxs) != 2 || ctxs[0].Name != "Locals" || ctxs[1].ID != 1 {
		t.Fatalf("unexpected contexts: %+v", ctxs)
	}
}

func TestContextGetNestedProperties(t *testing.T) {
	engine, c, _ := newFakeEngine(t)

	val := base64.StdEncoding.EncodeToString([]byte("hello"))
	go func() {
		line := engine.read()
		engine.send(`<response xmlns="urn:debugger_protocol_v1" command="context_get" transaction_id="` + tid(t, line) + `">` +
			`<property name="$s" fullname="$s" type="string" size="5" encoding="base64"><![CDATA[` + val + `]]></property>` +
			`<property name="$arr" fullname="$arr" type="array" children="1" numchildren="2" page="0" pagesize="32">` +
			`<property name="0" fullname="$arr[0]" type="int">1</property>` +
			`<property name="1" fullname="$arr[1]" type="int">2</property>` +
			`</property></response>`)
	}()
	props, err := c.ContextGet(0, 0)
	if err != nil {
		t.Fatalf("ContextGet: %v", err)
	}
	if len(props) != 2 {
		t.Fatalf("want 2 properties, got %+v", props)
	}
	if props[0].Value() != "hello" {
		t.Errorf("base64 value = %q, want hello", props[0].Value())
	}
	arr := props[1]
	if arr.HasChildren != 1 || arr.NumChildren != 2 || len(arr.Children) != 2 {
		t.Fatalf("unexpected array property: %+v", arr)
	}
	if arr.Children[1].Fullname != "$arr[1]" || arr.Children[1].Value() != "2" {
		t.Errorf("unexpected child: %+v", arr.Children[1])
	}
}

func TestPropertySetSendsBase64Data(t *testing.T) {
	engine, c, _ := newFakeEngine(t)

	go func() {
		line := engine.read()
		want := "-- " + base64.StdEncoding.EncodeToString([]byte("99"))
		if !strings.Contains(line, want) {
			t.Errorf("property_set line %q missing %q", line, want)
		}
		engine.send(`<response xmlns="urn:debugger_protocol_v1" command="property_set" transaction_id="` + tid(t, line) + `" success="1"/>`)
	}()
	if err := c.PropertySet("$x", 0, "99"); err != nil {
		t.Fatalf("PropertySet: %v", err)
	}
}

func TestCommandError(t *testing.T) {
	engine, c, _ := newFakeEngine(t)

	go func() {
		line := engine.read()
		engine.send(`<response xmlns="urn:debugger_protocol_v1" command="property_get" transaction_id="` + tid(t, line) + `">` +
			`<error code="300"><message>can not get property</message></error></response>`)
	}()
	_, err := c.PropertyGet("$nope", 0, 0)
	var derr *Error
	if !errors.As(err, &derr) || derr.Code != 300 {
		t.Fatalf("want *Error code 300, got %v", err)
	}
}

func TestStreamPacket(t *testing.T) {
	engine, _, streams := newFakeEngine(t)

	engine.send(`<stream xmlns="urn:debugger_protocol_v1" type="stdout" encoding="base64">` +
		base64.StdEncoding.EncodeToString([]byte("out!")) + `</stream>`)
	select {
	case s := <-streams:
		if s.Type != "stdout" || s.Text() != "out!" {
			t.Fatalf("unexpected stream: %+v", s)
		}
	case <-time.After(time.Second):
		t.Fatal("no stream packet arrived")
	}
}

func TestQuoteArg(t *testing.T) {
	cases := map[string]string{
		"plain":           "plain",
		"file:///a/b.php": "file:///a/b.php",
		`with space`:      `"with space"`,
		`quo"te`:          `"quo\"te"`,
		``:                `""`,
	}
	for in, want := range cases {
		if got := quoteArg(in); got != want {
			t.Errorf("quoteArg(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestURIRoundTrip(t *testing.T) {
	p := "/proj/dir with space/test.php"
	uri := ToURI(p)
	if !strings.HasPrefix(uri, "file://") {
		t.Fatalf("ToURI = %q", uri)
	}
	if got := FromURI(uri); got != p {
		t.Fatalf("round trip = %q, want %q", got, p)
	}
	if got := FromURI("eval://1"); got != "eval://1" {
		t.Fatalf("non-file URI mangled: %q", got)
	}
}

func TestCloseFailsPending(t *testing.T) {
	engine, c, _ := newFakeEngine(t)

	res := make(chan error, 1)
	go func() {
		_, err := c.Run()
		res <- err
	}()
	// Consume the command so the call is pending on its response, then close.
	engine.read()
	_ = c.Close()
	select {
	case err := <-res:
		if err != ErrClosed {
			t.Fatalf("want ErrClosed, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending call did not fail on close")
	}
}
