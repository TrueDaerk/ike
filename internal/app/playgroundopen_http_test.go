package app

import (
	nethttp "net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/httpclient"
	"ike/internal/httppane"
	"ike/internal/jqplay"
	"ike/internal/pane"
)

// playgroundopen_http_test.go covers the dispatcher's response-pane route
// (#2451): with the HTTP viewer focused, playground.open resolves the dialect
// from the shown body's content type and opens over the response — the same
// place "q" lands (#2157) — instead of doing nothing.

// typedResponse is a response of one content type, the input the response-pane
// route resolves its dialect from.
func typedResponse(ct, body string) *httpclient.Response {
	return &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Headers:    nethttp.Header{"Content-Type": {ct}},
		Body:       []byte(body),
		Duration:   3 * time.Millisecond,
		RequestKey: "one",
	}
}

// TestPlaygroundOpenFromResponsePane is the issue's acceptance case: with the
// viewer focused, the one chord opens the playground the body's type speaks —
// over the response, in the response's own pane.
func TestPlaygroundOpenFromResponsePane(t *testing.T) {
	for _, tc := range []struct {
		name string
		ct   string
		body string
		want jqplay.Dialect
	}{
		{"json", "application/json", `{"ok":true}`, jqplay.DialectJQ},
		{"json suffix", "application/vnd.api+json", `{"ok":true}`, jqplay.DialectJQ},
		{"yaml", "application/yaml", "tags:\n  - tui\n", jqplay.DialectYQ},
		{"yaml legacy", "text/x-yaml", "tags:\n  - tui\n", jqplay.DialectYQ},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := openDispatcher(t, filledHTTP(t, typedResponse(tc.ct, tc.body)))
			if !m.playOpen() {
				t.Fatalf("%s response: playground.open opened nothing", tc.name)
			}
			if m.play.dialect != tc.want {
				t.Errorf("dialect = %v, want %v", m.play.dialect, tc.want)
			}
			if m.play.source != "HTTP response" {
				t.Errorf("input source = %q, want the HTTP response", m.play.source)
			}
			if m.play.paneKey != pane.HTTPKey {
				t.Errorf("mounted in %q, want the response pane", m.play.paneKey)
			}
		})
	}
}

// TestPlaygroundOpenFromResponseMatchesJQKey: over a JSON response the chord
// lands exactly where "q" does (#2157) — same dialect, same source, same pane.
func TestPlaygroundOpenFromResponseMatchesJQKey(t *testing.T) {
	out, _ := filledHTTP(t, sampleResponse("one")).Update(httppane.JQPlaygroundMsg{})
	want := out.(Model)
	byChord := openDispatcher(t, filledHTTP(t, sampleResponse("one")))

	if !byChord.playOpen() || !want.playOpen() {
		t.Fatal("both routes must open the playground")
	}
	if byChord.play.dialect != want.play.dialect || byChord.play.source != want.play.source ||
		byChord.play.paneKey != want.play.paneKey {
		t.Errorf("chord opened (%v, %q, %q), want (%v, %q, %q)",
			byChord.play.dialect, byChord.play.source, byChord.play.paneKey,
			want.play.dialect, want.play.source, want.play.paneKey)
	}
}

// TestPlaygroundOpenFromResponseRoutesMarkupToXMQ: an XML/HTML response takes
// the xmq route, the hook standing in for the playground (#2415).
func TestPlaygroundOpenFromResponseRoutesMarkupToXMQ(t *testing.T) {
	for _, tc := range []struct{ name, ct, body string }{
		{"xml", "application/xml", "<root><item/></root>"},
		{"html", "text/html", "<html><body>hi</body></html>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := 0
			prev := startXMQPlayground
			startXMQPlayground = func(*Model) tea.Cmd { called++; return nil }
			t.Cleanup(func() { startXMQPlayground = prev })

			m := openDispatcher(t, filledHTTP(t, typedResponse(tc.ct, tc.body)))
			if called != 1 {
				t.Fatalf("%s response: xmq playground called %d times, want 1", tc.name, called)
			}
			if m.playOpen() {
				t.Fatalf("%s response must not fall back to jq/yq", tc.name)
			}
		})
	}
}

// TestPlaygroundOpenFromResponseWithoutXMQSaysSo: while the hook is nil the
// markup route answers instead of opening the wrong dialect — over a response
// exactly as over a buffer.
func TestPlaygroundOpenFromResponseWithoutXMQSaysSo(t *testing.T) {
	prev := startXMQPlayground
	startXMQPlayground = nil
	t.Cleanup(func() { startXMQPlayground = prev })

	m := dispatchAndNotify(t, filledHTTP(t, typedResponse("application/xml", "<root/>")))
	if m.playOpen() {
		t.Fatal("an xml response must not open the jq playground")
	}
	assertToast(t, m, "xmq")
}

// TestPlaygroundOpenFromResponseUnqueryableNotifies: a body no playground
// speaks names its type where it is known, and nothing opens.
func TestPlaygroundOpenFromResponseUnqueryableNotifies(t *testing.T) {
	for _, tc := range []struct{ name, ct, body, want string }{
		{"plain text", "text/plain", "hello\n", "no playground for this buffer"},
		{"css", "text/css", "a{color:red}", "no playground for css"},
		{"empty body", "application/json", "", "no playground for this buffer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := dispatchAndNotify(t, filledHTTP(t, typedResponse(tc.ct, tc.body)))
			if m.playOpen() {
				t.Fatalf("%s response must not open a playground", tc.name)
			}
			assertToast(t, m, tc.want)
		})
	}
}

// TestPlaygroundOpenPrefersFocusedEditorOverResponse: the editor route is
// unchanged — an open response pane that does *not* have the focus stays out
// of the way, dialect and input both resolved from the buffer.
func TestPlaygroundOpenPrefersFocusedEditorOverResponse(t *testing.T) {
	noDebounce(t)
	m := filledHTTP(t, sampleResponse("one")) // JSON response, would route to jq
	path := filepath.Join(t.TempDir(), "doc."+dispatchExt(t, "yaml"))
	if err := os.WriteFile(path, []byte("tags:\n  - tui\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(path, false)
	m = drainCmd(tm.(Model), cmd)

	m = openDispatcher(t, m)
	if !m.playOpen() {
		t.Fatal("playground.open opened nothing for the focused YAML buffer")
	}
	if m.play.dialect != jqplay.DialectYQ {
		t.Errorf("dialect = %v, want yq from the focused buffer", m.play.dialect)
	}
	if m.play.source == "HTTP response" {
		t.Error("the unfocused response must not win over the focused buffer")
	}
}
