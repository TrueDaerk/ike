package app

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/pane"
	"ike/internal/vcs"
)

// mousegaps_test.go covers the surfaces the #2259 audit found unreachable by
// the mouse: the Elasticsearch console and the three-way merge view got no
// wheel and no click at all, so a user could scroll every neighbouring pane
// but not these. The remote browser's own gestures are covered in
// internal/remote; here the point is the app's routing.

// esCluster is a minimal fake ES backend: "logs" with n documents next to an
// empty "other", so a sidebar click has somewhere to move.
func esCluster(t *testing.T, n int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_cat/indices":
			fmt.Fprintf(w, `[{"index":"logs","docs.count":"%d"},{"index":"other","docs.count":"0"}]`, n)
		case r.URL.Path == "/_cat/aliases":
			io.WriteString(w, `[]`)
		case strings.HasSuffix(r.URL.Path, "/_mapping"):
			io.WriteString(w, `{"logs":{"mappings":{"properties":{"msg":{"type":"text"}}}}}`)
		case strings.HasSuffix(r.URL.Path, "/_search"):
			if strings.HasPrefix(r.URL.Path, "/other/") {
				io.WriteString(w, `{"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`)
				return
			}
			var hits []string
			for i := 0; i < n && i < 100; i++ {
				hits = append(hits, fmt.Sprintf(`{"_id":"doc-%d","_score":1.0,"_source":{"msg":"m%d"}}`, i, i))
			}
			fmt.Fprintf(w, `{"hits":{"total":{"value":%d,"relation":"eq"},"hits":[%s]}}`, n, strings.Join(hits, ","))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// esApp opens a console pane against the fake cluster and returns the model,
// the pane key and its content origin.
func esApp(t *testing.T, docs int) (Model, string, int, int) {
	t.Helper()
	srv := esCluster(t, docs)
	// The app's own construction loads and installs a config, so the endpoint
	// is registered afterwards — otherwise New() overwrites it.
	m := newSized()
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	cfg, _ := config.Load(config.Options{})
	cfg.Elasticsearch.Endpoints = []config.ESEndpoint{{Name: "test", URL: srv.URL}}
	config.Set(cfg)
	m = drainCmd(m, m.openESPane("test"))
	var key string
	for _, k := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(k); inst != nil && inst.Kind() == pane.KindES {
			key = k
		}
	}
	if key == "" {
		t.Fatal("no ES console pane opened")
	}
	r, ok := m.lay.Panes[key]
	if !ok {
		t.Fatalf("no layout rect for %q", key)
	}
	return m, key, r.X + paneContentX, r.Y + paneContentY
}

// stepDrain runs one message through the app and then its whole command tree,
// so an asynchronous pane (the console's page fetches) settles inside the
// step.
func stepDrain(m Model, msg tea.Msg) Model {
	out, cmd := m.Update(msg)
	m = out.(Model)
	if len(m.pendingWheel) > 0 {
		out, wcmd := m.Update(wheelFlushMsg{})
		m = out.(Model)
		m = drainCmd(m, wcmd)
	}
	return drainCmd(m, cmd)
}

// TestESPaneDoubleClickLoadsIndex: the paneClick switch had no ES branch, so
// no click reached the console at all. A single click on another index selects
// it, the second loads it — the same end state enter produces. The pane opens
// on the first index, so the second row is the one to move to.
func TestESPaneDoubleClickLoadsIndex(t *testing.T) {
	m, key, x, y := esApp(t, 300)
	es := m.activeWS().Panes.Get(key).ES()
	if es.Indices() != 2 {
		t.Fatalf("indices = %d, want 2: err=%v opening=%v", es.Indices(), es.Err(), es.Opening())
	}
	if es.SelectedIndex() != "logs" {
		t.Fatalf("the pane opened on %q, want logs", es.SelectedIndex())
	}
	m = stepDrain(m, tea.MouseClickMsg{X: x + 2, Y: y + 2, Button: tea.MouseLeft})
	if m.activeWS().Panes.Focused() != key {
		t.Fatalf("the click did not focus the pane: %q", m.activeWS().Panes.Focused())
	}
	if es.CursorIndex() != "other" {
		t.Fatalf("the click selected %q, want other", es.CursorIndex())
	}
	if es.SelectedIndex() != "logs" {
		t.Fatalf("a single click loaded %q — it must only select", es.SelectedIndex())
	}
	m = stepDrain(m, tea.MouseClickMsg{X: x + 2, Y: y + 2, Button: tea.MouseLeft})
	if es.SelectedIndex() != "other" {
		t.Fatalf("the double click loaded %q, want other", es.SelectedIndex())
	}
}

