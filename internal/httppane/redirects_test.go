package httppane

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"ike/internal/highlight"
	"ike/internal/httpclient"
)

// chained returns the sample response as it looks after a 301 → 302 → 200,
// with the per-hop connection facts httptrace reports (#2716).
func chained() *httpclient.Response {
	resp := sample()
	resp.Redirects = []httpclient.Hop{
		{
			Method: "GET", URL: "http://example.com/", Status: 301,
			Location:   "https://example.com/",
			DNSAddrs:   []string{"93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"},
			RemoteAddr: "93.184.216.34:80",
		},
		{
			Method: "GET", URL: "https://example.com/", Status: 302,
			Location:   "https://www.example.com/",
			DNSAddrs:   []string{"93.184.216.34"},
			RemoteAddr: "93.184.216.34:443", TLSServerName: "example.com", Proto: "h2",
		},
		{
			Method: "GET", URL: "https://www.example.com/", Status: 200,
			RemoteAddr: "93.184.216.34:443", Reused: true,
		},
	}
	resp.FinalURL = "https://www.example.com/"
	return resp
}

// chainText joins the composed chain rows, so a test asserts on the block
// rather than on row indices.
func chainText(t *testing.T, m *Model) string {
	t.Helper()
	var lines []string
	for _, r := range m.rows {
		if r.kind == kindRedirects {
			lines = append(lines, r.text)
		}
	}
	return strings.Join(lines, "\n")
}

// TestRedirectBlockUnderStatusLine: the chain composes between the status row
// and the timing row, one step per hop, and the status row carries the
// counter (#2716).
func TestRedirectBlockUnderStatusLine(t *testing.T) {
	resp := chained()
	resp.Timing = &httpclient.Timing{TTFB: 210 * time.Millisecond}

	m := New(nil)
	m.SetSize(120, 24)
	m.Set("create", resp)

	if m.rows[0].kind != kindStatus {
		t.Fatalf("first row is not the status line: %+v", m.rows[0])
	}
	if !strings.Contains(m.rows[0].text, "· 2 redirects") {
		t.Fatalf("status row = %q, want the redirect counter", m.rows[0].text)
	}
	if m.rows[1].kind != kindRedirects {
		t.Fatalf("second row = %v, want the chain header", m.rows[1].kind)
	}
	// The timing line stays the accumulated sum and sits *below* the block it
	// is explained by.
	timing := -1
	for i, r := range m.rows {
		if r.kind == kindTiming {
			timing = i
		}
	}
	if start, end, ok := m.RedirectRows(); !ok || timing != end+1 {
		t.Fatalf("chain rows %d..%d ok=%v, timing row %d — timing must follow the block", start, end, ok, timing)
	}

	block := chainText(t, &m)
	for _, want := range []string{
		"2 redirects · ended at https://www.example.com/",
		"↳ GET  http://example.com/",
		"→ 301  Location: https://example.com/",
		"→ 302  Location: https://www.example.com/",
		"→ 200",
		"dns example.com → 93.184.216.34, 2606:2800:220:1:248:1893:25c8:1946",
		"connected 93.184.216.34:80",
		"tls example.com h2",
		"(reused: yes)",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("chain block misses %q:\n%s", want, block)
		}
	}
	if view := m.View(); !strings.Contains(view, "2 redirects") {
		t.Errorf("chain missing from the view:\n%s", view)
	}
}

// TestDirectResponseHasNoRedirectBlock: a plain 200 renders neither a counter
// nor a block, so nothing changes for the common case (#2716).
func TestDirectResponseHasNoRedirectBlock(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 24)
	m.Set("create", sample())

	for _, r := range m.rows {
		if r.kind == kindRedirects {
			t.Fatalf("chain row composed without a chain: %q", r.text)
		}
	}
	if strings.Contains(m.rows[0].text, "redirect") {
		t.Errorf("status row = %q, want no redirect counter", m.rows[0].text)
	}
	if _, _, ok := m.RedirectRows(); ok {
		t.Error("RedirectRows reports a block on a direct answer")
	}
}

// TestRedirectNotFollowedStatusAndLocation: with -L off a 3xx says the
// redirect was declined and the Location header is emphasised, since it is
// where the answer would have gone (#2716).
func TestRedirectNotFollowedStatusAndLocation(t *testing.T) {
	resp := sample()
	resp.Status, resp.StatusCode = "301 Moved Permanently", 301
	resp.Headers = http.Header{"Location": {"https://example.com/moved"}}
	resp.RedirectsOff = true

	m := New(nil)
	m.SetSize(120, 24)
	m.Set("create", resp)

	if !strings.Contains(m.rows[0].text, "redirect not followed (.curlrc location=off)") {
		t.Fatalf("status row = %q, want the declined-redirect note", m.rows[0].text)
	}
	for _, r := range m.rows {
		if r.kind == kindRedirects {
			t.Fatalf("chain row composed for a declined redirect: %q", r.text)
		}
	}
	loc := -1
	for i, r := range m.rows {
		if r.kind == kindHeader && strings.HasPrefix(r.text, "Location: ") {
			loc = i
		}
	}
	if loc < 0 {
		t.Fatalf("no Location header row: %+v", m.rows)
	}
	pal := m.theme()
	style, key := m.baseStyle(pal, loc, 0, len([]rune(m.rows[loc].text)))(0)
	if key != "location" {
		t.Fatalf("Location header style key = %q, want the emphasised one", key)
	}
	if style.GetForeground() != pal.Warning || !style.GetBold() {
		t.Errorf("Location header is not emphasised: fg=%v bold=%v", style.GetForeground(), style.GetBold())
	}
}

