package settings

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/theme"
)

// applypanel.go is the apply step (0460, #1296): staged edits are never
// written from under the user's fingers. The panel lists every change as
// `page · key · old → new`, names the layer it would land in, and lets a
// single line be dropped (u) or the whole batch retargeted at another layer
// (s) before enter writes it.

// applyPanel is the diff shown before a staged batch is written.
type applyPanel struct {
	m   *Model
	pal *theme.Palette
	sel int
	off int
	// closeAfter marks the panel as the answer to an esc-close attempt: once
	// the batch is written or discarded, the settings panel closes too.
	closeAfter bool
}

// finish pops the diff and, when it stood in for a close, closes the panel.
func (a *applyPanel) finish() {
	a.m.Pop()
	if a.closeAfter {
		a.m.Close()
	}
}

// openApply pushes the diff panel; with nothing staged it says so instead.
func (m *Model) openApply() tea.Cmd {
	if len(m.changes) == 0 {
		m.notice = "no changes to apply"
		return nil
	}
	m.Push(&applyPanel{m: m, pal: m.pal})
	return nil
}

func (a *applyPanel) Title() string {
	return "Apply " + plural(len(a.m.changes), "change") + " → " + a.m.targetFiles()
}

func (a *applyPanel) Capturing() bool { return false }

func (a *applyPanel) Buttons() []Button {
	return []Button{
		{Label: "Write", Key: "enter", Do: func() tea.Cmd {
			cmd := a.m.applyChanges()
			a.finish()
			return cmd
		}},
		{Label: "Undo line", Key: "u", Do: func() tea.Cmd {
			cmd := a.m.undoChange(a.sel)
			a.sel = clamp(a.sel, 0, len(a.m.changes)-1)
			if len(a.m.changes) == 0 {
				a.m.Pop()
			}
			return cmd
		}},
		{Label: "Scope", Key: "s", Do: func() tea.Cmd {
			a.m.writeScope = (a.m.writeScope + 1) % 3
			a.m.moveChangesTo(a.m.scopeForSelector())
			return nil
		}},
		{Label: "Discard all", Key: "d", Do: func() tea.Cmd {
			cmd := a.m.discardChanges()
			a.finish()
			return cmd
		}},
	}
}

func (a *applyPanel) Update(key tea.KeyPressMsg) tea.Cmd {
	listNav(key.String(), &a.sel, len(a.m.changes), navPage)
	return nil
}

func (a *applyPanel) Wheel(delta int) {
	a.sel = clamp(a.sel+delta, 0, len(a.m.changes)-1)
}

// Click selects the line under the pointer; a press on the selected line
// drops it, the same as "u".
func (a *applyPanel) Click(x, y int) tea.Cmd {
	idx := y + a.off
	if idx < 0 || idx >= len(a.m.changes) {
		return nil
	}
	if idx == a.sel {
		cmd := a.m.undoChange(idx)
		a.sel = clamp(a.sel, 0, len(a.m.changes)-1)
		if len(a.m.changes) == 0 {
			a.m.Pop()
		}
		return cmd
	}
	a.sel = idx
	return nil
}

func (a *applyPanel) View(w, h int) string {
	pal := a.pal
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	clip := lipgloss.NewStyle().MaxWidth(w)
	dim := lipgloss.NewStyle().Foreground(pal.Secondary)
	old := lipgloss.NewStyle().Foreground(pal.Secondary).Faint(true)
	next := lipgloss.NewStyle().Foreground(pal.Success)
	sel := lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)

	pageW := 12
	rows := make([]string, 0, len(a.m.changes))
	for i, c := range a.m.changes {
		page := a.m.pages[clamp(c.page, 0, len(a.m.pages)-1)].Title
		left := " " + pad(page, pageW) + " " + c.entry.Key
		right := old.Render(c.old+" →") + " " + next.Render(a.newLabel(c))
		gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 1
		if gap < 1 {
			gap = 1
		}
		line := left + strings.Repeat(" ", gap) + right
		if i == a.sel {
			line = sel.Render(" " + pad(page, pageW) + " " + c.entry.Key + strings.Repeat(" ", gap) + c.old + " → " + a.newLabel(c))
		}
		rows = append(rows, clip.Render(line))
	}
	footer := []string{
		"",
		clip.Render(dim.Render(" scope: " + a.m.scopeLabel() + " · every change lands in the " + a.m.targetFiles() + " layer")),
	}
	a.sel = clamp(a.sel, 0, len(rows)-1)
	return pinFooter(rows, footer, a.sel, a.sel, h, &a.off)
}

// newLabel renders the right-hand side of a diff line: the staged value, or
// what a reset lands on.
func (a *applyPanel) newLabel(c change) string {
	if c.reset {
		if c.shown == "" {
			return "(default)"
		}
		return c.shown + " (default)"
	}
	return c.shown
}

// scopeForSelector resolves the scope selector to a layer for the whole batch;
// on auto it keeps each entry's conventional scope.
func (m *Model) scopeForSelector() config.Scope {
	switch m.writeScope {
	case scopeProject:
		return config.ProjectScope
	default:
		return config.UserScope
	}
}
