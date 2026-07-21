package settings

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/keymap"
	"ike/internal/keymap/jbimport"
	"ike/internal/theme"
)

// keymap_page.go is the keymap editor (#93): a custom settings page listing
// the effective binding table (context, chord, command, source layer) with a
// capture-based rebind flow. All edits are keymap.bindings.* overrides through
// the write-back layer: rebinding writes the new chord (and unbinds the old
// one), unbinding writes chord→"", and reset removes the override so the
// preset default falls back through the layers.

// CommandEntry is a registered command the keymap page can offer for binding
// (#771): id plus human-facing title. Configured tool commands (tool.<name>,
// #741) are registry commands and therefore appear too.
type CommandEntry struct {
	ID    string
	Title string
}

// KeymapPage implements PageModel.
type KeymapPage struct {
	opts       config.Options
	registered func(commandID string) bool
	commands   func() []CommandEntry
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

	// JetBrains keymap import (#677): "i" opens an inline path input with
	// filesystem completion; enter runs the import, importNote reports it.
	importing   bool
	importField textField // shared cursor input (#888)
	importSug   pathSuggest
	importNote  string

	listH int // list-window height of the last render (mouse hit-testing, #674)
}

// NewKeymapPage builds the keymap editor writing overrides through opts;
// registered reports whether a command id resolves in the registry (blocked
// ids render disabled-with-reason instead of hidden). commands lists every
// registered command so ids without any binding — plugin commands, configured
// tools — appear as bindable "(no binding)" rows (#771); nil hides none.
func NewKeymapPage(opts config.Options, registered func(commandID string) bool, commands func() []CommandEntry) *KeymapPage {
	return &KeymapPage{opts: opts, registered: registered, commands: commands}
}

// SetPalette implements PageModel.
func (k *KeymapPage) SetPalette(p *theme.Palette) { k.pal = p }

// Capturing implements PageModel: while a rebind capture (or its conflict
// confirmation) or the filter input (#531) is active the page needs every key
// verbatim — filter text may contain the page's own action letters (u/r/j/k).
func (k *KeymapPage) Capturing() bool { return k.capturing || k.filtering || k.importing }

// keymapRow is one list entry: an effective binding, or a preset default that
// is no longer effective (#736). An unbound row keeps the default chord so "r"
// can remove exactly that override; enter captures a fresh chord for the
// command.
type keymapRow struct {
	keymap.Binding
	unbound bool
	// nobind marks a registered command with no binding at all (#771): no
	// chord to unbind or reset, enter captures its first chord.
	nobind bool
}

// table builds the effective binding table from the live config — the same
// construction the app's resolver uses, so the page always shows reality.
func (k *KeymapPage) table() *keymap.BindingTable {
	c := config.Get()
	return keymap.BuildTable(k.defaults(), c.Keymap.Bindings, keymap.GOOS)
}

// defaults returns the active preset's default bindings.
func (k *KeymapPage) defaults() []keymap.Binding {
	c := config.Get()
	preset := strings.TrimSpace(c.Keymap.Preset)
	if preset == "" {
		preset = keymap.PresetJetBrains
	}
	return keymap.Defaults(preset)
}

// rows returns the visible rows, filtered and deterministically sorted
// (context, then chord): the effective bindings plus one unbound row per
// preset default that is no longer effective — its chord was unbound or
// rebound to another command (#736). The row keeps the command reachable for
// rebinding and carries the default chord for a per-binding reset.
func (k *KeymapPage) rows() []keymapRow {
	all := k.table().Bindings()
	have := make(map[string]bool, len(all))
	for _, b := range all {
		have[b.Command+"\x00"+b.Chord.String()] = true
	}
	rows := make([]keymapRow, 0, len(all))
	for _, b := range all {
		rows = append(rows, keymapRow{Binding: b})
	}
	haveCmd := make(map[string]bool, len(all))
	for _, b := range all {
		haveCmd[b.Command] = true
	}
	for _, d := range k.defaults() {
		d.Chord = keymap.NormalizeChord(d.Chord, keymap.GOOS)
		haveCmd[d.Command] = true
		if have[d.Command+"\x00"+d.Chord.String()] {
			continue
		}
		rows = append(rows, keymapRow{Binding: d, unbound: true})
	}
	// Registered commands with no binding at all — plugin commands, configured
	// tools — are listed as bindable "(no binding)" rows (#771).
	if k.commands != nil {
		for _, c := range k.commands() {
			if c.ID == "" || haveCmd[c.ID] {
				continue
			}
			haveCmd[c.ID] = true
			rows = append(rows, keymapRow{
				Binding: keymap.Binding{Command: c.ID, Title: c.Title, Context: keymap.Global},
				nobind:  true,
			})
		}
	}
	needle := strings.ToLower(k.filter)
	var out []keymapRow
	for _, b := range rows {
		if needle != "" {
			hay := strings.ToLower(b.Chord.String() + " " + b.Command + " " + b.Title + " " + string(b.Context))
			if b.unbound {
				hay += " unbound"
			}
			if b.nobind {
				hay += " no binding"
			}
			if !strings.Contains(hay, needle) {
				continue
			}
		}
		out = append(out, b)
	}
	sort.SliceStable(out, func(i, j int) bool {
		// Bound (and unbound-default) rows first; never-bound commands trail
		// the list, sorted by id (#771).
		if out[i].nobind != out[j].nobind {
			return !out[i].nobind
		}
		if out[i].nobind {
			return out[i].Command < out[j].Command
		}
		if out[i].Context != out[j].Context {
			return out[i].Context < out[j].Context
		}
		return out[i].Chord.String() < out[j].Chord.String()
	})
	return out
}

