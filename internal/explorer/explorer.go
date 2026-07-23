// Package explorer implements the file-tree pane: it shows the project directory
// as an expandable tree rooted at a fixed base (the explorer never ascends above
// it), lets the user expand/collapse folders in place with vim-like keys, and
// opens a file by emitting an OpenFileMsg the root model routes to the editor.
package explorer

import (
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/overlay"
	"ike/internal/theme"
	"ike/internal/vcs"
	"ike/internal/watch"
)

// OpenFileMsg is emitted when the user selects a file to open. The root model
// listens for it and forwards the path to the editor. NewPane carries the
// open-target intent (Roadmap 0037): the plain open action leaves it false
// (replace the active editor), a modified open action sets it true (open in a
// fresh split). The explorer only states the intent; the root decides layout.
type OpenFileMsg struct {
	Path    string
	NewPane bool
}

// node is one entry in the tree. Directory children are loaded lazily the first
// time the node is expanded.
type node struct {
	name     string
	path     string
	isDir    bool
	depth    int
	expanded bool
	loaded   bool
	loading  bool // a scan Cmd is in flight for this directory
	children []*node
	modTime  time.Time // directory mtime at last scan; drives auto-refresh polling
	entMod   time.Time // this entry's own mtime, for the "modified" sort (#1037)
}

// Model is the file-explorer pane: an expandable tree rooted at a fixed base.
type Model struct {
	root    *node   // project base; never replaced, never escaped
	rows    []*node // flattened visible nodes, rebuilt on every expand/collapse
	cursor  int     // index into rows
	offset  int     // first visible row (vertical scroll)
	offsetX int     // first visible column (horizontal scroll)
	hover   int     // row index under the mouse pointer, -1 when none
	active  string  // path of the file focused in the editor, "" when none
	width   int
	height  int
	focused bool
	err     error

	open map[string]bool // paths currently open in any editor pane

	// Double-click detection: a single click only selects a row; activating
	// (opening a file, toggling a directory) needs a second click on the same row
	// within doubleClickWindow. now is injectable so tests control the clock.
	lastClickRow int
	lastClickAt  time.Time
	now          func() time.Time

	autoRefresh bool          // poll expanded directories for external changes
	pollEvery   time.Duration // interval between auto-refresh polls
	polling     bool          // a poll loop is running (or armed by Restore)

	showHidden bool // render dot-entries; toggled by explorer.toggleHidden
	// hiddenCfg is the last explorer.show_hidden config string actually applied
	// by Configure. A live reload only re-applies show_hidden when the config
	// value genuinely changed, so an unrelated Reconfigure never clobbers the
	// runtime `.` toggle (#629). "" means "not yet configured".
	hiddenCfg string
	indent    int            // spaces per depth level (config explorer.tree_indent)
	sort      string         // ordering within a level (config explorer.sort)
	icons     bool           // file-type marker glyphs (config explorer.icons, #1046)
	colors    colorTable     // per-filetype colour resolution
	pal       *theme.Palette // active theme (Roadmap 0110); nil = default
	cfgColors colorTable     // [explorer.colors] overrides retained for re-theming
	vcsSnap   *vcs.Snapshot  // git status snapshot (Roadmap 0320); nil = not a repo

	// File-operation state (fileops.go). prompt is the active modal (new-file
	// name entry, or a delete confirmation); ops/redoOps are the undo and redo
	// stacks of completed create/delete/rename operations; trashDir holds
	// trashed entries so any operation can be reversed. See fileops.go.
	prompt     *prompt
	ops        []fileOp
	redoOps    []fileOp
	trashDir   string
	trashSeq   int
	pendingSel string // path to put the cursor on once the next scan rebuilds rows
	sbGrab     int    // thumb grab offset of an active scrollbar drag (#1036)
	pendingG   bool   // first g of the gg top-jump chord is armed (#1032)

	// expand-all state (#1043): the subtree root being recursively expanded
	// and the remaining scan budget bounding it.
	expandAllRoot string
	expandBudget  int

	// Reveal state (#1042). pendingReveal is the absolute path a reveal is
	// descending toward: expanding an unloaded ancestor is async (scanCmd), so
	// applyScan re-enters continueReveal after every scan until the target row
	// exists or the path proves gone. autoReveal mirrors the
	// explorer.auto_reveal config; wantReveal arms a reveal that a Cmd-less
	// call site (SetActive on focus/tab switch, the CLI open flow) requested —
	// the app's Update wrapper drains it via PendingRevealCmd.
	pendingReveal string
	autoReveal    bool
	wantReveal    bool

	// Contiguous multi-select (#1044): the selection is the visible-row range
	// between selAnchor and the cursor, inclusive. -1 means no selection.
	// shift+j/k (and shift+up/down, shift+click) extend it; any plain motion
	// or click collapses it; esc clears it. Rebuilds clamp the anchor; a
	// hidden-toggle or manual refresh collapses the selection (the row set
	// shifts, so a stale range would cover the wrong entries — kept simple).
	selAnchor int
}

// New creates an explorer rooted at dir. The root is marked expanded and a scan
// of its children is kicked off by Init; until the scan result arrives the tree
// shows just the root row. A read error is retained and shown in place of the
// tree.
func New(dir string) Model {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	root := &node{
		name:     filepath.Base(abs),
		path:     abs,
		isDir:    true,
		depth:    0,
		expanded: true,
		loading:  true,
	}
	m := Model{
		root:         root,
		hover:        -1,
		indent:       2,
		sort:         "name",
		colors:       defaultColors(),
		lastClickRow: -1,
		selAnchor:    -1,
		now:          time.Now,
		autoRefresh:  true,
		pollEvery:    2 * time.Second,
	}
	m.rebuild()
	// Leftover trash from previous sessions is unreachable (the undo stacks
	// are in-memory) — clean it, including the legacy root ".ike-trash"
	// (#1038).
	m.purgeStaleTrash()
	return m
}

// ExpandAllMsg recursively expands the selected directory's subtree (#1043).
type ExpandAllMsg struct{}

func (ExpandAllMsg) explorerMsg() {}

// Navigation messages (#1041): the tree motions and open actions as
// registry-dispatched messages, so the keybinding layer can rebind them and
// the cheatsheet lists them. The raw key switch in Update stays as the
// zero-config fallback; registered bindings resolve first in the app's
// keymap layer.
type (
	// CursorMoveMsg moves the cursor by Delta rows (explorer.cursorDown/Up).
	CursorMoveMsg struct{ Delta int }
	// CursorPageMsg pages the cursor (explorer.pageDown/Up; Half = ctrl+d/u).
	CursorPageMsg struct {
		Dir  int
		Half bool
	}
	// CursorBottomMsg jumps to the last row (explorer.bottom, vim G).
	CursorBottomMsg struct{}
	// CursorTopMsg jumps to the first row (explorer.top, vim gg).
	CursorTopMsg struct{}
	// ActivateMsg toggles the selected directory or opens the selected file.
	ActivateMsg struct{}
	// ExpandOrOpenMsg is the l/right semantic: expand a directory, open a file.
	ExpandOrOpenMsg struct{}
	// CollapseOrParentMsg is the h/left semantic.
	CollapseOrParentMsg struct{}
	// OpenInSplitMsg opens the selected file in a fresh split.
	OpenInSplitMsg struct{}
)

func (CursorMoveMsg) explorerMsg()       {}
func (CursorPageMsg) explorerMsg()       {}
func (CursorBottomMsg) explorerMsg()     {}
func (CursorTopMsg) explorerMsg()        {}
func (ActivateMsg) explorerMsg()         {}
func (ExpandOrOpenMsg) explorerMsg()     {}
func (CollapseOrParentMsg) explorerMsg() {}
func (OpenInSplitMsg) explorerMsg()      {}

