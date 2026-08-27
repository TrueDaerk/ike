package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"ike/internal/layout"
	"ike/internal/pane"
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
// A tab host carrying nothing but tool tabs saves as Kind "tools" (#1989) so
// editor placement can tell it from a real editor slot; older files with the
// pre-#1989 "editor"+Tools shape restore identically.
type paneIdentity struct {
	Kind   string   `json:"kind"`
	Path   string   `json:"path,omitempty"`
	Path2  string   `json:"path2,omitempty"` // diff panes: the right-hand file (#60)
	Rev    string   `json:"rev,omitempty"`   // diff panes: revision backing the left side (#508)
	Rev2   string   `json:"rev2,omitempty"`  // diff panes: revision backing the right side
	Tool   string   `json:"tool,omitempty"`  // tool panes: the configured tool name (#741)
	Tabs   []string `json:"tabs,omitempty"`
	Tools  []string `json:"tools,omitempty"`  // editor panes: tool sessions hosted as tabs (#836), restarted on restore
	Pinned []int    `json:"pinned,omitempty"` // editor panes: indexes into Tabs of pinned tabs (#1172)
	Active int      `json:"active,omitempty"`
	// CTabs holds a tab host's content tabs (#1778) — previews, diffs, data
	// viewers and the like living in the tab strip — each with the identity
	// its dedicated-pane persistence would carry plus its position in the
	// full tab list. Older builds ignore the key and restore file tabs only.
	CTabs []contentTabIdentity `json:"ctabs,omitempty"`
	// ActiveCTab is 1 + the index into CTabs of the active tab when a
	// content tab was active; 0 when a file tab was (Active then indexes
	// Tabs as before).
	ActiveCTab int `json:"activeCtab,omitempty"`
	// ActiveTool is 1 + the index into Tools of the active tab when a tool
	// tab was active; 0 otherwise (#1906). Without it a restored pane always
	// came back on the last restored tool tab, so the tab a user selected in
	// one project was lost on the next switch into it.
	ActiveTool int `json:"activeTool,omitempty"`
}