// TestESPaneWheelScrolls: the wheel over the console reaches the pane instead
// of falling through the kind switch, as it did before #2259 — the grid half
// scrolls its loaded page of hits and clamps back at the first one.
func TestESPaneWheelScrolls(t *testing.T) {
	m, key, x, y := esApp(t, 300)
	es := m.activeWS().Panes.Get(key).ES()
	if es.PageRows() == 0 {
		t.Fatalf("no hits loaded: err=%v opening=%v", es.Err(), es.Opening())
	}
	// Move the region focus into the grid half, then scroll it.
	gx := x + es.SidebarWidth() + 2
	m = stepDrain(m, tea.MouseClickMsg{X: gx, Y: y + 3, Button: tea.MouseLeft})
	if !es.InGrid() {
		t.Fatal("the grid click did not take the region focus")
	}
	m = stepDrain(m, tea.MouseWheelMsg{X: gx, Y: y + 3, Button: tea.MouseWheelDown})
	if es.GridTop() == 0 {
		t.Fatal("the wheel did not scroll the grid")
	}
	if c := es.GridCursor(); c < es.GridTop() {
		t.Fatalf("the cursor %d was left above the window at top %d", c, es.GridTop())
	}
	for i := 0; i < 60; i++ {
		m = stepDrain(m, tea.MouseWheelMsg{X: gx, Y: y + 3, Button: tea.MouseWheelUp})
	}
	if es.PageFrom() != 0 || es.GridTop() != 0 {
		t.Fatalf("scrolling up parked at page %d top %d, want the first hit", es.PageFrom(), es.GridTop())
	}
}

// mergeApp opens a merge view over a long conflicted fixture and returns the
// model, the pane key and its content origin.
func mergeApp(t *testing.T) (Model, string, int, int) {
	t.Helper()
	var b strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	body := b.String()
	m := newSized()
	m.openMergePane(vcs.MergeStagesMsg{Path: "conflicted.txt", Base: body, Ours: body, Theirs: body})
	var key string
	for _, k := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(k); inst != nil && inst.Kind() == pane.KindMerge {
			key = k
		}
	}
	if key == "" {
		t.Fatal("no merge pane opened")
	}
	r, ok := m.lay.Panes[key]
	if !ok {
		t.Fatalf("no layout rect for %q", key)
	}
	return m, key, r.X + paneContentX, r.Y + paneContentY
}

// TestMergeWheelScrollsAllColumns: the wheel over the merge view moves the
// result editor's viewport, which all three columns render off — before #2259
// the kind fell through the wheel switch and the view could only be scrolled
// with the keyboard.
func TestMergeWheelScrollsAllColumns(t *testing.T) {
	m, key, x, y := mergeApp(t)
	mg := m.activeWS().Panes.Get(key).Merge()
	if top := mg.Editor().ScrollTop(); top != 0 {
		t.Fatalf("the view starts scrolled to %d", top)
	}
	m = step(m, tea.MouseWheelMsg{X: x + 4, Y: y + 3, Button: tea.MouseWheelDown})
	if mg.Editor().ScrollTop() == 0 {
		t.Fatal("the wheel did not scroll the merge view")
	}
	for i := 0; i < 50; i++ {
		m = step(m, tea.MouseWheelMsg{X: x + 4, Y: y + 3, Button: tea.MouseWheelUp})
	}
	if got := mg.Editor().ScrollTop(); got != 0 {
		t.Fatalf("scrolling up clamps at %d, want 0", got)
	}
}
