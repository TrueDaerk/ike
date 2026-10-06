package app

import "slices"

// popupmaximize.go — pane.maximize on the popup terminal layer (#2899). With
// the layer focused the command zooms the keyboard-owning surface instead of
// the layout pane underneath: the popup box (#1398) or a torn-out floating
// panel (#1793) fills the body rect — the area a zoomed pane gets, below the
// menu bar and above the status line — and a second invocation restores it.
// The zoom is runtime state only: the box's persisted size and position
// deltas (ui.WinSizes, #774/#1793/#2896) and the pinned dock geometry (#2406)
// are never written, so restoring is just dropping the zoom, and hiding the
// layer drops it too.

// floatRect is a floating panel's outer geometry, saved while it is
// maximized so the restore puts it back exactly.
type floatRect struct{ x, y, w, h int }

// togglePopupMaximize flips the zoom on the popup layer's keyboard owner: the
// focused floating panel when one holds the keyboard, else the popup box.
// Maximizing while popup zen (#2905) holds leaves zen first, like
// pane.maximize leaving layout zen: the chrome returns and the toggle then
// acts on the keyboard owner as usual.
func (m *Model) togglePopupMaximize() {
	if m.popupZenActive() {
		m.clearPopupZen()
		defer m.layout()
	}
	if f := m.floatFocused(); f != nil {
		m.toggleFloatTermMaximize(f)
		return
	}
	if m.popup.inst == nil {
		return
	}
	m.popup.maximized = !m.popup.maximized
	m.applyPopupSize()
}

// toggleFloatTermMaximize zooms panel f over the body rect, remembering its
// geometry, or puts a zoomed panel back where it was.
func (m *Model) toggleFloatTermMaximize(f *floatTerm) {
	if f.restore != nil {
		r := *f.restore
		f.restore = nil
		f.x, f.y, f.w, f.h = r.x, r.y, r.w, r.h
	} else {
		f.restore = &floatRect{f.x, f.y, f.w, f.h}
	}
	// applyPopupSize re-clamps: a zoomed panel snaps to the body rect, a
	// restored one back into the (possibly resized) terminal bounds.
	m.applyPopupSize()
}

// restorePopupMaximize drops every zoom on the layer — the box's flag and
// each panel's saved geometry — when the layer hides, and with them popup
// zen (#2905), so the chrome returns.
func (m *Model) restorePopupMaximize() {
	if m.popupZen {
		m.clearPopupZen()
		defer m.layout()
	}
	for _, f := range m.floatTerms {
		if f.restore != nil {
			m.toggleFloatTermMaximize(f)
		}
	}
	m.popup.maximized = false
	m.applyPopupSize()
}

// maximizedFloatRect is the geometry a zoomed panel takes: the body rect
// (which includes the status row while zen hides it).
func (m *Model) maximizedFloatRect() (x, y, w, h int) {
	r := m.bodyRect()
	return r.X, r.Y, r.W, r.H
}

// popupZenActive reports whether popup zen (#2905) still holds: the flag is
// set, the layer is shown and the zen target — the recorded floating panel,
// else the popup box — is still present and zoomed. Every path that closes,
// collapses or hides the target makes this false, which syncPopupZen turns
// into dropping the zen state.
func (m *Model) popupZenActive() bool {
	if !m.popupZen || !m.popup.open {
		return false
	}
	if f := m.popupZenFloat; f != nil {
		return f.restore != nil && slices.Contains(m.floatTerms, f)
	}
	return m.popup.inst != nil && m.popup.maximized
}

// chromeHidden reports whether the status line is hidden and its row joins
// the body: layout-pane zen (#359) or popup zen (#2905).
func (m *Model) chromeHidden() bool {
	return m.zen || m.popupZenActive()
}

// togglePopupZen is view.zenMode on the popup layer (#2905): the layer's
// keyboard owner — the focused floating panel, else the popup box — zooms
// over the body rect with the status line hidden, so it grows into the freed
// row. A second invocation leaves zen and restores the previous geometry;
// a surface already maximized before zen (#2899) stays maximized. Layout-pane
// zoom and zen state (zoomed, zenKeepZoom) are never touched.
func (m *Model) togglePopupZen() {
	if m.popupZenActive() {
		f, keep := m.popupZenFloat, m.popupZenKeep
		m.clearPopupZen()
		if !keep {
			if f != nil {
				m.toggleFloatTermMaximize(f)
			} else {
				m.popup.maximized = false
			}
		}
		m.layout()
		m.applyPopupSize()
		return
	}
	f := m.floatFocused()
	if f == nil && m.popup.inst == nil {
		return
	}
	m.popupZen, m.popupZenFloat = true, f
	if f != nil {
		m.popupZenKeep = f.restore != nil
		if f.restore == nil {
			f.restore = &floatRect{f.x, f.y, f.w, f.h}
		}
	} else {
		m.popupZenKeep = m.popup.maximized
		m.popup.maximized = true
	}
	m.layout() // the body rect gained the status row
	m.applyPopupSize()
}

// clearPopupZen drops the popup zen bookkeeping without touching geometry.
func (m *Model) clearPopupZen() {
	m.popupZen, m.popupZenFloat, m.popupZenKeep = false, nil, false
}

// syncPopupZen drops a popup zen whose target went away (closed, collapsed,
// torn into another box, layer hidden) and re-lays out, so the status line
// returns and the panes shrink back above it.
func (m *Model) syncPopupZen() {
	if m.popupZen && !m.popupZenActive() {
		m.clearPopupZen()
		m.layout()
		m.applyPopupSize()
	}
}
