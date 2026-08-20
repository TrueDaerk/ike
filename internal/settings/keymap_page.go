package settings

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	navRows    // last rendered height, the pgup/pgdn page (#1666)
	opts       config.Options
	registered func(commandID string) bool
	commands   func() []CommandEntry
	pal        *theme.Palette

	sel       int
	host      SubPanelHost
	off       int // list scroll offset (#537)
	filter    string
	filterCur int  // rune cursor inside filter (#2002)
	filtering bool // "/" opened the filter input; every key is filter text

	conflict string // colliding command id awaiting confirmation
	warn     string // fragile-chord honesty warning
	invalid  string

	// JetBrains keymap import (#677): "i" opens an inline path input with
	// filesystem completion; enter runs the import, importNote reports it.
	importField textField // shared cursor input (#888)
	importSug   pathSuggest
	importNote  string

	listH int // list-window height of the last render (mouse hit-testing, #674)
	listW int // settings-column width of the last render (#1298), 0 = full width
	// expanded remembers which numbered runs are unfolded (#1300), per
	// session — a fold the user opened stays open while the panel is used.
	expanded map[string]bool

	// Memoization (#1396): BuildTable resolves the full overlay stack and
	// rows() filters+sorts every command — repeated per key event and per
	// render frame, that saturated the Update loop and froze the panel. The
	// table is keyed on the live *config.Config (every edit reloads the config,
	// which swaps the pointer); the row list additionally on the filter text
	// and the fold generation. Commands registered after the cache filled
	// (plugins) appear on the next config reload.
	cachedTable  *keymap.BindingTable
	tableCfg     *config.Config
	rowCache     []keymapRow
	rowCacheOK   bool
	cacheFilter  string
	foldGen      int // bumped by toggleRange
	cacheFoldGen int
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

// SetSubPanelHost implements the hostAware injection seam (#883).
func (k *KeymapPage) SetSubPanelHost(h SubPanelHost) { k.host = h }

// Capturing implements PageModel: while a rebind capture (or its conflict
// confirmation) or the filter input (#531) is active the page needs every key
// verbatim — filter text may contain the page's own action letters (u/r/j/k).
func (k *KeymapPage) Capturing() bool { return k.filtering }

// keymapRow is one list entry: an effective binding, or a preset default that
// is no longer effective (#736). An unbound row keeps the default chord so "r"
// can remove exactly that override; enter captures a fresh chord for the
// command.
type keymapRow struct {
	keymap.Binding
	// rangeKey/rangeCount/rangeLast mark a folded run of numbered bindings
	// (#1300): the row stands for rangeCount bindings ending at rangeLast.
	rangeKey   string
	rangeCount int
	rangeLast  string
	unbound    bool
	// nobind marks a registered command with no binding at all (#771): no
	// chord to unbind or reset, enter captures its first chord.
	nobind bool
}

// table builds the effective binding table from the live config — the same
// construction the app's resolver uses, so the page always shows reality.
func (k *KeymapPage) table() *keymap.BindingTable {
	c := config.Get()
	if k.cachedTable == nil || k.tableCfg != c {
		k.cachedTable, k.tableCfg = keymap.BuildTable(k.defaults(), c.Keymap.Bindings, keymap.GOOS), c
		k.rowCacheOK = false // the rows derive from the table
	}
	return k.cachedTable
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
	k.table() // refresh the table cache; a config change drops the row cache too
	if !k.rowCacheOK || k.filter != k.cacheFilter || k.foldGen != k.cacheFoldGen {
		k.rowCache = k.foldRanges(k.unfoldedRows())
		k.rowCacheOK, k.cacheFilter, k.cacheFoldGen = true, k.filter, k.foldGen
	}
	return k.rowCache
}

