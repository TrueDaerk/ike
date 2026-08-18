package espane

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/theme"
)

// fakeCluster is a fake ES backend over httptest: nDocs documents named
// doc-0…doc-n in one index "logs", plus an empty index and an alias. It
// records the last _search body so tests can assert what the pane sent.
type fakeCluster struct {
	srv      *httptest.Server
	nDocs    int
	lastBody map[string]any
	searches int
}

func newFakeCluster(t *testing.T, nDocs int) *fakeCluster {
	t.Helper()
	f := &fakeCluster{nDocs: nDocs}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_cat/indices":
			fmt.Fprintf(w, `[{"index":"logs","docs.count":"%d"},{"index":"empty","docs.count":"0"}]`, f.nDocs)
		case r.URL.Path == "/_cat/aliases":
			io.WriteString(w, `[{"alias":"all-logs"}]`)
		case strings.HasSuffix(r.URL.Path, "/_mapping"):
			io.WriteString(w, `{"logs":{"mappings":{"properties":{"msg":{"type":"text"},"level":{"type":"keyword"}}}}}`)
		case strings.HasSuffix(r.URL.Path, "/_search"):
			f.searches++
			raw, _ := io.ReadAll(r.Body)
			f.lastBody = map[string]any{}
			json.Unmarshal(raw, &f.lastBody)
			from := int(f.lastBody["from"].(float64))
			size := int(f.lastBody["size"].(float64))
			n := f.nDocs
			if strings.HasPrefix(r.URL.Path, "/empty/") {
				n = 0
			}
			// A body carrying a term query on level pretends to match half.
			if q, ok := f.lastBody["query"].(map[string]any); ok {
				if _, filtered := q["term"]; filtered {
					n = f.nDocs / 2
				}
				if _, bad := q["brokenn"]; bad {
					w.WriteHeader(http.StatusBadRequest)
					io.WriteString(w, `{"error":{"reason":"unknown query [brokenn]"},"status":400}`)
					return
				}
			}
			var hits []string
			for i := from; i < n && i < from+size; i++ {
				hits = append(hits, fmt.Sprintf(`{"_id":"doc-%d","_score":1.0,"_source":{"msg":"m%d","level":"info","meta":{"seq":%d}}}`, i, i, i))
			}
			fmt.Fprintf(w, `{"hits":{"total":{"value":%d,"relation":"eq"},"hits":[%s]}}`, n, strings.Join(hits, ","))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// installEndpoint points the live config's "test" endpoint at url.
func installEndpoint(t *testing.T, url string) {
	t.Helper()
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	cfg, _ := config.Load(config.Options{})
	cfg.Elasticsearch.Endpoints = []config.ESEndpoint{{Name: "test", URL: url}}
	config.Set(cfg)
}

// pump runs commands like the bubbletea runtime would: batches are
// flattened, ResultMsg goes back into the model, everything else — the
// messages meant for the root model — is returned.
func pump(t *testing.T, m *Model, cmds ...tea.Cmd) []tea.Msg {
	t.Helper()
	var out []tea.Msg
	for len(cmds) > 0 {
		cmd := cmds[0]
		cmds = cmds[1:]
		if cmd == nil {
			continue
		}
		switch msg := cmd().(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				cmds = append(cmds, tea.Cmd(c))
			}
		case ResultMsg:
			cmds = append(cmds, m.Update(msg))
		case nil:
		default:
			out = append(out, msg)
		}
	}
	return out
}

