package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"ike/internal/forge"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/terminal"
)

// layouts.go implements saved window layouts (#1175), JetBrains' Window
// Layouts: the split tree plus a kind-level identity per leaf — no file
// contents, tab lists or paths — saved under a user-chosen name. Layouts are
// user preference and cross-project, so the store is user-scoped (unlike the
// per-project layout.json). One saved layout may be marked as the default: it
// replaces the built-in explorer+editor default for projects that have no
// persisted layout of their own, and window.restoreLayout (shift+f12)
// re-applies it to the current workspace.

// savedLayouts is the on-disk schema of the user layout store: named
// snapshots in the persistedLayout shape (tree + kind-only identity table)
// plus the name of the designated default ("" when none).
type savedLayouts struct {
	Layouts map[string]persistedLayout `json:"layouts"`
	Default string                     `json:"default,omitempty"`
}

// layoutsFile returns the path of the user-scoped layout store: it follows
// the IKE_CONFIG_DIR redirection seam like every other state file, and falls
// back to ~/.ike/layouts.json (the user config layer's home) — NOT the
// project's .ike directory, because layouts span projects.
func layoutsFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "layouts.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ike", "layouts.json")
}

// loadUserLayouts reads the store; a missing or malformed file yields an
// empty store rather than an error (same tolerance as loadLayout).
func loadUserLayouts() savedLayouts {
	var s savedLayouts
	path := layoutsFile()
	if path == "" {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &s) != nil {
		return savedLayouts{}
	}
	return s
}

// saveUserLayouts persists the store. Errors are swallowed like saveLayout's:
// failing to persist must never disrupt the session.
func saveUserLayouts(s savedLayouts) {
	path := layoutsFile()
	if path == "" {
		return
	}
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, data, 0o644)
}

// layoutNames lists the saved layout names sorted, plus the default marker.
func layoutNames() (names []string, def string) {
	s := loadUserLayouts()
	for name := range s.Layouts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, s.Default
}

// flexKey is the leaf key of the flexible placeholder region in a selective
// layout (#1568). It never collides with a registry key: the registry only
// mints the known singleton keys and "editor"/"terminal"/… bases. A snapshot
// stores it with the kind-only identity {Kind: "flex"}; apply grafts every
// live pane the layout's slots did not consume into its place.
const flexKey = "flex"

// snapshotLayout strips the live workspace down to a saveable layout: the
// split tree with canonically re-keyed leaves and a kind-only identity per
// leaf. Content panes (editors, markdown previews, diff viewers, tab hosts)
// all become anonymous editor slots; terminals keep only their kind (a tool
// pane its tool name, #741, so apply can restart the program); the singleton
// panels keep their fixed keys. It fails on a leaf without a registered
// instance — a malformed tree is not worth saving.
func snapshotLayout(tree layout.Node, reg *pane.Registry) (persistedLayout, bool) {
	if tree == nil {
		return persistedLayout{}, false
	}
	st := &snapState{reg: reg, ids: map[string]paneIdentity{}}
	normalized, ok := st.rebuild(tree)
	if !ok {
		return persistedLayout{}, false
	}
	data, err := layout.Encode(normalized)
	if err != nil {
		return persistedLayout{}, false
	}
	return persistedLayout{Tree: data, Panes: st.ids}, true
}

// snapshotLayoutSelected is snapshotLayout restricted to the selected leaves
// (#1568): deselected panes are pruned from the tree, and the largest
// deselected region survives as the flexible placeholder leaf so apply knows
// where the unsaved panes should flow. With every leaf selected (or sel nil)
// it degrades to the full snapshot — no placeholder, unchanged semantics.
func snapshotLayoutSelected(tree layout.Node, reg *pane.Registry, sel map[string]bool) (persistedLayout, bool) {
	if tree == nil {
		return persistedLayout{}, false
	}
	leaves := layout.Leaves(tree)
	selected := 0
	for _, key := range leaves {
		if sel == nil || sel[key] {
			selected++
		}
	}
	if selected == len(leaves) {
		return snapshotLayout(tree, reg)
	}
	if selected == 0 {
		return persistedLayout{}, false // a layout of nothing but flex is useless
	}
	pruned := layout.Clone(tree)
	// The largest deselected region hosts the placeholder — that's where the
	// user left the most flexible space; its siblings collapse away.
	lay := layout.Compute(pruned, layout.Rect{W: 1000, H: 1000})
	host, best := "", -1
	for _, key := range leaves {
		if sel[key] {
			continue
		}
		r := lay.Panes[key]
		if a := r.W * r.H; a > best {
			best, host = a, key
		}
	}
	for _, key := range leaves {
		if sel[key] || key == host {
			continue
		}
		if p, ok := layout.Close(pruned, key); ok {
			pruned = p
		}
	}
	pruned, _ = layout.Replace(pruned, host, flexKey)
	st := &snapState{reg: reg, ids: map[string]paneIdentity{}}
	normalized, ok := st.rebuild(pruned)
	if !ok {
		return persistedLayout{}, false
	}
	data, err := layout.Encode(normalized)
	if err != nil {
		return persistedLayout{}, false
	}
	return persistedLayout{Tree: data, Panes: st.ids}, true
}