// ScanDoneMsg carries the result of a directory scan back into the model. It is
// dispatched by the Cmd expand/refresh return; the root model routes it here.
type ScanDoneMsg struct {
	Path    string
	Entries []scanEntry
	ModTime time.Time // the directory's mtime at scan time (auto-refresh baseline)
	Err     error
}

func (ScanDoneMsg) explorerMsg() {}

// scanEntry is the minimal directory entry a scan reports; node construction
// (depth, sort) happens on the update thread, not in the Cmd.
type scanEntry struct {
	name  string
	isDir bool
	mod   time.Time // entry mtime, for the "modified" sort (#1037)
}

// scanCmd reads path's entries off the update loop and returns them as a
// ScanDoneMsg, so a slow or huge directory never blocks the UI.
func scanCmd(path string) tea.Cmd {
	return func() tea.Msg {
		des, err := os.ReadDir(path)
		if err != nil {
			return ScanDoneMsg{Path: path, Err: err}
		}
		es := make([]scanEntry, len(des))
		for i, de := range des {
			es[i] = scanEntry{name: de.Name(), isDir: de.IsDir()}
			if fi, err := de.Info(); err == nil {
				es[i].mod = fi.ModTime()
			}
		}
		var mod time.Time
		if fi, err := os.Stat(path); err == nil {
			mod = fi.ModTime()
		}
		return ScanDoneMsg{Path: path, Entries: es, ModTime: mod}
	}
}

// applyScan installs a completed scan's children onto the matching node and
// rebuilds the visible rows. Unknown paths (a node collapsed/refreshed before
// the scan returned) are ignored. The returned Cmd, when non-nil, is the next
// step of a pending reveal descent (#1042): each landing scan may unlock the
// next unloaded ancestor on the way to the reveal target.
func (m *Model) applyScan(msg ScanDoneMsg) tea.Cmd {
	n := nodeByPath(m.root, msg.Path)
	if n == nil {
		return nil
	}
	n.loading = false
	n.loaded = true
	if msg.Err != nil {
		m.err = msg.Err
		// Continue anyway: the failed directory now reads loaded-and-empty,
		// so a reveal descending through it abandons cleanly.
		return m.continueReveal()
	}
	m.err = nil
	n.modTime = msg.ModTime
	m.setChildren(n, msg.Entries)
	m.rebuild()
	return m.continueReveal()
}

// continueExpandAll resumes an in-flight expand-all (#1043) after a scan
// landed: newly loaded directories under the expand root get expanded and
// scanned in turn; the state clears when nothing is left (or the budget ran
// out).
func (m *Model) continueExpandAll(scanned string) tea.Cmd {
	if m.expandAllRoot == "" {
		return nil
	}
	if scanned != m.expandAllRoot && !strings.HasPrefix(scanned, m.expandAllRoot+string(filepath.Separator)) {
		return nil
	}
	root := nodeByPath(m.root, m.expandAllRoot)
	if root == nil {
		m.expandAllRoot = ""
		return nil
	}
	cmd := m.expandLoaded(root)
	m.rebuild()
	if cmd == nil {
		m.expandAllRoot = ""
	}
	return cmd
}

// setChildren installs sorted child nodes on n from a scan's entries. It is
// shared by the async scan path and the synchronous session-restore path.
// Existing child nodes are reused (matched by path and kind), so a re-scan
// preserves expansion state and already-loaded subtrees instead of collapsing
// everything back to fresh nodes.
func (m *Model) setChildren(n *node, entries []scanEntry) {
	prev := make(map[string]*node, len(n.children))
	for _, c := range n.children {
		prev[c.path] = c
	}
	children := make([]*node, 0, len(entries))
	for _, e := range entries {
		path := filepath.Join(n.path, e.name)
		if old, ok := prev[path]; ok && old.isDir == e.isDir {
			old.entMod = e.mod
			children = append(children, old)
			continue
		}
		children = append(children, &node{
			name:   e.name,
			path:   path,
			isDir:  e.isDir,
			depth:  n.depth + 1,
			entMod: e.mod,
		})
	}
	m.sortChildren(children)
	n.children = children
}

// sortChildren orders one level per explorer.sort (#1037) — "name" (default),
// "type" (extension, then name) or "modified" (newest first, name tiebreak) —
// directories always first.
func (m *Model) sortChildren(children []*node) {
	less := func(a, b *node) bool { return a.name < b.name }
	switch m.sort {
	case "type":
		less = func(a, b *node) bool {
			ea, eb := filepath.Ext(a.name), filepath.Ext(b.name)
			if ea != eb {
				return ea < eb
			}
			return a.name < b.name
		}
	case "modified":
		less = func(a, b *node) bool {
			if !a.entMod.Equal(b.entMod) {
				return a.entMod.After(b.entMod)
			}
			return a.name < b.name
		}
	}
	sort.SliceStable(children, func(i, j int) bool {
		if children[i].isDir != children[j].isDir {
			return children[i].isDir
		}
		return less(children[i], children[j])
	})
}

// resortAll re-sorts every loaded level, for a live explorer.sort change.
func (m *Model) resortAll(n *node) {
	m.sortChildren(n.children)
	for _, c := range n.children {
		m.resortAll(c)
	}
}

// nodeByPath finds the node with the given path in the subtree rooted at n.
func nodeByPath(n *node, path string) *node {
	if n.path == path {
		return n
	}
	for _, c := range n.children {
		if found := nodeByPath(c, path); found != nil {
			return found
		}
	}
	return nil
}

// expand opens a directory node, dispatching a scan Cmd on first use. The
// returned Cmd is nil when nothing needs scanning (leaf, or already loaded).
func (m *Model) expand(n *node) tea.Cmd {
	if !n.isDir {
		return nil
	}
	n.expanded = true
	if n.loaded || n.loading {
		return nil
	}
	n.loading = true
	return scanCmd(n.path)
}

// rebuild flattens the visible tree into m.rows and clamps the cursor. A pending
// selection (set by a file op before its re-scan) snaps the cursor onto the new
// or restored entry once it becomes visible.
func (m *Model) rebuild() {
	m.rows = m.rows[:0]
	m.appendVisible(m.root)
	if m.pendingSel != "" {
		for i, n := range m.rows {
			if n.path == m.pendingSel {
				m.cursor = i
				m.pendingSel = ""
				break
			}
		}
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	// A shrinking row set clamps the multi-select anchor (#1044); an empty
	// tree drops the selection entirely.
	if m.selAnchor >= len(m.rows) {
		m.selAnchor = len(m.rows) - 1
	}
	m.clampScroll()
}

// appendVisible walks the tree depth-first, emitting each node and recursing into
// expanded directories. Hidden (dot-prefixed) entries are skipped unless
// showHidden is on; the root is always emitted.
func (m *Model) appendVisible(n *node) {
	m.rows = append(m.rows, n)
	if n.isDir && n.expanded {
		for _, c := range n.children {
			if !m.showHidden && isHidden(c.name) {
				continue
			}
			m.appendVisible(c)
		}
	}
}

// isHidden reports whether name is a hidden (dot-prefixed) entry.
func isHidden(name string) bool { return strings.HasPrefix(name, ".") }

// Root returns the fixed project base directory.
func (m Model) Root() string { return m.root.path }

// SetSize sets the available width and number of rows.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.clampScroll()
}

// SetFocused toggles whether this pane receives key input.
func (m *Model) SetFocused(f bool) { m.focused = f }

// Init implements tea.Model: it kicks off the root directory scan, unless a
// restored session already loaded the root synchronously (an async re-scan would
// replace the children and discard the restored expansion state) — in that case
// it starts the auto-refresh poll loop instead, which the fresh-scan path starts
// on its first ScanDoneMsg (see startPoll).
func (m Model) Init() tea.Cmd {
	if m.root.loaded {
		return m.schedulePoll() // armed by Restore; nil when auto-refresh is off
	}
	return scanCmd(m.root.path)
}

