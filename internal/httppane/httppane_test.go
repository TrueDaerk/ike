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
	if strings.Contains(m.View(), "⧗ history") {
		t.Errorf("latest entry must not carry the header marker:\n%s", m.View())
	}

	// h steps to older entries, clamped at the oldest.
	m.handleKey(keyPress("h"))
	if !strings.Contains(m.View(), `"v": 2`) {
		t.Error("h must show the older response")
	}
	// An older entry marks the header so the content is identifiable as
	// historic at a glance (#1473), timestamp included.
	if !strings.Contains(m.View(), "⧗ history 2/3") {
		t.Errorf("header marker missing on older entry:\n%s", m.View())
	}
	if want := formatHistoryTime(time.Date(2026, 7, 27, 11, 0, 0, 0, time.UTC), time.Now()); !strings.Contains(m.View(), "⧗ history 2/3 ("+want+")") {
		t.Errorf("header marker must carry the timestamp %s:\n%s", want, m.View())
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

func TestFormatHistoryTime(t *testing.T) {
	now := time.Date(2026, 8, 10, 15, 4, 5, 0, time.Local)

	sameDay := time.Date(2026, 8, 10, 9, 30, 0, 0, time.Local)
	if got, want := formatHistoryTime(sameDay, now), "09:30:00"; got != want {
		t.Errorf("same day: got %q, want %q", got, want)
	}

	earlierDay := time.Date(2026, 8, 9, 15, 4, 5, 0, time.Local)
	if got, want := formatHistoryTime(earlierDay, now), "2026-08-09 15:04:05"; got != want {
		t.Errorf("earlier day: got %q, want %q", got, want)
	}
}

func keyPress(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// searchSample is a response whose composed view has hits in the status
// line, a header and the body — search spans the whole view (#1265).
func searchSample() *httpclient.Response {
	return &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Headers: http.Header{
			"Content-Type": {"application/json"},
			"X-Token":      {"token-abc"},
		},
		Body:     []byte(`{"token":"abc","list":[{"token":"def"}]}`),
		Duration: 3 * time.Millisecond,
	}
}

func searchViewer(t *testing.T) *Model {
	t.Helper()
	m := New(nil)
	m.SetSize(120, 20)
	m.Set("r", searchSample())
	return &m
}

// typeSearch opens the prompt and types pattern rune by rune.
func typeSearch(m *Model, pattern string) {
	m.handleKey(keyPress("/"))
	for _, r := range pattern {
		m.handleKey(keyPress(string(r)))
	}
}

func TestSearchFindsMatchesAcrossTheView(t *testing.T) {
	m := searchViewer(t)
	typeSearch(m, "token")
	if q, open := m.SearchQuery(); q != "token" || !open {
		t.Fatalf("prompt state: %q open=%v", q, open)
	}
	// Two hits in the X-Token header line (its name and its value, folded by
	// smartcase) plus two in the body.
	cur, total := m.MatchPosition()
	if cur != 1 || total != 4 {
		t.Fatalf("match position: %d/%d, want 1/4", cur, total)
	}
	// The hits really do span header and body rows.
	kinds := map[rowKind]bool{}
	for _, s := range m.matches {
		kinds[m.rows[s.Line].kind] = true
	}
	if !kinds[kindHeader] || !kinds[kindBody] {
		t.Errorf("matches must cover headers and body, got %v", kinds)
	}
}

// TestSearchChordAliases checks that ctrl+f and cmd+f open the search prompt
// exactly like "/" (#1830) — the muscle-memory chord used everywhere else in
// the app (editor find, terminal scrollback search) for users who reach for
// it before remembering the pane also takes "/".
func TestSearchChordAliases(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"ctrl+f", tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}},
		{"cmd+f", tea.KeyPressMsg{Code: 'f', Mod: tea.ModSuper}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := searchViewer(t)
			m.handleKey(tc.key)
			if _, open := m.SearchQuery(); !open {
				t.Fatalf("%s must open the search prompt", tc.name)
			}
			for _, r := range "token" {
				m.handleKey(keyPress(string(r)))
			}
			if q, _ := m.SearchQuery(); q != "token" {
				t.Fatalf("query: %q, want %q", q, "token")
			}
			if _, total := m.MatchPosition(); total != 4 {
				t.Errorf("matches: %d, want 4", total)
			}
		})
	}
}