// snapState carries the canonical key minting through the snapshot walk.
type snapState struct {
	reg       *pane.Registry
	ids       map[string]paneIdentity
	editors   int
	terminals int
	seen      map[string]bool // singleton keys already assigned
}

// rebuild clones the tree with canonical leaf keys, filling st.ids.
func (st *snapState) rebuild(n layout.Node) (layout.Node, bool) {
	switch v := n.(type) {
	case *layout.Leaf:
		if v.Pane == flexKey {
			// The flexible placeholder (#1568) has no live instance behind
			// it; it carries a kind-only identity like every other leaf.
			if st.ids[flexKey].Kind == "flex" {
				return nil, false // two placeholders: malformed
			}
			st.ids[flexKey] = paneIdentity{Kind: "flex"}
			return &layout.Leaf{Pane: flexKey}, true
		}
		key, id, ok := st.leafIdentity(v.Pane)
		if !ok {
			return nil, false
		}
		st.ids[key] = id
		return &layout.Leaf{Pane: key}, true
	case *layout.Split:
		a, ok := st.rebuild(v.A)
		if !ok {
			return nil, false
		}
		b, ok := st.rebuild(v.B)
		if !ok {
			return nil, false
		}
		return &layout.Split{Orient: v.Orient, Ratio: v.Ratio, A: a, B: b}, true
	}
	return nil, false
}

// leafIdentity maps one live leaf to its canonical key and kind-only identity.
func (st *snapState) leafIdentity(key string) (string, paneIdentity, bool) {
	inst := st.reg.Get(key)
	if inst == nil {
		return "", paneIdentity{}, false
	}
	singleton := func(k, kind string) (string, paneIdentity, bool) {
		if st.seen == nil {
			st.seen = map[string]bool{}
		}
		if st.seen[k] {
			return "", paneIdentity{}, false // duplicate singleton leaf: malformed
		}
		st.seen[k] = true
		return k, paneIdentity{Kind: kind}, true
	}
	switch inst.Kind() {
	case pane.KindExplorer:
		return singleton(pane.ExplorerKey, "explorer")
	case pane.KindVCS:
		return singleton(pane.VCSKey, "vcs")
	case pane.KindDebug:
		return singleton(pane.DebugKey, "debug")
	case pane.KindProblems:
		return singleton(pane.ProblemsKey, "problems")
	case pane.KindTests:
		return singleton(pane.TestsKey, "tests")
	case pane.KindIssues:
		return singleton(pane.IssuesKey, "issues")
	case pane.KindStructure:
		return singleton(pane.StructureKey, "structure")
	case pane.KindDOM:
		return singleton(pane.DOMKey, "dom")
	case pane.KindDoctor:
		return singleton(pane.DoctorKey, "xdoctor")
	case pane.KindUsages:
		return singleton(pane.UsagesKey, "usages")
	case pane.KindHTTP:
		return singleton(pane.HTTPKey, "http")
	case pane.KindBreakpoints:
		return singleton(pane.BreakpointsKey, "breakpoints")
	case pane.KindTerminal:
		k := st.mintTerminal()
		if tool := inst.Terminal().Tool(); tool != "" {
			return k, paneIdentity{Kind: "tool", Tool: tool}, true
		}
		return k, paneIdentity{Kind: "terminal"}, true
	case pane.KindEditor, pane.KindMarkdown, pane.KindImage, pane.KindDiff, pane.KindMerge, pane.KindArchive, pane.KindData, pane.KindES, pane.KindRemote:
		// Content panes are anonymous editor slots: what files they held is
		// session state, only the space they occupied is layout. Tool sessions
		// hosted as tabs (#836) are the exception, like in saveLayout: a host
		// carrying exactly one tool and no files snapshots as the dedicated
		// tool slot it visually is, any other host keeps the tool names so the
		// apply can restart them as tabs (#1277).
		if inst.Kind() == pane.KindEditor {
			tools, files := editorPaneTools(inst)
			if len(tools) == 1 && files == 0 {
				return st.mintTerminal(), paneIdentity{Kind: "tool", Tool: tools[0]}, true
			}
			if toolTabHost(inst) {
				// A host holding nothing but tool tabs is a tools pane, not an
				// editor slot (#1989): its own kind keeps editor placement and
				// slot resolution away from the layout's editor area.
				return st.mintEditor(), paneIdentity{Kind: "tools", Tools: tools}, true
			}
			if len(tools) > 0 {
				return st.mintEditor(), paneIdentity{Kind: "editor", Tools: tools}, true
			}
		}
		return st.mintEditor(), paneIdentity{Kind: "editor"}, true
	}
	return "", paneIdentity{}, false
}