// Update handles navigation/expand keys, scan results, and explorer command
// messages. It returns a Cmd for any work that must run off the update loop
// (directory scans) or be routed onward (OpenFileMsg).
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ScanDoneMsg:
		// applyScan may hand back a reveal continuation (#1042); an in-flight
		// expand-all (#1043) continues independently.
		reveal := m.applyScan(msg)
		if cmd := m.continueExpandAll(msg.Path); cmd != nil {
			return m, tea.Batch(reveal, cmd, m.startPoll())
		}
		return m, tea.Batch(reveal, m.startPoll())
	case pollMsg:
		return m, m.applyPoll(msg)
	case watch.EventMsg:
		// External directory change (Roadmap 0140, #83): the watcher replaces
		// most manual `r` refreshes; `r` stays as the escape hatch.
		if msg.Kind == watch.DirChanged {
			return m, m.externalRefresh(msg.Path)
		}
		return m, nil
	case ToggleHiddenMsg:
		// Keep the selection on the same entry across the toggle (#1033):
		// the row set shifts when dot-entries appear/vanish, and a cursor
		// that silently lands elsewhere makes the next d/R act on the wrong
		// file. pendingSel reuses the file-op snap; a now-hidden selection
		// falls back to the clamped cursor.
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			m.pendingSel = m.rows[m.cursor].path
		}
		// The row set shifts across the toggle, so the range would cover
		// the wrong entries: the multi-select collapses (#1044).
		m.clearSel()
		m.showHidden = !m.showHidden
		m.rebuild()
		show := m.showHidden
		// Persist right away so a kill/crash keeps the toggle (#629).
		return m, func() tea.Msg { return HiddenToggledMsg{ShowHidden: show} }
	case CollapseAllMsg:
		m.collapseAll()
		return m, nil
	case ExpandAllMsg:
		return m, m.expandAllUnderSelection()
	case CursorMoveMsg:
		m.clearSel()
		m.moveCursor(msg.Delta)
		return m, nil
	case CursorPageMsg:
		m.clearSel()
		m.movePage(msg.Dir, msg.Half)
		return m, nil
	case CursorTopMsg:
		m.clearSel()
		m.moveCursor(-len(m.rows))
		return m, nil
	case CursorBottomMsg:
		m.clearSel()
		m.moveCursor(len(m.rows))
		return m, nil
	case ActivateMsg:
		return m.activate()
	case ExpandOrOpenMsg:
		return m.expandOrOpen()
	case CollapseOrParentMsg:
		m.collapseOrParent()
		return m, nil
	case OpenInSplitMsg:
		return m.openInSplit()
	case RefreshMsg:
		m.clearSel() // a manual refresh collapses the multi-select (#1044)
		return m, m.refresh()
	case RevealMsg:
		return m, m.reveal()
	case NewFileMsg:
		m.promptNewEntry(false)
		return m, nil
	case NewDirMsg:
		m.promptNewEntry(true)
		return m, nil
	case DeleteMsg:
		m.promptDelete()
		return m, nil
	case RenameMsg:
		m.promptRename()
		return m, nil
	case RenamePathMsg:
		info, err := os.Lstat(msg.Path)
		if err != nil {
			m.fail(err)
			return m, nil
		}
		return m, m.renameEntry(msg.Path, msg.Name, info.IsDir())
	case MoveToMsg:
		info, err := os.Lstat(msg.Path)
		if err != nil {
			m.fail(err)
			return m, nil
		}
		return m, m.moveEntry(msg.Path, msg.TargetDir, info.IsDir())
	case UndoMsg:
		return m, m.undo()
	case RedoMsg:
		return m, m.redo()
	case tea.KeyPressMsg:
		// A modal prompt captures every key (filename entry, y/n, esc) until it
		// is accepted or cancelled, ahead of any navigation binding.
		if m.prompt != nil {
			return m, m.handlePromptKey(msg)
		}
		key := msg.String()
		// The vim gg chord (#1032): a first g arms, a second within the
		// switch below jumps to the top; any other key disarms.
		armedG := m.pendingG
		m.pendingG = false
		// Contiguous multi-select (#1044): shifted vertical motions extend
		// the range from an anchor; every other key — plain motions, esc —
		// collapses it before the normal handling below.
		switch key {
		case "J", "shift+down":
			m.extendSel(1)
			return m, nil
		case "K", "shift+up":
			m.extendSel(-1)
			return m, nil
		}
		m.clearSel()
		switch key {
		case "down", "j":
			m.moveCursor(1)
		case "up", "k":
			m.moveCursor(-1)
		case "g":
			if armedG {
				m.moveCursor(-len(m.rows)) // gg: top
			} else {
				m.pendingG = true
			}
		case "G":
			m.moveCursor(len(m.rows)) // bottom
		case "pgdown", "ctrl+d":
			m.movePage(1, key == "ctrl+d")
		case "pgup", "ctrl+u":
			m.movePage(-1, key == "ctrl+u")
		case "enter":
			return m.activate()
		case "l", "right":
			return m.expandOrOpen()
		case "h", "left":
			m.collapseOrParent()
		case "o":
			// Modified open: request the file in a fresh split rather than replacing
			// the active editor. A no-op on directories. The concrete binding is
			// owned by Roadmap 0080; "o" is the placeholder.
			return m.openInSplit()
		}
	}
	return m, nil
}

// Selected returns the selected entry's path and kind, for app commands that
// act on the explorer's selection (file.move, #175). ok is false with an empty
// tree or when the root itself is selected — the root is never movable.
func (m *Model) Selected() (path string, isDir bool, ok bool) {
	n := m.current()
	if n == nil || n == m.root {
		return "", false, false
	}
	return n.path, n.isDir, true
}

func (m *Model) current() *node {
	if len(m.rows) == 0 {
		return nil
	}
	// Heal a stale out-of-range cursor instead of panicking (#949): any path
	// that shrank rows (or advanced the cursor past them) clamps here.
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	return m.rows[m.cursor]
}

// extendSel grows the contiguous selection (#1044) by one shifted motion:
// the first extension anchors at the current cursor row, then the cursor
// moves, so the selection is always the anchor..cursor range.
func (m *Model) extendSel(delta int) {
	if len(m.rows) == 0 {
		return
	}
	if m.selAnchor < 0 {
		m.selAnchor = m.cursor
	}
	m.moveCursor(delta)
}

// clearSel collapses the multi-select range back to the bare cursor.
func (m *Model) clearSel() { m.selAnchor = -1 }

// selRange returns the inclusive visible-row bounds of the active contiguous
// selection (anchor..cursor, either direction); ok is false when none is
// active.
func (m Model) selRange() (lo, hi int, ok bool) {
	if m.selAnchor < 0 || len(m.rows) == 0 {
		return 0, 0, false
	}
	lo, hi = m.selAnchor, m.cursor
	if lo > hi {
		lo, hi = hi, lo
	}
	return clamp(lo, 0, len(m.rows)-1), clamp(hi, 0, len(m.rows)-1), true
}

// inSelRange reports whether visible row i is part of the active selection.
func (m Model) inSelRange(i int) bool {
	lo, hi, ok := m.selRange()
	return ok && i >= lo && i <= hi
}

// delTarget is one entry a multi-row file operation acts on, captured by path
// so a rescan between prompt and accept cannot swap the node out from under
// the closure.
type delTarget struct {
	path  string
	isDir bool
}

// selTargets resolves the entries a selection-wide operation acts on: every
// row of the active range, minus the root and minus entries nested under
// another selected directory — trashing the ancestor already moves its
// subtree, so a nested target's own rename would fail. Rows are in tree
// order, so ancestors always precede their descendants.
func (m *Model) selTargets() []delTarget {
	lo, hi, ok := m.selRange()
	if !ok {
		return nil
	}
	var ts []delTarget
	for i := lo; i <= hi; i++ {
		n := m.rows[i]
		if n == m.root {
			continue
		}
		nested := false
		for _, t := range ts {
			if t.isDir && strings.HasPrefix(n.path, t.path+string(filepath.Separator)) {
				nested = true
				break
			}
		}
		if !nested {
			ts = append(ts, delTarget{path: n.path, isDir: n.isDir})
		}
	}
	return ts
}

