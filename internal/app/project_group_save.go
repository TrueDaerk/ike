package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/project"
	"ike/internal/ui"
)

// project_group_save.go is `project.group.saveOpen` (Epic 0510, #2577): the
// onboarding path into project groups. Someone who has already parked three
// roots by switching between them names that set once and has a group — no
// list editor, no typing of paths.
//
// The dialog is the one-field shell prompt the clone/new-project prompts use
// (ui.Field, paste-capable): a name, the roots it will save listed below it,
// enter saves, esc cancels. A name already in use turns the prompt into the
// replace confirmation (`replace group "web"? [y/n]`) rather than failing —
// re-saving the open set under the same name is the natural way to extend a
// group.
//
// On success the saved group becomes the active one (marker in the model plus
// project.active_group on disk), so the status segment, the MRU ordering and
// find-in-group work without re-opening what is already open.

// groupSavePrompt is the dialog state: the name being typed, the roots the
// save would store (computed once on open, in save order), the validation
// message under the field, and — once enter hit an existing name — the group
// that name would replace.
type groupSavePrompt struct {
	name    ui.Field
	roots   []string
	err     string
	replace string
}

// groupSavePending is the in-flight write: the name and member count the
// landing toast reports once UpsertGroupCmd answers.
type groupSavePending struct {
	name  string
	count int
}

// openWorkspaceRoots is the set project.group.saveOpen saves: the active
// workspace first, then the parked ones in MRU order (most recent first —
// Manager.Background lists them least-recently-used first). A peeked active
// workspace (#2136) is not a project one chose to keep open, so it is left
// out; the peek's origin is parked and comes along with the rest.
func (m Model) openWorkspaceRoots() []string {
	var roots []string
	seen := map[string]bool{}
	add := func(root string) {
		root = strings.TrimSpace(root)
		if root == "" || seen[canonicalRoot(root)] {
			return
		}
		seen[canonicalRoot(root)] = true
		roots = append(roots, root)
	}
	if m.peek == nil {
		add(m.currentRoot())
	}
	bg := m.ws.Background()
	for i := len(bg) - 1; i >= 0; i-- {
		add(bg[i])
	}
	return roots
}

// handleSaveOpenGroup routes project.group.saveOpen: the name prompt over the
// current open set. With no workspace at all there is nothing to name.
func (m Model) handleSaveOpenGroup() (tea.Model, tea.Cmd) {
	roots := m.openWorkspaceRoots()
	if len(roots) == 0 {
		m.host.Notify(host.Info, "no open project to save as a group")
		return m, nil
	}
	m.groupSave = &groupSavePrompt{roots: roots}
	m.renderGroupSavePrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
	return m, nil
}

// groupSavePromptOpen reports whether the shell shows the save-group dialog.
func (m Model) groupSavePromptOpen() bool { return m.groupSave != nil && m.shell.IsOpen() }

// closeGroupSavePrompt drops the dialog state and the shell.
func (m *Model) closeGroupSavePrompt() {
	m.groupSave = nil
	m.shell.Close()
}

// renderGroupSavePrompt (re)fills the shell for the current state: the name
// field with the roots it would save, or the replace confirmation.
func (m *Model) renderGroupSavePrompt() {
	p := m.groupSave
	if p == nil {
		return
	}
	avail := m.width - 30
	if avail < 20 {
		avail = 20
	}
	name := p.name
	roots := p.roots
	errMsg := p.err
	replace := p.replace
	m.shell.SetContent(ui.ModelContent{
		Heading: "Save Open Projects as Group",
		Body: func() string {
			b := &strings.Builder{}
			if replace != "" {
				b.WriteString("replace group \"" + replace + "\"? [y/n]\n\n")
			} else {
				b.WriteString("> Group name : " + windowedInput(name.Text, name.Cur, avail) + "\n\n")
			}
			b.WriteString("Saves " + pluralProjects(len(roots)) + ":\n")
			for _, r := range roots {
				b.WriteString("  " + project.CompactPath(r) + "\n")
			}
			if errMsg != "" {
				b.WriteString("\nE: " + errMsg)
			}
			if replace != "" {
				b.WriteString("\n\ny replace · n keep editing · esc cancel")
			} else {
				b.WriteString("\n\nenter save · esc cancel")
			}
			return b.String()
		},
	})
}