func (st *snapState) mintEditor() string {
	st.editors++
	if st.editors == 1 {
		return "editor"
	}
	return "editor:" + strconv.Itoa(st.editors)
}

func (st *snapState) mintTerminal() string {
	st.terminals++
	if st.terminals == 1 {
		return "terminal"
	}
	return "terminal:" + strconv.Itoa(st.terminals)
}

// namedLayout decodes the stored layout name into a tree plus identity table,
// ready for restoreFromLayout / applySnapshot.
func namedLayout(name string) (layout.Node, map[string]paneIdentity, bool) {
	s := loadUserLayouts()
	p, ok := s.Layouts[name]
	if !ok || len(p.Tree) == 0 {
		return nil, nil, false
	}
	tree, leaves, ok := layout.DecodeTree(p.Tree)
	if !ok {
		return nil, nil, false
	}
	return tree, mergeIdentities(leaves, p.Panes), true
}

// defaultLayoutSnapshot resolves the designated default layout, if any.
func defaultLayoutSnapshot() (layout.Node, map[string]paneIdentity, bool) {
	s := loadUserLayouts()
	if s.Default == "" {
		return nil, nil, false
	}
	return namedLayout(s.Default)
}

// applyState carries the queues of the live instances a runtime apply
// re-slots into the target layout's leaves.
type applyState struct {
	content []string            // editor/markdown/diff/tab-host keys, in registry order
	hosts   []string            // pure tool-tab host keys (#1989), in registry order
	shells  []string            // plain shell terminal keys
	tools   map[string][]string // tool pane keys by tool name
	used    map[string]bool     // resolved singleton keys (duplicate guard)
	slots   []string            // resolved leaf keys in walk order
	skip    map[string]bool     // live slotted panes held out of queues and grafts (#1899)
}

// applyLayoutByName re-shapes the ACTIVE workspace to the named saved layout
// (#1175). Open files never close: the session's content panes re-slot into
// the layout's editor slots in order. Singleton tool panels absent from the
// layout lose their leaf but stay registered (the toolhide precedent, #791) —
// their toggles resurface them. Running terminals are never killed by
// applying a layout, and no leftover pane collapses into a tab (#1577):
// content panes and terminals the slots did not consume keep their own panes
// and graft into the flexible region — the explicit placeholder of a
// selective layout (#1568), or an implicit one at the last editor slot —
// preserving their current relative arrangement. Parked workspaces (#777)
// are untouched.
func (m *Model) applyLayoutByName(name string) {
	tree, ids, ok := namedLayout(name)
	if !ok {
		m.host.Notify(host.Warn, "layout "+name+" is missing or malformed")
		return
	}
	if m.applySnapshot(tree, ids) {
		m.host.Notify(host.Info, "applied layout "+name)
	}
}

// applyDefaultLayout is window.restoreLayout (shift+f12): re-apply the
// designated default layout, or the built-in explorer+editor default when
// none is set — JetBrains' Restore Default Layout.
func (m *Model) applyDefaultLayout() {
	if tree, ids, ok := defaultLayoutSnapshot(); ok {
		if m.applySnapshot(tree, ids) {
			m.host.Notify(host.Info, "restored default layout")
		}
		return
	}
	tree := layout.Default(m.width, explorerWidth)
	ids := map[string]paneIdentity{
		pane.ExplorerKey: {Kind: "explorer"},
		"editor":         {Kind: "editor"},
	}
	if m.applySnapshot(tree, ids) {
		m.host.Notify(host.Info, "restored default layout")
	}
}

