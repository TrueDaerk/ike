package pane

// tabview.go is the editor tab's swappable body (#2766): an HTML document tab
// shows either its source — the editor — or the rendered page, the way a
// JetBrains editor switches between its text and preview views. It is one tab
// with one buffer: the preview is a nested KindHTMLPreview instance bound to
// the tab's path, living beside the editor in the same Tab slot, and a
// one-row Source/Preview button strip (ui.Segmented) at the bottom of the
// pane body flips between the two.
//
// While the preview shows, the tab behaves like a content tab of that kind
// (#1778) everywhere the app asks about the pane's body — ActiveContent,
// ContextID, Searchable, key routing — while the editor stays the tab's
// document: Editor(), TabPath, the dirty sweeps, the save and the layout
// persistence keep seeing it. Hiding the preview interrupts its background
// work (the park semantics of a workspace switch), so a tab in Source view
// costs nothing but the model.
//
// The pane cannot mint content keys, so the app creates the preview through
// the registry and attaches it (AttachTabView); a tab asked into Preview view
// before it has one — a deferred restored tab, a fresh open — keeps showing
// the editor until the app's settled pass attaches it.

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/htmlpreview"
	"ike/internal/ui"
)

// ViewMode is an editor tab's body view (#2766).
type ViewMode int

const (
	// ViewSource shows the editor — every tab's only view, and an HTML
	// tab's default until it is asked into Preview.
	ViewSource ViewMode = iota
	// ViewPreview shows the rendered page of an HTML tab.
	ViewPreview
)

// ViewStripMinHeight is the smallest pane body that draws the view strip: a
// shorter one keeps every row for the text and switches views by command only.
const ViewStripMinHeight = 6

// StripAction is what a click on the view strip asks for.
type StripAction int

const (
	// StripNone: the click hit no button.
	StripNone StripAction = iota
	// StripSource / StripPreview switch the tab's view.
	StripSource
	StripPreview
	// StripBrowser toggles the preview's browser screenshot mode (#2746) —
	// the button only shows in Preview view.
	StripBrowser
)

// switchable reports whether t is a document tab with a Preview view: an
// editor tab — loaded or deferred — whose file is an HTML page.
func (t *Tab) switchable() bool {
	if t == nil || t.ed == nil {
		return false
	}
	path := ""
	switch {
	case t.deferred != nil:
		path = t.deferred.Path
	case t.ed.HasFile():
		path = t.ed.Path()
	}
	return path != "" && htmlpreview.IsHTMLPath(path)
}

// shownView returns the preview instance while the tab shows it, else nil.
func (t *Tab) shownView() *Instance {
	if t.mode == ViewPreview && t.alt != nil {
		return t.alt
	}
	return nil
}

// stripRowsFor is how many rows the view strip takes out of a body of height
// h: one for a switchable tab tall enough, none otherwise.
func (t *Tab) stripRowsFor(h int) int {
	if h >= ViewStripMinHeight && t.switchable() {
		return 1
	}
	return 0
}

// strip builds the tab's button strip from its current state.
func (t *Tab) strip() ui.Segmented {
	s := ui.Segmented{Segments: []ui.Segment{
		{Label: "Source", On: t.shownView() == nil},
		{Label: "Preview", On: t.shownView() != nil},
	}}
	if v := t.shownView(); v != nil {
		s.Segments = append(s.Segments, ui.Segment{Label: "Browser", On: v.hpv.BrowserMode()})
	}
	return s
}

// fitRows pads (or cuts) a rendered body to exactly rows lines, so the view
// strip below it sits on the pane's bottom row however short the text is.
func fitRows(body string, rows int) string {
	n := strings.Count(body, "\n") + 1
	switch {
	case n < rows:
		return body + strings.Repeat("\n", rows-n)
	case n > rows:
		lines := strings.SplitN(body, "\n", rows+1)
		return strings.Join(lines[:rows], "\n")
	}
	return body
}

// syncStrip re-sizes tab t when the strip it was sized for no longer matches
// what it should draw — the file loaded into it or was renamed after the
// last SetSize, which is when a tab turns out to be an HTML page.
func (i *Instance) syncStrip(t *Tab) {
	if t == nil || i.w <= 0 || i.h <= 0 {
		return
	}
	if t.stripRows != t.stripRowsFor(i.h) {
		t.setSize(i.w, i.h)
	}
}

