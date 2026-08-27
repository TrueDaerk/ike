package httppane

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/httpclient"
)

// key drives one pane key press through the public Update path.
func key(t *testing.T, m *Model, s string) tea.Cmd {
	t.Helper()
	return m.Update(tea.KeyPressMsg{Code: rune(s[0]), Text: s})
}

// jsonResponse builds a JSON response with body as its wire bytes.
func jsonResponse(body string) *httpclient.Response {
	return &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Headers: http.Header{"Content-Type": {"application/json"}},
		Body:    []byte(body),
	}
}

// spooledResponse writes body to a temp file and returns a response holding
// only its first head bytes in memory — what the dispatcher produces past
// SpoolThreshold.
func spooledResponse(t *testing.T, body string, head int) *httpclient.Response {
	t.Helper()
	path := filepath.Join(t.TempDir(), "body.bin")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	r := jsonResponse(body[:head])
	r.SpoolPath = path
	r.BodySize = len(body)
	return r
}

// TestJSONBodyIsPrettyPrintedByDefault: the minified answer reads indented
// without a keystroke — the default the raw toggle toggles away from.
func TestJSONBodyIsPrettyPrintedByDefault(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("one", jsonResponse(`{"a":1,"b":[2,3]}`))

	if m.Raw() {
		t.Error("a fresh viewer must start pretty-printed")
	}
	if !strings.Contains(m.BodyText(), "\"a\": 1") {
		t.Errorf("body not indented:\n%s", m.BodyText())
	}
}

// TestToggleRawBody: "t" swaps between the bytes as received and the
// pretty-printed view, and the flag survives history browsing — it describes
// how the user wants to read responses, not this one response.
func TestToggleRawBody(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("one", jsonResponse(`{"a":1,"b":[2,3]}`))

	key(t, &m, "t")
	if !m.Raw() {
		t.Fatal("t did not switch to raw")
	}
	if got := m.BodyText(); got != `{"a":1,"b":[2,3]}` {
		t.Errorf("raw body = %q, want the bytes as received", got)
	}
	if !strings.Contains(m.footerText(), "t pretty") {
		t.Errorf("footer does not offer the way back:\n%s", m.footerText())
	}

	// A second response keeps the choice.
	m.Set("two", jsonResponse(`{"c":3}`))
	if !m.Raw() || m.BodyText() != `{"c":3}` {
		t.Errorf("raw mode did not survive a new response: raw=%v body=%q", m.Raw(), m.BodyText())
	}

	key(t, &m, "t")
	if m.Raw() || !strings.Contains(m.BodyText(), "\"c\": 3") {
		t.Errorf("t did not switch back: raw=%v body=%q", m.Raw(), m.BodyText())
	}
}

// TestPrettyPrintCapSurfaced: past PrettyLimit the body renders raw and
// unhighlighted — and says so, rather than silently looking unformatted.
func TestPrettyPrintCapSurfaced(t *testing.T) {
	// One long array of short numbers: minified it is one line, so the cap is
	// what decides whether it is indented.
	nums := make([]int, PrettyLimit/2)
	raw, err := json.Marshal(nums)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) <= PrettyLimit {
		t.Fatalf("test body is only %d bytes, need more than %d", len(raw), PrettyLimit)
	}

	m := New(nil)
	m.SetSize(80, 20)
	m.Set("big", jsonResponse(string(raw)))

	found := false
	for _, w := range m.Warnings() {
		if strings.Contains(w, "larger than") {
			found = true
		}
	}
	if !found {
		t.Errorf("the pretty-print cap is not surfaced: %v", m.Warnings())
	}
	if strings.Contains(m.BodyText(), "\n") {
		t.Error("a capped body must not be indented")
	}
	if m.Highlighted() {
		t.Error("a capped body must not be highlighted")
	}
	if len(m.folds) != 0 {
		t.Error("a capped body must not be fold-scanned")
	}
}

