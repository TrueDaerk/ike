package htmlpreview

// async_test.go covers the off-loop render (0530/7, #2745): the generation
// that drops a stale result, the cancellation on close and on a newer
// dispatch, the notice over the previous document while a render is in
// flight, and the render budget.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
)

// settle runs cmd — a render dispatch or nothing — and feeds its message
// back, the way the program would.
func settle(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if msg := cmd(); msg != nil {
		m.Update(msg)
	}
}

// TestRenderIsDeferredToACmd: a source change renders nothing on the loop;
// the dispatched Cmd does, and its result lands through Update.
func TestRenderIsDeferredToACmd(t *testing.T) {
	m := New("htmlpreview", "/tmp/page.html", theme.DefaultPalette())
	m.SetSize(40, 6)
	m.SetSourceImmediate("<p>async body</p>")
	if len(m.Lines()) != 0 || !m.Pending() {
		t.Fatal("SetSourceImmediate must only owe the render")
	}
	cmd := m.RenderCmd()
	if cmd == nil {
		t.Fatal("an owed render must dispatch a Cmd")
	}
	if m.RenderCmd() != nil {
		t.Fatal("a dispatched render must not be dispatched twice")
	}
	msg, ok := cmd().(RenderedMsg)
	if !ok || msg.Key != "htmlpreview" || msg.Gen != m.gen {
		t.Fatalf("the Cmd must return this pane's RenderedMsg, got %#v", msg)
	}
	if len(m.Lines()) != 0 {
		t.Fatal("the result must not reach the pane before Update")
	}
	m.Update(msg)
	if !strings.Contains(plain(m), "async body") || m.Pending() {
		t.Fatalf("the landed render must show and clear the pending state:\n%s", plain(m))
	}
}

// TestStaleGenerationDropped: a render superseded by a newer dispatch is
// dropped even when it finishes, and the newer one lands.
func TestStaleGenerationDropped(t *testing.T) {
	m := sized(t, "<p>original</p>")
	m.SetSourceImmediate("<p>older edit</p>")
	older := m.RenderCmd()
	m.SetSourceImmediate("<p>newer edit</p>")
	newer := m.RenderCmd()

	// The older render was cancelled by the newer dispatch; a result it
	// produced anyway (raced past the cancellation) carries its old
	// generation.
	if msg := older(); msg != nil {
		t.Fatalf("a cancelled render must not wake the loop, got %T", msg)
	}
	m.Update(RenderedMsg{Key: "htmlpreview", Gen: m.gen - 1})
	if v := plain(m); !strings.Contains(v, "original") {
		t.Fatalf("a stale generation must be dropped:\n%s", v)
	}
	m.Update(RenderedMsg{Key: "other", Gen: m.gen})
	if v := plain(m); !strings.Contains(v, "original") {
		t.Fatalf("another pane's result must be dropped:\n%s", v)
	}
	settle(&m, newer)
	if v := plain(m); !strings.Contains(v, "newer edit") || strings.Contains(v, "older") {
		t.Fatalf("the newest generation renders:\n%s", v)
	}
}

// TestGenerationsAreProcessUnique: a pane rebuilt under the same key (a
// project switch) never adopts the old pane's queued result.
func TestGenerationsAreProcessUnique(t *testing.T) {
	old := sized(t, "<p>x</p>")
	old.SetSourceImmediate("<p>old workspace</p>")
	oldCmd := old.RenderCmd()
	fresh := New("htmlpreview", "/tmp/page.html", theme.DefaultPalette())
	fresh.SetSize(60, 10)
	fresh.SetSourceImmediate("<p>new workspace</p>")
	freshCmd := fresh.RenderCmd()
	settle(&fresh, oldCmd)
	if strings.Contains(plain(fresh), "old workspace") {
		t.Fatal("a result of another pane under the same key was adopted")
	}
	settle(&fresh, freshCmd)
	if !strings.Contains(plain(fresh), "new workspace") {
		t.Fatalf("the pane's own result must land:\n%s", plain(fresh))
	}
}

// TestCloseCancelsRender: closing the pane cancels the render in flight — it
// returns no message — and a late result is not adopted.
func TestCloseCancelsRender(t *testing.T) {
	m := sized(t, "<p>shown</p>")
	m.SetSourceImmediate(longDoc(3000))
	cmd := m.RenderCmd()
	gen := m.gen
	m.Close()
	if m.Pending() {
		t.Fatal("a closed pane owes no render")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("a cancelled render must return no message, got %T", msg)
	}
	m.Update(RenderedMsg{Key: "htmlpreview", Gen: gen})
	if v := plain(m); !strings.Contains(v, "shown") {
		t.Fatalf("a result after close must not be adopted:\n%s", v)
	}
	if m.RenderCmd() != nil {
		t.Fatal("a closed pane dispatches nothing")
	}
}

// TestCancelRenderKeepsDocument: the source buffer closing cancels the
// render and keeps the document already shown.
func TestCancelRenderKeepsDocument(t *testing.T) {
	m := sized(t, "<p>kept</p>")
	m.SetSourceImmediate("<p>never shown</p>")
	cmd := m.RenderCmd()
	m.CancelRender()
	settle(&m, cmd)
	if v := plain(m); !strings.Contains(v, "kept") || strings.Contains(v, "never") {
		t.Fatalf("a cancelled render keeps the old document:\n%s", v)
	}
}

