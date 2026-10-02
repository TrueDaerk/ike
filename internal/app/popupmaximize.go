package app

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
func (m *Model) togglePopupMaximize() {
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
// each panel's saved geometry — when the layer hides.
func (m *Model) restorePopupMaximize() {
	for _, f := range m.floatTerms {
		if f.restore != nil {
			m.toggleFloatTermMaximize(f)
		}
	}
	m.popup.maximized = false
	m.applyPopupSize()
}

// maximizedFloatRect is the geometry a zoomed panel takes: the body rect.
func (m *Model) maximizedFloatRect() (x, y, w, h int) {
	r := m.bodyRect()
	return r.X, r.Y, r.W, r.H
}