// TestSpooledBodyShowsHeadAndAffordances: the pane composes the in-memory head
// and names both ways on — one more window, or the whole thing as a file.
func TestSpooledBodyShowsHeadAndAffordances(t *testing.T) {
	body := `{"items":[` + strings.Repeat(`"x",`, 500) + `"end"]}`
	resp := spooledResponse(t, body, 40)

	m := New(nil)
	m.SetSize(120, 20)
	m.Set("big", resp)

	if m.ShownBodyBytes() != 40 || m.TotalBodyBytes() != len(body) {
		t.Fatalf("shown=%d total=%d (body %d)", m.ShownBodyBytes(), m.TotalBodyBytes(), len(body))
	}
	notice := strings.Join(m.Warnings(), "\n")
	if !strings.Contains(notice, "showing the first") ||
		!strings.Contains(notice, "m load more") || !strings.Contains(notice, "o open as file") {
		t.Errorf("spool notice missing its affordances: %q", notice)
	}
	if !m.CanLoadMore() {
		t.Error("CanLoadMore should hold while the spool has more")
	}
	if m.BodyFilePath() != resp.SpoolPath {
		t.Errorf("BodyFilePath = %q, want the spool file", m.BodyFilePath())
	}
	foot := m.footerText()
	if !strings.Contains(foot, "m more") || !strings.Contains(foot, "o open file") {
		t.Errorf("footer misses the spool keys:\n%s", foot)
	}
}

// TestLoadMoreGrowsTheView: "m" pulls the next window off the spool and stops
// once the whole body is composed.
func TestLoadMoreGrowsTheView(t *testing.T) {
	body := strings.Repeat("line\n", 4000) // ~20 KiB of plain text
	resp := spooledResponse(t, body, 1000)
	resp.Headers = http.Header{"Content-Type": {"text/plain"}}

	m := New(nil)
	m.SetSize(80, 20)
	m.Set("big", resp)
	before := m.Rows()

	for i := 0; m.CanLoadMore() && i < 10; i++ {
		if cmd := key(t, &m, "m"); cmd != nil {
			t.Fatalf("m returned a command: %v", cmd)
		}
	}
	if m.CanLoadMore() {
		t.Fatal("the body never finished loading")
	}
	if m.ShownBodyBytes() != len(body) {
		t.Errorf("shown = %d, want the whole body (%d)", m.ShownBodyBytes(), len(body))
	}
	if m.Rows() <= before {
		t.Errorf("rows did not grow: %d then %d", before, m.Rows())
	}
	if strings.Contains(m.footerText(), "m more") {
		t.Error("the footer still offers m after everything is loaded")
	}
	// The spool notice is gone once nothing is left behind it.
	for _, w := range m.Warnings() {
		if strings.Contains(w, "showing the first") {
			t.Errorf("stale spool notice: %q", w)
		}
	}
}

// TestLoadMoreResetsOnNewResponse: the loaded windows belong to the entry that
// was on show, never to the next one.
func TestLoadMoreResetsOnNewResponse(t *testing.T) {
	body := strings.Repeat("a", 5000)
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("big", spooledResponse(t, body, 100))
	key(t, &m, "m")
	if m.ShownBodyBytes() != len(body) {
		t.Fatalf("load more did not pull the rest in: %d", m.ShownBodyBytes())
	}

	m.Set("small", jsonResponse(`{"a":1}`))
	if m.ShownBodyBytes() != len(`{"a":1}`) {
		t.Errorf("loaded windows leaked into the next response: %d", m.ShownBodyBytes())
	}
}

// TestOpenBodyFileKey: "o" hands the host the spool path; a body held in
// memory has no file and the key does nothing.
func TestOpenBodyFileKey(t *testing.T) {
	resp := spooledResponse(t, strings.Repeat("z", 3000), 100)
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("big", resp)

	cmd := key(t, &m, "o")
	if cmd == nil {
		t.Fatal("o produced no command")
	}
	msg, ok := cmd().(OpenBodyFileMsg)
	if !ok {
		t.Fatalf("o produced %T", cmd())
	}
	if msg.Path != resp.SpoolPath {
		t.Errorf("path = %q, want %q", msg.Path, resp.SpoolPath)
	}

	m.Set("small", jsonResponse(`{"a":1}`))
	if cmd := key(t, &m, "o"); cmd != nil {
		t.Errorf("o offered a file for an in-memory body: %v", cmd())
	}
}