func (m *Model) moveCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	m.clampScroll()
}

// movePage moves the cursor by one page (PageUp/PageDown) or half a page
// (ctrl+u/ctrl+d, vim-style), clamped like moveCursor (#1032).
func (m *Model) movePage(dir int, half bool) {
	_, textH, _, _, _ := m.viewport()
	step := textH
	if half {
		step = maxz(textH / 2)
	}
	if step < 1 {
		step = 1
	}
	m.moveCursor(dir * step)
}

// activate toggles a directory (expand/collapse) or opens a file (enter).
func (m Model) activate() (Model, tea.Cmd) {
	n := m.current()
	if n == nil {
		return m, nil
	}
	if n.isDir {
		var cmd tea.Cmd
		if n.expanded {
			n.expanded = false
		} else {
			cmd = m.expand(n)
		}
		m.rebuild()
		return m, cmd
	}
	return m, openCmd(n.path)
}

// expandOrOpen expands a collapsed directory, steps into the first child of an
// expanded one, or opens a file (l / right).
func (m Model) expandOrOpen() (Model, tea.Cmd) {
	n := m.current()
	if n == nil {
		return m, nil
	}
	if !n.isDir {
		return m, openCmd(n.path)
	}
	if !n.expanded {
		cmd := m.expand(n)
		m.rebuild()
		return m, cmd
	}
	// Step onto the first child only when one is actually VISIBLE: a dir
	// whose children are all hidden (dot-entries with show-hidden off, e.g.
	// a project holding only .git) has children but no child row — stepping
	// blindly ran the cursor off the rows slice (#949).
	if m.cursor+1 < len(m.rows) && m.rows[m.cursor+1].depth > n.depth {
		m.cursor++
		m.clampScroll()
	}
	return m, nil
}

// collapseOrParent collapses an expanded directory, otherwise jumps to the
// parent node. It never moves above the root.
func (m *Model) collapseOrParent() {
	n := m.current()
	if n == nil {
		return
	}
	if n.isDir && n.expanded {
		n.expanded = false
		m.rebuild()
		return
	}
	m.jumpToParent()
}

// jumpToParent moves the cursor to the nearest preceding row one depth shallower.
func (m *Model) jumpToParent() {
	depth := m.rows[m.cursor].depth
	for i := m.cursor - 1; i >= 0; i-- {
		if m.rows[i].depth < depth {
			m.cursor = i
			m.clampScroll()
			return
		}
	}
}

// openInSplit opens the file under the cursor in a new pane (NewPane intent). It
// is a no-op on a directory or an empty tree.
func (m Model) openInSplit() (Model, tea.Cmd) {
	n := m.current()
	if n == nil || n.isDir {
		return m, nil
	}
	return m, openSplitCmd(n.path)
}

func openCmd(path string) tea.Cmd {
	return func() tea.Msg { return OpenFileMsg{Path: path} }
}

func openSplitCmd(path string) tea.Cmd {
	return func() tea.Msg { return OpenFileMsg{Path: path, NewPane: true} }
}

// collapseAll folds the whole tree back to the root and parks the cursor there.
func (m *Model) collapseAll() {
	collapse(m.root)
	m.root.expanded = true // the root stays open; the tree is anchored to it
	m.cursor = 0
	m.offset = 0
	m.rebuild()
}

// expandAllUnderSelection recursively expands the selected directory's
// subtree (#1043; the root when a file or the root row is selected). Lazy
// loading makes it asynchronous: loaded levels expand immediately, unloaded
// ones kick scans and applyScan continues the descent while expandAllRoot is
// set. expandBudget bounds runaway trees.
func (m *Model) expandAllUnderSelection() tea.Cmd {
	n := m.current()
	if n == nil || !n.isDir {
		n = m.root
	}
	m.expandAllRoot = n.path
	m.expandBudget = maxExpandAllScans
	cmd := m.expandLoaded(n)
	m.rebuild()
	return cmd
}

// maxExpandAllScans bounds how many directory scans one expand-all may spawn.
const maxExpandAllScans = 200

