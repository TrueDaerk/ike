package app

import (
	"image/color"
	"sort"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/host"
	"ike/internal/ui"
)

// panenumbers.go implements the pane numbers in the chrome and the
// focus-by-number commands (#2407). Pane focus is the most frequent layout
// event and, before this, the keyboard could only *cycle* through the panes
// (pane.switcher): reaching the third pane of four meant pressing ctrl+tab
// until it happened. The panes are numbered in layout reading order and drawn
// lazygit style in the title bar ("[1] EDITOR"), so the number a chord
// addresses is on screen next to the pane it means.
//
// The numbering is derived, never stored: it is recomputed from the live
// layout rectangles on every read, so a split, move, close or zoom renumbers
// the panes with no invalidation step.

// paneNumberMax is how many panes carry a number: the chords are ctrl+1…9
// (pane.focus1…9), and a badge nobody can press would be a lie. Panes beyond
// it stay unnumbered and are reached with the switcher or the mouse.
const paneNumberMax = 9

// paneNumberHintTTL is how long a pane switch keeps the which-pane hint up in
// focus-only mode — long enough to read the badges after a switcher step,
// short enough that the chrome is quiet again by the next glance.
const paneNumberHintTTL = 2 * time.Second

// paneNumberHintMsg expires the which-pane hint raised gen switches ago; a
// newer switch bumps the generation, so its own timer is the one that counts.
type paneNumberHintMsg struct{ gen int }

// paneNumberOrder lists the visible panes in layout reading order —
// left-to-right, top-to-bottom by their computed rectangles — which is the
// order the numbers follow. It reads the cached layout, so a zoomed pane
// (#358) is the only numbered one and a tool window counts exactly while it
// is on screen; the popup terminal and the floating panels are not layout
// leaves at all and never take a number. Before the first layout (no size
// yet) it falls back to the tree walk order the focus cycle uses.
func (m Model) paneNumberOrder() []string {
	if len(m.lay.Panes) == 0 {
		return m.leafOrder()
	}
	keys := make([]string, 0, len(m.lay.Panes))
	for k := range m.lay.Panes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := m.lay.Panes[keys[i]], m.lay.Panes[keys[j]]
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		if a.X != b.X {
			return a.X < b.X
		}
		return keys[i] < keys[j] // identical origins cannot happen; stay deterministic
	})
	return keys
}

// paneNumberOf returns the 1-based number of pane key, or 0 when it has none
// (not visible, or past paneNumberMax).
func (m Model) paneNumberOf(key string) int {
	for i, k := range m.paneNumberOrder() {
		if k == key {
			if i >= paneNumberMax {
				return 0
			}
			return i + 1
		}
	}
	return 0
}

// paneNumbersMode reads layout.pane_numbers (#2407): "on", "off" or
// "focus-only". An unknown or missing value reads as the default "on" — the
// config layer already validated and reported it.
func (m Model) paneNumbersMode() string {
	v, _ := m.host.Config().Get("layout.pane_numbers")
	switch v {
	case "off", "focus-only":
		return v
	}
	return "on"
}

// paneNumbersShown reports whether the badges are drawn right now: always in
// "on", never in "off", and only while the which-pane hint is up in
// "focus-only".
func (m Model) paneNumbersShown() bool {
	switch m.paneNumbersMode() {
	case "off":
		return false
	case "focus-only":
		return m.paneNumHint
	}
	return true
}

// raisePaneNumberHint puts the which-pane hint up and returns the command that
// takes it down again. It runs on every keyboard pane switch — the switcher
// and the focus-by-number chords — so in focus-only mode the numbers are on
// screen exactly while the user is switching panes. Outside focus-only mode it
// is a no-op with no timer.
func (m *Model) raisePaneNumberHint() tea.Cmd {
	if m.paneNumbersMode() != "focus-only" {
		return nil
	}
	m.paneNumHint = true
	m.paneNumHintGen++
	gen := m.paneNumHintGen
	return tea.Tick(paneNumberHintTTL, func(time.Time) tea.Msg {
		return paneNumberHintMsg{gen: gen}
	})
}

