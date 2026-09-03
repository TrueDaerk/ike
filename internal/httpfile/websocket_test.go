package httpfile

import "testing"

func TestWebSocketBlockParses(t *testing.T) {
	src := "### chat\n" +
		"WEBSOCKET wss://example.com/socket\n" +
		"Sec-WebSocket-Protocol: chat\n" +
		"\n" +
		"{\"join\": \"lobby\"}\n" +
		"===\n" +
		"{\"say\": \"hello\"}\n"
	f := Parse(src)
	if len(f.Errors) != 0 {
		t.Fatalf("errors: %v", f.Errors)
	}
	if len(f.Requests) != 1 {
		t.Fatalf("requests: %d", len(f.Requests))
	}
	req := f.Requests[0]
	if req.Method != "WEBSOCKET" || req.Target != "wss://example.com/socket" {
		t.Fatalf("request line: %s %s", req.Method, req.Target)
	}
	if v, ok := req.Header("Sec-WebSocket-Protocol"); !ok || v != "chat" {
		t.Fatalf("header: %q %v", v, ok)
	}
	if req.WebSocket == nil {
		t.Fatal("WebSocket spec missing")
	}
	msgs := req.WebSocket.Messages
	if len(msgs) != 2 {
		t.Fatalf("messages: %d", len(msgs))
	}
	if msgs[0].Text != `{"join": "lobby"}` || msgs[0].WaitForServer {
		t.Fatalf("msg 0: %+v", msgs[0])
	}
	if msgs[1].Text != `{"say": "hello"}` || msgs[1].WaitForServer {
		t.Fatalf("msg 1: %+v", msgs[1])
	}
}

func TestWebSocketWaitForServer(t *testing.T) {
	msgs := SplitWebSocketBody("first\n=== wait-for-server\nsecond\n===\nthird")
	if len(msgs) != 3 {
		t.Fatalf("messages: %d", len(msgs))
	}
	if msgs[0].WaitForServer || !msgs[1].WaitForServer || msgs[2].WaitForServer {
		t.Fatalf("wait flags: %+v", msgs)
	}
}

func TestWebSocketWaitCarriesOverEmptySection(t *testing.T) {
	// `=== wait-for-server` directly followed by `===` still pauses before the
	// next non-empty message.
	msgs := SplitWebSocketBody("first\n=== wait-for-server\n===\nsecond")
	if len(msgs) != 2 {
		t.Fatalf("messages: %d", len(msgs))
	}
	if !msgs[1].WaitForServer {
		t.Fatalf("wait flag lost: %+v", msgs)
	}
}

func TestWebSocketMultiLineMessage(t *testing.T) {
	msgs := SplitWebSocketBody("{\n  \"a\": 1\n}\n===\nplain")
	if len(msgs) != 2 {
		t.Fatalf("messages: %d", len(msgs))
	}
	if msgs[0].Text != "{\n  \"a\": 1\n}" {
		t.Fatalf("msg 0: %q", msgs[0].Text)
	}
}

func TestWebSocketEmptyBody(t *testing.T) {
	f := Parse("WEBSOCKET ws://example.com/live\n")
	if len(f.Requests) != 1 || f.Requests[0].WebSocket == nil {
		t.Fatalf("parse: %+v", f.Requests)
	}
	if got := f.Requests[0].WebSocket.Messages; len(got) != 0 {
		t.Fatalf("messages: %+v", got)
	}
}

func TestWebSocketResolveVars(t *testing.T) {
	f := Parse("@room=lobby\nWEBSOCKET ws://{{host}}/socket\n\njoin {{room}}\n")
	if len(f.Requests) != 1 {
		t.Fatalf("requests: %d", len(f.Requests))
	}
	resolved, err := f.Requests[0].ResolveVars(&Vars{
		File:   f.VarMap(),
		Lookup: func(name string) (string, bool) { return "example.com", name == "host" },
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Target != "ws://example.com/socket" {
		t.Fatalf("target: %q", resolved.Target)
	}
	if got := resolved.WebSocket.Messages; len(got) != 1 || got[0].Text != "join lobby" {
		t.Fatalf("messages: %+v", got)
	}
}

func TestJoinWebSocketBodyRoundTrip(t *testing.T) {
	body := "first\n=== wait-for-server\nsecond\n===\nthird"
	msgs := SplitWebSocketBody(body)
	joined := JoinWebSocketBody(msgs)
	again := SplitWebSocketBody(joined)
	if len(again) != len(msgs) {
		t.Fatalf("round trip length: %d vs %d", len(again), len(msgs))
	}
	for i := range msgs {
		if again[i] != msgs[i] {
			t.Fatalf("round trip msg %d: %+v vs %+v", i, again[i], msgs[i])
		}
	}
}
