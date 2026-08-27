package settings

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/theme"
	"ike/internal/ui"
)

// keys.go is the unified key layer (0420, #887): one shared list-navigation
// helper (adding pgup/pgdn/home/end to every list), the shared chord-capture
// sub-panel schema Chord entries use, and the "?" key-help sub-panel showing
// the effective keys of the panel and the active page.

// listNav applies the shared list-navigation keys to *sel over n rows —
// up/down (k/j), pgup/pgdn, home/end — and reports whether it consumed the
// key. page is the jump size for pgup/pgdn. Since #1666 the semantics come
// from ui.ListNav, shared with every other selectable list in the app: single
// steps wrap around, page jumps clamp at the ends.
func listNav(key string, sel *int, n, page int) bool {
	if page < 1 {
		page = navPage
	}
	return ui.ListNav(key, sel, n, page, ui.NavFull&^ui.NavVimExtremes)
}

// navPage is the fallback pgup/pgdn jump size for a settings list whose page
// has not been rendered yet, so its visible height is still unknown.
const navPage = 10

// navRows remembers a page's last rendered height so pgup/pgdn jump one
// visible screen rather than the fixed navPage fallback (#1666). Pages embed
// it, call setRows from View and pass navPageSize to listNav.
// The field is deliberately not called "rows": several pages that embed this
// have a rows() method, and a promoted field of the same name would read as a
// shadow even though Go's depth rule resolves it to the method.
type navRows struct{ viewH int }

// setRows records the height View was asked to render into.
func (n *navRows) setRows(h int) { n.viewH = h }

// navPageSize is the pgup/pgdn jump size: the last rendered height, or the
// navPage fallback before the first render.
func (n navRows) navPageSize() int {
	if n.viewH < 1 {
		return navPage
	}
	return n.viewH
}

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
	navRows
}

func (k *keyHelp) Title() string   { return "Keys" }
func (k *keyHelp) Capturing() bool { return false }
func (k *keyHelp) Buttons() []Button {
	return []Button{{Label: "Close", Key: "enter", Do: func() tea.Cmd { k.host.Pop(); return nil }}}
}

// Update scrolls the cheatsheet. This is a text scroller, not a selection
// list, so every key clamps — there is nothing to wrap around (#1666); the
// page keys page it by a screenful like every other list.
func (k *keyHelp) Update(key tea.KeyPressMsg) tea.Cmd {
	page := k.navPageSize()
	switch key.String() {
	case "up", "k":
		k.off = clamp(k.off-1, 0, len(k.lines))
	case "down", "j":
		k.off = clamp(k.off+1, 0, len(k.lines))
	case "pgup":
		k.off = clamp(k.off-page, 0, len(k.lines))
	case "pgdown":
		k.off = clamp(k.off+page, 0, len(k.lines))
	case "home":
		k.off = 0
	case "end":
		k.off = clamp(len(k.lines)-page, 0, len(k.lines))
	}
	return nil
}
func (k *keyHelp) Wheel(delta int) { k.off = clamp(k.off+delta, 0, len(k.lines)) }
func (k *keyHelp) View(w, h int) string {
	k.setRows(h)
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
	// The footer only shows three keys now (#1295); this overlay is where the
	// full set lives, grouped the way the wireframes' cheatsheet does.
	lines := []string{
		"move:   ↑↓ jk row · ↔ tab column (nav → settings → detail)",
		"        home/end pgup/pgdn top / bottom / page",
		"edit:   enter open editor · confirm · space toggle a boolean",
		"        ‹ › stepper (numbers) · cycle (enums) · d remove a list value",
		"        r reset to default",
		"apply:  ctrl+s review and write the staged changes",
		"        esc with changes pending opens the same review",
		"search: / fuzzy over key, label, description · rail = hit pages",
		"        enter sets here · tab opens the page · esc clears, then exits",
		"global: s write-scope: auto → user → project",
		"        ? this overlay · esc back / close",
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
