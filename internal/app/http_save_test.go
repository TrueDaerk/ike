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
	"ike/internal/pane"
)

// savableResponse is a response carrying an as-sent snapshot and a body, the
// state both #2059 actions work from.
func savableResponse(url, contentType string, body []byte) *httpclient.Response {
	resp := sampleResponse("one")
	resp.Headers = http.Header{"Content-Type": {contentType}}
	resp.Body = body
	resp.Request = &httpclient.RequestSnapshot{
		Method: "POST", URL: url,
		Headers: http.Header{"Authorization": {"Bearer tok-123"}, "Content-Type": {"application/json"}},
		Body:    []byte(`{"name":"thing"}`),
	}
	return resp
}

// typePath feeds a path into the open save prompt and confirms it.
func typePath(t *testing.T, m Model, path string) Model {
	t.Helper()
	for _, r := range path {
		out, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = out.(Model)
	}
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return out.(Model)
}

// TestHTTPSaveResponseWritesRawBody: the file holds the bytes as they
// arrived, not the pretty-printed view (#2059).
func TestHTTPSaveResponseWritesRawBody(t *testing.T) {
	m := httpApp(t)
	raw := []byte(`{"b":2,"a":1}`) // the viewer shows this re-indented
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: savableResponse("https://api.test/things", "application/json", raw)})
	m = out.(Model)

	out, _ = m.Update(HTTPSaveResponseMsg{})
	m = out.(Model)
	if !m.httpSavePromptOpen() {
		t.Fatal("http.saveResponse must open the path prompt")
	}
	if !strings.HasSuffix(m.httpSaveInput, ".json") {
		t.Errorf("prefilled name %q, want a .json proposal", m.httpSaveInput)
	}

	// Replace the proposal with an absolute path in a temp dir.
	dest := filepath.Join(t.TempDir(), "body.json")
	m.httpSaveInput, m.httpSavePos = "", 0
	m = typePath(t, m, dest)

	if m.httpSavePromptOpen() {
		t.Error("enter must close the prompt")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Errorf("file holds %q, want the raw body %q", got, raw)
	}
}

// TestHTTPSaveResponseWritesBinaryBody: a body the viewer cannot even render
// still lands on disk byte for byte.
func TestHTTPSaveResponseWritesBinaryBody(t *testing.T) {
	m := httpApp(t)
	raw := []byte{0x89, 'P', 'N', 'G', 0x00, 0x1a}
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: savableResponse("https://api.test/logo.png", "image/png", raw)})
	m = out.(Model)

	// The pane-local "S" is the same entry point as the palette command.
	m.setFocus(pane.HTTPKey)
	m.layout()
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("S must produce a command")
	}
	msg, ok := cmd().(httppane.SaveBodyMsg)
	if !ok {
		t.Fatalf("message type: %T", cmd())
	}
	out, _ = m.Update(msg)
	m = out.(Model)
	if !m.httpSavePromptOpen() {
		t.Fatal("S must open the path prompt")
	}
	if m.httpSaveInput != "logo.png" {
		t.Errorf("prefilled name %q, want logo.png", m.httpSaveInput)
	}

	dest := filepath.Join(t.TempDir(), "out.png")
	m.httpSaveInput, m.httpSavePos = "", 0
	m = typePath(t, m, dest)

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Errorf("file holds % x, want % x", got, raw)
	}
}

// TestHTTPSaveResponseIntoDirectory: a directory target receives the proposed
// file name instead of failing the write.
func TestHTTPSaveResponseIntoDirectory(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: savableResponse("https://api.test/things/42", "application/json", []byte(`{}`))})
	m = out.(Model)

	dir := t.TempDir()
	out, _ = m.Update(HTTPSaveResponseMsg{})
	m = out.(Model)
	m.httpSaveInput, m.httpSavePos = "", 0
	m = typePath(t, m, dir)

	if _, err := os.Stat(filepath.Join(dir, "42.json")); err != nil {
		t.Fatalf("the directory target must receive the proposed name: %v", err)
	}
}

// TestHTTPSaveResponseRefusesWithoutBody: no pane, no response, no body —
// each says why instead of opening a prompt for nothing.
func TestHTTPSaveResponseRefusesWithoutBody(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPSaveResponseMsg{})
	m = out.(Model)
	if m.httpSavePromptOpen() {
		t.Error("without a response pane there is nothing to save")
	}

	empty := savableResponse("https://api.test/things", "application/json", nil)
	out, _ = m.Update(HTTPResponseMsg{Request: "one", Resp: empty})
	m = out.(Model)
	out, _ = m.Update(HTTPSaveResponseMsg{})
	m = out.(Model)
	if m.httpSavePromptOpen() {
		t.Error("an empty body must not open the prompt")
	}
}

// TestHTTPSavePromptEscapeKeepsFile: esc cancels without writing anything.
func TestHTTPSavePromptEscapeKeepsFile(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: savableResponse("https://api.test/things", "application/json", []byte(`{}`))})
	m = out.(Model)
	out, _ = m.Update(HTTPSaveResponseMsg{})
	m = out.(Model)

	dest := filepath.Join(t.TempDir(), "nope.json")
	m.httpSaveInput, m.httpSavePos = dest, len(dest)
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)

	if m.httpSavePromptOpen() {
		t.Error("esc must close the prompt")
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("esc must not write the file")
	}
}