// TestRedirectMethodChangeShown: a POST redirected with 303 continues as a
// GET, and the step that caused it says so (#2716).
func TestRedirectMethodChangeShown(t *testing.T) {
	resp := sample()
	resp.Redirects = []httpclient.Hop{
		{Method: "POST", URL: "https://example.com/submit", Status: 303,
			Location: "https://example.com/done", MethodChanged: true},
		{Method: "GET", URL: "https://example.com/done", Status: 200},
	}
	resp.FinalURL = "https://example.com/done"

	m := New(nil)
	m.SetSize(120, 24)
	m.Set("create", resp)

	block := chainText(t, &m)
	if !strings.Contains(block, "↳ POST  https://example.com/submit") {
		t.Errorf("first step missing the POST:\n%s", block)
	}
	if !strings.Contains(block, "method changed") {
		t.Errorf("method change not reported:\n%s", block)
	}
	if !strings.Contains(block, "↳ GET   https://example.com/done") {
		t.Errorf("second step missing the rewritten GET:\n%s", block)
	}
	if strings.Contains(m.rows[0].text, "2 redirects") {
		t.Errorf("status row = %q, want 1 redirect for a two-hop chain", m.rows[0].text)
	}
}

// TestRedirectBlockFoldsOnEnter: enter toggles the block like every other
// foldable row, and a short chain starts expanded (#2716).
func TestRedirectBlockFoldsOnEnter(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 24)
	m.Set("create", chained())

	header, end, ok := m.RedirectRows()
	if !ok {
		t.Fatal("no chain block composed")
	}
	if _, collapsed := m.FoldedAt(header); collapsed {
		t.Fatal("a three-hop chain must compose expanded")
	}
	if !m.Foldable(header) {
		t.Fatal("the chain header is not foldable")
	}

	m.handleKey(keyPress("enter"))
	if got, collapsed := m.FoldedAt(header); !collapsed || got != end {
		t.Fatalf("enter did not collapse the chain: end=%d collapsed=%v, want %d", got, collapsed, end)
	}
	if m.RowVisible(header + 1) {
		t.Error("a collapsed chain still shows its steps")
	}
	m.handleKey(keyPress("enter"))
	if _, collapsed := m.FoldedAt(header); collapsed {
		t.Fatal("enter did not expand the chain again")
	}
}

// TestLongRedirectChainStartsFolded: past three hops the block would push the
// body off screen, so it composes collapsed (#2716).
func TestLongRedirectChainStartsFolded(t *testing.T) {
	resp := sample()
	for i := 0; i < 5; i++ {
		resp.Redirects = append(resp.Redirects, httpclient.Hop{
			Method: "GET", URL: "https://example.com/h", Status: 302, Location: "https://example.com/h",
		})
	}
	resp.Redirects[4].Status, resp.Redirects[4].Location = 200, ""

	m := New(nil)
	m.SetSize(120, 24)
	m.Set("create", resp)

	header, end, ok := m.RedirectRows()
	if !ok {
		t.Fatal("no chain block composed")
	}
	if got, collapsed := m.FoldedAt(header); !collapsed || got != end {
		t.Fatalf("a five-hop chain composed expanded: end=%d collapsed=%v", got, collapsed)
	}
}

// TestCopyKeyOnRedirectBlockCopiesChain: "y" over the block puts the whole
// chain on the clipboard rather than the body (#2716).
func TestCopyKeyOnRedirectBlockCopiesChain(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 24)
	m.Set("create", chained())

	cmd := m.handleKey(keyPress("y"))
	if cmd == nil {
		t.Fatal("y over the chain emitted no copy command")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("copy message = %T, want CopyMsg", cmd())
	}
	if msg.What != "redirect chain" {
		t.Fatalf("copy label = %q, want the chain label", msg.What)
	}
	if !strings.Contains(msg.Text, "↳ GET  http://example.com/") ||
		!strings.Contains(msg.Text, "tls example.com h2") {
		t.Fatalf("copied text is not the chain:\n%s", msg.Text)
	}
	if strings.Contains(msg.Text, `"b":2`) {
		t.Fatalf("copied text leaked the body:\n%s", msg.Text)
	}
}

