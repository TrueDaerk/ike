package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/httpclient"
	"ike/internal/httppane"
	"ike/internal/jqplay"
	"ike/internal/pane"
)

// http_body_test.go covers the response viewer's body actions (#2157): the
// raw/pretty toggle, the one-key jq handoff and the large-body affordances.

// filledHTTP opens the viewer on resp and focuses it, the state every
// pane-local key is pressed from.
func filledHTTP(t *testing.T, resp *httpclient.Response) Model {
	t.Helper()
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: resp})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)
	m.layout()
	return m
}

// pressHTTP sends one key to the focused viewer and returns the model and the
// command it produced.
func pressHTTP(t *testing.T, m Model, k rune) (Model, tea.Cmd) {
	t.Helper()
	out, cmd := m.Update(tea.KeyPressMsg{Code: k, Text: string(k)})
	return out.(Model), cmd
}

// TestHTTPToggleRawBodyCommand: http.toggleRawBody flips the viewer between
// pretty-printed and as-received, the palette pendant of "t".
func TestHTTPToggleRawBodyCommand(t *testing.T) {
	m := filledHTTP(t, sampleResponse("one"))
	if m.httpPanel().Raw() {
		t.Fatal("the viewer must open pretty-printed")
	}

	out, _ := m.Update(HTTPToggleRawBodyMsg{})
	m = out.(Model)
	if !m.httpPanel().Raw() {
		t.Error("http.toggleRawBody did not switch to raw")
	}
	if got := m.httpPanel().BodyText(); got != `{"ok":true}` {
		t.Errorf("raw body = %q", got)
	}

	out, _ = m.Update(HTTPToggleRawBodyMsg{})
	m = out.(Model)
	if m.httpPanel().Raw() {
		t.Error("http.toggleRawBody did not switch back")
	}

	// The pane key reaches the same place.
	m, _ = pressHTTP(t, m, 't')
	if !m.httpPanel().Raw() {
		t.Error(`"t" in the focused viewer did not toggle`)
	}
}

// TestHTTPJQPlaygroundFromResponse: "q" in the viewer opens the jq playground
// over the shown body — the acceptance criterion's documented key.
func TestHTTPJQPlaygroundFromResponse(t *testing.T) {
	m := filledHTTP(t, sampleResponse("one"))

	m, cmd := pressHTTP(t, m, 'q')
	if cmd == nil {
		t.Fatal(`"q" produced no command`)
	}
	if _, ok := cmd().(httppane.JQPlaygroundMsg); !ok {
		t.Fatalf(`"q" produced %T`, cmd())
	}
	out, _ := m.Update(httppane.JQPlaygroundMsg{})
	m = out.(Model)

	if !m.playOpen() {
		t.Fatal("the jq playground did not open")
	}
	if m.play.dialect != jqplay.DialectJQ {
		t.Errorf("dialect = %v, want jq", m.play.dialect)
	}
	if m.play.source != "HTTP response" {
		t.Errorf("input source = %q, want the HTTP response", m.play.source)
	}
	if m.play.paneKey != pane.HTTPKey {
		t.Errorf("the playground mounted in %q, want the response pane", m.play.paneKey)
	}
}

// TestHTTPJQPlaygroundCommand: the palette pendant works from anywhere, even
// with the focus on an editor.
func TestHTTPJQPlaygroundCommand(t *testing.T) {
	m := filledHTTP(t, sampleResponse("one"))
	m.setFocus(m.activeEditorKey())
	m.layout()

	out, _ := m.Update(HTTPJQPlaygroundMsg{})
	m = out.(Model)
	if !m.playOpen() || m.play.source != "HTTP response" {
		t.Fatalf("http.jqPlayground did not open over the response: open=%v", m.playOpen())
	}
}

// spooledHTTPResponse is a response whose body lives in a file, as the
// dispatcher produces past httpclient.SpoolThreshold.
func spooledHTTPResponse(t *testing.T, body string, head int) *httpclient.Response {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spool.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := sampleResponse("one")
	resp.Headers = http.Header{"Content-Type": {"application/json"}}
	resp.Body = []byte(body[:head])
	resp.SpoolPath = path
	resp.BodySize = len(body)
	return resp
}

// TestHTTPLargeBodyAffordances: a spooled response shows its head with the
// two ways on, and "m" grows the view without pulling the whole body in.
func TestHTTPLargeBodyAffordances(t *testing.T) {
	body := `{"items":[` + strings.Repeat(`"row",`, 2000) + `"end"]}`
	m := filledHTTP(t, spooledHTTPResponse(t, body, 64))

	p := m.httpPanel()
	if p.ShownBodyBytes() >= p.TotalBodyBytes() {
		t.Fatalf("the whole body was composed: %d of %d", p.ShownBodyBytes(), p.TotalBodyBytes())
	}
	if !p.CanLoadMore() || p.BodyFilePath() == "" {
		t.Fatalf("affordances missing: more=%v file=%q", p.CanLoadMore(), p.BodyFilePath())
	}

	out, _ := m.Update(HTTPLoadMoreBodyMsg{})
	m = out.(Model)
	if m.httpPanel().ShownBodyBytes() != len(body) {
		t.Errorf("load more composed %d of %d bytes", m.httpPanel().ShownBodyBytes(), len(body))
	}
}

