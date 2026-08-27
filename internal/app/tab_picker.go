package app

import (
	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/palette"
)

// tab_picker.go is the editor pane's tab picker (#2151, editor.tab.picker):
// the tabs of the focused editor pane listed most-recently-used first in a
// locked palette mode, so a tab scrolled out of the overflowing strip is one
// chord and a few keystrokes away. Recency is the per-instance activation
// stamp the tab-limit eviction already keeps (pane.Instance.TabsByMRU), so the
// list needs no second bookkeeping; enter activates the picked tab.

// tabPickerPrefix selects the picker mode inside the palette; opened locked
// only, so the rune has no user-facing prefix story.
const tabPickerPrefix = '`'

// TabPickedMsg activates one picked tab of one pane.
type TabPickedMsg struct {
	Pane  string
	Index int
}

// tabPickerEntry is one row of the picker: the bar's own label for the tab,
// its pane-relative index and the path detail that tells same-named tabs
// apart. current marks the tab the pane is showing right now.
type tabPickerEntry struct {
	label   string
	detail  string
	pane    string
	index   int
	current bool
}

// tabPickerMode is the palette Mode listing the pane's tabs; the model fills
// entries before each locked open (the runConfigsMode pattern), already in
// MRU order — Results only filters, so the recency ranking survives typing.
type tabPickerMode struct {
	entries []tabPickerEntry
}

func newTabPickerMode() *tabPickerMode { return &tabPickerMode{} }

// Prefix implements palette.Mode.
func (t *tabPickerMode) Prefix() rune { return tabPickerPrefix }

// Placeholder implements palette.Mode.
func (t *tabPickerMode) Placeholder() string { return "Switch tab…" }

// Results implements palette.Mode: the pane's tabs fuzzy-matched over their
// bar label, detailing the document path. The entry order is kept rather than
// re-sorted by score, so the recency ranking survives typing and the
// preselected first row is the tab to flip back to.
func (t *tabPickerMode) Results(query string, _ palette.Context) []palette.Item {
	var items []palette.Item
	for _, e := range t.entries {
		res, ok := fuzzy.Match(query, e.label)
		if !ok {
			continue
		}
		item := palette.Item{
			Title:  e.label,
			Spans:  res.Positions,
			Score:  res.Score,
			Detail: e.detail,
			Msg:    TabPickedMsg{Pane: e.pane, Index: e.index},
		}
		if e.current {
			item.Badge = "●"
		}
		items = append(items, item)
	}
	return items
}

// tabPickerEntries lists the focused editor pane's tabs in most-recently-used
// order with the active tab moved to the end: activating it would be a no-op,
// so the preselected first row is the tab used before it — the alternate-tab
// flip a switcher is opened for — while the rest keeps descending recency.
// It is the picker's data seam, kept separate from the palette plumbing so
// the ordering is testable on its own.
func (m *Model) tabPickerEntries() []tabPickerEntry {
	inst := m.tabPane()
	if inst == nil {
		return nil
	}
	key := m.activeEditorKey()
	labels := tabLabels(inst)
	var out, current []tabPickerEntry
	for _, idx := range inst.TabsByMRU() {
		if idx < 0 || idx >= len(labels) {
			continue
		}
		e := tabPickerEntry{
			label:   labels[idx],
			detail:  displayPath(inst.TabPath(idx)),
			pane:    key,
			index:   idx,
			current: idx == inst.ActiveTab(),
		}
		if e.current {
			current = append(current, e)
			continue
		}
		out = append(out, e)
	}
	return append(out, current...)
}

// openTabPicker opens the palette locked to the tab-picker mode
// (editor.tab.picker). A pane with a single tab has nothing to switch to and
// says so instead of showing a one-row list.
func (m *Model) openTabPicker() {
	entries := m.tabPickerEntries()
	if len(entries) < 2 {
		m.host.Notify(host.Info, "tabs: only one tab open in this pane")
		return
	}
	m.tabPicker.entries = entries
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, tabPickerPrefix)
}

// activatePickedTab activates one picked row's tab (#2151): the pane is
// focused first, so the picker also switches panes when its rows came from a
// pane that had lost focus meanwhile. A tab closed while the palette was open
// resolves to no pane and is ignored rather than activating a neighbour.
func (m *Model) activatePickedTab(msg TabPickedMsg) {
	inst := m.activeWS().Panes.Get(msg.Pane)
	if inst == nil || msg.Index < 0 || msg.Index >= inst.TabCount() {
		return
	}
	m.setFocus(msg.Pane)
	m.switchTab(inst, msg.Index)
}