func feed(t *testing.T, m *Model, msg tea.Msg) []tea.Msg {
	t.Helper()
	return pump(t, m, m.Update(msg))
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// newPane opens a console pane against the fake cluster and pumps the
// background open to completion.
func newPane(t *testing.T, f *fakeCluster) *Model {
	t.Helper()
	installEndpoint(t, f.srv.URL)
	m := New("es:test", "test", theme.DefaultPalette())
	m.SetSize(100, 24)
	m.SetFocused(true)
	pump(t, &m, m.Init())
	return &m
}

func TestOpenListsIndicesAndLoadsFirst(t *testing.T) {
	f := newFakeCluster(t, 250)
	m := newPane(t, f)
	if m.Opening() || m.Err() != nil {
		t.Fatalf("opening=%v err=%v after the open landed", m.Opening(), m.Err())
	}
	if m.Indices() != 3 {
		t.Fatalf("indices = %d, want 2 indices + 1 alias", m.Indices())
	}
	if m.SelectedIndex() != "empty" && m.SelectedIndex() != "logs" {
		t.Fatalf("selected = %q, want the first listed index", m.SelectedIndex())
	}
	// Indices sort by name: empty, logs, then the alias.
	if m.SelectedIndex() != "empty" {
		t.Fatalf("selected = %q, want empty (sorted first)", m.SelectedIndex())
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, "logs") || !strings.Contains(view, "250") {
		t.Errorf("sidebar should list logs with its doc count:\n%s", view)
	}
	if !strings.Contains(view, "all-logs") || !strings.Contains(view, "? a") {
		t.Errorf("sidebar should list the alias with ? count:\n%s", view)
	}
}

func TestSelectIndexLoadsHitsIntoGrid(t *testing.T) {
	f := newFakeCluster(t, 250)
	m := newPane(t, f)
	feed(t, m, key("j")) // empty -> logs
	feed(t, m, key("enter"))
	if !m.InGrid() || m.SelectedIndex() != "logs" {
		t.Fatalf("grid=%v selected=%q, want grid on logs", m.InGrid(), m.SelectedIndex())
	}
	if m.PageRows() != PageSize {
		t.Fatalf("rows = %d, want a full page of %d", m.PageRows(), PageSize)
	}
	view := stripANSI(m.View())
	for _, want := range []string{"_id", "_score", "level", "msg", "meta", "doc-0", `{"seq":0}`, "hits 1–100 of 250"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
}

func TestPagingPastFirstPage(t *testing.T) {
	f := newFakeCluster(t, 250)
	m := newPane(t, f)
	feed(t, m, key("j"))
	feed(t, m, key("enter"))
	feed(t, m, key("n"))
	if m.PageFrom() != 100 {
		t.Fatalf("from = %d after n, want 100", m.PageFrom())
	}
	feed(t, m, key("n"))
	if m.PageFrom() != 200 || m.PageRows() != 50 {
		t.Fatalf("from/rows = %d/%d, want 200/50 (the last partial page)", m.PageFrom(), m.PageRows())
	}
	feed(t, m, key("n")) // past the end: inert
	if m.PageFrom() != 200 {
		t.Fatalf("from = %d, n past the last page must stay", m.PageFrom())
	}
	feed(t, m, key("p"))
	if m.PageFrom() != 100 {
		t.Fatalf("from = %d after p, want 100", m.PageFrom())
	}
	feed(t, m, key("g"))
	if m.PageFrom() != 0 {
		t.Fatalf("from = %d after g, want 0", m.PageFrom())
	}
	feed(t, m, key("G"))
	if m.PageFrom() != 200 {
		t.Fatalf("from = %d after G, want the last page at 200", m.PageFrom())
	}
}

func TestRowStepCrossesPageEdges(t *testing.T) {
	f := newFakeCluster(t, 250)
	m := newPane(t, f)
	feed(t, m, key("j"))
	feed(t, m, key("enter"))
	feed(t, m, key("G")) // page at 200
	if got := stripANSI(m.View()); !strings.Contains(got, "hits 201–250") {
		t.Fatalf("view after G:\n%s", got)
	}
	feed(t, m, key("g"))
	feed(t, m, key("k")) // k at row 0 of page 0: inert
	if m.PageFrom() != 0 {
		t.Fatalf("from = %d, k at the very top must stay", m.PageFrom())
	}
	// Walk to the bottom edge, then one more j crosses to the next page.
	for i := 0; i < PageSize-1; i++ {
		feed(t, m, key("j"))
	}
	feed(t, m, key("j"))
	if m.PageFrom() != 100 {
		t.Fatalf("from = %d, j past the page edge must fetch the next page", m.PageFrom())
	}
	// And k off the top edge goes back, cursor on the previous page's bottom.
	feed(t, m, key("k"))
	if m.PageFrom() != 0 {
		t.Fatalf("from = %d, k off the top edge must fetch the previous page", m.PageFrom())
	}
	if got := m.rowCur; got != PageSize-1 {
		t.Fatalf("cursor = %d after paging back, want the bottom row %d", got, PageSize-1)
	}
}

func TestDeadEndpointDegradesToNoticeAndRetries(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead.Close()
	installEndpoint(t, dead.URL)
	m := New("es:test", "test", theme.DefaultPalette())
	m.SetSize(100, 24)
	pump(t, &m, m.Init())
	if m.Err() == nil {
		t.Fatal("a dead endpoint must land as an error, not hang")
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, "cannot reach cluster") || !strings.Contains(view, "r retry") {
		t.Fatalf("error view should explain and offer retry:\n%s", view)
	}
	// Fix the config (the cluster came back under a new address) and retry.
	f := newFakeCluster(t, 10)
	installEndpoint(t, f.srv.URL)
	feed(t, &m, key("r"))
	if m.Err() != nil || m.Indices() == 0 {
		t.Fatalf("err=%v indices=%d after retry, want a recovered pane", m.Err(), m.Indices())
	}
}

func TestUnconfiguredEndpointFailsFast(t *testing.T) {
	installEndpoint(t, "http://127.0.0.1:1")
	m := New("es:gone", "gone", theme.DefaultPalette())
	m.SetSize(100, 24)
	pump(t, &m, m.Init())
	if m.Err() == nil || !strings.Contains(m.Err().Error(), "not configured") {
		t.Fatalf("err = %v, want the not-configured notice", m.Err())
	}
}

func TestRunQueryUsesBodyAndMarksHeader(t *testing.T) {
	f := newFakeCluster(t, 250)
	m := newPane(t, f)
	cmd, ok := m.RunQuery("logs", `{"query":{"term":{"level":"info"}}}`)
	if !ok {
		t.Fatal("RunQuery: logs not found in the sidebar")
	}
	pump(t, m, cmd)
	if q, ok := f.lastBody["query"].(map[string]any); !ok || q["term"] == nil {
		t.Fatalf("search body = %v, want the buffer's term query", f.lastBody)
	}
	if f.lastBody["track_total_hits"] != true {
		t.Errorf("track_total_hits missing from %v", f.lastBody)
	}
	if got := stripANSI(m.View()); !strings.Contains(got, "hits 1–100 of 125") {
		t.Errorf("the filtered total should show:\n%s", got)
	}
	if got := stripANSI(m.View()); !strings.Contains(got, "query: logs") {
		t.Errorf("the header should mark the active query:\n%s", got)
	}
	if _, ok := m.RunQuery("no-such-index", "{}"); ok {
		t.Error("RunQuery on an unknown index must report not-found")
	}
}

func TestQueryErrorKeepsLastGoodPage(t *testing.T) {
	f := newFakeCluster(t, 250)
	m := newPane(t, f)
	feed(t, m, key("j"))
	feed(t, m, key("enter"))
	cmd, _ := m.RunQuery("logs", `{"query":{"brokenn":{}}}`)
	pump(t, m, cmd)
	view := stripANSI(m.View())
	if !strings.Contains(view, "unknown query [brokenn]") {
		t.Fatalf("the cluster's reason should show:\n%s", view)
	}
	// The error cleared res deliberately on RunQuery (a query switch), but a
	// paging error keeps the last good page: run a good query, then break
	// paging by killing the server.
	cmd, _ = m.RunQuery("logs", "")
	pump(t, m, cmd)
	rows := m.PageRows()
	f.srv.Close()
	feed(t, m, key("n"))
	if m.PageRows() != rows {
		t.Fatalf("rows = %d, want the last good page kept after a fetch error", m.PageRows())
	}
	if got := stripANSI(m.View()); !strings.Contains(got, "_search") {
		t.Errorf("the fetch error should render:\n%s", got)
	}
}

func TestShowHitMappingAggsAndQueryMessages(t *testing.T) {
	f := newFakeCluster(t, 10)
	m := newPane(t, f)
	feed(t, m, key("j"))
	feed(t, m, key("enter"))

	msgs := feed(t, m, key("v"))
	if len(msgs) != 1 {
		t.Fatalf("v emitted %v, want one ShowJSONMsg", msgs)
	}
	doc, ok := msgs[0].(ShowJSONMsg)
	if !ok || !strings.Contains(doc.JSON, `"doc-0"`) || !strings.Contains(doc.JSON, "\n") {
		t.Fatalf("v emitted %+v, want the pretty hit document", msgs[0])
	}
	if doc.Endpoint != "test" || !strings.Contains(doc.Name, "hit-1") {
		t.Errorf("hit msg naming = %+v", doc)
	}

	msgs = feed(t, m, key("s"))
	if len(msgs) != 1 {
		t.Fatalf("s emitted %v, want one ShowJSONMsg", msgs)
	}
	mp := msgs[0].(ShowJSONMsg)
	if !strings.Contains(mp.Name, "mapping") || !strings.Contains(mp.JSON, "keyword") {
		t.Fatalf("s emitted %+v, want the pretty mapping", mp)
	}

	msgs = feed(t, m, key("q"))
	if len(msgs) != 1 {
		t.Fatalf("q emitted %v, want one OpenQueryMsg", msgs)
	}
	oq := msgs[0].(OpenQueryMsg)
	if oq.Endpoint != "test" || oq.Index != "logs" {
		t.Fatalf("q emitted %+v, want endpoint test index logs", oq)
	}

	// a is inert without aggregations in the result.
	if msgs = feed(t, m, key("a")); len(msgs) != 0 {
		t.Fatalf("a emitted %v without aggs in the result", msgs)
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
		case r == 0x1b:
			inEsc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