// contentTabIdentity is the persisted identity of one content tab (#1778):
// the same kind/path fields a dedicated viewer pane persists, plus the tab's
// position and pin.
type contentTabIdentity struct {
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`
	Path2  string `json:"path2,omitempty"`
	Rev    string `json:"rev,omitempty"`
	Rev2   string `json:"rev2,omitempty"`
	Index  int    `json:"index"`
	Pinned bool   `json:"pinned,omitempty"`
}

// contentIdentity is the persisted identity of viewer content — shared by the
// dedicated-pane branches of saveLayout and the content-tab list (#1778).
// ok=false for kinds whose content does not persist by itself.
func contentIdentity(inst *pane.Instance) (paneIdentity, bool) {
	switch inst.Kind() {
	case pane.KindMarkdown:
		// Path names the previewed source file; restore re-reads it (#62).
		return paneIdentity{Kind: "markdown", Path: inst.Preview().Path()}, true
	case pane.KindImage:
		// Path names the previewed image; restore re-decodes it (#1479).
		return paneIdentity{Kind: "image", Path: inst.Image().Path()}, true
	case pane.KindArchive:
		// Path names the listed archive; restore re-reads it (#1762).
		return paneIdentity{Kind: "archive", Path: inst.Archive().Path()}, true
	case pane.KindData:
		// Path names the browsed database; restore re-opens it (#1764).
		return paneIdentity{Kind: "data", Path: inst.Data().Path()}, true
	case pane.KindES:
		// Path names the configured endpoint; restore reconnects to the
		// cluster in the background (#1927).
		return paneIdentity{Kind: "es", Path: inst.ES().Endpoint()}, true
	case pane.KindRemote:
		// Path names the ssh host alias; restore re-dials the host in the
		// background (#1997).
		return paneIdentity{Kind: "remote", Path: inst.Remote().Alias()}, true
	case pane.KindDiff:
		// Path/Path2 name the compared files; Rev/Rev2 mark revision-
		// backed sides so restore re-reads blobs instead of files (#508).
		lr, rr := inst.Diff().Revs()
		return paneIdentity{Kind: "diff", Path: inst.Diff().LeftPath(), Path2: inst.Diff().RightPath(), Rev: lr, Rev2: rr}, true
	}
	return paneIdentity{}, false
}

// contentKindFromString maps a persisted content kind back to its pane.Kind
// (#1778); ok=false for unknown strings (a newer build's kind) and for
// "http" — the HTTP viewer stopped nesting as a tab (#2042), so a legacy
// nested-http tab restores as nothing (the viewer restored empty anyway).
func contentKindFromString(s string) (pane.Kind, bool) {
	switch s {
	case "markdown":
		return pane.KindMarkdown, true
	case "image":
		return pane.KindImage, true
	case "archive":
		return pane.KindArchive, true
	case "data":
		return pane.KindData, true
	case "es":
		return pane.KindES, true
	case "diff":
		return pane.KindDiff, true
	}
	return 0, false
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

// usageFile returns the path of the per-project command-usage counter (#773),
// following the layout store's IKE_CONFIG_DIR redirection seam.
func usageFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "cmdusage.json")
	}
	return filepath.Join(".ike", "cmdusage.json")
}

// fileUsageFile returns the path of the per-project file-usage counter
// (#1419), following the layout store's IKE_CONFIG_DIR redirection seam.
func fileUsageFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "fileusage.json")
	}
	return filepath.Join(".ike", "fileusage.json")
}

// cmdFrecencyFile returns the path of the per-project command-execution
// history (#2153) backing the palette's frecency boost, following the layout
// store's IKE_CONFIG_DIR redirection seam.
func cmdFrecencyFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "cmdfrecency.json")
	}
	return filepath.Join(".ike", "cmdfrecency.json")
}

// winSizeFile returns the path of the per-project floating-window size store
// (#774), following the layout store's IKE_CONFIG_DIR redirection seam.
func winSizeFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "winsize.json")
	}
	return filepath.Join(".ike", "winsize.json")
}

// globalWinSizeFile returns the path of the user-scoped floating-window size
// store (#1714): the last size a window kind was resized to anywhere, used as
// the fallback for projects that carry no delta of their own. It follows the
// IKE_CONFIG_DIR redirection seam like every other state file but under its own
// file name (the project store is winsize.json), and otherwise lives in
// ~/.ike — NOT the project's .ike directory, because the fallback spans
// projects.
func globalWinSizeFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "winsize-global.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ike", "winsize-global.json")
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

// isTerminalKey reports whether key is a well-formed terminal instance key
// ("terminal" or "terminal:N") — an editor identity may live under one when
// a terminal/tool pane was converted into a tab host (#836).
func isTerminalKey(key string) bool {
	return key == "terminal" || strings.HasPrefix(key, "terminal:")
}

// isContentHostKey reports whether key is a well-formed viewer instance key
// ("preview", "diff:2", …) — an editor identity may live under one when a
// viewer pane was converted into a tab host (#1778).
func isContentHostKey(key string) bool {
	for _, base := range []string{"preview", "image", "diff", "archive", "data", "es", "http"} {
		if key == base || strings.HasPrefix(key, base+":") {
			return true
		}
	}
	return false
}

// saveLayout persists the tree plus the identity table built from the registry.
// Errors are swallowed: failing to persist layout must never disrupt the session.
func saveLayout(root layout.Node, reg *pane.Registry) {
	data, ok := encodeLayoutState(root, reg)
	if !ok {
		return
	}
	path := layoutFile()
	if dir := filepath.Dir(path); dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	_ = os.WriteFile(path, data, 0o644)
}

// encodeLayoutState marshals the layout store's on-disk payload without
// writing it — the peek return's unchanged check (#2136) compares this against
// the peek-enter snapshot before deciding whether to persist at all.
func encodeLayoutState(root layout.Node, reg *pane.Registry) ([]byte, bool) {
	if root == nil {
		return nil, false
	}
	treeData, err := layout.Encode(root)
	if err != nil {
		return nil, false
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
		case pane.KindMarkdown, pane.KindImage, pane.KindArchive, pane.KindData, pane.KindES, pane.KindDiff, pane.KindRemote:
			// Viewer panes persist their per-kind identity — the shared
			// convention content tabs reuse (#1778).
			if id, ok := contentIdentity(inst); ok {
				ids[key] = id
			}
		case pane.KindMerge:
			// A merge view is session state (index stages move on): the slot
			// restores as an empty editor pane (#1478).
			ids[key] = paneIdentity{Kind: "editor"}
		case pane.KindTerminal:
			// Path carries the session's origin dir so the restored fresh
			// shell spawns there (#96); the process itself never resurrects.
			// A tool pane (#741) persists its tool name instead and restarts
			// the configured program on restore. The Run tool (#1905) is
			// session state: a program must not re-run itself at startup
			// just because its output was on screen. (The former separate
			// debuggee terminal pane, "debugTerm", is gone — the console
			// lives inside the debug area since #2190; old layouts still
			// carrying the identity prune on restore.)
			if inst.Terminal().Tool() == runToolName {
				ids[key] = paneIdentity{Kind: "runTool"}
			} else if tool := inst.Terminal().Tool(); tool != "" {
				ids[key] = paneIdentity{Kind: "tool", Tool: tool}
			} else {
				ids[key] = paneIdentity{Kind: "terminal", Path: inst.Terminal().Dir()}
			}
		case pane.KindVCS:
			// The slot restores empty and re-feeds from the first status
			// snapshot (0330, #482).
			ids[key] = paneIdentity{Kind: "vcs"}
		case pane.KindDebug:
			// The panel restores empty (#580): its content is session state.
			ids[key] = paneIdentity{Kind: "debug"}
		case pane.KindProblems:
			// The panel restores empty (#1024): diagnostics are session state.
			ids[key] = paneIdentity{Kind: "problems"}
		case pane.KindTests:
			// The panel restores empty (#1911): test results are session state.
			ids[key] = paneIdentity{Kind: "tests"}
		case pane.KindIssues:
			// The panel restores empty (#1934): 'r' re-fetches the listing.
			ids[key] = paneIdentity{Kind: "issues"}
		case pane.KindStructure:
			// The panel restores empty (#1025): the first buffer-change sync
			// re-requests the symbols.
			ids[key] = paneIdentity{Kind: "structure"}
		case pane.KindDOM:
			// The panel restores empty (#1929): the first buffer-change sync
			// reparses the focused HTML buffer.
			ids[key] = paneIdentity{Kind: "dom"}
		case pane.KindDoctor:
			// The panel restores empty (#1991): the trace is session state;
			// the shared log re-wires on restore.
			ids[key] = paneIdentity{Kind: "xdoctor"}
		case pane.KindUsages:
			// The panel restores empty (#1155): find-references results are
			// session state; the next lsp.referencesPanel run re-fills it.
			ids[key] = paneIdentity{Kind: "usages"}
		case pane.KindHTTP:
			// The viewer restores empty (#1250): responses are session
			// state; the next http.run dispatch re-fills it.
			ids[key] = paneIdentity{Kind: "http"}
		case pane.KindBreakpoints:
			// The panel restores seeded from the persisted store (#1377).
			ids[key] = paneIdentity{Kind: "breakpoints"}
		case pane.KindEditor:
			id := paneIdentity{Kind: "editor"}
			if ed := inst.Editor(); ed != nil && !ed.ReadOnly() {
				id.Path = ed.Path()
			}
			// Terminal tabs (#573) are session-local like scratch tabs: their
			// processes never resurrect, so only file-backed tabs persist.
			// Tool sessions hosted as tabs (#836) are the exception — like
			// dedicated tool panes they remember their name and restart the
			// configured program on restore. The Run tool (#1905) is session
			// state either way and restores as nothing.
			for i := 0; i < inst.TabCount(); i++ {
				if tt := inst.TabTerminal(i); tt != nil {
					if tool := tt.Tool(); tool != "" && tool != runToolName {
						if i == inst.ActiveTab() {
							// The selected tool tab is per-project state
							// (#1906): the same convention Active/ActiveCTab
							// use, 1-based so 0 keeps meaning "not a tool tab".
							id.ActiveTool = len(id.Tools) + 1
						}
						id.Tools = append(id.Tools, tool)
					}
					continue
				}
				if c := inst.TabContent(i); c != nil {
					// Content tabs (#1778) persist their viewer identity plus
					// position and pin, so a mixed strip restores mixed.
					cid, ok := contentIdentity(c)
					if !ok {
						continue
					}
					id.CTabs = append(id.CTabs, contentTabIdentity{
						Kind: cid.Kind, Path: cid.Path, Path2: cid.Path2,
						Rev: cid.Rev, Rev2: cid.Rev2,
						Index: i, Pinned: inst.TabPinned(i),
					})
					if i == inst.ActiveTab() {
						id.ActiveCTab = len(id.CTabs)
					}
					continue
				}
				// TabPath, not the editor's: a restored tab never activated
				// since (#2177) still holds its file, just unread, and must
				// persist like any other document tab.
				path := inst.TabPath(i)
				if path == "" {
					continue // scratch/terminal tabs restore as nothing
				}
				if ed := inst.TabEditor(i); ed != nil && ed.ReadOnly() {
					// A read-only preview's path names an archive member, not
					// a file (#1762): restore could never re-read it, so the
					// tab is session state like a scratch tab.
					continue
				}
				if i == inst.ActiveTab() {
					id.Active = len(id.Tabs)
				}
				if inst.TabPinned(i) {
					// Pins (#1172) persist as indexes into the Tabs list, the
					// same convention Active uses; older builds ignore the key.
					id.Pinned = append(id.Pinned, len(id.Tabs))
				}
				id.Tabs = append(id.Tabs, path)
			}
			if toolTabHost(inst) {
				// A pane holding nothing but tool tabs persists as "tools"
				// (#1989), so nothing mistakes it for an editor slot; restore
				// treats it exactly like the old "editor"+Tools shape, which
				// older files keep loading under.
				id.Kind = "tools"
			}
			ids[key] = id
		}
	}
	data, err := json.Marshal(persistedLayout{Tree: treeData, Panes: ids})
	if err != nil {
		return nil, false
	}
	return data, true
}
