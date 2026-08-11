package httppane

import (
	"net/http"
	"strings"
	"testing"

	"ike/internal/httpclient"
)

func startStream(t *testing.T) *Model {
	t.Helper()
	m := New(nil)
	m.SetSize(80, 6) // bodyHeight 4
	m.StartStream("live", "HTTP/1.1", "200 OK", http.Header{"Content-Type": {"text/event-stream"}})
	return &m
}

func rowTexts(m *Model) []string {
	var out []string
	for i := 0; i < m.Rows(); i++ {
		out = append(out, m.RowText(i))
	}
	return out
}

func TestStartStreamComposesHeaders(t *testing.T) {
	m := startStream(t)
	if !m.Streaming() {
		t.Fatal("not streaming after StartStream")
	}
	rows := strings.Join(rowTexts(m), "\n")
	if !strings.Contains(rows, "HTTP/1.1 200 OK") {
		t.Errorf("status row missing: %q", rows)
	}
	if !strings.Contains(rows, "Content-Type: text/event-stream") {
		t.Errorf("header row missing: %q", rows)
	}
	if _, n := m.HistoryIndex(); n != 0 {
		t.Errorf("history not cleared: %d entries", n)
	}
}

func TestAppendStreamBuffersIncompleteLines(t *testing.T) {
	m := startStream(t)
	base := m.Rows()
	m.AppendStream([]byte("data: a\ndata: "))
	if got := m.Rows(); got != base+1 {
		t.Fatalf("rows after first chunk: %d, want %d", got, base+1)
	}
	if m.RowText(base) != "data: a" {
		t.Errorf("first line: %q", m.RowText(base))
	}
	// The incomplete "data: " tail is buffered, not shown.
	m.AppendStream([]byte("b\r\ndata: c\n"))
	if got := m.Rows(); got != base+3 {
		t.Fatalf("rows after second chunk: %d", got)
	}
	if m.RowText(base+1) != "data: b" || m.RowText(base+2) != "data: c" {
		t.Errorf("lines: %q %q", m.RowText(base+1), m.RowText(base+2))
	}
}

func TestAppendStreamAutoFollowsUnlessScrolledUp(t *testing.T) {
	m := startStream(t)
	for i := 0; i < 10; i++ {
		m.AppendStream([]byte("line\n"))
	}
	if m.top != m.maxTop() {
		t.Fatalf("not following: top=%d max=%d", m.top, m.maxTop())
	}
	// Manual scroll up detaches the follow.
	m.Scroll(-3)
	held := m.top
	m.AppendStream([]byte("more\nmore\n"))
	if m.top != held {
		t.Errorf("scrolled-up position moved: top=%d, want %d", m.top, held)
	}
	// G re-attaches.
	m.top = m.maxTop()
	m.AppendStream([]byte("tail\n"))
	if m.top != m.maxTop() {
		t.Errorf("follow did not re-attach: top=%d max=%d", m.top, m.maxTop())
	}
}

func TestSetFinalizesStream(t *testing.T) {
	m := startStream(t)
	m.AppendStream([]byte("{\"a\": 1}\n{\"b\": "))
	resp := &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Headers: http.Header{"Content-Type": {"application/json"}},
		Body:    []byte(`{"a": {"b": 1}}`),
	}
	m.Set("live", resp)
	if m.Streaming() {
		t.Error("still streaming after Set")
	}
	rows := strings.Join(rowTexts(m), "\n")
	if !strings.Contains(rows, `"b": 1`) {
		t.Errorf("final body missing: %q", rows)
	}
	// The finalized view behaves like a normal response: appends are dead.
	n := m.Rows()
	m.AppendStream([]byte("ghost\n"))
	if m.Rows() != n {
		t.Error("AppendStream mutated a finalized view")
	}
}

func TestAppendStreamIgnoredWithoutStream(t *testing.T) {
	m := New(nil)
	m.AppendStream([]byte("noise\n"))
	if m.Rows() != 0 {
		t.Errorf("rows composed without a stream: %d", m.Rows())
	}
}
