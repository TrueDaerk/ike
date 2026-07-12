package settings

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/keymap"
	"ike/internal/theme"
)

// keymap_page.go is the keymap editor (#93): a custom settings page listing
// the effective binding table (context, chord, command, source layer) with a
// capture-based rebind flow. All edits are keymap.bindings.* overrides through
// the write-back layer: rebinding writes the new chord (and unbinds the old
// one), unbinding writes chord→"", and reset removes the override so the
// preset default falls back through the layers.

// KeymapPage implements PageModel.
type KeymapPage struct {
	opts       config.Options
	registered func(commandID string) bool
	pal        *theme.Palette

	sel       int
	off       int // list scroll offset (#537)
	filter    string
	filtering bool // "/" opened the filter input; every key is filter text

	capturing bool
	steps     []keymap.Key // chord steps captured so far
	conflict  string       // colliding command id awaiting confirmation
	warn      string       // fragile-chord honesty warning
	invalid   string
}

// NewKeymapPage builds the keymap editor writing overrides through opts;
// registered reports whether a command id resolves in the registry (blocked
// ids render disabled-with-reason instead of hidden).
func NewKeymapPage(opts config.Options, registered func(commandID string) bool) *KeymapPage {
	return &KeymapPage{opts: opts, registered: registered}
}

// SetPalette implements PageModel.
func (k *KeymapPage) SetPalette(p *theme.Palette) { k.pal = p }

// Capturing implements PageModel: while a rebind capture (or its conflict
// confirmation) or the filter input (#531) is active the page needs every key
// verbatim — filter text may contain the page's own action letters (u/r/j/k).
func (k *KeymapPage) Capturing() bool { return k.capturing || k.filtering }

// table builds the effective binding table from the live config — the same
// construction the app's resolver uses, so the page always shows reality.
func (k *KeymapPage) table() *keymap.BindingTable {
	c := config.Get()
	preset := strings.TrimSpace(c.Keymap.Preset)
	if preset == "" {
		preset = keymap.PresetJetBrains
	}
	return keymap.BuildTable(keymap.Defaults(preset), c.Keymap.Bindings, keymap.GOOS)
}

// rows returns the visible bindings, filtered and deterministically sorted
// (context, then chord).
func (k *KeymapPage) rows() []keymap.Binding {
	all := k.table().Bindings()
	needle := strings.ToLower(k.filter)
	var out []keymap.Binding
	for _, b := range all {
		if needle != "" {
			hay := strings.ToLower(b.Chord.String() + " " + b.Command + " " + b.Title + " " + string(b.Context))
			if !strings.Contains(hay, needle) {
				continue
			}
		}
		out = append(out, b)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Context != out[j].Context {
			return out[i].Context < out[j].Context
		}
		return out[i].Chord.String() < out[j].Chord.String()
	})
	return out
}

// current returns the selected binding, if any.
func (k *KeymapPage) current() (keymap.Binding, bool) {
	rows := k.rows()
	if k.sel < 0 || k.sel >= len(rows) {
		return keymap.Binding{}, false
	}
	return rows[k.sel], true
}

// Update implements PageModel.
func (k *KeymapPage) Update(key tea.KeyPressMsg) tea.Cmd {
	if k.capturing {
		return k.updateCapture(key)
	}
	if k.filtering {
		return k.updateFilter(key)
	}
	switch key.String() {
	case "up", "k":
		if k.sel > 0 {
			k.sel--
		}
	case "down", "j":
		if k.sel < len(k.rows())-1 {
			k.sel++
		}
	case "enter":
		if _, ok := k.current(); ok {
			k.capturing = true
			k.steps, k.conflict, k.warn, k.invalid = nil, "", "", ""
		}
	case "u":
		// Unbind: an override chord→"" drops the binding on reload.
		if b, ok := k.current(); ok {
			return config.WriteAndReload(k.opts, config.UserScope, "keymap.bindings."+b.Chord.String(), "")
		}
	case "r":
		// Reset to preset: remove the override; the default falls back.
		if b, ok := k.current(); ok {
			return config.RemoveAndReload(k.opts, config.UserScope, "keymap.bindings."+b.Chord.String())
		}
	case "backspace":
		if k.filter != "" {
			k.filter = k.filter[:len(k.filter)-1]
			k.sel = 0
		}
	case "/":
		// Explicit filter input (#531), mirroring the schema pages: while it
		// is open every printable key is filter text, so terms containing the
		// action letters (u/r/j/k) type instead of firing actions.
		k.filtering = true
	}
	return nil
}