// unfoldedRows is rows() before numbered runs are folded (#1300).
func (k *KeymapPage) unfoldedRows() []keymapRow {
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
	if k.filtering {
		return k.updateFilter(key)
	}
	if listNav(key.String(), &k.sel, len(k.rows()), k.navPageSize()) {
		return nil
	}
	switch key.String() {
	case "z":
		k.toggleRange()
		return nil
	case "enter":
		if b, ok := k.current(); ok && b.rangeKey != "" {
			k.toggleRange() // a folded run opens before it can be rebound
			return nil
		}
		if b, ok := k.current(); ok && k.host != nil {
			k.host.Push(newKeymapCapture(k, k.host, b))
		}
	case "u":
		// Unbind: an override chord→"" drops the binding on reload. An
		// already-unbound row has nothing to drop. Confirmed (#891) — an
		// unbind is easy to fat-finger and non-obvious to restore.
		if b, ok := k.current(); ok && !b.unbound && !b.nobind && k.host != nil {
			chord := b.Chord.String()
			key := k.unbindKeyFor(b)
			k.host.Push(newConfirm(k.host, "unbind "+chord, "Unbind", k.pal, func() tea.Cmd {
				return config.WriteAndReload(k.opts, config.UserScope, key, "")
			}))
		}
	case "r":
		// Reset to preset: remove the override; the default falls back. A
		// never-bound command has no override to remove.
		if b, ok := k.current(); ok && !b.nobind {
			return config.RemoveAndReload(k.opts, config.UserScope, k.overrideKeyFor(b))
		}
	case "backspace":
		// List mode: backspace shortens the filter from the end. The typed
		// filter itself is edited in filter mode ("/"), through ui.EditKey.
		if r := []rune(k.filter); len(r) > 0 {
			k.filter, k.filterCur = string(r[:len(r)-1]), len(r)-1
			k.sel = 0
		}
	case "/":
		// Explicit filter input (#531), mirroring the schema pages: while it
		// is open every printable key is filter text, so terms containing the
		// action letters (u/r/j/k) type instead of firing actions.
		k.filtering = true
	case "i":
		// JetBrains keymap import (#677): inline path input with completion.
		if k.host != nil {
			k.host.Push(newKeymapImport(k, k.host))
		}
		k.importField.text = "~" + string(filepath.Separator)
		k.importNote = ""
		k.importSug.refresh(k.importField.text)
	}
	return nil
}

// updateFilter handles keys while the filter input is open: enter keeps the
// filter and returns to the list, esc clears it, everything else is shared
// line editing via ui.EditKey (#2002).
func (k *KeymapPage) updateFilter(key tea.KeyPressMsg) tea.Cmd {
	switch key.Code {
	case tea.KeyEscape:
		k.filtering = false
		k.filter, k.filterCur = "", 0
		k.sel = 0
	case tea.KeyEnter:
		k.filtering = false
	default:
		if _, changed := filterKey(key, &k.filter, &k.filterCur); changed {
			k.sel = 0
		}
	}
	return nil
}

// Paste inserts into the page's live filter while it is open (#2002). The
// page otherwise captures chords, not text, so there is nothing to paste
// into.
func (k *KeymapPage) Paste(text string) bool {
	if !k.filtering {
		return false
	}
	if !filterPaste(text, &k.filter, &k.filterCur) {
		return false
	}
	k.sel = 0
	return true
}