// TabSwitchable reports whether tab idx has a Preview view (#2766): a
// document tab — loaded or still deferred — showing an HTML page.
func (i *Instance) TabSwitchable(idx int) bool {
	return i.kind == KindEditor && i.Tab(idx).switchable()
}

// TabViewMode returns tab idx's view; ViewSource for every tab without a
// Preview view and an out-of-range index.
func (i *Instance) TabViewMode(idx int) ViewMode {
	if t := i.Tab(idx); t != nil {
		return t.mode
	}
	return ViewSource
}

// TabView returns tab idx's preview instance whether or not it shows, nil
// when none is attached yet.
func (i *Instance) TabView(idx int) *Instance {
	if t := i.Tab(idx); t != nil {
		return t.alt
	}
	return nil
}

// ShownTabView returns tab idx's preview instance while the tab shows it —
// the body the app's content walks treat like a content tab — else nil.
func (i *Instance) ShownTabView(idx int) *Instance {
	if t := i.Tab(idx); t != nil {
		return t.shownView()
	}
	return nil
}

// SetTabViewMode switches tab idx between Source and Preview (#2766) and
// reports whether the view changed. Only a switchable tab takes Preview;
// Source is always accepted. Leaving Preview interrupts the preview's
// background work — a render or screenshot in flight is owed again, its
// Kitty images re-sent — so a hidden preview costs nothing, and the focus
// moves onto whichever body now shows.
func (i *Instance) SetTabViewMode(idx int, mode ViewMode) bool {
	t := i.Tab(idx)
	if t == nil || t.ed == nil || t.mode == mode {
		return false
	}
	if mode == ViewPreview && !t.switchable() {
		return false
	}
	if t.alt != nil && mode == ViewSource {
		t.alt.hpv.Interrupt()
		t.alt.hpv.ResetImages()
	}
	t.mode = mode
	t.setFocused(i.focused && idx == i.active)
	i.cvValid = false
	return true
}

// AttachTabView gives tab idx its preview instance (#2766): a KindHTMLPreview
// the app minted through the registry for the tab's path. The preview takes
// the pane's palette, config and body size. It refuses a non-switchable tab,
// a tab that already has one, and anything but an HTML preview.
func (i *Instance) AttachTabView(idx int, view *Instance) bool {
	t := i.Tab(idx)
	if t == nil || view == nil || view.kind != KindHTMLPreview || t.alt != nil || !t.switchable() {
		return false
	}
	view.tabView = true
	view.setPalette(i.pal)
	view.configure(i.cfg)
	t.alt = view
	if i.w > 0 && i.h > 0 {
		t.setSize(i.w, i.h)
	}
	t.setFocused(i.focused && idx == i.active)
	i.cvValid = false
	return true
}

// IsTabView reports whether the instance is an editor tab's preview view
// (#2766) rather than a pane or content tab of its own.
func (i *Instance) IsTabView() bool { return i.tabView }

// ViewStripAt resolves a click at content-local (x, y) on the active tab's
// view strip: the action of the button under it, and ok=false when the pane
// draws no strip or the click is not on its row. A click on the row but off
// every button reports (StripNone, true), so the caller swallows it rather
// than handing it to the text above.
func (i *Instance) ViewStripAt(x, y int) (StripAction, bool) {
	if i.kind != KindEditor {
		return StripNone, false
	}
	t := i.activeTab()
	if t == nil || t.stripRowsFor(i.h) == 0 || y != i.h-1 {
		return StripNone, false
	}
	switch t.strip().At(x, i.w) {
	case 0:
		return StripSource, true
	case 1:
		return StripPreview, true
	case 2:
		return StripBrowser, true
	}
	return StripNone, true
}

// updateView routes a message for a tab showing its preview: input goes to
// the preview, everything else — the editor's own async results — to the
// editor, which stays the tab's document while hidden.
func (t *Tab) updateView(v *Instance, msg tea.Msg) tea.Cmd {
	switch msg.(type) {
	case tea.KeyPressMsg, tea.PasteMsg, htmlpreview.RenderTickMsg, htmlpreview.RenderedMsg, htmlpreview.ShotMsg:
		return v.Update(msg)
	}
	var cmd tea.Cmd
	*t.ed, cmd = t.ed.Update(msg)
	return cmd
}
