package pane

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// tabview_test.go covers the editor tab's swappable body (#2766): an HTML
// document tab draws the [Source] [Preview] strip in its bottom row, flips
// between the editor and an attached preview, and keeps the editor as its
// document either way.

// htmlEditorPane returns an editor pane of r showing path, sized w×h.
func htmlEditorPane(t *testing.T, r *Registry, path string, w, h int) *Instance {
	t.Helper()
	inst := r.Get(r.AddEditor())
	if err := inst.Editor().Load(path); err != nil {
		t.Fatal(err)
	}
	inst.SetSize(w, h)
	return inst
}

// attachPreview attaches a registry-minted preview to tab idx and fills it.
func attachPreview(t *testing.T, r *Registry, inst *Instance, idx int) *Instance {
	t.Helper()
	v := r.NewContentPane(KindHTMLPreview, inst.TabPath(idx), "", "", "")
	if !inst.AttachTabView(idx, v) {
		t.Fatal("an HTML tab must take a preview view")
	}
	v.HTMLPreview().SetSourceImmediate(inst.TabEditor(idx).Text())
	v.HTMLPreview().Flush()
	return v
}

func TestViewStripTakesTheBottomRow(t *testing.T) {
	r := NewRegistry(nil, nil)
	inst := htmlEditorPane(t, r, tmpFile(t, "page.html", "<p>hello</p>\n"), 60, 12)
	lines := strings.Split(ansi.Strip(inst.View()), "\n")
	if len(lines) != 12 {
		t.Fatalf("view has %d rows, want the pane's 12", len(lines))
	}
	if !strings.HasPrefix(lines[11], "[Source] [Preview]") || strings.Contains(lines[11], "Browser") {
		t.Fatalf("bottom row = %q, want the Source/Preview strip", lines[11])
	}
	// A plain file and a pane too short for the strip keep every row.
	plain := htmlEditorPane(t, r, tmpFile(t, "notes.txt", "x\n"), 60, 12)
	if strings.Contains(ansi.Strip(plain.View()), "[Source]") {
		t.Fatal("a non-HTML tab draws no strip")
	}
	short := htmlEditorPane(t, r, tmpFile(t, "short.html", "<p>x</p>\n"), 60, ViewStripMinHeight-1)
	if strings.Contains(ansi.Strip(short.View()), "[Source]") {
		t.Fatal("a pane below the minimum height draws no strip")
	}
	if _, ok := short.ViewStripAt(1, ViewStripMinHeight-2); ok {
		t.Fatal("no strip, no strip hit")
	}
}

func TestViewStripAppearsWhenTheFileLoadsLater(t *testing.T) {
	r := NewRegistry(nil, nil)
	inst := r.Get(r.AddEditor())
	inst.SetSize(60, 12)
	// The file lands in a tab that was sized as a scratch buffer.
	if err := inst.Editor().Load(tmpFile(t, "late.html", "<p>x</p>\n")); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(ansi.Strip(inst.View()), "\n")
	if len(lines) != 12 || !strings.HasPrefix(lines[11], "[Source]") {
		t.Fatalf("the strip must claim the bottom row once the tab shows HTML:\n%s", strings.Join(lines, "\n"))
	}
}

func TestTabViewSwitch(t *testing.T) {
	r := NewRegistry(nil, nil)
	inst := htmlEditorPane(t, r, tmpFile(t, "page.html", "<h2>Title</h2>\n"), 60, 12)
	inst.SetFocused(true)
	ed := inst.Editor()
	if inst.SetTabViewMode(0, ViewPreview) != true {
		t.Fatal("an HTML tab takes Preview view")
	}
	// Until the app attaches the preview the editor keeps showing.
	if inst.ActiveContent() != nil || inst.ContextID() != ctxEditor {
		t.Fatal("no preview attached yet: the editor is the body")
	}
	v := attachPreview(t, r, inst, 0)
	if inst.ActiveContent() != v || inst.ContextID() != ctxPreview || inst.Editor() != ed {
		t.Fatal("Preview view: the preview is the body, the editor stays the document")
	}
	if !v.IsTabView() {
		t.Fatal("the attached preview must know it is a tab view")
	}
	out := ansi.Strip(inst.View())
	if !strings.Contains(out, "## Title") || !strings.Contains(out, "[Source] [Preview] [Browser]") {
		t.Fatalf("Preview view draws the page and the three buttons:\n%s", out)
	}
	if act, ok := inst.ViewStripAt(1, 11); !ok || act != StripSource {
		t.Fatalf("hit on [Source] = %v %v", act, ok)
	}
	if act, ok := inst.ViewStripAt(10, 11); !ok || act != StripPreview {
		t.Fatalf("hit on [Preview] = %v %v", act, ok)
	}
	if act, ok := inst.ViewStripAt(20, 11); !ok || act != StripBrowser {
		t.Fatalf("hit on [Browser] = %v %v", act, ok)
	}
	if act, ok := inst.ViewStripAt(50, 11); !ok || act != StripNone {
		t.Fatalf("a hit on the row off the buttons = %v %v, want swallowed", act, ok)
	}
	if _, ok := inst.ViewStripAt(1, 3); ok {
		t.Fatal("a click in the text is no strip hit")
	}
	// Keys reach the preview, never the hidden buffer.
	before := ed.Text()
	inst.Update(tea.KeyPressMsg{Text: "x", Code: 'x'})
	if ed.Text() != before {
		t.Fatal("a key in Preview view must not edit the buffer")
	}
	if !inst.SetTabViewMode(0, ViewSource) || inst.ActiveContent() != nil || inst.ContextID() != ctxEditor {
		t.Fatal("Source view: the editor is the body again")
	}
}

func TestTabViewRefusesNonHTML(t *testing.T) {
	r := NewRegistry(nil, nil)
	inst := htmlEditorPane(t, r, tmpFile(t, "notes.txt", "x\n"), 60, 12)
	if inst.TabSwitchable(0) || inst.SetTabViewMode(0, ViewPreview) {
		t.Fatal("a non-HTML tab has no Preview view")
	}
	v := r.NewContentPane(KindHTMLPreview, "x.html", "", "", "")
	if inst.AttachTabView(0, v) {
		t.Fatal("a non-HTML tab must refuse a preview")
	}
}

func TestTabViewClosesWithItsTab(t *testing.T) {
	r := NewRegistry(nil, nil)
	inst := htmlEditorPane(t, r, tmpFile(t, "page.html", "<p>x</p>\n"), 60, 12)
	inst.SetTabViewMode(0, ViewPreview)
	v := attachPreview(t, r, inst, 0)
	inst.AddTab()
	v.HTMLPreview().SetSourceImmediate("<p>y</p>\n")
	inst.CloseTab(0)
	if v.HTMLPreview().Pending() {
		t.Fatal("closing the tab must release its preview's owed render")
	}
}