// applySnapshot re-shapes the active workspace to the snapshot's tree,
// preserving live instances. It reports success; failure leaves the
// workspace untouched.
func (m *Model) applySnapshot(tree layout.Node, ids map[string]paneIdentity) bool {
	ws := m.activeWS()
	reg := ws.Panes
	hasFlex := false
	for _, id := range ids {
		if id.Kind == "flex" {
			hasFlex = true
		}
	}
	// With a slot template active the slot config is authoritative (#1899):
	// slotted snapshot leaves are pruned here and re-open through the slot
	// engine after the ordinary apply, and live slotted panes are held out of
	// the queues and grafts to survive in their slots. Singleton panels
	// absent from a full layout keep the #791 hide semantics instead.
	tpl := slotTemplate()
	var slotOpens []slotOpen
	var slotLive []slotResident
	if tpl != nil {
		tree, slotOpens = pruneSlottedLeaves(tree, ids, tpl)
		for _, r := range m.liveSlotted(tpl) {
			if r.panel && !hasFlex {
				continue
			}
			slotLive = append(slotLive, r)
		}
	}
	// Unconsumed live panes graft — in their current relative arrangement —
	// into the flexible region (explicit placeholder or implicit host slot,
	// #1577), so the clone of the pre-apply tree is that arrangement's source
	// of truth.
	liveClone := layout.Clone(ws.Tree)
	st := &applyState{tools: map[string][]string{}, used: map[string]bool{}, skip: map[string]bool{}}
	for _, r := range slotLive {
		st.skip[r.key] = true
	}
	for _, key := range reg.Keys() {
		inst := reg.Get(key)
		if inst == nil || st.skip[key] {
			continue
		}
		switch inst.Kind() {
		case pane.KindEditor, pane.KindMarkdown, pane.KindImage, pane.KindDiff, pane.KindMerge, pane.KindArchive, pane.KindData, pane.KindES, pane.KindRemote:
			if toolTabHost(inst) {
				// A pure tool-tab host queues for "tools" slots (#1989): an
				// editor slot must never re-slot the tools pane into the
				// layout's editor area.
				st.hosts = append(st.hosts, key)
			} else {
				st.content = append(st.content, key)
			}
		case pane.KindTerminal:
			if tool := inst.Terminal().Tool(); tool != "" {
				st.tools[tool] = append(st.tools[tool], key)
			} else {
				st.shells = append(st.shells, key)
			}
		}
	}
	newTree, ok := m.resolveNode(tree, ids, st)
	if !ok {
		m.host.Notify(host.Warn, "layout is malformed — nothing applied")
		return false
	}
	if hasFlex {
		// A selective layout (#1568) never merges: everything the slots did
		// not consume keeps its pane and flows into the placeholder region.
		newTree = m.graftFlex(newTree, liveClone, st)
	} else {
		// A full layout gets an implicit flexible region (#1577): leftover
		// content panes and terminals keep their own panes and graft next to
		// the host slot instead of collapsing into tabs (#1275's reachability
		// guarantee, without the tab-merge).
		newTree = m.graftImplicit(newTree, liveClone, st)
	}
	// The old hide-all snapshot and zoom describe the old tree.
	m.toolHide = nil
	m.zoomed = ""
	ws.Tree = newTree
	if tpl != nil {
		// Slotted occupants re-materialize through the slot engine: live
		// slotted panes first, then the snapshot's pruned slot opens (#1899).
		m.applySlots(tpl, slotLive, slotOpens, st)
	}
	if key := firstEditorKey(st.slots); key != "" {
		m.setFocus(key)
	} else if len(st.slots) > 0 {
		m.setFocus(st.slots[0])
	}
	m.wireEditorEmitters()
	m.layout()
	saveLayout(ws.Tree, ws.Panes)
	return true
}

// graftFlex resolves the flexible placeholder of a selective layout (#1568):
// every live pane the slot walk did not consume keeps its pane, and the whole
// group replaces the placeholder leaf preserving the panes' pre-apply relative
// arrangement (the live tree with the consumed leaves collapsed away). With
// nothing left over the region becomes one scratch editor. st.slots is
// rewritten so the focus/merge helpers see real keys instead of the marker.
func (m *Model) graftFlex(newTree, liveClone layout.Node, st *applyState) layout.Node {
	reg := m.activeWS().Panes
	consumed := map[string]bool{}
	slots := st.slots[:0]
	for _, key := range st.slots {
		if key == flexKey {
			continue
		}
		consumed[key] = true
		slots = append(slots, key)
	}
	sub := liveClone
	for _, key := range layout.Leaves(liveClone) {
		// Slotted panes (st.skip) never count as leftovers (#1899): they
		// re-materialize in their template slots after the apply instead.
		if consumed[key] || st.skip[key] || reg.Get(key) == nil {
			if p, ok := layout.Close(sub, key); ok {
				sub = p
			}
		}
	}
	var leftovers []string
	for _, key := range layout.Leaves(sub) {
		if !consumed[key] && !st.skip[key] && reg.Get(key) != nil {
			leftovers = append(leftovers, key)
		}
	}
	if len(leftovers) == 0 {
		// Every live pane found a slot (or the sole surviving leaf was a
		// consumed one Close could not prune): fresh scratch space.
		sub = &layout.Leaf{Pane: reg.AddEditor()}
		leftovers = []string{sub.(*layout.Leaf).Pane}
	}
	st.slots = append(slots, leftovers...)
	return replaceFlexLeaf(newTree, sub)
}

// replaceFlexLeaf swaps the placeholder leaf for the grafted subtree in place.
func replaceFlexLeaf(n, sub layout.Node) layout.Node {
	return replaceLeaf(n, flexKey, sub)
}