// TestHTTPOpenBodyFileOpensTheSpool: "o" opens the complete body as an editor
// tab; the pane names the path, the host opens it.
func TestHTTPOpenBodyFileOpensTheSpool(t *testing.T) {
	body := strings.Repeat("payload\n", 500)
	resp := spooledHTTPResponse(t, body, 32)
	m := filledHTTP(t, resp)
	stored := m.httpPanel().BodyFilePath()
	if stored == "" {
		t.Fatal("no body file behind the shown response")
	}

	m, cmd := pressHTTP(t, m, 'o')
	if cmd == nil {
		t.Fatal(`"o" produced no command`)
	}
	msg, ok := cmd().(httppane.OpenBodyFileMsg)
	if !ok {
		t.Fatalf(`"o" produced %T`, cmd())
	}
	if msg.Path != stored {
		t.Errorf("path = %q, want %q", msg.Path, stored)
	}
	out, _ := m.Update(msg)
	m = out.(Model)
	if ed := m.activeEditor(); ed == nil || ed.Path() != stored {
		t.Errorf("the body file did not open in an editor: %v", m.activeEditor())
	}
}

// TestHTTPSpooledBodySurvivesTheHistoryRoundTrip is the #2385 regression at
// the app level: with the production-shaped *relative* config dir, a spooled
// response is dispatched with a source (so it is stored and the viewer's
// history is swapped in from the store), and "o" and "m" must work on the
// fresh state, on the history-restored state, and after the working directory
// changed.
func TestHTTPSpooledBodySurvivesTheHistoryRoundTrip(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	t.Setenv("IKE_CONFIG_DIR", ".ike") // relative on purpose — the production shape
	m := httpApp(t)
	t.Setenv("IKE_CONFIG_DIR", ".ike") // httpApp overrode it with an absolute dir

	body := `{"items":[` + strings.Repeat(`"row",`, 2000) + `"end"]}`
	resp := spooledHTTPResponse(t, body, 64)
	out, _ := m.Update(HTTPResponseMsg{Source: "/p/req.http", Request: "one", Resp: resp})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)
	m.layout()

	// The viewer now shows the store's history entry; the entry's body file
	// must open from anywhere, so chdir away before asking.
	t.Chdir(t.TempDir())
	freshPath := m.httpPanel().BodyFilePath()
	if freshPath == "" || !filepath.IsAbs(freshPath) {
		t.Fatalf("fresh body file path = %q, want absolute", freshPath)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Fatalf("the offered path does not open: %v", err)
	}
	if !m.httpPanel().LoadMore() {
		t.Error(`"m" loaded nothing on the fresh spooled response`)
	}

	// The restored state: load the stored responses back from the store, as a
	// restart or a project switch would — from the project directory, where
	// the relative store root resolves. The *path* it hands out must then
	// open from anywhere.
	t.Chdir(work)
	m.loadStoredHTTPResponse("/p/req.http", "one")
	t.Chdir(t.TempDir())
	restoredPath := m.httpPanel().BodyFilePath()
	if restoredPath == "" || !filepath.IsAbs(restoredPath) {
		t.Fatalf("restored body file path = %q, want absolute", restoredPath)
	}
	got, err := os.ReadFile(restoredPath)
	if err != nil {
		t.Fatalf("the restored path does not open: %v", err)
	}
	if string(got) != body {
		t.Errorf("restored body file holds %d bytes, want %d", len(got), len(body))
	}
	if !m.httpPanel().LoadMore() {
		t.Error(`"m" loaded nothing on the restored response`)
	}
}

// TestHTTPSaveSpooledBodyStreamsTheWholeThing: the raw-body save must write
// the complete body, not the head the pane happens to hold (#2157).
func TestHTTPSaveSpooledBodyStreamsTheWholeThing(t *testing.T) {
	body := strings.Repeat("chunk", 4000)
	m := filledHTTP(t, spooledHTTPResponse(t, body, 100))

	out, _ := m.Update(HTTPSaveResponseMsg{})
	m = out.(Model)
	if !m.httpSavePromptOpen() {
		t.Fatal("the save prompt did not open for a spooled body")
	}
	dest := filepath.Join(t.TempDir(), "full.json")
	m.httpSaveInput, m.httpSavePos = "", 0
	m = typePath(t, m, dest)

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("saved %d bytes, want the whole %d-byte body", len(got), len(body))
	}
}

// TestHTTPJQPlaygroundUsesTheWholeSpooledBody: the playground parses the whole
// document, not the head on screen.
func TestHTTPJQPlaygroundUsesTheWholeSpooledBody(t *testing.T) {
	body := `{"items":[` + strings.Repeat(`1,`, 3000) + `2]}`
	m := filledHTTP(t, spooledHTTPResponse(t, body, 40))

	out, _ := m.Update(httppane.JQPlaygroundMsg{})
	m = out.(Model)
	if !m.playOpen() {
		t.Fatal("the playground did not open")
	}
	if m.httpPanel().ShownBodyBytes() >= len(body) {
		t.Fatal("the test needs a body that is still partly spooled")
	}
	if got := m.httpPanel().JQInput(); len(got) != len(body) {
		t.Errorf("jq input is %d bytes, want the whole %d", len(got), len(body))
	}
}
