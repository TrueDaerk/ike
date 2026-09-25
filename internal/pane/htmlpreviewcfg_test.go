package pane

import (
	"testing"
	"time"

	"ike/internal/host"
)

// TestHTMLPreviewFollowsImagesConfig (#2743): preview.html_images reaches a
// freshly opened HTML preview (on by default, off when persisted off), the
// tab-restore constructor, and — on a reload — the panes already open.
func TestHTMLPreviewFollowsImagesConfig(t *testing.T) {
	r := NewRegistry(host.MapConfig{}, nil)
	on := r.Get(r.AddHTMLPreview("/tmp/a.html")).HTMLPreview()
	if !on.ImagesEnabled() {
		t.Fatal("images default to on without the key")
	}
	if !r.HTMLPreviewsMinted() {
		t.Fatal("opening an HTML preview mints one")
	}
	r2 := NewRegistry(host.MapConfig{"preview.html_images": "false"}, nil)
	if r2.HTMLPreviewsMinted() {
		t.Fatal("a fresh registry has minted no HTML preview")
	}
	if r2.Get(r2.AddHTMLPreview("/tmp/a.html")).HTMLPreview().ImagesEnabled() {
		t.Fatal("the persisted off must reach a new pane")
	}
	if r2.AddHTMLPreviewKey("htmlpreview:5", "/tmp/c.html").HTMLPreview().ImagesEnabled() {
		t.Fatal("the layout restore must apply the setting too")
	}
	if inst := r2.NewContentPane(KindHTMLPreview, "/tmp/b.html", "", "", ""); inst.HTMLPreview().ImagesEnabled() {
		t.Fatal("the tab restore constructor must apply the setting too")
	}
	r.Reconfigure(host.MapConfig{"preview.html_images": "false"})
	if on.ImagesEnabled() {
		t.Fatal("a reload must reach an open pane")
	}
}

// TestHTMLPreviewFollowsRenderBudget (#2745): preview.html_render_budget_kb
// reaches new and restored HTML previews and, on a reload, the open ones; a
// missing or malformed value keeps the 2048 KB default.
func TestHTMLPreviewFollowsRenderBudget(t *testing.T) {
	r := NewRegistry(host.MapConfig{"preview.html_render_budget_kb": "nope"}, nil)
	pv := r.Get(r.AddHTMLPreview("/tmp/a.html")).HTMLPreview()
	if pv.RenderBudgetKB() != 2048 {
		t.Fatalf("budget = %d, want the 2048 KB default", pv.RenderBudgetKB())
	}
	r2 := NewRegistry(host.MapConfig{"preview.html_render_budget_kb": "256"}, nil)
	if got := r2.Get(r2.AddHTMLPreview("/tmp/a.html")).HTMLPreview().RenderBudgetKB(); got != 256 {
		t.Fatalf("new pane budget = %d, want 256", got)
	}
	if got := r2.NewContentPane(KindHTMLPreview, "/tmp/b.html", "", "", "").HTMLPreview().RenderBudgetKB(); got != 256 {
		t.Fatalf("tab restore budget = %d, want 256", got)
	}
	r.Reconfigure(host.MapConfig{"preview.html_render_budget_kb": "512"})
	if pv.RenderBudgetKB() != 512 {
		t.Fatalf("a reload must reach an open pane, budget = %d", pv.RenderBudgetKB())
	}
}

// TestClosingHTMLPreviewCancelsRender (#2745): closing the pane cancels the
// render it has in flight.
func TestClosingHTMLPreviewCancelsRender(t *testing.T) {
	r := NewRegistry(host.MapConfig{}, nil)
	key := r.AddHTMLPreview("/tmp/a.html")
	inst := r.Get(key)
	inst.SetSize(40, 10)
	inst.HTMLPreview().SetSourceImmediate("<p>body</p>")
	cmd := inst.HTMLPreview().RenderCmd()
	if cmd == nil {
		t.Fatal("setup: the render must dispatch")
	}
	r.Close(key)
	if msg := cmd(); msg != nil {
		t.Fatalf("a closed pane's render must be cancelled, got %T", msg)
	}
}

// TestHTMLPreviewFollowsBrowserConfig (#2746): preview.html_browser and
// preview.html_browser_timeout_s reach new and restored HTML previews and,
// on a reload, the open ones; a malformed timeout keeps the 20 s default.
func TestHTMLPreviewFollowsBrowserConfig(t *testing.T) {
	r := NewRegistry(host.MapConfig{"preview.html_browser_timeout_s": "soon"}, nil)
	pv := r.Get(r.AddHTMLPreview("/tmp/a.html")).HTMLPreview()
	if bin, timeout := pv.BrowserSetting(); bin != "" || timeout != 20*time.Second {
		t.Fatalf("browser = %q, %s; want auto-detect, 20s", bin, timeout)
	}
	cfg := host.MapConfig{"preview.html_browser": "/opt/chrome", "preview.html_browser_timeout_s": "45"}
	r2 := NewRegistry(cfg, nil)
	for name, inst := range map[string]*Instance{
		"new":         r2.Get(r2.AddHTMLPreview("/tmp/a.html")),
		"restore":     r2.AddHTMLPreviewKey("htmlpreview:5", "/tmp/c.html"),
		"tab restore": r2.NewContentPane(KindHTMLPreview, "/tmp/b.html", "", "", ""),
	} {
		if bin, timeout := inst.HTMLPreview().BrowserSetting(); bin != "/opt/chrome" || timeout != 45*time.Second {
			t.Errorf("%s pane: browser = %q, %s", name, bin, timeout)
		}
	}
	r.Reconfigure(cfg)
	if bin, timeout := pv.BrowserSetting(); bin != "/opt/chrome" || timeout != 45*time.Second {
		t.Fatalf("a reload must reach an open pane: %q, %s", bin, timeout)
	}
}