// paneNumberBadgeText is the plain "[n] " prefix a pane's title carries, or ""
// when the pane has no number or the badges are hidden. It is uncolored so the
// caller can measure it before the chrome's colors are resolved.
func (m Model) paneNumberBadgeText(key string) string {
	if !m.paneNumbersShown() {
		return ""
	}
	n := m.paneNumberOf(key)
	if n == 0 {
		return ""
	}
	return "[" + strconv.Itoa(n) + "] "
}

// paneNumberBadge colors that prefix in the pane's own border color: dim on an
// unfocused pane, and on the focused one whatever the border currently says —
// the focus color, the editor's input mode (#1353), the drag colors. The badge
// is chrome, so it must never contradict the frame it sits in.
func paneNumberBadge(text string, border color.Color) string {
	if text == "" {
		return ""
	}
	return lipgloss.NewStyle().Foreground(border).Render(text)
}

// focusPaneNumber handles pane.focus1…9: it moves focus to the pane carrying
// that number. An out-of-range number is a no-op with a notification — a
// silent dead chord is indistinguishable from a broken one (#275).
func (m *Model) focusPaneNumber(n int) {
	order := m.paneNumberOrder()
	if len(order) > paneNumberMax {
		order = order[:paneNumberMax]
	}
	if n < 1 || n > len(order) {
		m.host.Notify(host.Info, "focus pane "+strconv.Itoa(n)+": only "+strconv.Itoa(len(order))+" panes are open")
		return
	}
	m.setFocus(order[n-1])
}

// panePromptHeading titles the shell prompt of pane.focusByIndex.
const panePromptHeading = "Focus pane"

// paneNumPromptOpen reports whether the shell shows the pane-number prompt.
func (m Model) paneNumPromptOpen() bool { return m.paneNumOpen && m.shell.IsOpen() }

// startPaneFocusByIndex opens pane.focusByIndex's prompt: the palette flavour
// of the ctrl+digit chords, for the panes past nine and for keymaps where the
// chords do not reach the terminal.
func (m *Model) startPaneFocusByIndex() tea.Cmd {
	m.paneNumOpen = true
	m.paneNumInput.Clear()
	m.renderPaneNumPrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
	// The prompt is a which-pane question of its own: the badges belong on
	// screen while it is answered.
	return m.raisePaneNumberHint()
}

// renderPaneNumPrompt (re)fills the shell for the current input.
func (m *Model) renderPaneNumPrompt() {
	avail := m.width - 10
	if avail < 20 {
		avail = 20
	}
	line := "pane: " + windowedInput(m.paneNumInput.Text, m.paneNumInput.Cur, avail)
	m.shell.SetContent(ui.ModelContent{
		Heading: panePromptHeading,
		Body: func() string {
			return line + "\n\nenter focus · esc cancel"
		},
	})
}

// updatePaneNumPrompt consumes every key while the prompt is open, like the
// other single-field shell prompts.
func (m Model) updatePaneNumPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closePrompt := func() {
		m.paneNumOpen = false
		m.paneNumInput.Clear()
		m.shell.Close()
	}
	switch {
	case msg.Code == tea.KeyEscape:
		closePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		text := m.paneNumInput.Text
		closePrompt()
		n, err := strconv.Atoi(text)
		if err != nil {
			m.host.Notify(host.Info, "focus pane: not a pane number: "+text)
			return m, nil
		}
		m.focusPaneNumber(n)
		return m, m.raisePaneNumberHint()
	default:
		m.paneNumInput.Key(msg)
	}
	m.renderPaneNumPrompt()
	return m, nil
}

// pastePaneNumPrompt inserts a paste into the pane input at its cursor, like
// every other single-field prompt (#1936).
func (m *Model) pastePaneNumPrompt(text string) bool {
	if !m.paneNumInput.Paste(flattenExpr(text)) {
		return false
	}
	m.renderPaneNumPrompt()
	return true
}
