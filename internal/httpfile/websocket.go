package httpfile

import "strings"

// websocket.go adds the `WEBSOCKET <url>` request block (#2422), the JetBrains
// spelling of a WebSocket session: headers parse as usual, and the body holds
// the initial messages to send once the connection is open, separated by lines
// holding `===`. A separator spelled `=== wait-for-server` additionally pauses
// until a server message arrives before the next message goes out.
//
//	### chat
//	WEBSOCKET wss://example.com/socket
//	Sec-WebSocket-Protocol: chat
//
//	{"join": "lobby"}
//	=== wait-for-server
//	{"say": "hello"}
//
// The parser only *splits* the block — Request.WebSocket holds the messages
// with their wait flags. Opening the connection, sending the messages and
// streaming the frames is internal/httpclient's DispatchWS; placeholders in
// the messages resolve in ResolveVars like any other body text.

// WebSocketMethod is the request-line method that marks a WebSocket block.
const WebSocketMethod = "WEBSOCKET"

// WSMessage is one initial message of a WEBSOCKET block (#2422).
type WSMessage struct {
	// Text is the message payload, placeholders unresolved at parse time.
	Text string
	// WaitForServer marks a message whose separator was `=== wait-for-server`:
	// it only goes out after a server message has arrived.
	WaitForServer bool
}

// WebSocketSpec is the split of one WEBSOCKET block's body (#2422): the
// initial messages in file order. Empty Messages means "connect and listen".
type WebSocketSpec struct {
	Messages []WSMessage
}

// wsSeparator opens every message separator line of a WEBSOCKET body.
const wsSeparator = "==="

// wsWaitDirective is the separator variant that pauses for a server message.
const wsWaitDirective = "wait-for-server"

// SplitWebSocketBody splits a WEBSOCKET block's body into its messages
// (#2422): lines holding `===` separate them, `=== wait-for-server` marks the
// following message as wait-gated. Blank lines around each message are
// trimmed; a message left empty by that is dropped, but its wait flag carries
// over to the next one so `=== wait-for-server` directly followed by `===`
// still pauses. Exported so a stored session snapshot — whose body is this
// very text, resolved — can be replayed without a re-parse of the .http file.
func SplitWebSocketBody(body string) []WSMessage {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	var out []WSMessage
	wait := false
	flush := func(lines []string) {
		text := strings.Trim(strings.Join(lines, "\n"), "\n")
		if text == "" {
			return // an empty section keeps its wait flag for the next one
		}
		out = append(out, WSMessage{Text: text, WaitForServer: wait})
		wait = false
	}
	var cur []string
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if t == wsSeparator || (strings.HasPrefix(t, wsSeparator) && strings.TrimSpace(strings.TrimPrefix(t, wsSeparator)) == wsWaitDirective) {
			flush(cur)
			cur = nil
			if strings.Contains(t, wsWaitDirective) {
				wait = true
			}
			continue
		}
		cur = append(cur, line)
	}
	flush(cur)
	return out
}

// JoinWebSocketBody is SplitWebSocketBody's inverse: it renders messages back
// into the `===`-separated body text a session snapshot stores (#2422), so a
// re-send replays exactly the initial messages that were sent.
func JoinWebSocketBody(messages []WSMessage) string {
	var b strings.Builder
	for i, msg := range messages {
		if i > 0 || msg.WaitForServer {
			sep := wsSeparator
			if msg.WaitForServer {
				sep += " " + wsWaitDirective
			}
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(sep)
			b.WriteString("\n")
		}
		b.WriteString(msg.Text)
	}
	return b.String()
}

// webSocketSpec splits a WEBSOCKET block's body into its spec.
func webSocketSpec(body string) *WebSocketSpec {
	return &WebSocketSpec{Messages: SplitWebSocketBody(body)}
}
