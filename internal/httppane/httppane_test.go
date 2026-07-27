package httppane

import (
	tea "charm.land/bubbletea/v2"

	"net/http"
	"strings"
	"testing"
	"time"

	"ike/internal/httpclient"
)

func sample() *httpclient.Response {
	return &httpclient.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		Headers:    http.Header{"Content-Type": {"application/json"}, "X-A": {"1"}},
		Body:       []byte(`{"b":2,"a":1}`),
		Duration:   12 * time.Millisecond,
		RequestKey: "create",
	}
}

func TestSetComposesRows(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("create", sample())

	if m.Request() != "create" {
		t.Errorf("request: %q", m.Request())
	}
	if m.Title() != "HTTP Response: create" {
		t.Errorf("title: %q", m.Title())
	}
	view := m.View()
	if !strings.Contains(view, "HTTP/1.1 200 OK") {
		t.Errorf("status line missing:\n%s", view)
	}
	if !strings.Contains(view, "Content-Type") || !strings.Contains(view, "X-A") {
		t.Errorf("headers missing:\n%s", view)
	}
	// JSON body pretty-printed: multi-line with 2-space indent.
	if !strings.Contains(view, `"a": 1`) {
		t.Errorf("pretty-printed body missing:\n%s", view)
	}
}

func TestSetReplacesContent(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("one", sample())
	first := m.Rows()

	resp := sample()
	resp.Body = []byte("plain")
	resp.Headers = http.Header{"Content-Type": {"text/plain"}}
	m.Set("two", resp)
	if m.Request() != "two" {
		t.Errorf("request after replace: %q", m.Request())
	}
	if m.Rows() >= first {
		t.Errorf("rows not replaced: %d -> %d", first, m.Rows())
	}
	if !strings.Contains(m.View(), "plain") {
		t.Error("new body missing")
	}
}

func TestBinaryBodyNotice(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	resp := sample()
	resp.Body = []byte{0x00, 0x01, 0xff, 0xfe}
	resp.Headers = http.Header{"Content-Type": {"application/octet-stream"}}
	m.Set("bin", resp)
	view := m.View()
	if !strings.Contains(view, "binary body, 4 bytes") {
		t.Errorf("binary notice missing:\n%s", view)
	}
	if strings.Contains(view, "\x00") {
		t.Error("raw binary leaked into view")
	}
}

func TestWarningsShown(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 20)
	resp := sample()
	resp.Warnings = []string{"response body exceeded 10485760 bytes and was truncated"}
	m.Set("big", resp)
	if !strings.Contains(m.View(), "truncated") {
		t.Error("warning missing from view")
	}
}

func TestScrollClamps(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 5) // tiny: body height 3
	resp := sample()
	resp.Body = []byte(strings.Repeat("line\n", 50))
	resp.Headers = http.Header{"Content-Type": {"text/plain"}}
	m.Set("s", resp)

	m.Scroll(1000)
	if m.top != m.maxTop() {
		t.Errorf("top past max: %d vs %d", m.top, m.maxTop())
	}
	m.Scroll(-1000)
	if m.top != 0 {
		t.Errorf("top below zero: %d", m.top)
	}
}

func TestEmptyStates(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	if !strings.Contains(m.View(), "no response yet") {
		t.Error("initial empty text missing")
	}
	resp := sample()
	resp.Body = nil
	m.Set("x", resp)
	if strings.Contains(m.View(), "no response yet") {
		t.Error("initial empty text must vanish after Set")
	}
}

func TestContentTag(t *testing.T) {
	cases := map[string]string{
		"application/json":          "json",
		"application/hal+json; q=1": "json",
		"text/xml":                  "xml",
		"application/soap+xml":      "xml",
		"text/html; charset=utf-8":  "html",
		"text/javascript":           "js",
		"application/octet-stream":  "",
		"":                          "",
		// Real-world headers (#1270): charset parameters, vendor suffixes
		// and the javascript spellings servers actually send.
		"application/json; charset=utf-8":   "json",
		"application/vnd.api+json":          "json",
		"  APPLICATION/JSON  ":              "json",
		"application/xhtml+xml":             "xml",
		"text/css; charset=UTF-8":           "css",
		"application/x-javascript":          "js",
		"application/ecmascript":            "js",
		"text/plain; charset=iso-8859-1":    "",
		"multipart/form-data; boundary=xyz": "",
	}
	for ct, want := range cases {
		if got := contentTag(ct); got != want {
			t.Errorf("%q: want %q, got %q", ct, want, got)
		}
	}
}

func TestHistoryBrowsing(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 20)
	newest := sample()
	newest.Body = []byte(`{"v":3}`)
	mid := sample()
	mid.Body = []byte(`{"v":2}`)
	oldest := sample()
	oldest.Body = []byte(`{"v":1}`)

	m.Set("r", newest)
	m.SetHistory([]HistoryItem{
		{Resp: newest},
		{Resp: mid, At: time.Date(2026, 7, 27, 11, 0, 0, 0, time.UTC)},
		{Resp: oldest, At: time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)},
	})
	if idx, n := m.HistoryIndex(); idx != 0 || n != 3 {
		t.Fatalf("initial index: %d/%d", idx, n)
	}
	if !strings.Contains(m.View(), `"v": 3`) {
		t.Error("latest response must show first")
	}
	if !strings.Contains(m.View(), "1/3") {
		t.Errorf("history position missing:\n%s", m.View())
	}

	// h steps to older entries, clamped at the oldest.
	m.handleKey(keyPress("h"))
	if !strings.Contains(m.View(), `"v": 2`) {
		t.Error("h must show the older response")
	}
	m.handleKey(keyPress("h"))
	m.handleKey(keyPress("h"))
	if idx, _ := m.HistoryIndex(); idx != 2 {
		t.Errorf("index must clamp at oldest, got %d", idx)
	}
	if !strings.Contains(m.View(), `"v": 1`) {
		t.Error("oldest response must show")
	}

	// l steps back to newer.
	m.handleKey(keyPress("l"))
	if !strings.Contains(m.View(), `"v": 2`) {
		t.Error("l must show the newer response")
	}

	// A new dispatch resets browsing to the fresh response only.
	m.Set("r", newest)
	if idx, n := m.HistoryIndex(); idx != 0 || n != 1 {
		t.Errorf("Set must reset history: %d/%d", idx, n)
	}
}

func keyPress(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}
