package app

import (
	"ike/internal/layout"
	"ike/internal/pane"
)

// diff_placement.go — where a diff-open lands (#2507). Every entry point
// (diff.files, the VCS panel's HEAD and commit diffs, local history, the
// clipboard compare, the HTTP response diff) creates its viewer through
// openDiffLeaf, which either nests it as a content tab (#1778) in the pane
// the user works in — config diff.placement = "focused", the default — or
// falls back to placeDiffLeaf's split beside the active editor
// (diff.placement = "split", and every layout without a flexible pane).

// flexPane reports whether inst sits in the layout's flexible region — the
// editor area a diff may open into. Content kinds qualify: editor panes and
// the viewer panes (markdown, diff, image, archive, data, hex, notebook) that
// convert into tab hosts on demand. The explorer, the singleton tool windows,
// terminal panes and a pure tool-tab host (#1989) do not: a diff must never
// take over the tool strip the user reads results in. The popup terminal and
// the floating panels are no layout leaves at all, so they never apply.
func flexPane(inst *pane.Instance) bool {
	if inst == nil || !pane.KindTabbable(inst.Kind()) || inst.Kind() == pane.KindTerminal {
		return false
	}
	return !toolTabHost(inst)
}

// diffPlacementFocused reports whether diff.placement asks for the focused
// pane (the default) rather than the historic split.
func (m Model) diffPlacementFocused() bool {
	v, ok := m.host.Config().Get("diff.placement")
	return !ok || v != "split"
}

// diffTabTarget names the pane a diff opens into as a content tab: the
// focused pane when it lies in the flexible region, else the most recently
// focused flex pane, else the first one in tree order. ok=false in "split"
// placement and for a layout with no flexible pane at all — the caller then
// keeps the pre-#2507 split.
func (m Model) diffTabTarget() (string, bool) {
	if !m.diffPlacementFocused() {
		return "", false
	}
	if flexPane(m.activeWS().Panes.FocusedInstance()) {
		return m.activeWS().Panes.Focused(), true
	}
	if m.recentFlex != "" && flexPane(m.activeWS().Panes.Get(m.recentFlex)) {
		return m.recentFlex, true
	}
	if m.recentEditor != "" && flexPane(m.activeWS().Panes.Get(m.recentEditor)) {
		return m.recentEditor, true
	}
	for _, key := range m.leafOrder() {
		if flexPane(m.activeWS().Panes.Get(key)) {
			return key, true
		}
	}
	return "", false
}

// diffInPane returns the diff viewer hosted by pane key — the pane itself
// when it is a dedicated diff, else its first diff content tab (#1778) — with
// the tab index locating it for focusContentAt (-1 for the pane).
func (m Model) diffInPane(key string) (*pane.Instance, int, bool) {
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		return nil, -1, false
	}
	if inst.Kind() == pane.KindDiff {
		return inst, -1, true
	}
	for i := 0; i < inst.TabCount(); i++ {
		if c := inst.TabContent(i); c != nil && c.Kind() == pane.KindDiff {
			return c, i, true
		}
	}
	return nil, -1, false
}

// openDiffLeaf creates a diff viewer through add — one of the registry's
// AddDiff* constructors — and places it (#2507): as a focused content tab of
// diffTabTarget's pane, or, when there is no such pane or diff.placement is
// "split", beside the active editor the pre-#2507 way. It returns the
// instance owning the diff so the caller can fill in contents, and false when
// there is nowhere to place it (the pane is closed again in that case).
func (m *Model) openDiffLeaf(add func() string) (*pane.Instance, bool) {
	key := add()
	if target, ok := m.diffTabTarget(); ok && target != key {
		if inst, ok := m.nestDiffTab(key, target); ok {
			return inst, true
		}
	}
	if !m.placeDiffLeaf(key) {
		return nil, false
	}
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		return nil, false
	}
	m.setFocus(key)
	return inst, true
}

// nestDiffTab moves the freshly created diff pane key into pane target as a
// focused content tab (#2507), closing the now content-less pane. An empty
// scratch editor is taken over in place instead (#628): the diff becomes that
// leaf rather than the sole tab of a blank pane. A viewer pane converts into a
// tab host on the way (#1778). It returns the instance owning the diff, or
// false when the target refuses — the caller then splits instead.
func (m *Model) nestDiffTab(key, target string) (*pane.Instance, bool) {
	tinst, inst := m.activeWS().Panes.Get(target), m.activeWS().Panes.Get(key)
	if tinst == nil || inst == nil || m.activeWS().Tree == nil {
		return nil, false
	}
	if tinst.IsEmptyEditor() {
		if _, ok := layout.Replace(m.activeWS().Tree, target, key); ok {
			m.activeWS().Panes.Close(target)
			m.layout()
			m.setFocus(key)
			return inst, true
		}
	}
	if !m.ensureTabHost(target) {
		return nil, false
	}
	nested, ok := inst.DetachContent()
	if !ok {
		return nil, false
	}
	if !tinst.AddContentTab(nested) {
		m.activeWS().Panes.Close(key) // unreachable: ensureTabHost made it an editor
		return nil, false
	}
	m.activeWS().Panes.Close(key) // content moved out, so this releases nothing
	m.setFocus(target)
	m.layout()
	return nested, true
}