func TestSearchSmartcase(t *testing.T) {
	m := searchViewer(t)
	typeSearch(m, "Token") // an uppercase rune forces an exact match
	if _, total := m.MatchPosition(); total != 1 {
		t.Errorf("exact-case matches: %d, want 1 (the X-Token header)", total)
	}
}

func TestSearchNextPrevWrap(t *testing.T) {
	m := searchViewer(t)
	typeSearch(m, "token")
	m.handleKey(keyPress("enter"))
	if _, open := m.SearchQuery(); open {
		t.Fatal("enter must close the prompt and keep the query")
	}
	for want := 2; want <= 4; want++ {
		m.handleKey(keyPress("n"))
		if cur, _ := m.MatchPosition(); cur != want {
			t.Fatalf("n step: got %d, want %d", cur, want)
		}
	}
	m.handleKey(keyPress("n")) // wraps
	if cur, _ := m.MatchPosition(); cur != 1 {
		t.Errorf("n must wrap to 1, got %d", cur)
	}
	m.handleKey(keyPress("N")) // wraps backwards
	if cur, _ := m.MatchPosition(); cur != 4 {
		t.Errorf("N must wrap to 4, got %d", cur)
	}
}

func TestSearchEscClearsState(t *testing.T) {
	m := searchViewer(t)
	typeSearch(m, "token")
	m.handleKey(keyPress("esc"))
	if q, open := m.SearchQuery(); q != "" || open {
		t.Fatalf("esc during typing must clear: %q open=%v", q, open)
	}
	if _, total := m.MatchPosition(); total != 0 {
		t.Error("esc must drop the matches")
	}

	// Esc after committing clears just as well.
	typeSearch(m, "token")
	m.handleKey(keyPress("enter"))
	m.handleKey(keyPress("esc"))
	if q, _ := m.SearchQuery(); q != "" {
		t.Errorf("esc after commit must clear the query, got %q", q)
	}
}

func TestSearchBackspaceReRuns(t *testing.T) {
	m := searchViewer(t)
	typeSearch(m, "tokenz")
	if _, total := m.MatchPosition(); total != 0 {
		t.Fatalf("no hits expected for 'tokenz', got %d", total)
	}
	m.handleKey(keyPress("backspace"))
	if q, _ := m.SearchQuery(); q != "token" {
		t.Fatalf("query after backspace: %q", q)
	}
	if _, total := m.MatchPosition(); total != 4 {
		t.Errorf("matches must come back after backspace, got %d", total)
	}
}

func TestSearchFooterShowsPosition(t *testing.T) {
	m := searchViewer(t)
	typeSearch(m, "token")
	if !strings.Contains(m.View(), "1/4") {
		t.Errorf("typing footer must show the position:\n%s", m.View())
	}
	m.handleKey(keyPress("enter"))
	view := m.View()
	if !strings.Contains(view, "1/4") || !strings.Contains(view, "n/N") {
		t.Errorf("committed footer must show position and hints:\n%s", view)
	}
	typeSearch(m, "zzz")
	if !strings.Contains(m.View(), "no matches") {
		t.Errorf("a pattern without hits must say so:\n%s", m.View())
	}
}