// TestInterruptReowesRender: a parked workspace cancels the render in flight
// but owes it again, so the resume renders the newest source.
func TestInterruptReowesRender(t *testing.T) {
	m := sized(t, "<p>before</p>")
	m.SetSourceImmediate("<p>after</p>")
	stale := m.RenderCmd()
	m.Interrupt()
	if stale() != nil {
		t.Fatal("the interrupted render must be cancelled")
	}
	if !m.Pending() {
		t.Fatal("the interrupted render must still be owed")
	}
	settle(&m, m.RenderCmd())
	if !strings.Contains(plain(m), "after") {
		t.Fatalf("the re-dispatched render must land:\n%s", plain(m))
	}
}

// TestPendingShowsPreviousContentWithNotice: while a render is in flight the
// pane keeps drawing the previous document, with the notice on its first row.
func TestPendingShowsPreviousContentWithNotice(t *testing.T) {
	m := sized(t, longDoc(40))
	m.SetSourceImmediate("<p>next</p>")
	cmd := m.RenderCmd()
	v := plain(m)
	rows := strings.Split(v, "\n")
	if !strings.Contains(rows[0], strings.TrimSpace(renderingNotice)) {
		t.Fatalf("the first row must carry the notice:\n%s", v)
	}
	if !strings.Contains(v, "para00") {
		t.Fatalf("the previous document must stay visible:\n%s", v)
	}
	for i, r := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(r); w > m.w {
			t.Fatalf("row %d is %d cells, wider than the pane (%d)", i, w, m.w)
		}
	}
	settle(&m, cmd)
	if strings.Contains(plain(m), strings.TrimSpace(renderingNotice)) {
		t.Fatal("the notice must go once the render lands")
	}
}

// TestRenderBudgetTruncates: the pane renders at most its budget and shows
// the truncation line; a bigger budget renders the whole page.
func TestRenderBudgetTruncates(t *testing.T) {
	m := New("htmlpreview", "/tmp/page.html", theme.DefaultPalette())
	m.SetSize(60, 10)
	m.SetRenderBudget(64)
	page := longDoc(8000) // ~140 KB
	m.SetSourceImmediate(page)
	m.Flush()
	if !m.Truncated() || m.RenderBudgetKB() != 64 {
		t.Fatalf("a %d-byte page must hit a 64 KB budget", len(page))
	}
	lines := m.Lines()
	if last := ansi.Strip(lines[len(lines)-1]); last != "… truncated after 64 KB" {
		t.Fatalf("last line = %q, want the truncation notice", last)
	}
	// Raising the budget re-renders the whole page.
	m.SetRenderBudget(1024)
	if !m.Pending() {
		t.Fatal("a budget change must owe a render")
	}
	m.Flush()
	if m.Truncated() {
		t.Fatal("a page within the budget renders whole")
	}
	m.SetRenderBudget(0)
	if m.Pending() || m.RenderBudgetKB() != 1024 {
		t.Fatal("a non-positive budget is ignored")
	}
}

// TestImageRenderOffLoopDoesNotRace: the image hook runs on the render
// goroutine against its own copy of the cache while the loop keeps
// reconciling the placements of the shown document (run under -race), and
// the landed render adopts the grids it decided.
func TestImageRenderOffLoopDoesNotRace(t *testing.T) {
	dir := t.TempDir()
	writeImage(t, dir, "a.png", 8, 8)
	writeImage(t, dir, "b.png", 8, 8)
	m := newImaged(dir, `<img src="a.png">`, true)
	m.SyncSeqs()
	m.SetSourceImmediate(`<p>x</p><img src="a.png"><img src="b.png">`)
	cmd := m.RenderCmd()
	done := make(chan tea.Msg)
	go func() { done <- cmd() }()
	for range 50 {
		m.SyncSeqs()
		_ = m.ImageIDs()
		_ = m.View()
	}
	m.Update(<-done)
	if ids := m.ImageIDs(); len(ids) != 2 {
		t.Fatalf("the landed render must place both images, got %v", ids)
	}
	if placeholders(m.Lines()) == 0 {
		t.Fatal("the landed render must draw the image blocks")
	}
}

// TestResizeWhilePendingRedispatches: a width change during a render owes a
// new one at the new width; the older result is dropped.
func TestResizeWhilePendingRedispatches(t *testing.T) {
	m := sized(t, "<p>a b c d e f g h i j k l m n o p q r s t u v w x y z</p>")
	m.SetSourceImmediate("<p>alpha beta gamma delta epsilon zeta eta theta</p>")
	first := m.RenderCmd()
	m.SetSize(12, 10)
	second := m.RenderCmd()
	if second == nil {
		t.Fatal("a resize during a render must dispatch a new one")
	}
	settle(&m, first)
	settle(&m, second)
	for _, l := range m.Lines() {
		if w := ansi.StringWidth(l); w > 12 {
			t.Fatalf("line %q is %d cells: the stale wide render landed", ansi.Strip(l), w)
		}
	}
}