// expandLoaded expands n's loaded subtree and returns scans for the unloaded
// directories it uncovers, decrementing the expand budget per scan.
func (m *Model) expandLoaded(n *node) tea.Cmd {
	if !n.isDir {
		return nil
	}
	n.expanded = true
	if !n.loaded {
		if m.expandBudget <= 0 {
			return nil
		}
		m.expandBudget--
		n.loading = true
		return scanCmd(n.path)
	}
	var cmds []tea.Cmd
	for _, c := range n.children {
		if c.isDir {
			if cmd := m.expandLoaded(c); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func collapse(n *node) {
	n.expanded = false
	for _, c := range n.children {
		collapse(c)
	}
}

// refresh re-scans the selected directory (or its parent, when a file is
// selected) and every expanded directory beneath it, so external changes show
// up. Children are merged in place (setChildren reuses matching nodes), so
// expansion state survives the refresh.
func (m *Model) refresh() tea.Cmd {
	n := m.current()
	if n == nil {
		n = m.root
	}
	if !n.isDir {
		n = m.parentOf(n)
	}
	if n == nil {
		n = m.root
	}
	return m.rescanSubtree(n)
}

// rescanSubtree dispatches scans for n and every expanded, already-loaded
// directory below it. Existing children stay visible until each scan result
// merges over them.
func (m *Model) rescanSubtree(n *node) tea.Cmd {
	var cmds []tea.Cmd
	var walk func(d *node)
	walk = func(d *node) {
		if !d.isDir || !d.loaded {
			return
		}
		d.loading = true
		cmds = append(cmds, scanCmd(d.path))
		for _, c := range d.children {
			if c.expanded {
				walk(c)
			}
		}
	}
	if n.isDir && !n.loaded {
		// A never-loaded directory (collapsed ancestor): scan just it.
		n.loading = true
		return scanCmd(n.path)
	}
	walk(n)
	return tea.Batch(cmds...)
}

// externalRefresh re-scans one directory the watcher reported changed — just
// the affected subtree entry, not a full re-scan. setChildren's merge keeps
// expansion state and loaded subtrees; pendingSel keeps the cursor on its
// entry across the rebuild. A node that is absent, never loaded, or already
// scanning is skipped (a collapsed directory picks the change up when first
// expanded).
func (m *Model) externalRefresh(path string) tea.Cmd {
	n := nodeByPath(m.root, path)
	if n == nil || !n.isDir || !n.loaded || n.loading {
		return nil
	}
	if cur := m.current(); cur != nil {
		m.pendingSel = cur.path
	}
	n.loading = true
	return scanCmd(n.path)
}

// dirStamp pairs a directory path with the mtime it had at its last scan; the
// poll Cmd compares the two off the update loop.
type dirStamp struct {
	path string
	mod  time.Time
}

// pollMsg reports directories whose mtime changed since their last scan. It is
// the auto-refresh heartbeat: handling it re-scans the changed directories and
// schedules the next poll.
type pollMsg struct {
	changed []string
}

func (pollMsg) explorerMsg() {}

// startPoll begins the auto-refresh loop once, on the first completed scan.
// Later scans see the polling flag set and return nil, so only one loop runs.
func (m *Model) startPoll() tea.Cmd {
	if !m.autoRefresh || m.polling {
		return nil
	}
	m.polling = true
	return m.schedulePoll()
}

// schedulePoll snapshots the mtimes of every visible loaded directory and
// returns a Cmd that re-checks them after the poll interval. Returns nil when
// auto-refresh is disabled.
func (m *Model) schedulePoll() tea.Cmd {
	if !m.autoRefresh {
		return nil
	}
	var stamps []dirStamp
	var walk func(n *node)
	walk = func(n *node) {
		if !n.isDir || !n.loaded {
			return
		}
		stamps = append(stamps, dirStamp{path: n.path, mod: n.modTime})
		if !n.expanded {
			return
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(m.root)
	interval := m.pollEvery
	return func() tea.Msg {
		// Idle-friendly loop (#1001): an unchanged tree re-checks in place
		// instead of waking the program's whole Update/View cycle every
		// interval — with many panes those idle repaints add up. At most one
		// wake per pollIdleRounds intervals refreshes the stamp set, so
		// newly expanded directories join monitoring on that wake.
		for i := 0; i < pollIdleRounds; i++ {
			time.Sleep(interval)
			var changed []string
			for _, s := range stamps {
				fi, err := os.Stat(s.path)
				if err != nil {
					// The directory vanished: its parent's listing changed.
					changed = append(changed, filepath.Dir(s.path))
					continue
				}
				if !fi.ModTime().Equal(s.mod) {
					changed = append(changed, s.path)
				}
			}
			if len(changed) > 0 {
				return pollMsg{changed: changed}
			}
		}
		return pollMsg{}
	}
}

// pollIdleRounds is how many quiet poll intervals run inside one Cmd before
// the loop wakes the app anyway to refresh its directory-stamp snapshot
// (#1001): 30 × the 2s default ≈ one idle wake per minute instead of one
// per interval.
const pollIdleRounds = 30

// applyPoll re-scans every changed directory still present in the tree and
// schedules the next poll tick.
func (m *Model) applyPoll(msg pollMsg) tea.Cmd {
	m.polling = true
	var cmds []tea.Cmd
	seen := map[string]bool{}
	for _, p := range msg.changed {
		if seen[p] {
			continue
		}
		seen[p] = true
		n := nodeByPath(m.root, p)
		if n == nil || !n.isDir || !n.loaded || n.loading {
			continue
		}
		n.loading = true
		cmds = append(cmds, scanCmd(n.path))
	}
	cmds = append(cmds, m.schedulePoll())
	return tea.Batch(cmds...)
}

// Reveal arms a reveal of the active file — the programmatic twin of the
// explorer.reveal command, used by the CLI open flow (Roadmap 0270). The walk
// itself starts when the app drains PendingRevealCmd on the next settled
// update pass, because callers here cannot dispatch the expansion scans.
func (m *Model) Reveal() { m.wantReveal = true }

// PendingRevealCmd returns the reveal walk armed by Reveal or by SetActive
// under explorer.auto_reveal (#1042), or nil when none is armed. The app's
// Update wrapper drains it once per settled pass — mirroring the structure
// sync — since the arming call sites (focus changes, tab switches) cannot
// return Cmds themselves.
func (m *Model) PendingRevealCmd() tea.Cmd {
	if !m.wantReveal {
		return nil
	}
	m.wantReveal = false
	return m.reveal()
}

// reveal puts the cursor on the row of the currently open file, expanding
// every collapsed ancestor on the way (#1042). Lazy loading makes the descent
// async: the returned Cmd scans the first unloaded ancestor, and applyScan
// re-enters continueReveal after every scan until the target row exists.
func (m *Model) reveal() tea.Cmd {
	if m.active == "" {
		return nil
	}
	m.pendingReveal = m.active
	return m.continueReveal()
}

// continueReveal advances a pending reveal: it walks from the root toward the
// target, expanding each ancestor. A loaded ancestor expands in place; the
// first unloaded one dispatches its scan and pauses the walk (applyScan calls
// back in when the result lands, so each scan resumes one level deeper). A
// target that left the tree — deleted, renamed, or outside the root — abandons
// the reveal, so the descent is bounded by the ancestor depth.
func (m *Model) continueReveal() tea.Cmd {
	path := m.pendingReveal
	if path == "" {
		return nil
	}
	rel, err := filepath.Rel(m.root.path, path)
	if err != nil || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		m.abandonReveal()
		return nil
	}
	n := m.root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if !n.isDir {
			// A file where a directory was expected: the path is stale.
			m.abandonReveal()
			return nil
		}
		if !n.loaded {
			if n.loading {
				return nil // a scan is in flight; applyScan resumes the walk
			}
			// pendingSel doubles as the cursor snap for the row rebuilds the
			// landing scans trigger.
			m.pendingSel = path
			cmd := m.expand(n)
			m.rebuild()
			return cmd
		}
		n.expanded = true
		next := childNamed(n, part)
		if next == nil {
			// The path vanished under us (or never existed): give up cleanly.
			m.abandonReveal()
			m.rebuild()
			return nil
		}
		n = next
	}
	// Every ancestor is loaded and expanded: the target row exists (unless the
	// hidden-files filter conceals it — then the cursor simply stays put).
	m.pendingReveal = ""
	m.pendingSel = ""
	m.rebuild()
	for i, r := range m.rows {
		if r.path == path {
			m.cursor = i
			m.clampScroll()
			break
		}
	}
	return nil
}

// childNamed returns n's direct child with the given base name, or nil.
func childNamed(n *node, name string) *node {
	for _, c := range n.children {
		if c.name == name {
			return c
		}
	}
	return nil
}

// abandonReveal drops a pending reveal whose target is unreachable, including
// the cursor snap it armed.
func (m *Model) abandonReveal() {
	if m.pendingSel == m.pendingReveal {
		m.pendingSel = ""
	}
	m.pendingReveal = ""
}

// parentOf returns the parent node of target, or nil for the root / not found.
func (m *Model) parentOf(target *node) *node {
	var find func(n *node) *node
	find = func(n *node) *node {
		for _, c := range n.children {
			if c == target {
				return n
			}
			if p := find(c); p != nil {
				return p
			}
		}
		return nil
	}
	return find(m.root)
}

// clampScroll keeps the cursor within the visible window and the scroll offset
// within the renderable range. Clamping to maxOff is essential: View() clamps a
// stale offset only for display, but MouseClick and hover hit-testing read the
// raw offset, so an offset past the last page would make clicks land on rows far
// below the ones actually shown.
func (m *Model) clampScroll() {
	_, textH, _, _, _ := m.viewport()
	if textH <= 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+textH {
		m.offset = m.cursor - textH + 1
	}
	if maxOff := len(m.rows) - textH; m.offset > maxOff {
		m.offset = maxOff
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// rowParts is the plain (unstyled) content of a row, split where styling
// differs: the guides (one indent-guide segment per ancestor level), the
// two-cell expand marker (plus, with explorer.icons on, a two-cell file-type
// glyph, #1046), the name (directories gain a trailing slash), and — on the
// root row only — the dimmed project-path context suffix (#1046). The split
// lets View paint the guides in the semantic IndentGuide colour (#1050,
// mirroring the editor), decorate just the name (the open-file underline)
// and dim just the context, without touching guides or padding.
func (m Model) rowParts(n *node) (guides, mark, name, ctx string) {
	var b strings.Builder
	for d := 0; d < n.depth; d++ {
		b.WriteString("│")
		b.WriteString(strings.Repeat(" ", maxz(m.indent-1)))
	}
	name = n.name
	if n.isDir {
		name += "/"
	}
	mark = m.marker(n)
	if m.icons {
		mark += typeGlyph(n) + " "
	}
	if n == m.root {
		ctx = m.rootContext(ansi.StringWidth(mark) + ansi.StringWidth(name))
	}
	return b.String(), mark, name, ctx
}

// rowText is the full plain content of a row. It is the single source of truth
// for width measurement, so clipping and the scrollbars agree with rendering.
func (m Model) rowText(n *node) string {
	guides, mark, name, ctx := m.rowParts(n)
	return guides + mark + name + ctx
}

// minRootContextWidth is the narrowest pane that still shows the root row's
// project-path context (#1046); anything narrower suppresses it entirely.
const minRootContextWidth = 30

// rootContext is the dimmed " — ~/path" suffix the root row carries (#1046):
// the project root, home-abbreviated, JetBrains-style path context. used is
// the display width the row already spends on marker + name. The suffix is
// pre-truncated to the remaining pane width (one column reserved for a
// possible scrollbar) so it never widens the content — the horizontal
// scrollbar keeps tracking real tree overflow only — and it is suppressed
// when the pane is too narrow to read it.
func (m Model) rootContext(used int) string {
	if m.width < minRootContextWidth {
		return ""
	}
	avail := m.width - 1 - used
	if avail < 4 {
		return ""
	}
	return ansi.Truncate(" — "+abbrevHome(m.root.path), avail, "…")
}

// abbrevHome replaces a leading home directory with "~", the conventional
// terminal abbreviation.
func abbrevHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return p
}

// marker is the two-cell expand glyph for a row: a caret for directories,
// blank for files — and for directories known to hold nothing visible
// (#1039): a loaded dir with no (filter-surviving) children shows no
// expander, JetBrains-style, instead of expanding to nothing. Unloaded dirs
// keep the caret (contents unknown until the first scan).
func (m Model) marker(n *node) string {
	if !n.isDir {
		return "  "
	}
	if n.loaded && !m.hasVisibleChildren(n) {
		return "  "
	}
	if n.expanded {
		return "▾ "
	}
	return "▸ "
}

// hasVisibleChildren reports whether n has at least one child surviving the
// hidden-files filter.
func (m Model) hasVisibleChildren(n *node) bool {
	for _, c := range n.children {
		if m.showHidden || !isHidden(c.name) {
			return true
		}
	}
	return false
}

// contentWidth is the display width of the widest visible row.
func (m Model) contentWidth() int {
	w := 0
	for _, n := range m.rows {
		if cw := ansi.StringWidth(m.rowText(n)); cw > w {
			w = cw
		}
	}
	return w
}

// viewport resolves the inner text area: its width/height after reserving a
// column for the vertical scrollbar and a row for the horizontal one, whether
// each bar is needed, and the total content width. Two passes settle the mutual
// dependence (reserving for one bar can push the other axis into overflow).
func (m Model) viewport() (textW, textH int, needV, needH bool, contentW int) {
	vw, vh := m.width, m.height
	if vw < 1 {
		vw = 1
	}
	if vh < 1 {
		vh = 1
	}
	contentW = m.contentWidth()
	total := len(m.rows)
	for i := 0; i < 2; i++ {
		textW, textH = vw, vh
		if needV {
			textW--
		}
		if needH {
			textH--
		}
		needV = total > textH
		needH = contentW > textW
	}
	textW, textH = vw, vh
	if needV {
		textW--
	}
	if needH {
		textH--
	}
	if textW < 1 {
		textW = 1
	}
	if textH < 1 {
		textH = 1
	}
	return
}

// scrollThumb sizes and positions a scrollbar thumb on a track of the given
// length for a window of visible cells over a total content size at offset.
func scrollThumb(track, total, visible, offset int) (start, length int) {
	if track <= 0 {
		return 0, 0
	}
	if total <= visible {
		return 0, track
	}
	length = track * visible / total
	if length < 1 {
		length = 1
	}
	if length > track {
		length = track
	}
	maxOff := total - visible
	start = (track - length) * offset / maxOff
	if start < 0 {
		start = 0
	}
	if start > track-length {
		start = track - length
	}
	return
}

// ScrollBy moves the vertical viewport by delta rows (positive scrolls down)
// without moving the cursor — the way a mouse wheel scrolls independently of the
// selection.
func (m *Model) ScrollBy(delta int) {
	_, textH, _, _, _ := m.viewport()
	maxOff := len(m.rows) - textH
	if maxOff < 0 {
		maxOff = 0
	}
	m.offset += delta
	if m.offset > maxOff {
		m.offset = maxOff
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// ScrollXBy moves the horizontal viewport by delta columns (positive scrolls
// right). It is the seam for shift-wheel / horizontal-wheel scrolling.
func (m *Model) ScrollXBy(delta int) {
	textW, _, _, _, contentW := m.viewport()
	maxOff := contentW - textW
	if maxOff < 0 {
		maxOff = 0
	}
	m.offsetX = clamp(m.offsetX+delta, 0, maxOff)
}

// SetActive marks path as the file currently open in the editor so its row is
// highlighted distinctly from the cursor and hover. Pass "" to clear it. With
// explorer.auto_reveal on, a genuinely new active file also arms a reveal
// (#1042) — the JetBrains autoscroll-from-source — drained by
// PendingRevealCmd, since SetActive's call sites cannot dispatch Cmds.
func (m *Model) SetActive(path string) {
	if m.autoReveal && path != "" && path != m.active {
		m.wantReveal = true
	}
	m.active = path
}

// SetOpen replaces the set of files currently open in any editor pane. Open
// rows render underlined and italic (in addition to any cursor/hover/active
// highlight). A stale active path not in the set is cleared.
func (m *Model) SetOpen(paths []string) {
	m.open = make(map[string]bool, len(paths))
	for _, p := range paths {
		m.open[p] = true
	}
	if m.active != "" && !m.open[m.active] {
		m.active = ""
	}
}

// IsOpen reports whether path is marked open in an editor.
func (m Model) IsOpen(path string) bool { return m.open[path] }

// SetHoverAt records the row under the mouse at content-local coordinates, or
// clears the hover when the pointer is off a content row.
func (m *Model) SetHoverAt(x, y int) {
	textW, textH, _, _, _ := m.viewport()
	if x < 0 || y < 0 || x >= textW || y >= textH {
		m.hover = -1
		return
	}
	if i := m.offset + y; i < len(m.rows) {
		m.hover = i
		return
	}
	m.hover = -1
}

// ClearHover drops any hover highlight (pointer left the pane).
func (m *Model) ClearHover() { m.hover = -1 }

// HoverRow returns the visible row index under the pointer, or -1 when none.
func (m Model) HoverRow() int { return m.hover }

// Active returns the path of the file currently marked open, or "" when none.
func (m Model) Active() string { return m.active }

// doubleClickWindow is the maximum delay between two clicks on the same row for
// the pair to count as a double-click.
const doubleClickWindow = 400 * time.Millisecond

// MouseClick handles a left-press at content-local coordinates (0-based from the
// top-left of the tree area). A press on a scrollbar jumps that axis. A single
// press on a row only selects it; activating (opening a file, toggling a
// directory) takes a double-click — except on a directory's expand caret, which
// toggles on a single click.
func (m Model) MouseClick(x, y int) (Model, tea.Cmd) {
	if len(m.rows) == 0 || x < 0 || y < 0 {
		return m, nil
	}
	textW, textH, needV, needH, contentW := m.viewport()

	if needV && x == textW && y < textH { // vertical scrollbar track
		// Delegated to ScrollbarPress (#1036): thumb press starts a drag at
		// the app layer, track press keeps the click-to-jump.
		m.ScrollbarPress(y)
		return m, nil
	}
	if needH && y == textH && x < textW { // horizontal scrollbar track
		if maxOff := contentW - textW; maxOff > 0 && textW > 1 {
			m.offsetX = clamp(x*maxOff/(textW-1), 0, maxOff)
		}
		return m, nil
	}
	if x >= textW || y >= textH { // chrome / empty space
		return m, nil
	}
	i := m.offset + y
	if i >= len(m.rows) {
		return m, nil
	}
	m.clearSel() // a plain click collapses the multi-select (#1044)
	m.cursor = i
	m.clampScroll()

	n := m.rows[i]
	if n.isDir && m.onMarker(n, x) {
		// The expand caret answers to a single click, like the IDE tree it mimics.
		m.resetClick()
		return m.activate()
	}
	clickAt := m.now()
	if i == m.lastClickRow && clickAt.Sub(m.lastClickAt) <= doubleClickWindow {
		m.resetClick()
		return m.activate()
	}
	m.lastClickRow, m.lastClickAt = i, clickAt
	return m, nil
}

// ShiftClick extends the contiguous multi-select to the clicked row (#1044):
// with no active selection it anchors at the current cursor first, so a plain
// click followed by a shift+click selects the range between them. Presses on
// chrome or below the rows are ignored.
func (m *Model) ShiftClick(x, y int) {
	textW, textH, _, _, _ := m.viewport()
	if x < 0 || y < 0 || x >= textW || y >= textH {
		return
	}
	i := m.offset + y
	if i >= len(m.rows) {
		return
	}
	if m.selAnchor < 0 {
		m.selAnchor = m.cursor
	}
	m.cursor = i
	m.clampScroll()
	m.resetClick() // a shift+click never chains into a double-click activation
}

// ContextClick selects the row under the content-local cell for a
// right-click (#1040) without activating it; the app then opens the context
// menu at the pointer. Reports false when the press lands on chrome (the
// scrollbar column or below the pane), where no menu applies. A press on
// empty space below the rows keeps the current selection — the menu's
// create actions then target the selected (or root) directory.
func (m *Model) ContextClick(x, y int) bool {
	textW, textH, needV, _, _ := m.viewport()
	if x < 0 || y < 0 || y >= textH || x > textW || (needV && x == textW) {
		return false
	}
	if i := m.offset + y; i < len(m.rows) {
		// A right-click inside the active multi-select keeps it untouched —
		// cursor included, or moving it would shrink the anchor..cursor
		// range — so the menu's Delete acts on the whole selection (#1044);
		// outside it the selection collapses like any plain click.
		if m.inSelRange(i) {
			m.resetClick()
			return true
		}
		m.clearSel()
		m.cursor = i
		m.clampScroll()
	}
	m.resetClick()
	return true
}

// ScrollbarHit reports whether a content-local press lands on the vertical
// scrollbar track (#1036) — the app checks it before the row click so a
// thumb press can start a drag, mirroring the editor scrollbar (#1022).
func (m Model) ScrollbarHit(x, y int) bool {
	textW, textH, needV, _, _ := m.viewport()
	return needV && x == textW && y >= 0 && y < textH
}

// ScrollbarPress handles a left press on the track at row y: on the thumb it
// records the grab offset and reports true (the app then tracks a drag
// feeding ScrollbarDrag), on the track it jumps proportionally (#1036).
func (m *Model) ScrollbarPress(y int) (drag bool) {
	_, textH, needV, _, _ := m.viewport()
	if !needV || textH <= 1 {
		return false
	}
	start, length := scrollThumb(textH, len(m.rows), textH, m.offset)
	if y >= start && y < start+length {
		m.sbGrab = y - start
		return true
	}
	if maxOff := len(m.rows) - textH; maxOff > 0 {
		m.offset = clamp(y*maxOff/(textH-1), 0, maxOff)
	}
	return false
}

// ScrollbarDrag continues a thumb drag: the thumb's top follows the pointer
// minus the recorded grab offset, mapped back to a scroll offset (#1036).
func (m *Model) ScrollbarDrag(y int) {
	_, textH, needV, _, _ := m.viewport()
	if !needV {
		return
	}
	_, length := scrollThumb(textH, len(m.rows), textH, m.offset)
	if textH-length <= 0 {
		return
	}
	maxOff := len(m.rows) - textH
	if maxOff <= 0 {
		return
	}
	m.offset = clamp((y-m.sbGrab)*maxOff/(textH-length), 0, maxOff)
}

// onMarker reports whether content-local column x (before horizontal scroll)
// falls on n's two-cell expand marker.
func (m Model) onMarker(n *node, x int) bool {
	cx := x + m.offsetX
	start := n.depth * (1 + maxz(m.indent-1))
	return cx >= start && cx < start+2
}

// resetClick clears the pending single-click state after an activation.
func (m *Model) resetClick() {
	m.lastClickRow = -1
	m.lastClickAt = time.Time{}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// theme returns the active palette, defaulting when none was threaded in
// (tests, zero values), so chrome renderers never nil-check.
func (m Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// Scrollbar styling: a dim track with a brighter, heavier thumb, in the spirit
// of table TUIs that surface overflow on the right and bottom edges. Highlight
// styles overlay the semantic foreground (#1052): the cursor and hover only
// add a background, so the VCS/accent hue stays readable while cursoring.
func (m Model) barTrack() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.theme().ScrollbarTrack)
}

func (m Model) barThumb() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.theme().ScrollbarThumb)
}

// activeStyle marks the focused editor's file with the theme accent — visible
// next to the per-filetype colours without shouting (no bold).
func (m Model) activeStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.theme().Accent)
}

// nodeStyle is a row's base style (#1051, suffix-tint model): the plain
// foreground — the colour channel belongs to the VCS status, JetBrains-style,
// so a changed file reads entirely in its status hue — plus italics for
// hidden (dot-prefixed) entries. The filetype colour no longer paints rows;
// it tints only the extension suffix of clean files (see View).
func (m Model) nodeStyle(n *node) lipgloss.Style {
	s := lipgloss.NewStyle().Foreground(m.theme().Foreground)
	if c := vcs.StatusColor(m.theme(), m.nodeVCSStatus(n)); c != nil {
		s = s.Foreground(c)
	} else if m.nodeIgnored(n) {
		// Gitignored rows read uniformly dimmed, JetBrains-style (#1045):
		// the foreground mixed toward the surface. Ranks below every real
		// VCS status (an ignored path never carries one) and below the
		// untracked hue.
		s = s.Foreground(ignoredFg(m.theme()))
	}
	if isHidden(n.name) {
		s = s.Italic(true)
	}
	return s
}

// ignoredFg is the dimmed foreground for gitignored rows (#1045): the plain
// foreground blended halfway toward the surface.
func ignoredFg(p *theme.Palette) color.Color {
	return theme.Mix(p.Foreground, p.Surface, 0.5)
}

// nodeIgnored reports whether a row's path is gitignored (#1045); a nil
// snapshot (not a git repo) reports false.
func (m Model) nodeIgnored(n *node) bool {
	return m.vcsSnap.Ignored(n.path)
}

// suffixTint resolves the filetype colour for a clean file's extension
// (#1051): nil for directories, VCS-statused rows (status owns the whole
// row), gitignored rows (uniformly dim, #1045) and unmatched extensions.
func (m Model) suffixTint(n *node) color.Color {
	if n.isDir || m.nodeVCSStatus(n) != vcs.StatusNone || m.nodeIgnored(n) {
		return nil
	}
	return m.colors.suffixColor(n)
}

// statusLetter is the one-cell non-colour VCS cue rendered at the row's right
// edge (#1051): redundancy for ANSI256 terminals and colour-blind users.
func statusLetter(st vcs.FileStatus) string {
	switch st {
	case vcs.StatusModified:
		return "M"
	case vcs.StatusRenamed:
		return "R"
	case vcs.StatusAdded:
		return "A"
	case vcs.StatusUntracked:
		return "U"
	case vcs.StatusDeleted:
		return "D"
	case vcs.StatusConflicted:
		return "C"
	}
	return ""
}

// nodeVCSStatus resolves the snapshot status backing a row's coloring; a
// dirty directory reads as its subtree's dominant status (#1053).
func (m Model) nodeVCSStatus(n *node) vcs.FileStatus {
	if m.vcsSnap == nil {
		return vcs.StatusNone
	}
	if n.isDir {
		// The dominant subtree status (#1053): an untracked-only directory
		// reads untracked like its children, not modified.
		return m.vcsSnap.DirStatus(n.path)
	}
	return m.vcsSnap.Status(n.path)
}

// View renders the tree, clipping each row to the horizontal window and drawing
// vertical/horizontal scrollbars whenever the content overflows the pane.
func (m Model) View() string {
	if len(m.rows) == 0 {
		// A palette slot, not terminal Faint, so light themes render the
		// placeholder legibly (#1058).
		return lipgloss.NewStyle().Foreground(m.theme().InlayHint).Render("(empty)")
	}
	textW, textH, needV, needH, contentW := m.viewport()
	offY := clamp(m.offset, 0, maxz(len(m.rows)-textH))
	offX := clamp(m.offsetX, 0, maxz(contentW-textW))

	vStart, vLen := scrollThumb(textH, len(m.rows), textH, offY)

	var lines []string
	for k := 0; k < textH; k++ {
		i := offY + k
		var line string
		if i < len(m.rows) {
			n := m.rows[i]
			style := m.rowStyle(i, n)
			nameStyle := style
			if m.open[n.path] {
				// Every file open in an editor reads underlined — on the name
				// only, so indent guides and padding stay clean. No italic:
				// italics already mean "hidden entry" (#1055).
				nameStyle = nameStyle.Underline(true)
			}
			guides, mark, name, rctx := m.rowParts(n)
			// The suffix-tint model (#1051): on clean files whose row renders
			// the plain foreground, the extension alone takes the filetype
			// colour. Cursor/active rows keep their own foreground whole.
			nameRendered := nameStyle.Render(name)
			if k := m.rowKind(i); k != rowSelected && k != rowActive &&
				!(n.path == m.active && m.active != "") {
				if c := m.suffixTint(n); c != nil {
					if dot := strings.LastIndex(name, "."); dot > 0 {
						nameRendered = nameStyle.Render(name[:dot]) +
							nameStyle.Foreground(c).Render(name[dot:])
					}
				}
			}
			// Guides take the semantic IndentGuide colour (#1050, editor
			// parity) and, with the marker, stay un-bold so the caret column
			// keeps its metrics while cursoring (#1059); both keep the row's
			// background.
			styled := m.guideStyle(style).Render(guides) +
				style.Bold(false).Render(mark) + nameRendered
			if rctx != "" {
				// The root's path context dims to the InlayHint slot (#1046)
				// over the row's own background, never bold — secondary
				// information that must not compete with the name.
				styled += style.Bold(false).
					Foreground(m.theme().InlayHint).Render(rctx)
			}
			vis := ansi.Cut(styled, offX, offX+textW)
			// Right-clipped rows end in an ellipsis (#1035) so truncation is
			// visible; a VCS status letter below takes that cell instead.
			clipped := ansi.StringWidth(styled) > offX+textW
			// A VCS-statused row carries a one-cell status letter at the
			// right edge (#1051): a non-colour cue that survives ANSI256
			// quantisation and colour blindness.
			if letter := statusLetter(m.nodeVCSStatus(n)); letter != "" && textW >= 2 {
				if ansi.StringWidth(vis) >= textW {
					vis = ansi.Cut(vis, 0, textW-1)
				}
				if pad := textW - 1 - ansi.StringWidth(vis); pad > 0 {
					vis += style.Render(strings.Repeat(" ", pad))
				}
				ls := style.Bold(false)
				if c := vcs.StatusColor(m.theme(), m.nodeVCSStatus(n)); c != nil {
					ls = ls.Foreground(c)
				}
				vis += ls.Render(letter)
			} else if clipped && textW >= 2 {
				vis = ansi.Cut(vis, 0, textW-1) + style.Bold(false).Render("…")
			} else if pad := textW - ansi.StringWidth(vis); pad > 0 {
				vis += style.Render(strings.Repeat(" ", pad))
			}
			line = vis
		} else {
			line = strings.Repeat(" ", textW)
		}
		if needV {
			line += m.bar("│", "┃", k >= vStart && k < vStart+vLen)
		}
		lines = append(lines, line)
	}

	if needH {
		hStart, hLen := scrollThumb(textW, contentW, textW, offX)
		var b strings.Builder
		for k := 0; k < textW; k++ {
			b.WriteString(m.bar("─", "━", k >= hStart && k < hStart+hLen))
		}
		row := b.String()
		if needV {
			row += m.barTrack().Render("╯")
		}
		lines = append(lines, row)
	}

	out := lipgloss.JoinVertical(lipgloss.Left, lines...)
	if m.err != nil && m.prompt == nil {
		// A non-modal (scan/poll) error keeps the tree and takes the last
		// row as a themed banner (#1030) — never a full-view replacement;
		// the next successful scan clears it.
		banner := lipgloss.NewStyle().Foreground(m.theme().Error).
			Render(ansi.Truncate("error: "+m.err.Error(), maxz(m.width), "…"))
		if n := len(lines); n > 0 {
			lines[n-1] = banner
			out = lipgloss.JoinVertical(lipgloss.Left, lines...)
		} else {
			out = banner
		}
	}
	if m.prompt != nil {
		// Place, not Center: Center drops a box that does not fit, which would
		// leave an invisible prompt capturing keys (#373). promptBox fits the
		// pane width by construction; a too-short pane clips the box instead.
		bx, by, _, _, _ := m.promptBoxOrigin()
		out = overlay.Place(out, m.promptBox(), bx, by, m.width, m.height)
	}
	return out
}

// rowStyle resolves the full row style for visible row i: the semantic base
// foreground (VCS/plain via nodeStyle, or the active-file accent), then the
// highlight as a background overlay — the cursor and hover never replace the
// foreground (#1052/#1056, matching the structure/problems/VCS lists), so git
// status stays readable while cursoring. An unfocused explorer keeps a muted
// cursor row (#1034, SelectionMuted) so refocusing lands visibly.
func (m Model) rowStyle(i int, n *node) lipgloss.Style {
	base := m.nodeStyle(n)
	if n.path == m.active && m.active != "" {
		base = m.activeStyle()
	}
	switch m.rowKind(i) {
	case rowSelected:
		return base.Background(m.theme().Selection).Bold(true)
	case rowRange, rowCursorIdle:
		// Multi-select members (#1044) share the muted-selection recipe with
		// the unfocused cursor (#1034): a background overlay only, so the
		// semantic foreground stays readable; the cursor row keeps the full
		// Selection recipe above and reads as the range's active end.
		return base.Background(m.theme().SelectionMuted)
	case rowHover:
		return base.Background(m.theme().Panel)
	default:
		return base
	}
}

// guideStyle strips a row style down for the indent-guide cells (#1050): the
// semantic IndentGuide foreground over the row's background, never bold.
func (m Model) guideStyle(row lipgloss.Style) lipgloss.Style {
	return row.Foreground(m.theme().IndentGuide).Bold(false)
}

// rowKind classifies how visible row i is highlighted. Precedence, strongest
// first: the focused cursor, a multi-select range member (#1044), the mouse
// hover, the unfocused cursor (#1034), the open file, a directory, then a
// plain file. View maps each kind to a
// style; tests exercise the logic here so they do not depend on the
// terminal's colour profile.
type rowKind int

const (
	rowPlain rowKind = iota
	rowDir
	rowActive
	rowCursorIdle
	rowRange
	rowHover
	rowSelected
)

func (m Model) rowKind(i int) rowKind {
	n := m.rows[i]
	switch {
	case i == m.cursor && m.focused:
		return rowSelected
	case m.inSelRange(i):
		// A multi-select member (#1044) outranks hover: the range must stay
		// visible while the mouse sweeps over it. The cursor row is caught
		// above (focused) or by the muted-idle case below.
		return rowRange
	case i == m.hover:
		return rowHover
	case i == m.cursor:
		return rowCursorIdle
	case n.path == m.active && m.active != "":
		return rowActive
	case n.isDir:
		return rowDir
	default:
		return rowPlain
	}
}

// bar renders one scrollbar cell, picking the thumb glyph over the track glyph.
func (m Model) bar(track, thumb string, isThumb bool) string {
	if isThumb {
		return m.barThumb().Render(thumb)
	}
	return m.barTrack().Render(track)
}

func maxz(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
