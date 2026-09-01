package httppane

import (
	"net/http"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/httpclient"
)

// exported returns a viewer showing one response with an as-sent request
// snapshot — the state both #2059 actions need.
func exported() *httpclient.Response {
	return &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Headers: http.Header{"Content-Type": {"application/json"}},
		Body:    []byte(`{"ok":true}`),
		Request: &httpclient.RequestSnapshot{
			Method: "POST", URL: "https://api.example.com/things",
			Headers: http.Header{"Authorization": {"Bearer tok"}},
			Body:    []byte(`{"name":"x"}`),
		},
	}
}

// TestCopyCurlKeyEmitsMsg: "C" asks the host to export the shown request as
// curl (#2059) — the pane holds the snapshot but neither the clipboard nor
// the notification channel.
func TestCopyCurlKeyEmitsMsg(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("create", exported())

	cmd := m.Update(tea.KeyPressMsg{Code: 'C', Text: "C"})
	if cmd == nil {
		t.Fatal("C must emit a command")
	}
	if _, ok := cmd().(CopyCurlMsg); !ok {
		t.Fatalf("message type: %T", cmd())
	}
	// The snapshot the host will render is the shown entry's.
	snap := m.CurrentRequest()
	if snap == nil || snap.URL != "https://api.example.com/things" {
		t.Fatalf("CurrentRequest() = %+v", snap)
	}
}

// TestCopyHttpieKeyEmitsMsg: "H" is "C"'s second format (#2384) — the same
// snapshot, the httpie spelling; lowercase "h" stays the history step.
func TestCopyHttpieKeyEmitsMsg(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("create", exported())

	cmd := m.Update(tea.KeyPressMsg{Code: 'H', Text: "H"})
	if cmd == nil {
		t.Fatal("H must emit a command")
	}
	if _, ok := cmd().(CopyHttpieMsg); !ok {
		t.Fatalf("message type: %T", cmd())
	}
	if cmd := m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"}); cmd != nil {
		if _, ok := cmd().(CopyHttpieMsg); ok {
			t.Error("lowercase h must stay the history step")
		}
	}
}

// TestSaveBodyKeyEmitsMsg: "S" asks the host for the save prompt; "s" stays
// the keep-scroll toggle (#1493).
func TestSaveBodyKeyEmitsMsg(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("create", exported())

	cmd := m.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	if cmd == nil {
		t.Fatal("S must emit a command")
	}
	if _, ok := cmd().(SaveBodyMsg); !ok {
		t.Fatalf("message type: %T", cmd())
	}
	if cmd := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"}); cmd != nil {
		t.Fatalf("lowercase s must stay the keep-scroll toggle, got %T", cmd())
	}
	if !m.KeepScroll() {
		t.Error("s must toggle keep-scroll")
	}
}

// TestHasRawBodyCoversBinary: the save gate reads the raw bytes, so a binary
// body — which renders as a notice, not as text — still counts (#2059).
func TestHasRawBodyCoversBinary(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	resp := exported()
	resp.Body = []byte{0x00, 0x01, 0xff}
	m.Set("create", resp)

	if m.HasBodyText() {
		t.Error("a binary body has no copyable text")
	}
	if !m.HasRawBody() {
		t.Error("a binary body is exactly what saving to a file is for")
	}
	if got := m.CurrentResponse(); got == nil || len(got.Body) != 3 {
		t.Errorf("CurrentResponse() = %+v", got)
	}
}

// TestStreamHasNothingToExport: while a stream is live there is neither a
// finished body nor a snapshot, and the footer says so by omission.
func TestStreamHasNothingToExport(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 20)
	m.StartStream("events", "HTTP/1.1", "200 OK", http.Header{"Content-Type": {"text/event-stream"}})
	m.AppendStream([]byte("data: one\n"))

	if m.CurrentResponse() != nil || m.HasRawBody() {
		t.Error("a live stream has no finished response to save")
	}
	if view := m.View(); strings.Contains(view, "S save") {
		t.Errorf("the footer must not offer the save mid-stream:\n%s", view)
	}
}

// TestFooterAdvertisesExports: once a response is shown, both actions are
// discoverable from the footer.
func TestFooterAdvertisesExports(t *testing.T) {
	m := New(nil)
	m.SetSize(200, 20)
	m.Set("create", exported())
	view := m.View()
	if !strings.Contains(view, "C curl") || !strings.Contains(view, "S save") {
		t.Errorf("footer must advertise the curl export and the save:\n%s", view)
	}
}