// current returns the selected row, if any.
func (k *KeymapPage) current() (keymapRow, bool) {
	rows := k.rows()
	if k.sel < 0 || k.sel >= len(rows) {
		return keymapRow{}, false
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
	if k.importing {
		return k.updateImport(key)
	}
	if listNav(key.String(), &k.sel, len(k.rows()), navPage) {
		return nil
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
		// Unbind: an override chord→"" drops the binding on reload. An
		// already-unbound row has nothing to drop.
		if b, ok := k.current(); ok && !b.unbound && !b.nobind {
			return config.WriteAndReload(k.opts, config.UserScope, "keymap.bindings."+b.Chord.String(), "")
		}
	case "r":
		// Reset to preset: remove the override; the default falls back. A
		// never-bound command has no override to remove.
		if b, ok := k.current(); ok && !b.nobind {
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
	case "i":
		// JetBrains keymap import (#677): inline path input with completion.
		k.importing = true
		k.importField.text = "~" + string(filepath.Separator)
		k.importNote = ""
		k.importSug.refresh(k.importField.text)
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

// updateImport handles keys while the JetBrains import path input is open
// (#677): tab completes against the filesystem, enter runs the import, esc
// cancels, backspace edits, printable text appends verbatim.
func (k *KeymapPage) updateImport(key tea.KeyPressMsg) tea.Cmd {
	switch key.Code {
	case tea.KeyEscape:
		k.importing = false
		k.importField = textField{}
		k.importSug.clear()
	case tea.KeyEnter:
		k.importing = false
		k.importSug.clear()
		return k.commitImport()
	case tea.KeyTab:
		k.importField.Set(k.importSug.complete(k.importField.text))
	default:
		// Shared cursor input (#888): rune-safe editing with word ops.
		if _, changed := k.importField.Handle(key); changed {
			k.importSug.refresh(k.importField.text)
		}
	}
	return nil
}

// commitImport runs the JetBrains keymap import for the typed path: the
// export's shortcuts become keymap.bindings.* overrides at user scope
// (replaced default chords are unbound), then the config reloads through the
// normal pipeline. The outcome lands in importNote for the footer.
func (k *KeymapPage) commitImport() tea.Cmd {
	path := strings.TrimSpace(k.importField.text)
	k.importField = textField{}
	if path == "" {
		return nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		if path == "~" || strings.HasPrefix(path, "~"+string(filepath.Separator)) {
			path = home + path[1:]
		}
	}
	f, err := os.Open(path)
	if err != nil {
		k.importNote = "import failed: " + err.Error()
		return nil
	}
	defer f.Close()
	c := config.Get()
	preset := strings.TrimSpace(c.Keymap.Preset)
	if preset == "" {
		preset = keymap.PresetJetBrains
	}
	opts := k.opts
	res, err := jbimport.Apply(f, keymap.Defaults(preset), func(key, value string) error {
		return config.WriteKey(opts, config.UserScope, key, value)
	})
	if err != nil {
		k.importNote = "import failed: " + err.Error()
		return nil
	}
	k.importNote = res.Summary()
	return func() tea.Msg {
		cfg, diags := config.Load(opts)
		return config.ConfigReloadedMsg{Config: cfg, Diags: diags}
	}
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
func (k *KeymapPage) conflictWith(chord keymap.Chord, self keymapRow) (string, bool) {
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

// commitRebind writes the captured chord for the selected row's command
// and unbinds the old chord when it changed. Both writes land before one
// reload, so the table re-resolves atomically. An unbound row (#736) has no
// live chord to drop — its default chord's ""-override stays as-is (it is what
// keeps that chord unbound) and the new chord simply binds the command again.
func (k *KeymapPage) commitRebind(b keymapRow) tea.Cmd {
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
	unbound := b.unbound || b.nobind
	return func() tea.Msg {
		var diags []config.Diagnostic
		if err := config.WriteKey(opts, config.UserScope, newKey, command); err != nil {
			diags = append(diags, config.Diagnostic{Field: newKey, Message: err.Error()})
		}
		if !sameChord && !unbound {
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
	if k.importing {
		footer = k.importFooter(w)
	} else if b, ok := k.current(); ok {
		footer = wrapFooter([]footerLine{k.detailLine(b)}, w, 2)
	}
	headLine := lipgloss.NewStyle().Foreground(pal.Secondary).Render(head)
	k.listH = h - 1 - len(footer)
	return headLine + "\n" + pinFooter(list, footer, k.sel, k.sel, h-1, &k.off)
}

// Click implements the optional PageClicker seam (#674): the header row opens
// the filter input, a press on a binding selects it and a press on the
// selection starts the chord capture (enter semantics). A press during a
// capture or its conflict confirmation cancels it (the mouse cannot be part
// of a chord); a press while the filter input is open keeps the filter and
// returns to the list (enter semantics).
func (k *KeymapPage) Click(_, y int) tea.Cmd {
	if k.capturing {
		k.capturing, k.conflict, k.steps, k.warn = false, "", nil, ""
		return nil
	}
	if k.filtering {
		k.filtering = false
		return nil
	}
	if y == 0 { // header row carries the filter display
		k.filtering = true
		return nil
	}
	row := y - 1
	if row < 0 || (k.listH > 0 && row >= k.listH) {
		return nil
	}
	idx := row + k.off
	if idx >= len(k.rows()) {
		return nil
	}
	if idx == k.sel {
		k.capturing = true
		k.steps, k.conflict, k.warn, k.invalid = nil, "", "", ""
		return nil
	}
	k.sel = idx
	return nil
}

// Wheel implements the optional PageWheeler seam (#674): the list moves its
// selection (it follows, like j/k); inert during capture/filter input.
func (k *KeymapPage) Wheel(delta int) {
	if k.capturing || k.filtering {
		return
	}
	if n := len(k.rows()); n > 0 {
		k.sel = clamp(k.sel+delta, 0, n-1)
	}
}

// renderRow renders one binding line.
func (k *KeymapPage) renderRow(b keymapRow, selected bool, w int) string {
	pal := k.theme()
	chord := b.Chord.String()
	if b.unbound {
		chord = "(unbound)"
	}
	if b.nobind {
		chord = "(no binding)"
	}
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
func (k *KeymapPage) detailLine(b keymapRow) footerLine {
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
	case k.importNote != "":
		return footerLine{
			text:  "   " + k.importNote + " — " + b.Command + " · enter rebind · u unbind · r reset · i import",
			style: lipgloss.NewStyle().Foreground(pal.Info),
		}
	case b.unbound:
		return footerLine{
			text:  "   " + b.Command + " — unbound (default " + b.Chord.String() + ") · enter set binding · r reset to preset",
			style: lipgloss.NewStyle().Foreground(pal.Secondary),
		}
	case b.nobind:
		return footerLine{
			text:  "   " + b.Command + " — no binding · enter set binding",
			style: lipgloss.NewStyle().Foreground(pal.Secondary),
		}
	default:
		return footerLine{
			text:  "   " + b.Command + " — enter rebind · u unbind · r reset to preset · i import JetBrains XML",
			style: lipgloss.NewStyle().Foreground(pal.Secondary),
		}
	}
}

// importFooter renders the JetBrains import path input pinned under the list
// (#677): the typed path plus the completion candidates.
func (k *KeymapPage) importFooter(w int) []string {
	pal := k.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	lines := []footerLine{
		{text: "   import JetBrains keymap XML: " + k.importField.View(), style: lipgloss.NewStyle()},
		{text: "   tab completes · enter imports · esc cancels", style: sec},
	}
	sug := k.importSug.lines()
	for _, s := range sug {
		lines = append(lines, footerLine{text: s, style: sec})
	}
	return wrapFooter(lines, w, 2+len(sug))
}

// pad right-pads (or trims) s to width n.
func pad(s string, n int) string {
	if lipgloss.Width(s) >= n {
		return s[:n-1] + " "
	}
	return s + strings.Repeat(" ", n-lipgloss.Width(s))
}
