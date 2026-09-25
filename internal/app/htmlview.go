package app

// htmlview.go wires the HTML editor tab's two views (#2766) into the root
// model: an HTML file's tab shows either its source (the editor) or the
// rendered page (an HTML preview bound to the same buffer, internal/pane's
// tabview.go), switched by the [Source] [Preview] strip in the pane body's
// bottom-left corner, html.view.toggle and the two palette entries. Opening
// an HTML file lands in the view preview.html_open_mode names; a navigation
// that targets a source position always lands in Source.
//
// The preview is the html.preview pane's model (#2740) — live updates, cursor
// sync, links, images, browser mode, off-loop render — attached to the tab
// instead of split beside it, so html.preview keeps meaning "a second pane
// beside the source".

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/pane"
)

// htmlViewNeedsHTML is the toast of the view commands on anything but an HTML
// file's tab.
const htmlViewNeedsHTML = "Source/Preview needs an HTML file tab"

// htmlViewPreviewMode is the persisted view of a document tab in Preview
// view — paneIdentity.Views (#2766).
const htmlViewPreviewMode = "preview"

// htmlOpenInPreview reports whether preview.html_open_mode opens HTML files
// in Preview view (the default) rather than Source.
func htmlOpenInPreview() bool {
	c := config.Get()
	return c == nil || c.Preview.HTMLOpenMode != config.HTMLOpenSource
}

// htmlViewTarget resolves the tab the view commands act on: the focused
// pane's active tab when it is an editor pane, else the active editor pane's.
func (m Model) htmlViewTarget() (*pane.Instance, int) {
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindEditor {
		key = m.activeEditorKey()
		inst = m.activeWS().Panes.Get(key)
	}
	if inst == nil || inst.Kind() != pane.KindEditor {
		return nil, -1
	}
	return inst, inst.ActiveTab()
}

// setHTMLView switches the tab the view commands target to mode —
// html.view.source / html.view.preview — toasting on a tab without a Preview
// view.
func (m *Model) setHTMLView(mode pane.ViewMode) {
	inst, idx := m.htmlViewTarget()
	if inst == nil || !inst.TabSwitchable(idx) {
		m.host.Notify(host.Info, htmlViewNeedsHTML)
		return
	}
	m.setTabView(inst, idx, mode)
}

// toggleHTMLView is html.view.toggle: the target tab flips between Source
// and Preview.
func (m *Model) toggleHTMLView() {
	inst, idx := m.htmlViewTarget()
	mode := pane.ViewPreview
	if inst != nil && inst.TabViewMode(idx) == pane.ViewPreview {
		mode = pane.ViewSource
	}
	m.setHTMLView(mode)
}

// setTabView switches tab idx of the editor pane inst to mode and persists the
// layout, so the view round-trips a restart. Entering Preview attaches the
// tab's preview on first use and brings it up to date with the buffer — a
// preview hidden since the last edit re-renders, an unchanged one keeps its
// scroll. It reports whether the view changed.
func (m *Model) setTabView(inst *pane.Instance, idx int, mode pane.ViewMode) bool {
	if !inst.SetTabViewMode(idx, mode) {
		return false
	}
	if mode == pane.ViewPreview {
		m.ensureTabView(inst, idx)
	}
	if m.activeWS().Tree != nil {
		m.layout()
		saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	}
	return true
}

// ensureTabView gives a tab in Preview view its preview instance when it has
// none yet — minted through the registry like a content tab, bound to the
// tab's path — and resyncs it with the buffer: a preview hidden since the
// last edit re-renders, an unchanged one keeps its scroll.
func (m *Model) ensureTabView(inst *pane.Instance, idx int) {
	ed := inst.TabEditor(idx)
	path := inst.TabPath(idx)
	if inst.TabViewMode(idx) != pane.ViewPreview || ed == nil || path == "" {
		return
	}
	view := inst.TabView(idx)
	if view == nil {
		view = m.activeWS().Panes.NewContentPane(pane.KindHTMLPreview, path, "", "", "")
		if view == nil || !inst.AttachTabView(idx, view) {
			return
		}
	}
	line, _ := ed.CursorPos()
	view.HTMLPreview().Resync(ed.Text(), line)
}

// restoreTabView brings a restored document tab back in the view the layout
// saved (#2766): mode "preview" or "browser" (Preview view with the
// preview's browser screenshot mode, #2746). The tab may still be deferred
// (#2177), so the preview renders from the file on disk — the restored
// content-tab preview's path — and follows the buffer's edits once it loads.
func (m *Model) restoreTabView(reg *pane.Registry, inst *pane.Instance, idx int, mode string) {
	path := inst.TabPath(idx)
	if path == "" || !inst.SetTabViewMode(idx, pane.ViewPreview) {
		return
	}
	view := reg.NewContentPane(pane.KindHTMLPreview, path, "", "", "")
	if view == nil {
		return
	}
	m.restoreHTMLPreview(view, mode)
	if !inst.AttachTabView(idx, view) {
		inst.SetTabViewMode(idx, pane.ViewSource)
	}
}

// openInHTMLView puts a freshly opened HTML file's tab (key's active tab) in
// the view preview.html_open_mode names. A navigation to a source position
// (m.htmlSourceNav) always stays in Source.
func (m *Model) openInHTMLView(key string) {
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || m.htmlSourceNav || !htmlOpenInPreview() {
		return
	}
	idx := inst.ActiveTab()
	if inst.TabSwitchable(idx) {
		m.setTabView(inst, idx, pane.ViewPreview)
	}
}

// showSourceFor puts every tab showing path back in Source view — a jump to
// a source position must land on the caret, never on a rendered page.
func (m *Model) showSourceFor(path string) {
	changed := false
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		if idx := inst.TabForPath(path); idx >= 0 && inst.SetTabViewMode(idx, pane.ViewSource) {
			changed = true
		}
	}
	if changed && m.activeWS().Tree != nil {
		m.layout()
		saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	}
}

// tabViewForPath returns the shown Preview view of a tab showing path, nil
// when no tab shows its page rendered.
func (m Model) tabViewForPath(path string) *pane.Instance {
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		if idx := inst.TabForPath(path); idx >= 0 {
			if v := inst.ShownTabView(idx); v != nil {
				return v
			}
		}
	}
	return nil
}

// viewStripClick handles a left click on the view strip of pane inst (#2766):
// [Source] and [Preview] switch the active tab's view, [Browser] toggles the
// preview's browser mode exactly like b in the pane.
func (m *Model) viewStripClick(inst *pane.Instance, act pane.StripAction) tea.Cmd {
	idx := inst.ActiveTab()
	switch act {
	case pane.StripSource:
		m.setTabView(inst, idx, pane.ViewSource)
	case pane.StripPreview:
		m.setTabView(inst, idx, pane.ViewPreview)
	case pane.StripBrowser:
		if v := inst.ShownTabView(idx); v != nil {
			return v.HTMLPreview().ToggleBrowser()
		}
	}
	return nil
}
