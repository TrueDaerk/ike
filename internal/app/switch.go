package app

import (
	"os"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/project"
	"ike/internal/ui"
)

// switch.go is the root-model side of project switching (Roadmap 0090, #3).
// internal/project validates the candidate root and emits SwitchProjectMsg;
// this file guards it against unsaved buffers and performs the re-root as one
// transaction: persist the old project's session/layout, chdir, rebuild the
// model exactly like a fresh start (the whole IDE is anchored at "."), and
// record the open into the recent-projects history.

// handleSwitchProject routes a validated switch request: a root equal to the
// current one is a friendly no-op, dirty buffers gate the switch behind the
// unsaved-changes prompt, otherwise the re-root runs immediately.
func (m Model) handleSwitchProject(msg project.SwitchProjectMsg) (tea.Model, tea.Cmd) {
	if cwd, err := os.Getwd(); err == nil && cwd == msg.Root {
		m.host.Notify(host.Info, "already in "+msg.Root)
		return m, nil
	}
	if m.dirtyEditorCount() > 0 {
		// Emit the guard msg rather than opening the prompt inline, keeping the
		// transaction observable (and testable) step by step.
		return m, func() tea.Msg { return project.UnsavedChangesMsg{Root: msg.Root} }
	}
	return m.performSwitch(msg.Root)
}

// dirtyEditorCount counts dirty editor buffers across every pane and tab.
func (m Model) dirtyEditorCount() int {
	n := 0
	for _, key := range m.panes.Keys() {
		inst := m.panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			if inst.TabEditor(i).Dirty() {
				n++
			}
		}
	}
	return n
}

// openSwitchPrompt shows the unsaved-changes guard for a pending switch to
// root: save all and switch, discard and switch, or cancel (the current
// project stays untouched).
func (m *Model) openSwitchPrompt(root string) {
	m.switchPending = root
	dirty := m.dirtyEditorCount()
	m.shell.SetContent(ui.ModelContent{
		Heading: "Unsaved changes",
		Body: func() string {
			// CompactPath bounds the line width: the shell drops a box wider
			// than the terminal, which a raw absolute root can force.
			return plural(dirty, "buffer has", "buffers have") + " unsaved changes; switching to\n" +
				project.CompactPath(root) + " closes every open file.\n\n" +
				"  [s]   save all, then switch\n" +
				"  [d]   discard changes and switch\n" +
				"  [esc] cancel — stay in the current project"
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// plural renders "1 buffer has" / "3 buffers have" style phrases.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// switchPromptOpen reports whether the shell currently shows the guard.
func (m Model) switchPromptOpen() bool { return m.switchPending != "" && m.shell.IsOpen() }

// updateSwitchPrompt consumes every key while the guard is open: s saves all
// dirty buffers and switches, d discards them and switches, esc cancels.
// Other keys are swallowed so nothing leaks past a modal decision.
func (m Model) updateSwitchPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	root := m.switchPending
	switch msg.String() {
	case "s":
		m.switchPending = ""
		m.shell.Close()
		// Editor writes apply synchronously inside UpdateTab (the returned
		// cmds only carry follow-up events), so the switch can proceed in the
		// same step; the batch keeps the events flowing.
		saves := m.saveAllDirty()
		next, cmd := m.performSwitch(root)
		return next, tea.Batch(append(saves, cmd)...)
	case "d":
		m.switchPending = ""
		m.shell.Close()
		return m.performSwitch(root)
	case "esc":
		m.switchPending = ""
		m.shell.Close()
		return m, nil
	}
	return m, nil
}

// saveAllDirty writes every dirty editor buffer (background tabs included) and
// returns the editors' follow-up cmds — the same walk editor.saveAll performs.
func (m *Model) saveAllDirty() []tea.Cmd {
	var cmds []tea.Cmd
	for _, key := range m.panes.Keys() {
		inst := m.panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			if inst.TabEditor(i).Dirty() {
				cmds = append(cmds, inst.UpdateTab(i, editor.ActionMsg{Action: "write"}))
			}
		}
	}
	return cmds
}

// adoptTerminals carries the old model's live terminal sessions across a
// project switch (#96): existing shells keep running, split below the new
// workspace's active editor and titled with their origin root. Dead sessions
// are closed for good. When the target workspace's layout restore already
// recreated a terminal under the same key (a fresh placeholder shell for this
// very session), the live session takes over that pane instead of gaining a
// second leaf — a duplicate key in the tree would render the same instance
// twice (#320).
func (m *Model) adoptTerminals(old *Model) {
	for _, key := range old.panes.Keys() {
		inst := old.panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindTerminal {
			continue
		}
		if !inst.Terminal().Running() {
			inst.Terminal().Close()
			continue
		}
		if m.panes.AdoptTerminal(inst) {
			continue // took over the restored placeholder's leaf (#320)
		}
		if !m.panes.Has(inst.Key()) {
			inst.Terminal().Close() // key collision with a non-terminal pane
			continue
		}
		target := m.activeEditorKey()
		if target == "" {
			target = m.panes.Focused()
		}
		if target == "" || m.tree == nil {
			m.panes.Close(inst.Key())
			continue
		}
		if tree, ok := layout.SplitLeaf(m.tree, target, inst.Key(), layout.ZoneBottom); ok {
			m.tree = tree
		} else {
			m.panes.Close(inst.Key())
		}
	}
	m.layout()
}

// performSwitch re-roots the IDE at root, which must already be validated and
// absolute. The old project's session and layout are persisted first, then the
// process chdirs and the model is rebuilt through the fresh-start path — config
// discovery, theme, panes, layout/session restore and the watcher all resolve
// against the new root — with the live host carried over so the program sender
// and the LSP bridge stay wired. Nothing is mutated when the chdir fails.
func (m Model) performSwitch(root string) (tea.Model, tea.Cmd) {
	saveSession(m.snapshotSession())
	if m.tree != nil {
		saveLayout(m.tree, m.panes)
	}
	if err := os.Chdir(root); err != nil {
		return m, func() tea.Msg { return project.SwitchFailedMsg{Path: root, Err: err} }
	}
	m.watcher.Stop()

	cfg, _ := config.Load(config.Discover("."))
	config.Set(cfg)
	fresh := newWithHost(m.reg, host.FromConfig(cfg), m.host)
	fresh.StartWatcher(".")

	// Size the fresh model like the first WindowSizeMsg would, then run its
	// Init and the post-switch effects: record the open (success only — we are
	// past every failure point) and announce the switch.
	sizedTM, sizeCmd := fresh.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	sized := sizedTM.(Model)
	// Live terminal sessions survive the switch (#96): adopt them into the
	// freshly laid-out workspace, titled with their origin root.
	sized.adoptTerminals(&m)
	return sized, tea.Batch(
		fresh.Init(),
		sizeCmd,
		project.RecordOpenCmd(config.Discover("."), root, time.Now()),
		func() tea.Msg { return project.SwitchedMsg{Root: root} },
	)
}