// replaceLeaf swaps the named leaf for the grafted subtree in place.
func replaceLeaf(n layout.Node, key string, sub layout.Node) layout.Node {
	switch t := n.(type) {
	case *layout.Leaf:
		if t.Pane == key {
			return sub
		}
		return n
	case *layout.Split:
		t.A = replaceLeaf(t.A, key, sub)
		t.B = replaceLeaf(t.B, key, sub)
		return t
	}
	return n
}

// graftImplicit is the full-layout counterpart of graftFlex (#1577): a layout
// without a placeholder gets an implicit flexible region at the host slot
// (the last editor slot, with terminal-slot fallbacks). Every live content
// pane or terminal the slot walk did not consume keeps its own pane; the
// group replaces the host leaf — host pane included — preserving the panes'
// pre-apply relative arrangement. Nothing collapses into tabs and no tool
// pane becomes a tab host. Singleton panels absent from the layout keep the
// toolhide semantics (#791): registered but leafless, resurfaced by their
// toggles.
func (m *Model) graftImplicit(newTree, liveClone layout.Node, st *applyState) layout.Node {
	reg := m.activeWS().Panes
	host := implicitHostSlot(reg, st.slots)
	if host == "" {
		return newTree
	}
	consumed := map[string]bool{}
	for _, key := range st.slots {
		consumed[key] = true
	}
	graftable := func(key string) bool {
		if st.skip[key] {
			return false // slotted panes re-materialize in their slots (#1899)
		}
		inst := reg.Get(key)
		if inst == nil {
			return false
		}
		switch inst.Kind() {
		case pane.KindEditor, pane.KindMarkdown, pane.KindImage, pane.KindDiff,
			pane.KindMerge, pane.KindArchive, pane.KindData, pane.KindES, pane.KindRemote, pane.KindTerminal:
			return true
		}
		return false
	}
	sub := liveClone
	for _, key := range layout.Leaves(liveClone) {
		if key == host {
			continue
		}
		if consumed[key] || !graftable(key) {
			if p, ok := layout.Close(sub, key); ok {
				sub = p
			}
		}
	}
	hostInSub := false
	var leftovers []string
	for _, key := range layout.Leaves(sub) {
		if key == host {
			hostInSub = true
			continue
		}
		if !consumed[key] && graftable(key) {
			leftovers = append(leftovers, key)
		}
	}
	if len(leftovers) == 0 {
		return newTree // every live pane found a slot: the layout stands as saved
	}
	if !hostInSub {
		// The host slot resolved to a fresh instance with no pre-apply leaf:
		// it takes half the region, the leftovers keep their arrangement in
		// the other half, split along the region's longer axis.
		lay := layout.Compute(newTree, layout.Rect{W: m.width, H: m.height})
		orient := layout.Vertical
		if r := lay.Panes[host]; r.W >= 2*r.H {
			orient = layout.Horizontal
		}
		sub = &layout.Split{Orient: orient, Ratio: 0.5, A: &layout.Leaf{Pane: host}, B: sub}
	}
	st.slots = append(st.slots, leftovers...)
	return replaceLeaf(newTree, host, sub)
}

// implicitHostSlot picks the leaf hosting the implicit flexible region of a
// full layout: the last editor-kind slot, then the last plain shell slot,
// then the last terminal slot of any kind, then the last leaf outright — the
// leftovers split next to it, they never become its tabs.
func implicitHostSlot(reg *pane.Registry, slots []string) string {
	last := func(match func(*pane.Instance) bool) string {
		for i := len(slots) - 1; i >= 0; i-- {
			if inst := reg.Get(slots[i]); inst != nil && match(inst) {
				return slots[i]
			}
		}
		return ""
	}
	// A pure tool-tab host is not an editor slot (#1989): leftovers must not
	// graft into the layout's tools area.
	if key := last(func(i *pane.Instance) bool { return i.Kind() == pane.KindEditor && !toolTabHost(i) }); key != "" {
		return key
	}
	if key := last(func(i *pane.Instance) bool {
		return i.Kind() == pane.KindTerminal && i.Terminal().Tool() == ""
	}); key != "" {
		return key
	}
	if key := last(func(i *pane.Instance) bool { return i.Kind() == pane.KindTerminal }); key != "" {
		return key
	}
	if len(slots) > 0 {
		return slots[len(slots)-1]
	}
	return ""
}

