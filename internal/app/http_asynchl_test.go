package app

// http_asynchl_test.go is the regression test for the http.run freeze
// (#2353): filling the response viewer must never run the body's syntax pass
// inside the update pass. The reproducing shape — a large minified single
// line with non-ASCII content — rides through fillHTTPPanel, and the pass
// arrives as a command instead.

import (
	"net/http"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	_ "ike/plugins/languages/json"

	"ike/internal/highlight"
	"ike/internal/httpclient"
	"ike/internal/httppane"
)

// largeMinifiedJSON builds a single-line JSON body of roughly n bytes with
// non-ASCII content — the Elasticsearch-answer shape of #2353.
func largeMinifiedJSON(n int) []byte {
	var b strings.Builder
	b.Grow(n + 64)
	b.WriteString(`{"hüts":[`)
	for b.Len() < n {
		b.WriteString(`{"kü":"wört mit Ümläuten"},`)
	}
	return []byte(strings.TrimSuffix(b.String(), ",") + `]}`)
}

// drainHighlighted walks cmd (possibly a batch) until the pane's finished
// syntax pass appears.
func drainHighlighted(t *testing.T, cmd tea.Cmd) httppane.HighlightedMsg {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case httppane.HighlightedMsg:
			return msg
		case tea.BatchMsg:
			queue = append(queue, msg...)
		}
	}
	t.Fatal("no HighlightedMsg produced")
	return httppane.HighlightedMsg{}
}

// TestHTTPResponseHighlightOffLoop: the update pass composes the body plain
// and returns the syntax pass as a command; pumping the command's message
// back in paints the rows (#2353).
func TestHTTPResponseHighlightOffLoop(t *testing.T) {
	if !highlight.FencedSupported("json") {
		t.Skip("no JSON grammar in this build")
	}
	m := httpApp(t)
	resp := &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       largeMinifiedJSON(256 << 10),
		Duration:   3 * time.Millisecond,
		RequestKey: "big",
	}

	start := time.Now()
	out, cmd := m.Update(HTTPResponseMsg{Request: "big", Resp: resp})
	elapsed := time.Since(start)
	m = out.(Model)

	p := m.httpPanel()
	if p == nil || p.Rows() == 0 {
		t.Fatal("the response must compose immediately")
	}
	if p.Highlighted() {
		t.Fatal("the update pass must not highlight synchronously (#2353)")
	}
	// Generous: composing a quarter-megabyte body is milliseconds; only a
	// synchronous parse regression (or worse, the old quadratic one) fails it.
	if elapsed > 5*time.Second {
		t.Fatalf("the update pass took %v — it must not wait on the body", elapsed)
	}

	msg := drainHighlighted(t, cmd)
	out, _ = m.Update(msg)
	m = out.(Model)
	if !m.httpPanel().Highlighted() {
		t.Error("the routed HighlightedMsg must paint the body")
	}
}

// TestHTTPResponseHighlightCapNotice: a body past http.highlight_limit_kb
// composes plain, schedules nothing, and the pane says why (#2353).
func TestHTTPResponseHighlightCapNotice(t *testing.T) {
	if !highlight.FencedSupported("json") {
		t.Skip("no JSON grammar in this build")
	}
	m := httpApp(t)
	// After httpApp: app construction re-applies the config default.
	httppane.SetHighlightLimit(64)
	defer httppane.SetHighlightLimit(httppane.DefaultHighlightLimitKB)
	resp := &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       largeMinifiedJSON(256 << 10),
		Duration:   3 * time.Millisecond,
		RequestKey: "big",
	}
	out, _ := m.Update(HTTPResponseMsg{Request: "big", Resp: resp})
	m = out.(Model)

	p := m.httpPanel()
	if p.Highlighted() || p.FinishHighlight() {
		t.Fatal("past the limit no pass may be scheduled")
	}
	found := false
	for _, w := range p.Warnings() {
		if strings.Contains(w, "highlight limit") {
			found = true
		}
	}
	if !found {
		t.Errorf("the cap must be surfaced, warnings: %v", p.Warnings())
	}
}