func TestSearchScrollsCurrentMatchIntoView(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 6) // 4 body rows visible
	resp := searchSample()
	var b strings.Builder
	b.WriteString("{\n")
	for i := 0; i < 60; i++ {
		b.WriteString("  \"pad\": 0,\n")
	}
	b.WriteString(`  "needle": 1` + "\n}")
	resp.Body = []byte(b.String())
	resp.Headers = http.Header{"Content-Type": {"text/plain"}}
	m.Set("r", resp)

	typeSearch(&m, "needle")
	cur, total := m.MatchPosition()
	if cur != 1 || total != 1 {
		t.Fatalf("match position: %d/%d", cur, total)
	}
	line := m.matches[0].Line
	if line < m.top || line >= m.top+m.bodyHeight() {
		t.Errorf("match on row %d not in view [%d,%d)", line, m.top, m.top+m.bodyHeight())
	}
}

func TestSearchSurvivesHistoryBrowsing(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 20)
	newest := searchSample()
	older := searchSample()
	older.Body = []byte(`{"token":"only-once"}`)
	m.Set("r", newest)
	m.SetHistory([]HistoryItem{{Resp: newest}, {Resp: older}})

	typeSearch(&m, "token")
	m.handleKey(keyPress("enter"))
	if _, total := m.MatchPosition(); total != 4 {
		t.Fatalf("newest response matches: %d, want 4", total)
	}
	m.handleKey(keyPress("h")) // older entry
	if _, total := m.MatchPosition(); total != 3 {
		t.Errorf("older response matches: %d, want 3 (two in the header, one in the body)", total)
	}
}

func TestSearchHighlightsMatches(t *testing.T) {
	m := searchViewer(t)
	plain := m.View()
	typeSearch(m, "token")
	if m.View() == plain {
		t.Error("matches must change the rendered output")
	}
}

// TestSearchPromptSwallowsNavigationKeys: while typing, j/k/h/l are pattern
// text, not scroll and history commands.
func TestSearchPromptSwallowsNavigationKeys(t *testing.T) {
	m := searchViewer(t)
	top := m.top
	m.handleKey(keyPress("/"))
	for _, k := range []string{"j", "k", "h", "l"} {
		m.handleKey(keyPress(k))
	}
	if q, _ := m.SearchQuery(); q != "jkhl" {
		t.Errorf("prompt must collect the keys, got %q", q)
	}
	if m.top != top {
		t.Errorf("prompt keys must not scroll: top %d → %d", top, m.top)
	}
}

// TestSearchCursorMotion covers left/right/home/end/word motions on the
// prompt (#1845), matching ui.EditKey's contract used by the terminal
// scrollback search field.
func TestSearchCursorMotion(t *testing.T) {
	m := searchViewer(t)
	typeSearch(m, "token abc")
	if m.qcur != len([]rune("token abc")) {
		t.Fatalf("cursor after typing: %d, want end", m.qcur)
	}
	m.handleKey(keyPress("left"))
	m.handleKey(keyPress("left"))
	if want := len([]rune("token abc")) - 2; m.qcur != want {
		t.Fatalf("cursor after two lefts: %d, want %d", m.qcur, want)
	}
	m.handleKey(keyPress("right"))
	if want := len([]rune("token abc")) - 1; m.qcur != want {
		t.Fatalf("cursor after right: %d, want %d", m.qcur, want)
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyHome})
	if m.qcur != 0 {
		t.Fatalf("home must move the cursor to 0, got %d", m.qcur)
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnd})
	if m.qcur != len([]rune("token abc")) {
		t.Fatalf("end must move the cursor to the query length, got %d", m.qcur)
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	if want := len([]rune("token ")); m.qcur != want {
		t.Fatalf("alt+left must jump to the start of 'abc', got %d want %d", m.qcur, want)
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModSuper})
	if m.qcur != 0 {
		t.Fatalf("super+left must jump to the query start, got %d", m.qcur)
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModSuper})
	if m.qcur != len([]rune("token abc")) {
		t.Fatalf("super+right must jump to the query end, got %d", m.qcur)
	}
}

