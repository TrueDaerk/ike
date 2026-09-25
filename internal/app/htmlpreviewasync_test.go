package app

// htmlpreviewasync_test.go covers the root-model half of the HTML preview's
// off-loop render (0530/7, #2745): the settled pass dispatches owed renders
// as Cmds instead of rendering on the loop, the RenderedMsg route drops a
// stale generation and books the render in the performance HUD, closing the
// source buffer cancels the render in flight, and a multi-MB page is cut at
// the budget.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/htmlpreview"
	"ike/internal/perfhud"
)

// TestHTMLPreviewRendersOffLoop: opening the preview leaves the render owed
// to a Cmd — the Update pass itself lays nothing out — and the settled pass
// dispatches it.
func TestHTMLPreviewRendersOffLoop(t *testing.T) {
	m, path := openHTMLFile(t, "<h1>Deferred</h1>\n")
	out, cmd := m.Update(HTMLPreviewMsg{})
	m = out.(Model)
	pv := m.activeWS().Panes.Get(htmlPreviewKeyFor(m, path)).HTMLPreview()
	if len(pv.Lines()) != 0 || !pv.Pending() {
		t.Fatal("the open pass must not render on the loop")
	}
	if cmd == nil {
		t.Fatal("the settled pass must dispatch the owed render")
	}
	if v := ansi.Strip(pv.View()); !strings.Contains(v, "rendering…") {
		t.Fatalf("a pending preview shows the rendering notice:\n%s", v)
	}
	if again := m.htmlPreviewRenderCmd(); again != nil {
		t.Fatal("a dispatched render must not be dispatched again by the next pass")
	}
}

// TestHTMLPreviewStaleRenderDroppedAndAccounted: a RenderedMsg of an older
// generation routes to the pane and is dropped there; the HUD books every
// delivered render against the pane's own "render" row.
func TestHTMLPreviewStaleRenderDroppedAndAccounted(t *testing.T) {
	hudOff(t)
	m, path := openHTMLFile(t, "<p>current</p>\n")
	m = stepHTML(m, HTMLPreviewMsg{})
	key := htmlPreviewKeyFor(m, path)
	out, _ := m.Update(TogglePerfHUDMsg{})
	m = out.(Model)

	m = step(m, htmlpreview.RenderedMsg{Key: key, Gen: -1, Took: 7 * time.Millisecond})
	if v := ansi.Strip(m.activeWS().Panes.Get(key).View()); !strings.Contains(v, "current") {
		t.Fatalf("a stale render must not replace the page:\n%s", v)
	}
	s := perfhud.Collect(m.armedTimers())
	found := false
	for _, p := range s.Panes {
		if p.Key == key+" render" {
			found = p.Total == 7*time.Millisecond
		}
	}
	if !found {
		t.Fatalf("the HUD must book the render under %q, got %+v", key+" render", s.Panes)
	}
}

// TestHTMLPreviewSourceCloseCancelsRender: closing the last view of the
// previewed buffer cancels the preview's render in flight and keeps the
// page it shows.
func TestHTMLPreviewSourceCloseCancelsRender(t *testing.T) {
	m, path := openHTMLFile(t, "<p>shown page</p>\n")
	m = stepHTML(m, HTMLPreviewMsg{})
	key := htmlPreviewKeyFor(m, path)
	pv := m.activeWS().Panes.Get(key).HTMLPreview()
	pv.SetSourceImmediate(paragraphs(3000))
	inFlight := pv.RenderCmd()
	if inFlight == nil {
		t.Fatal("setup: the render must be in flight")
	}
	m = step(m, CloseTabMsg{})
	if m.editorForPath(path) != nil {
		t.Fatal("setup: the source buffer must be closed")
	}
	if msg := inFlight(); msg != nil {
		t.Fatalf("the source close must cancel the render, got %T", msg)
	}
	// The closed editor pane hands its room to the preview, whose resize
	// owes a fresh render at the new width — a new generation, dispatched
	// by the settled pass; the cancelled one stays dead.
	pv = m.activeWS().Panes.Get(key).HTMLPreview()
	if v := ansi.Strip(pv.View()); !strings.Contains(v, "shown page") {
		t.Fatalf("the preview keeps the page it showed:\n%s", v)
	}
}

// TestHTMLPreviewLargePageTruncated: a 5 MB page opens its preview without
// rendering on the loop, and the off-loop render stops at the 2048 KB
// default budget with the truncation line.
func TestHTMLPreviewLargePageTruncated(t *testing.T) {
	var b strings.Builder
	b.WriteString("<html><body>\n")
	for i := 0; b.Len() < 5<<20; i++ {
		fmt.Fprintf(&b, "<p>row %06d of a generated report with some filler text</p>\n", i)
	}
	b.WriteString("</body></html>\n")
	m := newSized()
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	path := filepath.Join(t.TempDir(), "report.html")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(path, false)
	m = tm.(Model)

	start := time.Now()
	out, _ := m.Update(HTMLPreviewMsg{})
	took := time.Since(start)
	m = out.(Model)
	pv := m.activeWS().Panes.Get(htmlPreviewKeyFor(m, path)).HTMLPreview()
	if !pv.Pending() || len(pv.Lines()) != 0 {
		t.Fatal("the 5 MB page must not render on the loop")
	}
	t.Logf("open pass on a 5 MB page: %v", took)

	pv.Flush()
	if !pv.Truncated() {
		t.Fatal("a 5 MB page must hit the default budget")
	}
	lines := pv.Lines()
	if last := ansi.Strip(lines[len(lines)-1]); last != "… truncated after 2048 KB" {
		t.Fatalf("last line = %q, want the truncation notice", last)
	}
	m.activeWS().Panes.SetFocused(htmlPreviewKeyFor(m, path))
	if v := ansi.Strip(m.render()); !strings.Contains(v, "truncated at 2048 KB") {
		t.Fatal("the focused preview's status line must say the page was cut")
	}
}