// updateFilter handles keys while the filter input is open: enter keeps the
// filter and returns to the list, esc clears it, backspace edits, printable
// text appends verbatim.
func (k *KeymapPage) updateFilter(key tea.KeyPressMsg) tea.Cmd {
	switch key.Code {
	case tea.KeyEscape:
		k.filtering = false
		k.filter = ""
		k.sel = 0
	case tea.KeyEnter:
		k.filtering = false
	case tea.KeyBackspace:
		if k.filter != "" {
			k.filter = k.filter[:len(k.filter)-1]
			k.sel = 0
		}
	default:
		if key.Text != "" {
			k.filter += key.Text
			k.sel = 0
		}
	}
	return nil
}

// updateCapture accumulates chord steps; enter confirms (running conflict
// detection first), esc cancels.
func (k *KeymapPage) updateCapture(key tea.KeyPressMsg) tea.Cmd {
	b, ok := k.current()
	if !ok {
		k.capturing = false
		return nil
	}
	// A pending conflict waits for an explicit confirm/cancel.
	if k.conflict != "" {
		switch key.Code {
		case tea.KeyEnter:
			return k.commitRebind(b)
		default:
			k.capturing, k.conflict, k.steps = false, "", nil
		}
		return nil
	}
	switch key.Code {
	case tea.KeyEscape:
		k.capturing, k.steps, k.warn = false, nil, ""
		return nil
	case tea.KeyEnter:
		if len(k.steps) == 0 {
			k.capturing = false
			return nil
		}
		chord := k.captured()
		k.warn = fragileWarning(chord)
		if other, found := k.conflictWith(chord, b); found {
			k.conflict = other
			return nil
		}
		return k.commitRebind(b)
	}
	if kk, ok := keymap.FromKeyMsg(key); ok {
		k.steps = append(k.steps, keymap.NormalizeKey(kk, keymap.GOOS))
		k.warn = fragileWarning(k.captured())
	}
	return nil
}

// captured is the chord built from the recorded steps.
func (k *KeymapPage) captured() keymap.Chord { return keymap.Chord{Steps: k.steps} }

// conflictWith reports the command a chord would collide with in the effective
// table (same chord, overlapping context), ignoring the binding being rebound.
func (k *KeymapPage) conflictWith(chord keymap.Chord, self keymap.Binding) (string, bool) {
	cs := chord.String()
	for _, b := range k.table().Bindings() {
		if b.Chord.String() != cs {
			continue
		}
		if b.Chord.Equal(self.Chord) && b.Command == self.Command {
			continue
		}
		if b.Context.Matches(self.Context) || self.Context.Matches(b.Context) ||
			b.Context == keymap.Global || self.Context == keymap.Global {
			return b.Command, true
		}
	}
	return "", false
}

// commitRebind writes the captured chord for the selected binding's command
// and unbinds the old chord when it changed. Both writes land before one
// reload, so the table re-resolves atomically.
func (k *KeymapPage) commitRebind(b keymap.Binding) tea.Cmd {
	chord := k.captured()
	k.capturing, k.conflict, k.steps = false, "", nil
	if chord.Len() == 0 {
		return nil
	}
	opts := k.opts
	newKey := "keymap.bindings." + chord.String()
	oldKey := "keymap.bindings." + b.Chord.String()
	command := b.Command
	sameChord := chord.Equal(b.Chord)
	return func() tea.Msg {
		var diags []config.Diagnostic
		if err := config.WriteKey(opts, config.UserScope, newKey, command); err != nil {
			diags = append(diags, config.Diagnostic{Field: newKey, Message: err.Error()})
		}
		if !sameChord {
			if err := config.WriteKey(opts, config.UserScope, oldKey, ""); err != nil {
				diags = append(diags, config.Diagnostic{Field: oldKey, Message: err.Error()})
			}
		}
		c, loadDiags := config.Load(opts)
		return config.ConfigReloadedMsg{Config: c, Diags: append(loadDiags, diags...)}
	}
}