// TestSearchWordDeletion covers opt+backspace/opt+delete word deletion and
// plain backspace/delete at the cursor (#1845).
func TestSearchWordDeletion(t *testing.T) {
	m := searchViewer(t)
	typeSearch(m, "token abc")
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if q, _ := m.SearchQuery(); q != "token " {
		t.Fatalf("alt+backspace must delete the last word, got %q", q)
	}
	if m.qcur != len([]rune("token ")) {
		t.Fatalf("cursor after alt+backspace: %d, want %d", m.qcur, len([]rune("token ")))
	}

	m.handleKey(tea.KeyPressMsg{Code: tea.KeyHome})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModAlt})
	if q, _ := m.SearchQuery(); q != " " {
		t.Fatalf("alt+delete at the start must delete the next word, got %q", q)
	}

	// Plain backspace/delete edit at the cursor position, not the end.
	m.clearSearch()
	typeSearch(m, "abcdef")
	for i := 0; i < 3; i++ {
		m.handleKey(keyPress("left"))
	}
	m.handleKey(keyPress("backspace"))
	if q, _ := m.SearchQuery(); q != "abdef" {
		t.Fatalf("backspace at cursor: %q, want %q", q, "abdef")
	}
	m.handleKey(keyPress("delete"))
	if q, _ := m.SearchQuery(); q != "abef" {
		t.Fatalf("delete at cursor: %q, want %q", q, "abef")
	}
}

// TestSearchKillToStart covers cmd+backspace killing from the cursor to the
// line start (#1845).
func TestSearchKillToStart(t *testing.T) {
	m := searchViewer(t)
	typeSearch(m, "token abc")
	for i := 0; i < 3; i++ {
		m.handleKey(keyPress("left"))
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper})
	if q, _ := m.SearchQuery(); q != "abc" {
		t.Fatalf("super+backspace must kill to the line start, got %q", q)
	}
	if m.qcur != 0 {
		t.Fatalf("cursor after kill-to-start: %d, want 0", m.qcur)
	}
}

// TestSearchPromptRendersCursor guards the visible block cursor via
// ui.CursorView (#1845), consistent with the terminal scrollback search.
func TestSearchPromptRendersCursor(t *testing.T) {
	m := searchViewer(t)
	typeSearch(m, "token")
	withCursor := m.View()
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	moved := m.View()
	if withCursor == moved {
		t.Error("moving the cursor must change the rendered prompt")
	}
}

// TestPendingMarkerShownInHeader covers the pane side of #1272: while a
// dispatch runs the header says so, and the previous response stays readable.
func TestPendingMarkerShownInHeader(t *testing.T) {
	m := searchViewer(t)
	if m.Pending() != "" {
		t.Fatal("a fresh viewer has nothing pending")
	}
	m.SetPending("one", time.Now())
	view := m.View()
	if !strings.Contains(view, "running one") {
		t.Errorf("header must mark the in-flight request:\n%s", view)
	}
	if !strings.Contains(view, "token") {
		t.Error("the previous response must stay readable while running")
	}
	m.ClearPending()
	if strings.Contains(m.View(), "running") {
		t.Error("clearing must drop the marker")
	}
}

// TestCancelKeyEmitsCancelMsg: "x" asks the host to abort (#1272).
func TestCancelKeyEmitsCancelMsg(t *testing.T) {
	m := searchViewer(t)
	cmd := m.handleKey(keyPress("x"))
	if cmd == nil {
		t.Fatal("x must emit a command")
	}
	if _, ok := cmd().(CancelMsg); !ok {
		t.Fatalf("message type: %T", cmd())
	}
}

// bigSample returns a response whose pretty-printed body overflows a small
// viewport in both directions, so scroll offsets are meaningful.
func bigSample(v int) *httpclient.Response {
	var b strings.Builder
	b.WriteString(`{"v":` + string(rune('0'+v)) + `,"wide":"`)
	b.WriteString(strings.Repeat("x", 200))
	b.WriteString(`"`)
	for i := 0; i < 40; i++ {
		b.WriteString(`,"k` + string(rune('a'+i%26)) + string(rune('0'+i/26)) + `":` + string(rune('0'+i%10)))
	}
	b.WriteString(`}`)
	r := sample()
	r.Body = []byte(b.String())
	return r
}

