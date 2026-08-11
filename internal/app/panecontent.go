package app

import (
	"ike/internal/pane"
)

// panecontent.go — helpers to reach viewer content wherever it lives (#1778):
// as a dedicated pane or nested inside a tab host's tab. Async messages,
// dedupe-and-focus opens and mouse routing all resolve through these, so a
// preview, diff, data viewer or HTTP response keeps working after it was
// dragged into a tab.

// forEachContent calls fn for every content instance of reg: top-level panes
// and tab-nested content. hostKey is the owning pane key; tabIdx is -1 for a
// dedicated pane. Returning false stops the walk.
func forEachContent(reg *pane.Registry, fn func(hostKey string, tabIdx int, inst *pane.Instance) bool) {
	if reg == nil {
		return
	}
	for _, key := range reg.Keys() {
		inst := reg.Get(key)
		if inst == nil {
			continue
		}
		if !fn(key, -1, inst) {
			return
		}
		if inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			if c := inst.TabContent(i); c != nil {
				if !fn(key, i, c) {
					return
				}
			}
		}
	}
}

// contentInstances is forEachContent over the active workspace's registry.
func (m Model) contentInstances(fn func(hostKey string, tabIdx int, inst *pane.Instance) bool) {
	forEachContent(m.activeWS().Panes, fn)
}

// findContent returns the first content instance satisfying pred, with its
// host key and tab index (-1 for a dedicated pane); ok=false when none
// matches.
func (m Model) findContent(pred func(*pane.Instance) bool) (hostKey string, tabIdx int, inst *pane.Instance, ok bool) {
	m.contentInstances(func(key string, idx int, c *pane.Instance) bool {
		if pred(c) {
			hostKey, tabIdx, inst, ok = key, idx, c, true
			return false
		}
		return true
	})
	return hostKey, tabIdx, inst, ok
}

// focusContentAt focuses the pane at hostKey and, for a tab-nested hit,
// activates its tab — the "refocus the existing viewer" move for content
// that may live in a tab (#1778).
func (m *Model) focusContentAt(hostKey string, tabIdx int) {
	m.setFocus(hostKey)
	if tabIdx < 0 {
		return
	}
	if inst := m.activeWS().Panes.Get(hostKey); inst != nil {
		m.switchTab(inst, tabIdx)
	}
}

// focusedContent resolves the focused pane to the instance owning its body:
// the active content tab's nested instance of a tab host, else the focused
// instance itself (#1778).
func (m Model) focusedContent() *pane.Instance {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil {
		return nil
	}
	if c := inst.ActiveContent(); c != nil {
		return c
	}
	return inst
}

// bodyContent resolves key's pane to the instance owning its body: the
// active content tab's nested instance of a tab host, else the pane's own
// instance — so mouse routing treats a content tab like the equivalent
// dedicated pane (#1778).
func (m Model) bodyContent(key string) *pane.Instance {
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		return nil
	}
	if c := inst.ActiveContent(); c != nil {
		return c
	}
	return inst
}