// fragileWarning flags chords terminals/OSes commonly intercept (the 0081
// honesty rule): cmd-modified keys rarely reach a macOS terminal, and ctrl+tab
// is fragile in most emulators.
func fragileWarning(c keymap.Chord) string {
	for _, s := range c.Steps {
		str := s.String()
		if strings.HasPrefix(str, "cmd+") {
			return "cmd chords may be intercepted by the terminal/OS"
		}
		if str == "ctrl+tab" || str == "ctrl+i" || str == "ctrl+m" {
			return str + " is fragile in many terminals"
		}
	}
	return ""
}

// theme returns the active palette, defaulting when none was threaded in.
func (k *KeymapPage) theme() *theme.Palette {
	if k.pal != nil {
		return k.pal
	}
	return theme.DefaultPalette()
}

// View implements PageModel.
func (k *KeymapPage) View(w, h int) string {
	pal := k.theme()
	rows := k.rows()
	head := " chord · command · context · layer"
	switch {
	case k.filtering:
		head += "   filter: " + k.filter + "▌"
	case k.filter != "":
		head += "   filter: " + k.filter
	default:
		head += "   (/ to filter)"
	}
	var list []string
	for i, b := range rows {
		list = append(list, k.renderRow(b, i == k.sel, w))
	}
	if len(rows) == 0 {
		list = append(list, "no bindings match")
	}
	// The detail line is a footer pinned below the list (#537): moving the
	// selection never shifts the rows, and the list scrolls to follow it.
	// It wraps to the column width over a constant two lines (#553).
	var footer []string
	if b, ok := k.current(); ok {
		footer = wrapFooter([]footerLine{k.detailLine(b)}, w, 2)
	}
	headLine := lipgloss.NewStyle().Foreground(pal.Secondary).Render(head)
	return headLine + "\n" + pinFooter(list, footer, k.sel, k.sel, h-1, &k.off)
}

// renderRow renders one binding line.
func (k *KeymapPage) renderRow(b keymap.Binding, selected bool, w int) string {
	pal := k.theme()
	chord := b.Chord.String()
	if selected && k.capturing {
		if len(k.steps) > 0 {
			chord = k.captured().String() + "…"
		} else {
			chord = "press keys, enter to confirm…"
		}
	}
	label := " " + pad(chord, 22) + pad(b.Title, 32) + pad(string(b.Context), 10) + "@" + b.Layer.String()
	if reason, blocked := keymap.BlockedReason(b.Command); blocked || (k.registered != nil && !k.registered(b.Command)) {
		hint := reason
		if hint == "" {
			hint = "not registered"
		}
		style := lipgloss.NewStyle().Foreground(pal.Secondary).Faint(true)
		if selected {
			style = style.Background(pal.Selection).Foreground(pal.SelectionText)
		}
		return style.Render(label + "  ✗ " + hint)
	}
	style := lipgloss.NewStyle()
	switch {
	case selected:
		style = style.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	case b.Layer != keymap.LayerDefault:
		style = style.Foreground(pal.Info)
	}
	if b.Fragile {
		label += "  ⚠"
	}
	return style.Render(label)
}

// detailLine names the capture status / warning / hint under the selection,
// as text + style (wrapped by the caller, #553).
func (k *KeymapPage) detailLine(b keymap.Binding) footerLine {
	pal := k.theme()
	switch {
	case k.conflict != "":
		return footerLine{
			text:  "   conflicts with " + k.conflict + " — enter overrides, any other key cancels",
			style: lipgloss.NewStyle().Foreground(pal.Error),
		}
	case k.warn != "":
		return footerLine{text: "   ⚠ " + k.warn, style: lipgloss.NewStyle().Foreground(pal.Warning)}
	case k.capturing:
		return footerLine{text: "   esc cancels the capture", style: lipgloss.NewStyle().Foreground(pal.Secondary)}
	default:
		return footerLine{
			text:  "   " + b.Command + " — enter rebind · u unbind · r reset to preset",
			style: lipgloss.NewStyle().Foreground(pal.Secondary),
		}
	}
}

// pad right-pads (or trims) s to width n.
func pad(s string, n int) string {
	if lipgloss.Width(s) >= n {
		return s[:n-1] + " "
	}
	return s + strings.Repeat(" ", n-lipgloss.Width(s))
}
