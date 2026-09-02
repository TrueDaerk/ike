package app

import (
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/pane"
)

// switch_autosave.go is the auto-save gate of an orderly project switch
// (#2186), JetBrains' behaviour: leaving a project writes its edits instead of
// leaving their fate to the parked workspace and a later prompt. With
// project.auto_save_on_switch on (the default), every dirty file-backed buffer
// of the departing workspace saves through the *normal* save path — so
// format-on-save and organize-imports-on-save run exactly as on a manual `:w`
// — and only then does performSwitch swap the workspace. Buffers with no
// writable home (untitled, read-only, changed on disk since the edits) never
// block the switch (#2396): they stay dirty and park with the departing
// workspace — kept, not lost — announced by one notification. The quit guard
// remains the single prompt that ever asks about unsaved buffers.
//
// The normal save path is asynchronous when the save chain applies (#1148):
// the write is parked until the language server answered. A switch must not
// run before those writes land — the parked workspace's editors are cut loose
// from the model's services, so a chain completing afterwards would never
// write. The gate therefore waits for the pending chains (autoSaveSwitch),
// resumed by every ilsp.SaveChainDoneMsg, with a timeout that falls back to a
// raw write so a wedged server can never strand the switch.

// autoSaveSwitchTimeout bounds the wait for the pending save chains; past it
// the still-pending buffers are written raw (format skipped) and the switch
// proceeds. Generous compared to the chain's own per-step budget — this is the
// backstop, not the schedule.
const autoSaveSwitchTimeout = 3 * time.Second

// autoSaveSwitch is the switch parked behind the in-flight save chains: the
// target root to resume with once every write landed.
type autoSaveSwitch struct{ root string }

// autoSaveSwitchTimeoutMsg fires when the parked switch waited long enough.
type autoSaveSwitchTimeoutMsg struct{}

// switchBuffer is one dirty buffer of the departing workspace. reason is empty
// while the buffer is writable; a non-empty reason names why it is not.
type switchBuffer struct {
	key    string
	tab    int
	path   string
	name   string
	reason string
}

// autoSaveOnSwitch reads project.auto_save_on_switch. Unset reads as on: the
// config default is true and a model built without a loaded config (tests,
// embedders) should behave like production.
func (m Model) autoSaveOnSwitch() bool {
	cfg := m.host.Config()
	if cfg == nil {
		return true
	}
	v, ok := cfg.Get("project.auto_save_on_switch")
	return !ok || v != "false"
}

// switchDirtyBuffers lists every dirty buffer of the active workspace, each
// classified by whether the auto-save can write it. Documents shown in several
// panes (#142) count once — one write clears them all.
func (m Model) switchDirtyBuffers() []switchBuffer {
	var out []switchBuffer
	seen := map[string]bool{}
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			ed := inst.TabEditor(i)
			if ed == nil || !ed.Dirty() {
				continue
			}
			b := switchBuffer{key: key, tab: i, path: ed.Path(), name: "untitled"}
			switch {
			case !ed.HasFile():
				b.reason = "no file path"
			case ed.ReadOnly():
				b.reason = "read-only"
			case ed.Stale():
				// Writing over an external change is the conflict guard's
				// decision, never the switch's (#1515).
				b.reason = "changed on disk"
			}
			if b.path != "" {
				if seen[b.path] {
					continue
				}
				seen[b.path] = true
				b.name = filepath.Base(b.path)
			}
			out = append(out, b)
		}
	}
	return out
}

// beginSwitchAutoSave is the gated switch (#2186): write what can be written,
// then either switch straight away, wait for the in-flight save chains, or ask
// about the buffers that have no writable home.
func (m Model) beginSwitchAutoSave(root string) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	for _, b := range m.switchDirtyBuffers() {
		if b.reason != "" {
			continue
		}
		inst := m.activeWS().Panes.Get(b.key)
		if inst == nil {
			continue
		}
		// The manual save action on purpose: format-on-save and the other
		// save hooks must run exactly as they do on a normal write.
		if cmd := inst.UpdateTab(b.tab, editor.ActionMsg{Action: "write"}); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(m.pendingSwitchSaves()) > 0 {
		m.autoSaveSwitch = &autoSaveSwitch{root: root}
		cmds = append(cmds, tea.Tick(autoSaveSwitchTimeout, func(time.Time) tea.Msg {
			return autoSaveSwitchTimeoutMsg{}
		}))
		return m, tea.Batch(cmds...)
	}
	next, cmd := m.finishSwitchAutoSave(root)
	return next, tea.Batch(append(cmds, cmd)...)
}

// pendingSwitchSaves lists the buffers whose write is parked behind a running
// save chain (#1148).
func (m Model) pendingSwitchSaves() []switchBuffer {
	var out []switchBuffer
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			if ed := inst.TabEditor(i); ed != nil && ed.SavePending() {
				out = append(out, switchBuffer{key: key, tab: i, path: ed.Path()})
			}
		}
	}
	return out
}

// resumeSwitchAutoSave continues a parked switch once the save chains finished;
// it reports handled=false when no switch is waiting or chains are still
// running, so the caller keeps its own result.
func (m Model) resumeSwitchAutoSave() (tea.Model, tea.Cmd, bool) {
	if m.autoSaveSwitch == nil || len(m.pendingSwitchSaves()) > 0 {
		return m, nil, false
	}
	root := m.autoSaveSwitch.root
	m.autoSaveSwitch = nil
	next, cmd := m.finishSwitchAutoSave(root)
	return next, cmd, true
}

// forceSwitchAutoSave is the timeout backstop: every write still parked behind
// its chain is performed raw (format skipped — a wedged server must not cost
// the edits), then the switch resumes.
func (m Model) forceSwitchAutoSave() (tea.Model, tea.Cmd) {
	if m.autoSaveSwitch == nil {
		return m, nil
	}
	root := m.autoSaveSwitch.root
	m.autoSaveSwitch = nil
	var cmds []tea.Cmd
	for _, b := range m.pendingSwitchSaves() {
		if inst := m.activeWS().Panes.Get(b.key); inst != nil {
			if cmd := inst.UpdateTab(b.tab, editor.ActionMsg{Action: "write_raw"}); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	next, cmd := m.finishSwitchAutoSave(root)
	return next, tea.Batch(append(cmds, cmd)...)
}

// finishSwitchAutoSave runs the switch once the auto-save is through. Since
// #2396 nothing is left to ask: buffers with no writable home (untitled,
// read-only, changed on disk) and buffers whose write failed simply stay
// dirty and park with the departing workspace — they come back exactly as
// left on the next visit, and the quit guard still protects them at exit.
// The "Cannot save every buffer" dialog is gone; a switch never prompts.
func (m Model) finishSwitchAutoSave(root string) (tea.Model, tea.Cmd) {
	if unsaved := m.switchDirtyBuffers(); len(unsaved) > 0 {
		m.host.Notify(host.Info,
			plural(len(unsaved), "buffer stays", "buffers stay")+" unsaved in the parked workspace")
	}
	return m.performSwitch(root)
}
