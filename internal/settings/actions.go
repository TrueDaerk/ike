package settings

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

// actions.go is the panel's action bar: the bottom row names the verbs the
// focused surface offers, each behind its key, in one consistent keycap
// style — "[a] Add · [d] Delete · [s] Scope: auto · [?] Keys". It replaced
// the three-key hint row, which only ever named the panel's own keys: a
// custom page's letters (a, d, u, i, …) were discoverable through "?" alone,
// and staging a change silently replaced "r reset" with "ctrl+s apply".
//
// Pages describe their verbs through the ActionLister seam; the bar, the "?"
// overlay and the mouse hit map all derive from that one list.

// Action is one key-bound verb a page offers.
type Action struct {
	// Key as the user presses it: "a", "enter", "R", "ctrl+r".
	Key string
	// Verb in sentence case, short: "Add", "Delete", "Reset".
	Verb string
	// Hint explains the verb in the "?" overlay; optional.
	Hint string
}

// ActionLister is an optional PageModel extension: the page's verbs, in
// bar order — the most-used first, because the bar shows as many as fit
// and the overlay shows them all.
type ActionLister interface {
	Actions() []Action
}

// maxBarActions caps how many page verbs the bar shows before it defers to
// "[?] Keys"; a longer bar is a legend, not a bar.
const maxBarActions = 6

// pageActions returns the active custom page's verbs (nil for schema pages).
func (m *Model) pageActions() []Action {
	page := m.customPage()
	if page == nil || m.filter != "" {
		return nil
	}
	if l, ok := page.(ActionLister); ok {
		return l.Actions()
	}
	return nil
}

// barItem is one rendered keycap: the action plus the click action the
// panel runs for it ("key:<k>" forwards the key to the page).
type barItem struct {
	Action
	click string
}

// barItems assembles the action bar for the current state.
func (m *Model) barItems() []barItem {
	var items []barItem
	add := func(key, verb, click string) {
		items = append(items, barItem{Action{Key: key, Verb: verb}, click})
	}
	switch {
	case m.SubOpen():
		add("esc", "Back", "")
		return items
	case m.filtering:
		add("enter", "Keep", "")
		add("esc", "Clear", "")
		return items
	case m.filter != "" && m.focus == formColumn:
		add("enter", "Set here", "edit")
		add("tab", "Open page", "openpage")
		add("esc", "Clear", "")
	case m.focus == catColumn:
		add("enter", "Open", "edit")
		add("/", "Search", "filter")
	case m.focus == detailColumn:
		for _, h := range m.editorHint() {
			k, v, _ := strings.Cut(h.text, " ")
			add(k, sentence(v), "")
		}
	case m.customPage() != nil:
		acts := m.pageActions()
		if len(acts) > maxBarActions {
			acts = acts[:maxBarActions]
		}
		for _, a := range acts {
			add(a.Key, a.Verb, "key:"+a.Key)
		}
	default:
		add("enter", "Edit", "edit")
		if r, ok := m.current(); ok && r.kind == rowEntry && r.entry.Type == Bool {
			add("space", "Toggle", "toggle")
		}
		add("r", "Reset", "reset")
	}
	if m.customPage() == nil && m.filter == "" {
		// The write scope only routes schema writes; custom pages keep their own.
		add("s", "Scope: "+m.scopeLabel(), "scope")
	}
	if m.Dirty() {
		add("ctrl+s", "Apply "+strconv.Itoa(len(m.changes)), "apply")
	}
	add("?", "Keys", "help")
	return items
}

// sentence upper-cases the first rune of a hint verb.
func sentence(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// renderActionBar renders the bottom row and records the clickable spans.
// Items that do not fit the width are dropped from the right, keeping "[?]"
// — the overlay lists everything the bar could not.
func (m *Model) renderActionBar(pal *theme.Palette, innerW int) string {
	m.hintHits = nil
	keyStyle := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	verbStyle := lipgloss.NewStyle().Foreground(pal.Secondary)
	sepStyle := lipgloss.NewStyle().Foreground(pal.Secondary).Faint(true)

	items := m.barItems()
	// Measure first: every item costs "[key] verb" plus a " · " separator.
	widths := make([]int, len(items))
	for i, it := range items {
		widths[i] = lipgloss.Width("["+it.Key+"] "+it.Verb) + 3
	}
	total := 1
	for _, w := range widths {
		total += w
	}
	if total > innerW && len(items) > 1 {
		help := items[len(items)-1]
		keep := items[:len(items)-1]
		total = 1 + widths[len(items)-1]
		var fit []barItem
		for i, it := range keep {
			if total+widths[i] > innerW {
				break
			}
			total += widths[i]
			fit = append(fit, it)
		}
		items = append(fit, help)
	}

	x := 1 // border column 0
	var b strings.Builder
	b.WriteString(" ")
	x++
	for i, it := range items {
		if i > 0 {
			b.WriteString(sepStyle.Render(" · "))
			x += 3
		}
		text := "[" + it.Key + "] " + it.Verb
		w := lipgloss.Width(text)
		if it.click != "" {
			m.hintHits = append(m.hintHits, hintAction{start: x, end: x + w, action: it.click})
		}
		b.WriteString(keyStyle.Render("[" + it.Key + "]"))
		b.WriteString(" ")
		b.WriteString(verbStyle.Render(it.Verb))
		x += w
	}
	return b.String()
}

// keyPress builds the key press a bar keycap stands for, so a click on
// "[R] Restart" reaches the page as the R key would.
func keyPress(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	}
	var mod tea.KeyMod
	for {
		switch {
		case strings.HasPrefix(s, "ctrl+"):
			mod, s = mod|tea.ModCtrl, s[5:]
		case strings.HasPrefix(s, "alt+"):
			mod, s = mod|tea.ModAlt, s[4:]
		case strings.HasPrefix(s, "shift+"):
			mod, s = mod|tea.ModShift, s[6:]
		case strings.HasPrefix(s, "cmd+"), strings.HasPrefix(s, "super+"):
			_, s, _ = strings.Cut(s, "+")
			mod |= tea.ModSuper
		default:
			r := []rune(s)
			if len(r) != 1 {
				return tea.KeyPressMsg{}
			}
			if mod == 0 {
				return tea.KeyPressMsg{Code: r[0], Text: s}
			}
			if mod == tea.ModShift {
				return tea.KeyPressMsg{Code: r[0], Text: s, Mod: mod}
			}
			return tea.KeyPressMsg{Code: r[0], Mod: mod}
		}
	}
}