// updateGroupSavePrompt consumes every key while the dialog is open: in the
// replace stage only y/n/esc answer, otherwise enter saves, esc cancels and
// the rest is line editing.
func (m Model) updateGroupSavePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.groupSave
	if p == nil {
		return m, nil
	}
	if msg.Code == tea.KeyEscape {
		m.closeGroupSavePrompt()
		return m, nil
	}
	if p.replace != "" {
		switch strings.ToLower(msg.String()) {
		case "y":
			return m.writeOpenGroup(p.replace)
		case "n":
			p.replace = ""
			m.renderGroupSavePrompt()
		}
		return m, nil
	}
	if msg.Code == tea.KeyEnter {
		return m.acceptGroupSavePrompt()
	}
	if handled, _ := p.name.Key(msg); handled {
		p.err = ""
		m.renderGroupSavePrompt()
	}
	return m, nil
}

// pasteGroupSavePrompt inserts a paste into the name field (the shared
// overlay-paste seam). The replace stage takes no text.
func (m *Model) pasteGroupSavePrompt(text string) bool {
	p := m.groupSave
	if p == nil || p.replace != "" {
		return false
	}
	if !p.name.Paste(text) {
		return false
	}
	p.err = ""
	m.renderGroupSavePrompt()
	return true
}

// acceptGroupSavePrompt validates the typed name against the roots on offer.
// A validation failure keeps the dialog open with the reason attached; a name
// already in use asks before replacing; anything else writes straight away.
func (m Model) acceptGroupSavePrompt() (tea.Model, tea.Cmd) {
	p := m.groupSave
	cfg := config.Get()
	valid, err := project.ValidateGroup(cfg, project.Group{Name: p.name.Text, Roots: p.roots})
	if err != nil {
		p.err = err.Error()
		m.renderGroupSavePrompt()
		return m, nil
	}
	if existing, ok := project.FindGroup(cfg, valid.Name); ok {
		p.err = ""
		p.replace = existing.Name
		m.renderGroupSavePrompt()
		return m, nil
	}
	return m.writeOpenGroup(valid.Name)
}

// writeOpenGroup persists the group off the loop (the RecordOpenCmd rule) and
// closes the dialog; GroupSavedMsg lands the toast and the marker.
func (m Model) writeOpenGroup(name string) (tea.Model, tea.Cmd) {
	roots := m.groupSave.roots
	m.closeGroupSavePrompt()
	m.groupSavePending = &groupSavePending{name: name, count: len(roots)}
	return m, project.UpsertGroupCmd(m.cfgOpts, project.Group{Name: name, Roots: roots})
}

// handleGroupSaved answers the write: a failure is one toast, a success makes
// the fresh group the active one — in the model right away, persisted as
// project.active_group off the loop — so the status segment, the MRU ordering
// and find-in-group work without re-opening the members.
func (m Model) handleGroupSaved(msg project.GroupSavedMsg) (tea.Model, tea.Cmd) {
	pending := m.groupSavePending
	m.groupSavePending = nil
	if msg.Err != nil {
		m.host.Notify(host.Error, "could not save group \""+msg.Name+"\": "+msg.Err.Error())
		return m, nil
	}
	if pending == nil || pending.name != msg.Name {
		return m, nil
	}
	m.activeGroup = msg.Name
	m.host.Notify(host.Info, "group "+msg.Name+" saved · "+pluralProjects(pending.count))
	return m, tea.Batch(config.Reload(m.cfgOpts), project.SetActiveGroupCmd(m.cfgOpts, msg.Name))
}