// TestHTTPCopyShownAsCurl: the palette command and the pane's "C" both put
// the shown response's as-sent request on the clipboard (#2059).
func TestHTTPCopyShownAsCurl(t *testing.T) {
	var copied string
	orig := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = orig })

	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: savableResponse("https://api.test/things", "application/json", []byte(`{}`))})
	m = out.(Model)

	out, cmd := m.Update(HTTPCopyShownAsCurlMsg{})
	m = out.(Model)
	if cmd != nil {
		cmd()
	}
	want := `curl https://api.test/things -X POST -H 'Authorization: Bearer tok-123' ` +
		`-H 'Content-Type: application/json' --data-raw '{"name":"thing"}'`
	if copied != want {
		t.Errorf("clipboard:\n got %q\nwant %q", copied, want)
	}

	// The pane key routes into the same host action.
	copied = ""
	m.setFocus(pane.HTTPKey)
	m.layout()
	out, cmd = m.Update(tea.KeyPressMsg{Code: 'C', Text: "C"})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("C must produce a command")
	}
	msg, ok := cmd().(httppane.CopyCurlMsg)
	if !ok {
		t.Fatalf("message type: %T", cmd())
	}
	if _, cmd := m.Update(msg); cmd != nil {
		cmd()
	}
	if copied != want {
		t.Errorf("pane key clipboard:\n got %q\nwant %q", copied, want)
	}
}

// TestHTTPCopyShownAsCurlWithoutSnapshot: a history entry stored before the
// snapshot existed cannot be exported — the clipboard stays untouched.
func TestHTTPCopyShownAsCurlWithoutSnapshot(t *testing.T) {
	var copied string
	orig := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = orig })

	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")}) // no Request field
	m = out.(Model)
	out, cmd := m.Update(HTTPCopyShownAsCurlMsg{})
	_ = out
	if cmd != nil {
		cmd()
	}
	if copied != "" {
		t.Errorf("clipboard written without a snapshot: %q", copied)
	}
}

// TestHTTPCopyShownAsHttpie: the palette command and the pane's "H" both put
// the shown response's as-sent request on the clipboard as httpie (#2384).
func TestHTTPCopyShownAsHttpie(t *testing.T) {
	var copied string
	orig := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = orig })

	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: savableResponse("https://api.test/things", "application/json", []byte(`{}`))})
	m = out.(Model)

	out, cmd := m.Update(HTTPCopyShownAsHttpieMsg{})
	m = out.(Model)
	if cmd != nil {
		cmd()
	}
	want := `http POST https://api.test/things 'Authorization:Bearer tok-123' ` +
		`Content-Type:application/json name=thing`
	if copied != want {
		t.Errorf("clipboard:\n got %q\nwant %q", copied, want)
	}

	// The pane key routes into the same host action.
	copied = ""
	m.setFocus(pane.HTTPKey)
	m.layout()
	out, cmd = m.Update(tea.KeyPressMsg{Code: 'H', Text: "H"})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("H must produce a command")
	}
	msg, ok := cmd().(httppane.CopyHttpieMsg)
	if !ok {
		t.Fatalf("message type: %T", cmd())
	}
	if _, cmd := m.Update(msg); cmd != nil {
		cmd()
	}
	if copied != want {
		t.Errorf("pane key clipboard:\n got %q\nwant %q", copied, want)
	}
}

// TestHTTPResponseFileNameProposals covers the default-name derivation from
// URL and Content-Type (#2059).
func TestHTTPResponseFileNameProposals(t *testing.T) {
	cases := []struct {
		name, url, ct, want string
	}{
		{"extension from the url wins", "https://x.test/logo.png", "application/octet-stream", "logo.png"},
		{"json content type", "https://x.test/things/42", "application/json; charset=utf-8", "42.json"},
		{"problem json", "https://x.test/things", "application/problem+json", "things.json"},
		{"plain text", "https://x.test/notes", "text/plain", "notes.txt"},
		{"html", "https://x.test/page", "text/html", "page.html"},
		{"xml suffix type", "https://x.test/feed", "application/atom+xml", "feed.xml"},
		{"binary", "https://x.test/blob", "application/octet-stream", "blob.bin"},
		{"query and fragment drop", "https://x.test/search?q=a#top", "application/json", "search.json"},
		{"trailing slash falls back", "https://x.test/", "application/json", "response.json"},
		{"bare host falls back", "https://x.test", "", "response"},
		{"escaped segment is unescaped", "https://x.test/a%20b", "text/plain", "a b.txt"},
		{"unknown content type stays bare", "https://x.test/thing", "application/x-ike-unknown", "thing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := savableResponse(tc.url, tc.ct, []byte("x"))
			if tc.ct == "" {
				resp.Headers = http.Header{}
			}
			if got := httpResponseFileName(resp); got != tc.want {
				t.Errorf("httpResponseFileName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestHTTPResponseFileNameWithoutSnapshot: a legacy entry has no URL to name
// the file after.
func TestHTTPResponseFileNameWithoutSnapshot(t *testing.T) {
	resp := sampleResponse("one")
	if got := httpResponseFileName(resp); got != "response.json" {
		t.Errorf("httpResponseFileName() = %q, want response.json", got)
	}
}

// TestByteCountLabel: the notification reads in the unit that fits.
func TestByteCountLabel(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{{0, "0 bytes"}, {512, "512 bytes"}, {2048, "2.0 KiB"}, {3 * 1024 * 1024, "3.0 MiB"}}
	for _, tc := range cases {
		if got := byteCountLabel(tc.n); got != tc.want {
			t.Errorf("byteCountLabel(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
