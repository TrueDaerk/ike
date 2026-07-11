package app

import (
	"os"

	"ike/internal/cli"
)

// cli_open.go opens the command-line targets at startup (Roadmap 0270, #343):
// `ike file.go:42` starts with the file open at that line. main.go parses argv
// via cli.Parse and hands the targets in after construction — session restore
// already ran in newWithHost, so CLI files win focus over the restored layout.
// The opens go through the standard funnel (openPathAt); the EventFileOpened
// hooks and the initial reparse fire for them in Init like for every file
// already open at launch (#332), so no commands are lost here.

// OpenCLITargets opens targets as tabs in argument order and leaves the first
// one focused with its cursor placed; the explorer reveals it. Line/Col are
// 1-based as typed (0 = unset); out-of-range positions clamp. A nonexistent
// path opens as an empty unsaved buffer with that path, vim-style.
func (m Model) OpenCLITargets(targets []cli.Target) Model {
	if len(targets) == 0 {
		return m
	}
	for _, t := range targets {
		m = m.openCLITarget(t)
	}
	// Re-open the first target: activates its tab and re-places the cursor,
	// so the first file wins focus while tab order stays argument order.
	if len(targets) > 1 {
		m = m.openCLITarget(targets[0])
	}
	m.explorer().Reveal()
	return m
}

// openCLITarget opens one target through the standard open funnel, falling
// back to a fresh unsaved buffer when the path does not exist on disk.
func (m Model) openCLITarget(t cli.Target) Model {
	if _, err := os.Stat(t.Path); err != nil {
		if m.editorForPath(canonicalPath(t.Path)) == nil {
			return m.openMissing(t.Path)
		}
		// Second mention of the same missing path: fall through — openPathAt
		// activates the existing tab (Load never runs for shared buffers).
	}
	// CLI positions are 1-based; the editor is 0-based (unset stays 0 → -1,
	// which SetCursor clamps to the buffer start).
	model, _ := m.openPathAt(t.Path, t.Line-1, t.Col-1)
	if mm, ok := model.(Model); ok {
		return mm
	}
	return m
}

// OpenStdinBuffer opens text as a pathless scratch buffer in the active
// editor pane's tab list and focuses it (`ike -`, #344). The buffer is marked
// dirty and never-saved (RestoreText), so quitting runs the unsaved-changes
// guard and `:w <path>` names it — the same flow crash recovery uses for
// untitled restores.
func (m Model) OpenStdinBuffer(text string) Model {
	key := m.activeEditorKey()
	if key == "" {
		key = m.spawnEditor()
	}
	inst := m.panes.Get(key)
	if inst.Editor().HasFile() {
		inst.AddTab()
		m.installEmitter(key)
	}
	inst.Editor().RestoreText(text)
	m.setFocus(key)
	m.layout()
	return m
}

// openMissing lands a nonexistent path in the active editor pane as an empty
// unsaved buffer — the CLI-only sibling of openInTab, which requires the file
// to be readable.
func (m Model) openMissing(path string) Model {
	path = canonicalPath(path)
	key := m.activeEditorKey()
	if key == "" {
		key = m.spawnEditor()
	}
	inst := m.panes.Get(key)
	if inst.Editor().HasFile() {
		inst.AddTab()
		m.installEmitter(key)
	}
	inst.Editor().NewFile(path)
	m.explorer().SetActive(path)
	m.syncExplorerOpen()
	m.setFocus(key)
	m.layout()
	saveLayout(m.tree, m.panes)
	return m
}
