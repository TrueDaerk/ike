package settings

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/theme"
)

// keys.go is the unified key layer (0420, #887): one shared list-navigation
// helper (adding pgup/pgdn/home/end to every list), the shared chord-capture
// sub-panel schema Chord entries use, and the "?" key-help sub-panel showing
// the effective keys of the panel and the active page.

// listNav applies the shared list-navigation keys to *sel over n rows —
// up/down (k/j), pgup/pgdn, home/end — and reports whether it consumed the
// key. page is the jump size for pgup/pgdn.
func listNav(key string, sel *int, n, page int) bool {
	if n <= 0 {
		return false
	}
	if page < 1 {
		page = 10
	}
	switch key {
	case "up", "k":
		*sel = clamp(*sel-1, 0, n-1)
	case "down", "j":
		*sel = clamp(*sel+1, 0, n-1)
	case "pgup":
		*sel = clamp(*sel-page, 0, n-1)
	case "pgdown":
		*sel = clamp(*sel+page, 0, n-1)
	case "home":
		*sel = 0
	case "end":
		*sel = n - 1
	default:
		return false
	}
	return true
}

// navPage is the shared pgup/pgdn jump size for settings lists.
const navPage = 10

// --- chord capture sub-panel ---

// chordCapture is the shared chord-capture flow (#887): schema Chord entries
// use the same capture semantics as the keymap page — press the chord
// (multi-step supported), enter confirms, backspace drops the last step, esc
// cancels — instead of the old grab-the-next-keypress.
type chordCapture struct {
	host  SubPanelHost
	opts  config.Options
	scope config.Scope
	key   string
	title string
	pal   *theme.Palette

	steps []string
}

// newChordCapture builds the capture for one config key.
func newChordCapture(host SubPanelHost, opts config.Options, scope config.Scope, key, title string, pal *theme.Palette) *chordCapture {
	return &chordCapture{host: host, opts: opts, scope: scope, key: key, title: title, pal: pal}
}

func (c *chordCapture) Title() string   { return "Set " + c.title }
func (c *chordCapture) Capturing() bool { return true }

func (c *chordCapture) Buttons() []Button {
	return []Button{
		{Label: "Apply", Do: c.commit, Disabled: len(c.steps) == 0},
		{Label: "Cancel", Do: func() tea.Cmd { c.host.Pop(); return nil }},
	}
}

func (c *chordCapture) Update(key tea.KeyPressMsg) tea.Cmd {
	switch key.Code {
	case tea.KeyEscape:
		c.host.Pop()
		return nil
	case tea.KeyEnter:
		return c.commit()
	case tea.KeyBackspace:
		if len(c.steps) > 0 {
			c.steps = c.steps[:len(c.steps)-1]
			return nil
		}
	}
	c.steps = append(c.steps, key.String())
	return nil
}

func (c *chordCapture) commit() tea.Cmd {
	if len(c.steps) == 0 {
		return nil
	}
	c.host.Pop()
	return config.WriteAndReload(c.opts, c.scope, c.key, strings.Join(c.steps, " "))
}

func (c *chordCapture) View(w, h int) string {
	pal := c.pal
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	shown := strings.Join(c.steps, " ")
	if shown == "" {
		shown = "…"
	}
	lines := []string{
		sec.Render(" Press the new chord (multi-step chords supported):"),
		" " + lipgloss.NewStyle().Bold(true).Render(shown),
		"",
		sec.Render(" enter apply · backspace undo a step · esc cancel"),
	}
	return strings.Join(lines, "\n")
}

// --- key help sub-panel ---

// KeyHelper is an optional PageModel extension (#887): pages list their
// effective keys for the "?" overlay.
type KeyHelper interface {
	KeyHelp() []string
}

// keyHelp is the "?" overlay: the panel's shared keys plus the active page's.
type keyHelp struct {
	host  SubPanelHost
	title string
	lines []string
	pal   *theme.Palette
	off   int
}

func (k *keyHelp) Title() string   { return "Keys" }
func (k *keyHelp) Capturing() bool { return false }
func (k *keyHelp) Buttons() []Button {
	return []Button{{Label: "Close", Key: "enter", Do: func() tea.Cmd { k.host.Pop(); return nil }}}
}
func (k *keyHelp) Update(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "up", "k":
		k.off = clamp(k.off-1, 0, len(k.lines))
	case "down", "j":
		k.off = clamp(k.off+1, 0, len(k.lines))
	}
	return nil
}
func (k *keyHelp) Wheel(delta int) { k.off = clamp(k.off+delta, 0, len(k.lines)) }
func (k *keyHelp) View(w, h int) string {
	pal := k.pal
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	clip := lipgloss.NewStyle().MaxWidth(w)
	lines := k.lines
	if k.off > len(lines)-1 {
		k.off = clamp(len(lines)-1, 0, len(lines))
	}
	end := k.off + h
	if end > len(lines) {
		end = len(lines)
	}
	out := make([]string, 0, h)
	for _, l := range lines[k.off:end] {
		out = append(out, clip.Render(sec.Render(" "+l)))
	}
	return strings.Join(out, "\n")
}

// openKeyHelp pushes the "?" overlay for the active page.
func (m *Model) openKeyHelp() {
	lines := []string{
		"Shared:",
		"  ↑↓/jk pgup/pgdn home/end   navigate",
		"  enter                      activate · space toggles a boolean",
		"  r                          reset to default",
		"  s                          write-scope (auto → user → project)",
		"  /                          filter across every page",
		"  ?                          this overlay · esc close",
	}
	title := "Settings"
	if m.cat >= 0 && m.cat < len(m.pages) {
		title = m.pages[m.cat].Title
		if kh, ok := m.pages[m.cat].Custom.(KeyHelper); ok {
			lines = append(lines, "", title+":")
			for _, l := range kh.KeyHelp() {
				lines = append(lines, "  "+l)
			}
		}
	}
	m.Push(&keyHelp{host: m, title: title, lines: lines, pal: m.pal})
}