// resolveNode clones the snapshot tree, resolving every leaf to a live
// instance key: existing instances re-slot in order, missing ones are created
// (empty editors, fresh shells, restarted tools, empty singleton panels).
func (m *Model) resolveNode(n layout.Node, ids map[string]paneIdentity, st *applyState) (layout.Node, bool) {
	switch v := n.(type) {
	case *layout.Leaf:
		key, ok := m.resolveLeaf(ids[v.Pane], st)
		if !ok {
			return nil, false
		}
		st.slots = append(st.slots, key)
		return &layout.Leaf{Pane: key}, true
	case *layout.Split:
		a, ok := m.resolveNode(v.A, ids, st)
		if !ok {
			return nil, false
		}
		b, ok := m.resolveNode(v.B, ids, st)
		if !ok {
			return nil, false
		}
		return &layout.Split{Orient: v.Orient, Ratio: v.Ratio, A: a, B: b}, true
	}
	return nil, false
}

// resolveLeaf maps one snapshot identity to a live instance key.
func (m *Model) resolveLeaf(id paneIdentity, st *applyState) (string, bool) {
	reg := m.activeWS().Panes
	singleton := func(add func() string) (string, bool) {
		key := add()
		if st.used[key] {
			return "", false // the same singleton twice: malformed store file
		}
		st.used[key] = true
		return key, true
	}
	switch id.Kind {
	case "flex":
		// The flexible placeholder (#1568) resolves after the walk: graftFlex
		// swaps the marker leaf for the unconsumed live panes.
		if st.used[flexKey] {
			return "", false // two placeholders: malformed store file
		}
		st.used[flexKey] = true
		return flexKey, true
	case "explorer":
		return singleton(reg.AddExplorer)
	case "vcs":
		return singleton(reg.AddVCS)
	case "debug":
		return singleton(reg.AddDebug)
	case "problems":
		key, ok := singleton(reg.AddProblems)
		if ok {
			p := reg.Get(key).Problems()
			p.SetDisplayPath(displayPath)
			p.SetStore(m.probStore)
		}
		return key, ok
	case "structure":
		return singleton(reg.AddStructure)
	case "dom":
		return singleton(reg.AddDOM)
	case "xdoctor":
		key, ok := singleton(reg.AddDoctor)
		if ok {
			m.wireDoctorPanel(reg.Get(key).Doctor())
		}
		return key, ok
	case "tests":
		return singleton(reg.AddTests)
	case "issues":
		key, ok := singleton(reg.AddIssues)
		if ok {
			// A restored pane re-arms its refresh so 'r' works before any
			// open-path injection (#1934).
			reg.Get(key).Issues().SetRefresh(forge.RefreshCmd("."))
		}
		return key, ok
	case "usages":
		key, ok := singleton(reg.AddUsages)
		if ok {
			reg.Get(key).Usages().SetDisplayPath(displayPath)
		}
		return key, ok
	case "http":
		return singleton(reg.AddHTTP)
	case "breakpoints":
		key, ok := singleton(reg.AddBreakpoints)
		if ok {
			m.wireBreakpointsPanel(reg.Get(key).Breakpoints())
		}
		return key, ok
	case "terminal":
		if len(st.shells) > 0 {
			key := st.shells[0]
			st.shells = st.shells[1:]
			return key, true
		}
		return m.spawnShellPane(), true
	case "tool":
		if q := st.tools[id.Tool]; len(q) > 0 {
			key := q[0]
			st.tools[id.Tool] = q[1:]
			return key, true
		}
		if id.Tool == runToolName {
			// The Run tool (#1905) is session state: with no live one to
			// re-slot, the snapshot's slot restores as an empty editor —
			// re-running the program behind the user's back is not an option.
			return reg.AddEditor(), true
		}
		// Restart the configured tool in the slot (#741); a tool no longer
		// configured degrades to a fresh shell rather than breaking the apply.
		if entry, ok := toolEntry(id.Tool); ok {
			if entry.Global && m.ws != nil {
				// Applying a layout is an explicit reopen (#1903); a parked
				// live session would have been adopted via st.tools instead.
				m.ws.ClearGlobalToolClosed(entry.Name)
			}
			dir := entry.Cwd
			if dir == "" {
				dir = "."
			}
			argv := append([]string{entry.Command}, entry.Args...)
			return reg.AddTool(entry.Name, argv, dir, toolSpawnEnv(m.pal()), m.host.Send), true
		}
		return m.spawnShellPane(), true
	case "tools":
		// A tools pane (#1989): a live tool-tab host re-slots, else the saved
		// tools restart as tabs of a fresh host. The content queue is never
		// consumed — the layout's editor slots keep their panes.
		if len(st.hosts) > 0 {
			key := st.hosts[0]
			st.hosts = st.hosts[1:]
			return key, true
		}
		key := reg.AddEditor()
		m.restartToolTabs(reg, key, id.Tools)
		return key, true
	case "editor", "":
		if len(id.Tools) > 0 && len(st.hosts) > 0 {
			// A legacy snapshot wrote a pure tool host as "editor"+Tools
			// (pre-#1989): a live tool host re-slots there before any content
			// pane is consumed, so the tools pane never swaps places with an
			// editor.
			key := st.hosts[0]
			st.hosts = st.hosts[1:]
			return key, true
		}
		if len(st.content) > 0 {
			key := st.content[0]
			st.content = st.content[1:]
			return key, true
		}
		key := reg.AddEditor()
		// A fresh editor slot restarts the tool sessions the snapshot hosted
		// as tabs (#1277), mirroring the startup restore of id.Tools; a live
		// content pane re-slotted above keeps its own tabs instead.
		if len(id.Tools) > 0 {
			m.restartToolTabs(reg, key, id.Tools)
		}
		return key, true
	}
	return "", false
}

