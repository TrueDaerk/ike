package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"

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
	case pane.KindStructure:
		return singleton(pane.StructureKey, "structure")
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
	case pane.KindEditor, pane.KindMarkdown, pane.KindImage, pane.KindDiff, pane.KindMerge:
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
	shells  []string            // plain shell terminal keys
	tools   map[string][]string // tool pane keys by tool name
	used    map[string]bool     // resolved singleton keys (duplicate guard)
	slots   []string            // resolved leaf keys in walk order
}

// applyLayoutByName re-shapes the ACTIVE workspace to the named saved layout
// (#1175). Open files never close: the session's content panes re-slot into
// the layout's editor slots in order, surplus panes merge their tabs into the
// last slot. Singleton tool panels absent from the layout lose their leaf but
// stay registered (the toolhide precedent, #791) — their toggles resurface
// them. Running terminals are never killed by applying a layout: shells and
// TUI tools that don't fit a slot merge as live tabs into a remaining
// terminal or editor slot (#1275). Parked workspaces (#777) are untouched.
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
	// A selective layout (#1568) grafts the unconsumed live panes — in their
	// current relative arrangement — into the placeholder, so the clone of the
	// pre-apply tree is that arrangement's source of truth.
	var liveClone layout.Node
	if hasFlex {
		liveClone = layout.Clone(ws.Tree)
	}
	st := &applyState{tools: map[string][]string{}, used: map[string]bool{}}
	for _, key := range reg.Keys() {
		inst := reg.Get(key)
		if inst == nil {
			continue
		}
		switch inst.Kind() {
		case pane.KindEditor, pane.KindMarkdown, pane.KindImage, pane.KindDiff, pane.KindMerge:
			st.content = append(st.content, key)
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
		// Surplus content panes: editor panes merge their tabs into the last
		// editor-kind slot (files are sacred, splits are not); markdown/diff
		// viewers close (their content rebuilds from disk on demand). With no
		// editor slot in the layout the surplus panes just stay registered.
		target := lastEditorSlot(reg, st.slots)
		for _, key := range st.content {
			inst := reg.Get(key)
			if inst == nil {
				continue
			}
			switch inst.Kind() {
			case pane.KindEditor:
				if target != nil && target.Key() != key {
					mergeEditorPane(reg, inst, target)
				}
			case pane.KindMarkdown, pane.KindImage, pane.KindDiff, pane.KindMerge:
				reg.Close(key)
			}
		}
		// Surplus running shells and tool panes must stay reachable (#1275):
		// they merge as live terminal tabs into the last terminal slot — which
		// becomes a tab host (#836) — or, when the layout has no terminal slot,
		// into the last editor slot. Sessions never restart. Only a layout with
		// neither slot kind leaves them registered but leafless.
		surplus := append([]string{}, st.shells...)
		toolNames := make([]string, 0, len(st.tools))
		for name := range st.tools {
			toolNames = append(toolNames, name)
		}
		sort.Strings(toolNames)
		for _, name := range toolNames {
			surplus = append(surplus, st.tools[name]...)
		}
		if len(surplus) > 0 {
			dst := lastTerminalSlot(reg, st.slots)
			if dst != nil {
				dst.ConvertToTabHost()
			} else {
				dst = target
			}
			if dst != nil {
				for _, key := range surplus {
					inst := reg.Get(key)
					if inst == nil {
						continue
					}
					if tm, ok := inst.DetachTerminal(); ok {
						reg.Close(key)
						dst.AddTerminalTab(tm)
					}
				}
			}
		}
	}
	// The old hide-all snapshot and zoom describe the old tree.
	m.toolHide = nil
	m.zoomed = ""
	ws.Tree = newTree
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
		if consumed[key] || reg.Get(key) == nil {
			if p, ok := layout.Close(sub, key); ok {
				sub = p
			}
		}
	}
	var leftovers []string
	for _, key := range layout.Leaves(sub) {
		if !consumed[key] && reg.Get(key) != nil {
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
	switch t := n.(type) {
	case *layout.Leaf:
		if t.Pane == flexKey {
			return sub
		}
		return n
	case *layout.Split:
		t.A = replaceFlexLeaf(t.A, sub)
		t.B = replaceFlexLeaf(t.B, sub)
		return t
	}
	return n
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
		// Restart the configured tool in the slot (#741); a tool no longer
		// configured degrades to a fresh shell rather than breaking the apply.
		if entry, ok := toolEntry(id.Tool); ok {
			dir := entry.Cwd
			if dir == "" {
				dir = "."
			}
			argv := append([]string{entry.Command}, entry.Args...)
			return reg.AddTool(entry.Name, argv, dir, toolSpawnEnv(m.pal()), m.host.Send), true
		}
		return m.spawnShellPane(), true
	case "editor", "":
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
			inst := reg.Get(key)
			spawned := 0
			for _, tool := range id.Tools {
				entry, ok := toolEntry(tool)
				if !ok {
					continue // tool no longer configured: restores as nothing
				}
				dir := entry.Cwd
				if dir == "" {
					dir = "."
				}
				argv := append([]string{entry.Command}, entry.Args...)
				inst.AddTerminalTab(reg.NewToolSession(entry.Name, argv, dir, toolSpawnEnv(m.pal()), m.host.Send))
				spawned++
			}
			if spawned > 0 {
				inst.CloseTab(0) // drop the placeholder scratch tab
			}
		}
		return key, true
	}
	return "", false
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

// lastEditorSlot returns the last resolved slot holding an editor-kind
// instance — the merge target for surplus content panes — or nil.
func lastEditorSlot(reg *pane.Registry, slots []string) *pane.Instance {
	for i := len(slots) - 1; i >= 0; i-- {
		if inst := reg.Get(slots[i]); inst != nil && inst.Kind() == pane.KindEditor {
			return inst
		}
	}
	return nil
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

// lastTerminalSlot returns the last resolved slot holding a terminal-kind
// instance — the preferred host for surplus running shells (#1275) — or nil.
func lastTerminalSlot(reg *pane.Registry, slots []string) *pane.Instance {
	for i := len(slots) - 1; i >= 0; i-- {
		if inst := reg.Get(slots[i]); inst != nil && inst.Kind() == pane.KindTerminal {
			return inst
		}
	}
	return nil
}

// mergeEditorPane moves every tab of src into target, then closes src.
// Terminal tabs move live (detach, re-attach — the session never restarts);
// editor tabs re-share their document into a fresh tab on the target, which
// carries text, dirtiness and undo history over (#142's sharing seam). A
// pristine empty pane has nothing worth moving.
func mergeEditorPane(reg *pane.Registry, src, target *pane.Instance) {
	if !src.IsEmptyEditor() {
		// Terminal tabs first: detaching shifts indices, so always take the
		// first remaining one until none are left.
		for {
			moved := false
			for i := 0; i < src.TabCount(); i++ {
				if src.TabTerminal(i) == nil {
					continue
				}
				if tm, ok := src.DetachTerminalTab(i); ok {
					target.AddTerminalTab(tm)
					moved = true
				}
				break
			}
			if !moved {
				break
			}
		}
		for i := 0; i < src.TabCount(); i++ {
			ed := src.TabEditor(i)
			if ed == nil {
				continue
			}
			dst := target.Editor()
			if dst == nil || !target.IsEmptyEditor() {
				dst = target.AddTab()
			}
			dst.ShareDocumentWith(ed)
			if src.TabPinned(i) {
				target.SetTabPinned(target.ActiveTab(), true)
			}
		}
	}
	reg.Close(src.Key())
}