// TestCopyKeyBelowRedirectBlockCopiesBody: once the view has scrolled past
// the chain the copy key means what it always meant (#2716).
func TestCopyKeyBelowRedirectBlockCopiesBody(t *testing.T) {
	resp := chained()
	// A body long enough that the viewport can actually leave the block
	// behind — otherwise the clamp keeps the chain at the top of the view.
	lines := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		lines = append(lines, "line")
	}
	resp.Headers = http.Header{"Content-Type": {"text/plain"}}
	resp.Body = []byte(strings.Join(lines, "\n"))

	m := New(nil)
	m.SetSize(120, 10)
	m.Set("create", resp)

	_, end, _ := m.RedirectRows()
	m.ScrollToRow(end + 1)
	if m.rowAt(m.top) <= end {
		t.Fatalf("the view is still on the chain block (top row %d, block ends at %d)", m.rowAt(m.top), end)
	}
	cmd := m.handleKey(keyPress("y"))
	if cmd == nil {
		t.Fatal("y emitted no copy command")
	}
	msg := cmd().(CopyMsg)
	if msg.What != "response body" {
		t.Fatalf("copy label = %q, want the body label", msg.What)
	}
}

// TestRedirectBlockSurvivesHighlightPass: the body's syntax pass replaces the
// body's folds wholesale; the chain's own fold — and its collapse state —
// must not go with them (#2716).
func TestRedirectBlockSurvivesHighlightPass(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 24)
	m.Set("create", chained())

	header, end, _ := m.RedirectRows()
	m.ToggleFold(header)

	bodyStart := -1
	for i, r := range m.rows {
		if r.kind == kindBody {
			bodyStart = i
			break
		}
	}
	if bodyStart < 0 {
		t.Fatalf("no body rows composed: %+v", m.rows)
	}
	m.setFolds([]highlight.Fold{{HeaderLine: 0, EndLine: 1}}, bodyStart)

	if !m.Foldable(header) {
		t.Fatal("the chain fold was dropped by the highlight pass")
	}
	if got, collapsed := m.FoldedAt(header); !collapsed || got != end {
		t.Fatalf("the chain's collapse state was lost: end=%d collapsed=%v", got, collapsed)
	}
}

// TestRedirectChainSurvivesHistoryBrowsing: the chain travels with the stored
// entry, and an older entry recorded without one renders as it always did
// (#2716).
func TestRedirectChainSurvivesHistoryBrowsing(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 24)
	m.Set("create", chained())
	m.SetHistory([]HistoryItem{{Resp: chained()}, {Resp: sample()}})

	if _, _, ok := m.RedirectRows(); !ok {
		t.Fatal("the newest entry lost its chain")
	}
	m.handleKey(keyPress("h")) // step to the older entry
	if _, _, ok := m.RedirectRows(); ok {
		t.Error("an entry without a recorded chain composed one")
	}
	for _, r := range m.rows {
		if r.kind == kindRedirects {
			t.Fatalf("legacy entry composed a chain row: %q", r.text)
		}
	}
	m.handleKey(keyPress("l")) // and back
	if _, _, ok := m.RedirectRows(); !ok {
		t.Fatal("stepping back lost the chain")
	}
	if !strings.Contains(m.rows[0].text, "· 2 redirects") {
		t.Errorf("status row = %q, want the counter back", m.rows[0].text)
	}
}

// TestTruncateURLKeepsTheHost: a URL too long for the pane loses its path's
// head, never the host — "where did it go" is the question the block answers
// (#2716).
func TestTruncateURLKeepsTheHost(t *testing.T) {
	long := "https://api.example.com/v1/tenants/42/resources/abcdef/children?page=3"
	got := truncateURL(long, 40)
	if len([]rune(got)) > 40 {
		t.Fatalf("truncated to %d runes, want at most 40: %q", len([]rune(got)), got)
	}
	if !strings.HasPrefix(got, "https://api.example.com…") {
		t.Fatalf("truncated URL = %q, want the host kept", got)
	}
	if !strings.HasSuffix(got, "page=3") {
		t.Fatalf("truncated URL = %q, want the tail kept", got)
	}
	if short := truncateURL(long, 200); short != long {
		t.Fatalf("a URL that fits was changed: %q", short)
	}
	// A host that alone overruns the budget still wins over the path.
	if got := truncateURL("https://very-long-host.example.com/x", 12); !strings.HasPrefix(got, "https://very-long-host") {
		t.Fatalf("truncated URL = %q, want the host kept whole", got)
	}
}

// TestRedirectCounterSingular: one redirect is not "1 redirects" (#2716).
func TestRedirectCounterSingular(t *testing.T) {
	if got := pluralRedirects(1); got != "1 redirect" {
		t.Fatalf("pluralRedirects(1) = %q", got)
	}
	if got := pluralRedirects(3); got != "3 redirects" {
		t.Fatalf("pluralRedirects(3) = %q", got)
	}
}