// commitImport runs the JetBrains keymap import for the typed path: the
// export's shortcuts become keymap.bindings.* overrides at user scope
// (replaced default chords are unbound), then the config reloads through the
// normal pipeline. The outcome lands in importNote for the footer.
func (k *KeymapPage) commitImportPath(raw string) tea.Cmd {
	path := strings.TrimSpace(raw)
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

// commitRebind writes the captured chord for the selected row's command
// and unbinds the old chord when it changed. Both writes land before one
// reload, so the table re-resolves atomically. An unbound row (#736) has no
// live chord to drop — its default chord's ""-override stays as-is (it is what
// keeps that chord unbound) and the new chord simply binds the command again.
//
// byContext qualifies both writes with the row's context (0460, #1312): the
// chord then binds — and the old one unbinds — in that pane only, which is how
// "keep both, resolve by context" leaves the colliding command untouched
// everywhere else.
func (k *KeymapPage) commitRebindChord(b keymapRow, chord keymap.Chord, byContext bool) tea.Cmd {
	if chord.Len() == 0 {
		return nil
	}
	opts := k.opts
	newKey := keymap.BindingConfigKey(b.Context, chord.String(), byContext)
	oldKey := keymap.BindingConfigKey(b.Context, b.Chord.String(), byContext)
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

// View implements PageModel. The page renders on the panel's grid (#1298):
// the chord/command table in the settings column, the selected command's
// detail — every binding, its provenance, its conflicts — in the third.
func (k *KeymapPage) View(w, h int) string {
	k.setRows(h)
	listW, detailW, side := splitGrid(w)
	list := k.renderList(listW, h)
	if !side {
		return list
	}
	k.listW = listW
	return lipgloss.JoinHorizontal(lipgloss.Top, list, columnRule(h), k.renderDetail(detailW, h))
}

// renderList renders the chord/command table.
func (k *KeymapPage) renderList(w, h int) string {
	pal := k.theme()
	rows := k.rows()
	head := " chord · command"
	switch {
	case k.filtering:
		head += "   filter: " + filterView(k.filter, k.filterCur)
	case k.filter != "":
		head += "   filter: " + k.filter
	default:
		head += "   (/ to filter)"
	}
	// Every line is clipped to the column width so the settings column has a
	// fixed edge (#1298): an over-long row must not push the detail column
	// sideways on its line alone.
	clip := lipgloss.NewStyle().MaxWidth(w)
	// The detail line is a footer pinned below the list (#537): moving the
	// selection never shifts the rows, and the list scrolls to follow it.
	// It wraps to the column width over a constant two lines (#553).
	var footer []string
	if b, ok := k.current(); ok {
		footer = wrapFooter([]footerLine{k.detailLine(b)}, w, 2)
	}
	headLine := lipgloss.NewStyle().Foreground(pal.Secondary).Render(head)
	k.listH = h - 1 - len(footer)
	if len(rows) == 0 {
		return headLine + "\n" + pinFooter([]string{"no bindings match"}, footer, 0, 0, h-1, &k.off)
	}
	// Only the visible window is styled (#1396): the full command list runs to
	// hundreds of rows, and rendering them all per frame is what a mouse move
	// over the panel used to pay.
	render := func(i int) string { return clip.Render(k.renderRow(rows[i], i == k.sel, w)) }
	return headLine + "\n" + pinFooterLazy(len(rows), render, footer, k.sel, k.sel, h-1, &k.off)
}

// Click implements the optional PageClicker seam (#674): the header row opens
// the filter input, a press on a binding selects it and a press on the
// selection starts the chord capture (enter semantics). A press during a
// capture or its conflict confirmation cancels it (the mouse cannot be part
// of a chord); a press while the filter input is open keeps the filter and
// returns to the list (enter semantics).
func (k *KeymapPage) Click(x, y int) tea.Cmd {
	if k.filtering {
		k.filtering = false
		return nil
	}
	if k.listW > 0 && x >= k.listW {
		return nil // the detail column is read-only chrome (#1298)
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
		if b, ok := k.current(); ok && k.host != nil {
			k.host.Push(newKeymapCapture(k, k.host, b))
		}
		return nil
	}
	k.sel = idx
	return nil
}

// Wheel implements the optional PageWheeler seam (#674): the list moves its
// selection (it follows, like j/k); inert during capture/filter input.
func (k *KeymapPage) Wheel(delta int) {
	if k.filtering {
		return
	}
	if n := len(k.rows()); n > 0 {
		k.sel = clamp(k.sel+delta, 0, n-1)
	}
}

// renderRow renders one binding line.
func (k *KeymapPage) renderRow(b keymapRow, selected bool, w int) string {
	pal := k.theme()
	chord, title := b.Chord.String(), b.Title
	if b.unbound {
		chord = "(unbound)"
	}
	if b.nobind {
		chord = "(no binding)"
	}
	if b.rangeKey != "" {
		// A folded run reads as one fact (#1300); ▸ says it opens.
		chord, title = b.rangeLabel()
		title += " ▸ " + strconv.Itoa(b.rangeCount)
	}
	// Layer and provenance live in the detail column (#1298); the table is
	// the chord, what it runs, and — since the context set covers every pane
	// (#1794) — the pane scope a chord is confined to, so per-context pairs
	// (ctrl+t in terminal vs editor) read apart at a glance. Global rows stay
	// untagged: the scope tag marks the exception, not the rule.
	chordW := 18
	if w < 44 {
		chordW = 14
	}
	if !b.nobind && b.Context != keymap.Global {
		title += " [" + keymap.ContextName(b.Context) + "]"
	}
	// Cross-context shadow marker (#1875): both halves of a pane-vs-global
	// overlap are flagged in the gutter at the chord — the leftmost column, so
	// a clipped title can never hide it; the detail column names the direction.
	gutter := " "
	if shadowing, shadowedBy := k.shadowInfo(b); len(shadowing)+len(shadowedBy) > 0 {
		gutter = "⊘"
	}
	label := gutter + pad(chord, chordW) + title
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

// conflictWith reports the binding a chord already carries in the effective
// table, ignoring the binding being rebound. A binding in a context that never
// overlaps is reported too (#1312): the plain rebind writes an unqualified
// override, which takes the chord in *every* context and would clobber it
// silently — the decision belongs to the user, who can keep both instead.
// An overlapping binding is preferred as the reported one, since it is the
// collision that would actually shadow something.
func (k *KeymapPage) conflictWith(chord keymap.Chord, self keymapRow) (keymap.Binding, bool) {
	cs := chord.String()
	var fallback keymap.Binding
	found := false
	for _, b := range k.table().Bindings() {
		if b.Chord.String() != cs {
			continue
		}
		if b.Chord.Equal(self.Chord) && b.Command == self.Command {
			continue
		}
		if !separableContexts(b.Context, self.Context) {
			return b, true
		}
		if !found {
			fallback, found = b, true
		}
	}
	return fallback, found
}

// overrideKeyFor returns the config key that carries this row's override: the
// context-qualified spelling when one is set for exactly this chord+context
// (#1312), the flat one otherwise. It is what "r" removes to reset a binding.
func (k *KeymapPage) overrideKeyFor(b keymapRow) string {
	chord := b.Chord.String()
	qualified := keymap.BindingConfigKey(b.Context, chord, true)
	if _, ok := config.Get().Keymap.Bindings[strings.TrimPrefix(qualified, "keymap.bindings.")]; ok {
		return qualified
	}
	return "keymap.bindings." + chord
}

// unbindKeyFor returns the config key that drops exactly this binding. A flat
// chord→"" unbinds every context at once, so a pane-scoped binding whose chord
// another context also uses is unbound through its qualified key instead —
// otherwise "unbind" on one half of a "keep both" pair would silently remove
// the other half too (#1312).
func (k *KeymapPage) unbindKeyFor(b keymapRow) string {
	chord := b.Chord.String()
	if b.Context != keymap.Global {
		for _, other := range k.table().Bindings() {
			if other.Chord.String() == chord && other.Context != b.Context {
				return keymap.BindingConfigKey(b.Context, chord, true)
			}
		}
	}
	return "keymap.bindings." + chord
}

// separableContexts reports whether two bindings can share a chord without
// shadowing each other — both are pane-scoped and the panes differ (0460,
// #1312). A Global binding matches in every pane, so it always overlaps and
// "keep both" is not on offer for it.
func separableContexts(a, b keymap.Context) bool {
	return a != keymap.Global && b != keymap.Global && a != b
}