// TestHistoryStepResetsScrollByDefault pins the pre-#1493 behavior: without
// the toggle, stepping through history returns to the top-left corner.
func TestHistoryStepResetsScrollByDefault(t *testing.T) {
	m := New(nil)
	m.SetSize(40, 10)
	m.Set("r", bigSample(2))
	m.SetHistory([]HistoryItem{{Resp: bigSample(2)}, {Resp: bigSample(1)}})

	m.Scroll(5)
	m.ScrollX(16)
	m.handleKey(keyPress("h"))
	if m.top != 0 || m.Left() != 0 {
		t.Errorf("default step must reset scroll, got top=%d left=%d", m.top, m.Left())
	}
}

// TestKeepScrollPreservesPositionAcrossHistory covers the #1493 toggle: with
// keep-scroll on, h/l keep the viewport offsets, clamped to the shown entry.
func TestKeepScrollPreservesPositionAcrossHistory(t *testing.T) {
	m := New(nil)
	m.SetSize(40, 10)
	m.Set("r", bigSample(2))
	m.SetHistory([]HistoryItem{{Resp: bigSample(2)}, {Resp: bigSample(1)}})

	m.handleKey(keyPress("s"))
	if !m.KeepScroll() {
		t.Fatal("s must enable keep-scroll")
	}
	m.Scroll(5)
	m.ScrollX(16)
	m.handleKey(keyPress("h"))
	if m.top != 5 || m.Left() != 16 {
		t.Errorf("keep-scroll step lost the position, got top=%d left=%d", m.top, m.Left())
	}
	m.handleKey(keyPress("l"))
	if m.top != 5 || m.Left() != 16 {
		t.Errorf("stepping back lost the position, got top=%d left=%d", m.top, m.Left())
	}

	// A shorter historic entry clamps instead of overshooting.
	short := sample()
	m.Set("r", bigSample(2))
	m.SetHistory([]HistoryItem{{Resp: bigSample(2)}, {Resp: short}})
	m.Scroll(1000)
	m.handleKey(keyPress("h"))
	if m.top > m.maxTop() {
		t.Errorf("kept offset must clamp, got top=%d max=%d", m.top, m.maxTop())
	}

	// s again turns it off; the next step resets like before.
	m.handleKey(keyPress("s"))
	if m.KeepScroll() {
		t.Fatal("s must toggle keep-scroll off")
	}
	m.handleKey(keyPress("l"))
	if m.top != 0 {
		t.Errorf("disabled toggle must reset scroll, got top=%d", m.top)
	}
}

// TestKeepScrollIsPerRequest: enabling the toggle for one request must not
// change another's behavior (#1493).
func TestKeepScrollIsPerRequest(t *testing.T) {
	m := New(nil)
	m.SetSize(40, 10)
	m.Set("a", bigSample(1))
	m.handleKey(keyPress("s"))
	if !m.KeepScroll() {
		t.Fatal("keep-scroll must be on for request a")
	}

	m.Set("b", bigSample(2))
	if m.KeepScroll() {
		t.Error("request b must default to off")
	}

	m.Set("a", bigSample(1))
	if !m.KeepScroll() {
		t.Error("request a must remember its toggle")
	}
}

// TestKeepScrollFooter: the footer hints the key once history is browsable
// and anchors the active state (#1493).
func TestKeepScrollFooter(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 20)
	m.Set("r", sample())
	m.SetHistory([]HistoryItem{{Resp: sample()}, {Resp: sample()}})
	if !strings.Contains(m.footerText(), "s keep pos") {
		t.Errorf("footer hint missing: %q", m.footerText())
	}
	m.handleKey(keyPress("s"))
	if !strings.Contains(m.footerText(), "⚓ keep pos") {
		t.Errorf("footer anchor missing: %q", m.footerText())
	}
}