// restartToolTabs restarts the snapshot's tool sessions as terminal tabs of
// the fresh editor-kind pane key (#1277), dropping the placeholder scratch
// tab once at least one tool spawned — shared by the "tools" (#1989) and
// legacy "editor"+Tools slot resolutions.
func (m *Model) restartToolTabs(reg *pane.Registry, key string, tools []string) {
	inst := reg.Get(key)
	spawned := 0
	for _, tool := range tools {
		entry, ok := toolEntry(tool)
		if !ok {
			continue // tool no longer configured: restores as nothing
		}
		// A parked live global instance re-attaches instead of spawning a
		// duplicate (#1890); restoredToolSession decides.
		inst.AddTerminalTab(m.restoredToolSession(reg, entry))
		spawned++
	}
	if spawned > 0 {
		inst.CloseTab(0) // drop the placeholder scratch tab
	}
}

// spawnShellPane creates a fresh shell terminal pane in the project root,
// the same recipe every runtime terminal creation uses.
func (m *Model) spawnShellPane() string {
	shell := ""
	if v, ok := m.host.Config().Get("terminal.shell"); ok {
		shell = v
	}
	return m.activeWS().Panes.AddTerminal(terminal.Shell(shell), ".", terminalEnv(), m.host.Send)
}

// editorSlotAnchor picks where a fresh editor pane splits in when no live
// pane edits files (#1989): a tool-tab host must not attract the split, so
// the designated default layout is consulted for its editor slot and the new
// editor splits off the nearest layout neighbour with a live counterpart —
// with all editors closed, opening a file recreates the editor in its
// original layout slot. Without a usable default layout the built-in
// explorer+editor arrangement anchors at the explorer; target "" leaves the
// caller its focused-pane fallback. ratio is the anchor split's saved share
// (0 keeps the even split).
func (m *Model) editorSlotAnchor() (target string, zone layout.Zone, ratio float64) {
	if tree, ids, ok := defaultLayoutSnapshot(); ok {
		if target, zone, ratio, ok := m.anchorFromLayout(tree, ids); ok {
			return target, zone, ratio
		}
	}
	if m.liveLeaf(pane.ExplorerKey) {
		r := 0.0
		if m.width > 0 {
			r = float64(explorerWidth) / float64(m.width)
		}
		return pane.ExplorerKey, layout.ZoneRight, r
	}
	return "", m.splitZone, 0
}

// anchorFromLayout resolves the saved layout's editor slot to a live split
// anchor: the path from the slot up to the root is walked inside-out, and the
// first sibling subtree holding a leaf with a live counterpart wins. The zone
// puts the new editor on the side of the anchor the slot occupied; the ratio
// is that split's saved share.
func (m *Model) anchorFromLayout(tree layout.Node, ids map[string]paneIdentity) (string, layout.Zone, float64, bool) {
	slot := editorSlotLeaf(tree, ids)
	if slot == "" {
		return "", 0, 0, false
	}
	type hop struct {
		sibling layout.Node
		zone    layout.Zone
		ratio   float64
	}
	var path []hop // innermost hop first: descend appends on the way back up
	var descend func(n layout.Node) bool
	descend = func(n layout.Node) bool {
		switch t := n.(type) {
		case *layout.Leaf:
			return t.Pane == slot
		case *layout.Split:
			zoneA, zoneB := layout.ZoneLeft, layout.ZoneRight
			if t.Orient == layout.Vertical {
				zoneA, zoneB = layout.ZoneTop, layout.ZoneBottom
			}
			if descend(t.A) {
				path = append(path, hop{sibling: t.B, zone: zoneA, ratio: t.Ratio})
				return true
			}
			if descend(t.B) {
				path = append(path, hop{sibling: t.A, zone: zoneB, ratio: t.Ratio})
				return true
			}
		}
		return false
	}
	if !descend(tree) {
		return "", 0, 0, false
	}
	for _, h := range path {
		for _, key := range layout.Leaves(h.sibling) {
			if live := m.liveCounterpart(ids[key]); live != "" {
				return live, h.zone, h.ratio, true
			}
		}
	}
	return "", 0, 0, false
}

