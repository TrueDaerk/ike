package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/vcspanel"
)

// Layout persistence is runtime UI state, not user configuration, so it lives in
// its own per-project state file rather than settings.toml. The discovery seam
// mirrors what Roadmap 0040 will expose: IKE_CONFIG_DIR (or an explicit path)
// overrides the default location so tests can redirect writes. Save is called
// only on op/drag commit, never per motion frame.
//
// Roadmap 0037 grows the format from a bare tree to a tree plus a per-leaf
// identity side table (kind + file), so dynamically created editor panes restore
// their buffers. Old 0036 files (a bare tree) still load: their leaves are
// inferred (the "explorer" leaf is the explorer, every other leaf an editor with
// no remembered file).

// paneIdentity is the persisted identity of one leaf: its kind and, for an
// editor, the file it held (empty for a scratch buffer). Editor tabs (0190,
// #160) grow it by the ordered tab list: Tabs holds every file-backed tab's
// path in tab order and Active indexes the active one within that list.
// Scratch tabs are not persisted (their text is the crash-recovery side's
// job). Path stays the active tab's file so older builds — and the legacy
// reader below — keep working; files without Tabs restore as single-tab panes.
type paneIdentity struct {
	Kind   string   `json:"kind"`
	Path   string   `json:"path,omitempty"`
	Path2  string   `json:"path2,omitempty"` // diff panes: the right-hand file (#60)
	Tabs   []string `json:"tabs,omitempty"`
	Active int      `json:"active,omitempty"`
}

// persistedLayout is the on-disk layout schema: the encoded split tree plus the
// identity side table keyed by instance key.
type persistedLayout struct {
	Tree  json.RawMessage         `json:"tree"`
	Panes map[string]paneIdentity `json:"panes,omitempty"`
}

// layoutFile returns the path of the per-project layout state file. When
// IKE_CONFIG_DIR is set its value is used as the base directory; otherwise the
// store lives under the project's own ".ike" directory.
func layoutFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "layout.json")
	}
	return filepath.Join(".ike", "layout.json")
}

// loadLayout reads the saved tree and identity table. It returns ok=false on any
// missing, unreadable, or structurally malformed file so the caller falls back
// to the default layout. Identity validation (explorer singleton, well-formed
// editor keys, best-effort file reload) is the caller's job.
func loadLayout() (layout.Node, map[string]paneIdentity, bool) {
	data, err := os.ReadFile(layoutFile())
	if err != nil {
		return nil, nil, false
	}
	// Preferred format: the {tree, panes} wrapper.
	var p persistedLayout
	if json.Unmarshal(data, &p) == nil && len(p.Tree) > 0 {
		tree, leaves, ok := layout.DecodeTree(p.Tree)
		if !ok {
			return nil, nil, false
		}
		return tree, mergeIdentities(leaves, p.Panes), true
	}
	// Legacy 0036 format: a bare tree. Infer identities from the leaf ids.
	tree, leaves, ok := layout.DecodeTree(data)
	if !ok {
		return nil, nil, false
	}
	return tree, mergeIdentities(leaves, nil), true
}

// mergeIdentities builds the identity for every leaf, preferring the saved table
// and inferring from the key when an entry is missing (legacy files, or a saved
// table that drifted from the tree).
func mergeIdentities(leaves []string, saved map[string]paneIdentity) map[string]paneIdentity {
	out := make(map[string]paneIdentity, len(leaves))
	for _, key := range leaves {
		if id, ok := saved[key]; ok && id.Kind != "" {
			out[key] = id
			continue
		}
		out[key] = inferIdentity(key)
	}
	return out
}

// inferIdentity guesses a leaf's identity from its key alone: the explorer key is
// the explorer, everything else an editor with no remembered file.
func inferIdentity(key string) paneIdentity {
	if key == pane.ExplorerKey {
		return paneIdentity{Kind: "explorer"}
	}
	return paneIdentity{Kind: "editor"}
}

// isEditorKey reports whether key is a well-formed editor instance key
// ("editor" or "editor:N").
func isEditorKey(key string) bool {
	return key == "editor" || strings.HasPrefix(key, "editor:")
}

// saveLayout persists the tree plus the identity table built from the registry.
// Errors are swallowed: failing to persist layout must never disrupt the session.
func saveLayout(root layout.Node, reg *pane.Registry) {
	if root == nil {
		return
	}
	treeData, err := layout.Encode(root)
	if err != nil {
		return
	}
	ids := map[string]paneIdentity{}
	for _, key := range reg.Keys() {
		inst := reg.Get(key)
		if inst == nil {
			continue
		}
		switch inst.Kind() {
		case pane.KindExplorer:
			ids[key] = paneIdentity{Kind: "explorer"}
		case pane.KindMarkdown:
			// Path names the previewed source file; restore re-reads it (#62).
			ids[key] = paneIdentity{Kind: "markdown", Path: inst.Preview().Path()}
		case pane.KindDiff:
			// Path/Path2 name the compared files; restore re-reads both (#60).
			ids[key] = paneIdentity{Kind: "diff", Path: inst.Diff().LeftPath(), Path2: inst.Diff().RightPath()}
		case pane.KindTerminal:
			// Path carries the session's origin dir so the restored fresh
			// shell spawns there (#96); the process itself never resurrects.
			ids[key] = paneIdentity{Kind: "terminal", Path: inst.Terminal().Dir()}
		case pane.KindVCS:
			// The slot restores empty and re-feeds from the first status
			// snapshot (0330, #482); Path carries the active tab (#504).
			tab := "changes"
			if inst.VCS().ActiveTab() == vcspanel.TabLog {
				tab = "log"
			}
			ids[key] = paneIdentity{Kind: "vcs", Path: tab}
		case pane.KindEditor:
			id := paneIdentity{Kind: "editor", Path: inst.Editor().Path()}
			for i, ed := range inst.Editors() {
				if !ed.HasFile() {
					continue // scratch tabs restore as nothing, not as ""
				}
				if i == inst.ActiveTab() {
					id.Active = len(id.Tabs)
				}
				id.Tabs = append(id.Tabs, ed.Path())
			}
			ids[key] = id
		}
	}
	data, err := json.Marshal(persistedLayout{Tree: treeData, Panes: ids})
	if err != nil {
		return
	}
	path := layoutFile()
	if dir := filepath.Dir(path); dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	_ = os.WriteFile(path, data, 0o644)
}
