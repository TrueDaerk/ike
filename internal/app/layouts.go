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
	case pane.KindTerminal:
		k := st.mintTerminal()
		if tool := inst.Terminal().Tool(); tool != "" {
			return k, paneIdentity{Kind: "tool", Tool: tool}, true
		}
		return k, paneIdentity{Kind: "terminal"}, true
	case pane.KindEditor, pane.KindMarkdown, pane.KindDiff:
		// Content panes are anonymous editor slots: what files they held is
		// session state, only the space they occupied is layout.
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
// last slot. Tool panes absent from the layout lose their leaf but stay
// registered (the toolhide precedent, #791) — running terminals are never
// killed by applying a layout. Parked workspaces (#777) are untouched.
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
	st := &applyState{tools: map[string][]string{}, used: map[string]bool{}}
	for _, key := range reg.Keys() {
		inst := reg.Get(key)
		if inst == nil {
			continue
		}
		switch inst.Kind() {
		case pane.KindEditor, pane.KindMarkdown, pane.KindDiff:
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
		case pane.KindMarkdown, pane.KindDiff:
			reg.Close(key)
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
		return reg.AddEditor(), true
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