// editorSlotLeaf returns the saved layout's designated editor slot: the first
// editor-kind leaf in walk order. A leaf carrying tool tabs only counts when
// no plain editor slot exists — a legacy snapshot wrote a pure tool host as
// "editor"+Tools (pre-#1989), and that pane is the tools area, not the
// editor area.
func editorSlotLeaf(tree layout.Node, ids map[string]paneIdentity) string {
	plain, tooled := "", ""
	for _, key := range layout.Leaves(tree) {
		id := ids[key]
		if id.Kind != "editor" {
			continue
		}
		if len(id.Tools) == 0 {
			if plain == "" {
				plain = key
			}
		} else if tooled == "" {
			tooled = key
		}
	}
	if plain != "" {
		return plain
	}
	return tooled
}

// liveCounterpart maps one saved-layout identity to a leaf of the current
// tree whose live pane can stand for it, "" when none does.
func (m *Model) liveCounterpart(id paneIdentity) string {
	ws := m.activeWS()
	if ws.Tree == nil {
		return ""
	}
	for _, key := range layout.Leaves(ws.Tree) {
		if inst := ws.Panes.Get(key); inst != nil && identityMatches(id, inst) {
			return key
		}
	}
	return ""
}

// identityMatches reports whether the live pane inst stands for the saved
// identity id when anchoring a fresh editor split (#1989). Editor slots match
// panes hosting file or viewer content, tool-tab identities match live tool
// hosts, terminals and tools match by kind and tool name, and the singleton
// panels match their fixed keys.
func identityMatches(id paneIdentity, inst *pane.Instance) bool {
	switch id.Kind {
	case "tools":
		return toolTabHost(inst)
	case "editor":
		if len(id.Tools) > 0 {
			// Legacy pure tool hosts persist as "editor"+Tools: a live tool
			// host is their counterpart, never an editor pane.
			return toolTabHost(inst)
		}
		switch inst.Kind() {
		case pane.KindMarkdown, pane.KindImage, pane.KindDiff, pane.KindMerge,
			pane.KindArchive, pane.KindData, pane.KindES, pane.KindRemote:
			return true
		}
		return inst.Kind() == pane.KindEditor && !toolTabHost(inst)
	case "terminal":
		return inst.Kind() == pane.KindTerminal && !inst.IsDebugTerm() && inst.Terminal().Tool() == ""
	case "tool":
		return inst.Kind() == pane.KindTerminal && inst.Terminal().Tool() == id.Tool
	default:
		if key := singletonSlotKey(id.Kind); key != "" {
			return inst.Key() == key
		}
	}
	return false
}

// liveLeaf reports whether key is a leaf of the current tree backed by a
// registered instance.
func (m *Model) liveLeaf(key string) bool {
	ws := m.activeWS()
	return ws.Tree != nil && layout.Panes(ws.Tree)[key] && ws.Panes.Get(key) != nil
}

// parentSplit returns the split whose direct child is the leaf key, nil when
// the leaf is the root or absent.
func parentSplit(n layout.Node, key string) *layout.Split {
	sp, ok := n.(*layout.Split)
	if !ok {
		return nil
	}
	if lf, ok := sp.A.(*layout.Leaf); ok && lf.Pane == key {
		return sp
	}
	if lf, ok := sp.B.(*layout.Leaf); ok && lf.Pane == key {
		return sp
	}
	if p := parentSplit(sp.A, key); p != nil {
		return p
	}
	return parentSplit(sp.B, key)
}

// editorPaneTools returns the tool names hosted as terminal tabs of an
// editor-kind pane and the count of its file-backed editor tabs (#1277).
// Plain terminal tabs stay session-local, like in saveLayout.
func editorPaneTools(inst *pane.Instance) (tools []string, files int) {
	for i := 0; i < inst.TabCount(); i++ {
		if tt := inst.TabTerminal(i); tt != nil {
			if tool := tt.Tool(); tool != "" {
				tools = append(tools, tool)
			}
			continue
		}
		if ed := inst.TabEditor(i); ed != nil && ed.HasFile() {
			files++
		}
	}
	return tools, files
}

// toolTabHost reports whether an editor-kind pane hosts nothing but terminal
// tabs with at least one tool session among them — no editor tabs (scratch
// included) and no content tabs (#1989). Such a pane is the layout's tools
// area, not an editor slot: persistence marks it "tools" and editor placement
// never targets it.
func toolTabHost(inst *pane.Instance) bool {
	if inst == nil || inst.Kind() != pane.KindEditor {
		return false
	}
	tools := 0
	for i := 0; i < inst.TabCount(); i++ {
		if inst.TabEditor(i) != nil || inst.TabContent(i) != nil {
			return false
		}
		if tt := inst.TabTerminal(i); tt != nil && tt.Tool() != "" {
			tools++
		}
	}
	return tools > 0
}
