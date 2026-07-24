package app

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/ui"
)

// layouts_ui.go is the user-facing side of saved window layouts (#1175): the
// save-name prompt (the save-as pattern, #730) and the palette picker listing
// the saved layouts — enter applies (or marks the default, when opened via
// window.setDefaultLayout), shift+delete deletes in place (#1113's aux
// convention).

// layoutsPrefix selects the layouts mode inside the palette; the root model
// only opens it locked, so the rune has no user-facing prefix story.
const layoutsPrefix = '"'

// SaveLayoutPromptMsg starts the window.saveLayout name prompt.
type SaveLayoutPromptMsg struct{}

// ShowLayoutsMsg opens the layout picker; SetDefault switches enter from
// "apply" to "use as default for new projects".
type ShowLayoutsMsg struct{ SetDefault bool }

// ApplyLayoutMsg re-shapes the active workspace to the named saved layout.
type ApplyLayoutMsg struct{ Name string }

// SetDefaultLayoutMsg marks the named layout as the new-project default.
type SetDefaultLayoutMsg struct{ Name string }

// DeleteLayoutMsg removes the named layout from the user store.
type DeleteLayoutMsg struct{ Name string }

// RestoreDefaultLayoutMsg is window.restoreLayout (shift+f12): re-apply the
// designated default layout (built-in explorer+editor when none is set).
type RestoreDefaultLayoutMsg struct{}

// layoutsMode is the palette Mode listing the saved layouts. setDefault is
// flipped by the root model before each locked open.
type layoutsMode struct {
	list       func() (names []string, def string)
	setDefault bool
}

func newLayoutsMode(list func() ([]string, string)) *layoutsMode {
	return &layoutsMode{list: list}
}

// Prefix implements palette.Mode.
func (l *layoutsMode) Prefix() rune { return layoutsPrefix }

// Placeholder implements palette.Mode.
func (l *layoutsMode) Placeholder() string {
	if l.setDefault {
		return "Set default layout…"
	}
	return "Apply layout…"
}

// Results implements palette.Mode: the saved names sorted, fuzzy-matched; the
// designated default carries a marker chip. Every row deletes via the aux
// action.
func (l *layoutsMode) Results(query string, _ palette.Context) []palette.Item {
	names, def := l.list()
	sort.Strings(names)
	var items []palette.Item
	for _, name := range names {
		res, ok := fuzzy.Match(query, name)
		if !ok {
			continue
		}
		it := palette.Item{
			Title: name,
			Spans: res.Positions,
			Score: res.Score,
			Aux:   DeleteLayoutMsg{Name: name},
		}
		if name == def {
			it.Detail = "default"
		}
		if l.setDefault {
			it.Msg = SetDefaultLayoutMsg{Name: name}
		} else {
			it.Msg = ApplyLayoutMsg{Name: name}
		}
		items = append(items, it)
	}
	return items
}

// openLayoutPicker opens the picker locked to the layouts mode; with no saved
// layouts it explains instead of showing an empty list.
func (m *Model) openLayoutPicker(setDefault bool) {
	names, _ := layoutNames()
	if len(names) == 0 {
		m.host.Notify(host.Info, "no saved layouts — use Save Window Layout first")
		return
	}
	m.layoutsPicker.setDefault = setDefault
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, layoutsPrefix)
}

// startLayoutSavePrompt opens the shell prompt naming the snapshot of the
// current window layout.
func (m *Model) startLayoutSavePrompt() {
	if m.activeWS().Tree == nil {
		return
	}
	m.layoutSaveOpen = true
	m.layoutSaveInput = ""
	m.layoutSavePos = 0
	m.layoutSaveErr = ""
	m.renderLayoutSavePrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// layoutSavePromptOpen reports whether the shell shows the save-layout prompt.
func (m Model) layoutSavePromptOpen() bool { return m.layoutSaveOpen && m.shell.IsOpen() }

// renderLayoutSavePrompt (re)fills the shell for the current input.
func (m *Model) renderLayoutSavePrompt() {
	line := "> " + ui.CursorView(m.layoutSaveInput, m.layoutSavePos)
	hint := ""
	if m.layoutSaveErr != "" {
		hint = "\n" + m.layoutSaveErr
	}
	m.shell.SetContent(ui.ModelContent{
		Heading: "Save window layout as",
		Body: func() string {
			return line + hint + "\n\nenter save · esc cancel"
		},
	})
}

// updateLayoutSavePrompt consumes every key while the prompt is open: enter
// saves (a name already taken asks for a second enter before overwriting),
// esc cancels, everything else is line editing.
func (m Model) updateLayoutSavePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closePrompt := func() {
		m.layoutSaveOpen = false
		m.layoutSaveInput = ""
		m.layoutSavePos = 0
		m.layoutSaveErr = ""
		m.shell.Close()
	}
	switch {
	case msg.Code == tea.KeyEscape:
		closePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		name := strings.TrimSpace(m.layoutSaveInput)
		if name == "" {
			return m, nil
		}
		s := loadUserLayouts()
		if _, exists := s.Layouts[name]; exists && m.layoutSaveErr == "" {
			// Never silently clobber a saved layout: the second enter confirms.
			m.layoutSaveErr = "layout exists — enter again overwrites"
			m.renderLayoutSavePrompt()
			return m, nil
		}
		snap, ok := snapshotLayout(m.activeWS().Tree, m.activeWS().Panes)
		if !ok {
			closePrompt()
			m.host.Notify(host.Warn, "cannot snapshot the current layout")
			return m, nil
		}
		if s.Layouts == nil {
			s.Layouts = map[string]persistedLayout{}
		}
		s.Layouts[name] = snap
		saveUserLayouts(s)
		closePrompt()
		m.host.Notify(host.Info, "saved layout "+name)
		return m, nil
	}
	if out, pos, handled, changed := ui.EditKey(msg, m.layoutSaveInput, m.layoutSavePos); handled {
		m.layoutSaveInput, m.layoutSavePos = out, pos
		if changed {
			m.layoutSaveErr = "" // a new name re-arms the overwrite guard
		}
		m.renderLayoutSavePrompt()
	}
	return m, nil
}

// deleteLayout removes name from the store (clearing the default marker when
// it pointed there) and refreshes the open picker in place.
func (m *Model) deleteLayout(name string) {
	s := loadUserLayouts()
	if _, ok := s.Layouts[name]; !ok {
		return
	}
	delete(s.Layouts, name)
	if s.Default == name {
		s.Default = ""
	}
	saveUserLayouts(s)
	m.palette.Refresh()
	m.host.Notify(host.Info, "deleted layout "+name)
}

// setDefaultLayout marks name as the default for new projects.
func (m *Model) setDefaultLayout(name string) {
	s := loadUserLayouts()
	if _, ok := s.Layouts[name]; !ok {
		return
	}
	s.Default = name
	saveUserLayouts(s)
	m.host.Notify(host.Info, name+" is now the default layout for new projects")
}
