package httppane

import (
	"strings"
	"testing"
	"time"
)

// TestRequestLineShownWithSnapshot: the response pane header carries the
// method and final URL of the as-sent request (#2424), directly under the
// summary line.
func TestRequestLineShownWithSnapshot(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("create", withSnapshot())

	lines := strings.Split(stripANSI(m.View()), "\n")
	if len(lines) < 2 || !strings.Contains(lines[1], "POST") || !strings.Contains(lines[1], "https://example.test/things") {
		t.Fatalf("request line missing or misplaced:\n%v", lines[:min(3, len(lines))])
	}
}

// TestRequestLineHiddenWithoutSnapshot: a legacy entry stored before #1832
// has no snapshot, so there is nothing truthful to show — the header stays
// one line.
func TestRequestLineHiddenWithoutSnapshot(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("create", sample())

	if _, _, ok := m.requestLine(); ok {
		t.Error("requestLine must report ok=false without a snapshot")
	}
	// Without a request line the second rendered line is the first scrollable
	// body row — the status line — not a "METHOD url" line.
	lines := strings.Split(stripANSI(m.View()), "\n")
	if strings.HasPrefix(lines[1], " GET ") || strings.HasPrefix(lines[1], " POST ") {
		t.Fatalf("unexpected request line without a snapshot: %q", lines[1])
	}
}

// TestRequestLineFollowsHistory: stepping through history shows each entry's
// own request line, so historic paths and query parameters stay visible
// (#2424).
func TestRequestLineFollowsHistory(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	newest := withSnapshot()
	newest.Request.URL = "https://example.test/newest?x=1"
	older := withSnapshot()
	older.Request.URL = "https://example.test/older?y=2"
	m.Set("create", newest)
	m.SetHistory([]HistoryItem{
		{Resp: newest, At: time.Now()},
		{Resp: older, At: time.Now().Add(-time.Hour)},
	})

	if !strings.Contains(stripANSI(m.View()), "/newest?x=1") {
		t.Fatalf("newest request line missing:\n%s", m.View())
	}
	m.handleKey(keyPress("h")) // step to the older entry
	if !strings.Contains(stripANSI(m.View()), "/older?y=2") {
		t.Fatalf("older request line missing after history step:\n%s", m.View())
	}
}

// TestMiddleTruncateURLKeepsHostAndTail: a URL longer than the available
// width elides its middle, keeping the scheme+host and the tail of the
// query — the two places an environment difference usually shows up.
func TestMiddleTruncateURLKeepsHostAndTail(t *testing.T) {
	url := "https://api.example.test/v2/users/12345/orders?limit=50&offset=100&sort=desc&filter=active"
	got := middleTruncateURL(url, 50)

	if len([]rune(got)) != 50 {
		t.Fatalf("truncated length: %d, want 50 (%q)", len([]rune(got)), got)
	}
	if !strings.HasPrefix(got, "https://api.example.test") {
		t.Errorf("host not preserved: %q", got)
	}
	if !strings.HasSuffix(got, "filter=active") {
		t.Errorf("query tail not preserved: %q", got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("no ellipsis in truncated URL: %q", got)
	}
}

// TestMiddleTruncateURLNoop: a URL that already fits is returned unchanged.
func TestMiddleTruncateURLNoop(t *testing.T) {
	url := "https://example.test/short"
	if got := middleTruncateURL(url, 80); got != url {
		t.Errorf("short URL truncated: %q", got)
	}
}

// TestCopyURLKeyEmitsCopyMsg: "U" puts the full, untruncated URL on the
// clipboard (#2424) — the escape hatch for whatever the middle truncation
// hid.
func TestCopyURLKeyEmitsCopyMsg(t *testing.T) {
	m := New(nil)
	m.SetSize(30, 20) // narrow enough that the header line truncates
	m.Set("create", withSnapshot())

	cmd := m.handleKey(keyPress("U"))
	if cmd == nil {
		t.Fatal("U must emit a command")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("message type: %T", cmd())
	}
	if msg.Text != "https://example.test/things" {
		t.Errorf("copied text: %q", msg.Text)
	}
}

// TestCopyURLKeyNoopWithoutSnapshot: nothing to copy on a legacy entry.
func TestCopyURLKeyNoopWithoutSnapshot(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("create", sample())

	if cmd := m.handleKey(keyPress("U")); cmd != nil {
		t.Error("U must be a no-op without a snapshot")
	}
}

// TestToggleRequestDetailsShowsHeadersAndBody: "i" expands the request line
// into the as-sent headers and (small) body, collapsed by default (#2424).
func TestToggleRequestDetailsShowsHeadersAndBody(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("create", withSnapshot())

	if m.RequestDetailsOpen() {
		t.Fatal("details must start collapsed")
	}
	if strings.Contains(stripANSI(m.View()), `"name":"example"`) {
		t.Fatal("body must not show while collapsed")
	}

	m.handleKey(keyPress("i"))
	if !m.RequestDetailsOpen() {
		t.Fatal("i must open the details block")
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, "Content-Type: application/json") {
		t.Errorf("request headers missing:\n%s", view)
	}
	if !strings.Contains(view, `"name":"example"`) {
		t.Errorf("small request body missing:\n%s", view)
	}

	m.handleKey(keyPress("i"))
	if m.RequestDetailsOpen() {
		t.Fatal("second i must collapse again")
	}
}

// TestRequestDetailsLargeBodyNotInlined: a body past the inline limit is
// named instead of dumped into the header block.
func TestRequestDetailsLargeBodyNotInlined(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	resp := withSnapshot()
	resp.Request.Body = make([]byte, requestDetailBodyLimit+1)
	for i := range resp.Request.Body {
		resp.Request.Body[i] = 'a'
	}
	m.Set("create", resp)
	m.handleKey(keyPress("i"))

	view := stripANSI(m.View())
	if strings.Contains(view, strings.Repeat("a", 100)) {
		t.Error("oversized body must not be inlined")
	}
	if !strings.Contains(view, "too large to show") {
		t.Errorf("no size notice for oversized body:\n%s", view)
	}
}

// TestRequestDetailsSurviveHistoryStep: like raw (#2157), the expanded toggle
// belongs to the view, not to one entry.
func TestRequestDetailsSurviveHistoryStep(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	newest := withSnapshot()
	older := withSnapshot()
	m.Set("create", newest)
	m.SetHistory([]HistoryItem{{Resp: newest}, {Resp: older, At: time.Now()}})

	m.handleKey(keyPress("i"))
	m.handleKey(keyPress("h"))
	if !m.RequestDetailsOpen() {
		t.Error("the details toggle must survive a history step")
	}
}