// TestJQPlaygroundKey: "q" asks the host to open the playground; the pane
// itself knows nothing about panes or modes.
func TestJQPlaygroundKey(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("one", jsonResponse(`{"a":1}`))

	cmd := key(t, &m, "q")
	if cmd == nil {
		t.Fatal("q produced no command")
	}
	if _, ok := cmd().(JQPlaygroundMsg); !ok {
		t.Fatalf("q produced %T", cmd())
	}
	if !strings.Contains(m.footerText(), "q jq") {
		t.Errorf("footer does not advertise the handoff:\n%s", m.footerText())
	}
}

// TestJQInputIsTheWholeBody: a spooled body hands jq all of it, not the head
// the pane happens to render — a program against a truncated document answers
// the wrong question.
func TestJQInputIsTheWholeBody(t *testing.T) {
	body := `{"items":[` + strings.Repeat(`1,`, 2000) + `2]}`
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("big", spooledResponse(t, body, 50))

	if got := m.JQInput(); got != body {
		t.Errorf("JQInput is %d bytes, want the whole %d-byte body", len(got), len(body))
	}
	// An in-memory body answers with what is shown, pretty-printing included.
	m.Set("small", jsonResponse(`{"a":1}`))
	if got := m.JQInput(); !strings.Contains(got, "\"a\": 1") {
		t.Errorf("JQInput for an in-memory body = %q", got)
	}
}

// TestSpoolGoneKeepsTheHeadReadable: a spool file that disappeared must not
// take the composed head with it.
func TestSpoolGoneKeepsTheHeadReadable(t *testing.T) {
	resp := spooledResponse(t, strings.Repeat("q", 4000), 80)
	if err := os.Remove(resp.SpoolPath); err != nil {
		t.Fatal(err)
	}
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("big", resp)

	if !m.HasBodyText() {
		t.Error("the head should still compose")
	}
	if m.LoadMore() {
		t.Error("LoadMore reported progress without a spool file")
	}
	if got := m.JQInput(); got != m.BodyText() {
		t.Error("JQInput should fall back to what is shown")
	}
}

// TestPartialHeadIsNotMistakenForBinary: the head was cut at a byte offset, so
// it may end inside a multi-byte rune — dropping that stump is what keeps a
// perfectly good UTF-8 body from collapsing to the binary notice.
func TestPartialHeadIsNotMistakenForBinary(t *testing.T) {
	body := `{"name":"` + strings.Repeat("ä", 200) + `"}`
	// Cut one byte into the hundredth "ä" of the head.
	head := len(`{"name":"`) + 2*99 + 1
	resp := spooledResponse(t, body, head)

	m := New(nil)
	m.SetSize(80, 20)
	m.Set("big", resp)

	for _, w := range m.Warnings() {
		if strings.Contains(w, "binary body") {
			t.Fatalf("a truncated rune was read as a binary body: %q", w)
		}
	}
	if !m.HasBodyText() {
		t.Error("the head should still compose")
	}
	if strings.HasSuffix(m.BodyText(), "\xc3") {
		t.Error("the incomplete rune was kept")
	}
}

// TestLoadMoreKeepsThePlace: the view grows at its tail, so pressing "m" at
// the end of the head must not throw the reader back to row 0.
func TestLoadMoreKeepsThePlace(t *testing.T) {
	body := strings.Repeat("line\n", 2000)
	resp := spooledResponse(t, body, 500)
	resp.Headers = http.Header{"Content-Type": {"text/plain"}}

	m := New(nil)
	m.SetSize(80, 20)
	m.Set("big", resp)
	m.Scroll(50)
	top := m.top
	if top == 0 {
		t.Fatal("the test needs a scrolled view")
	}

	if !m.LoadMore() {
		t.Fatal("load more did nothing")
	}
	if m.top != top {
		t.Errorf("top moved from %d to %d", top, m.top)
	}
}

// TestToggleRawDuringAStreamKeepsTheView: a live stream has no finished
// response to recompose from — flipping the flag must not wipe what arrived.
func TestToggleRawDuringAStreamKeepsTheView(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.StartStream("live", "HTTP/1.1", "200 OK", http.Header{"Content-Type": {"text/event-stream"}})
	m.AppendStream([]byte("data: one\ndata: two\n"))
	rows := m.Rows()

	if !m.ToggleRaw() {
		t.Fatal("the flag did not flip")
	}
	if !m.Streaming() || m.Rows() != rows {
		t.Errorf("the live view was recomposed away: streaming=%v rows=%d want %d", m.Streaming(), m.Rows(), rows)
	}
}
