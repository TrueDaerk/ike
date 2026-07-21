package settings

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

// confirm.go is the destructive-action confirmation sub-panel (0420, #891):
// deletes and removals ask once — a small pushed dialog with Delete/Cancel
// buttons — instead of acting on a single keypress with no undo.

// confirmPanel implements SubPanel.
type confirmPanel struct {
	host  SubPanelHost
	what  string // "delete the tool htop"
	verb  string // button label, e.g. "Delete"
	doIt  func() tea.Cmd
	pal   *theme.Palette
}

// newConfirm builds the dialog; verb labels the destructive button.
func newConfirm(host SubPanelHost, what, verb string, pal *theme.Palette, do func() tea.Cmd) *confirmPanel {
	return &confirmPanel{host: host, what: what, verb: verb, doIt: do, pal: pal}
}

func (c *confirmPanel) Title() string   { return "Confirm" }
func (c *confirmPanel) Capturing() bool { return false }

func (c *confirmPanel) Buttons() []Button {
	return []Button{
		{Label: c.verb, Key: "enter", Do: func() tea.Cmd { c.host.Pop(); return c.doIt() }},
		{Label: "Cancel", Key: "n", Do: func() tea.Cmd { c.host.Pop(); return nil }},
	}
}

func (c *confirmPanel) Update(key tea.KeyPressMsg) tea.Cmd {
	// enter/n run through the button keys; y is a spoken-for synonym.
	if key.String() == "y" {
		c.host.Pop()
		return c.doIt()
	}
	return nil
}

func (c *confirmPanel) View(w, h int) string {
	pal := c.pal
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	warn := lipgloss.NewStyle().Foreground(pal.Error).Bold(true)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	lines := []string{
		warn.Render(" Really " + c.what + "?"),
		sec.Render(" This cannot be undone."),
		"",
		sec.Render(" enter/y confirm · esc/n cancel"),
	}
	return strings.Join(lines, "\n")
}
