package settings

import (
	"encoding/json"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/keymap"
	"ike/internal/lang"
)

// migrated_panels.go completes the sub-panel migrations (0420, #892): the
// keymap chord capture and JetBrains-import path, the LSP override editor,
// and the uv-install Python picker leave their inline footer states for
// pushed sub-panels — breadcrumbs, buttons, one esc level, mouse-complete.

// --- keymap chord capture ---

// keymapCapture rebinds one command: the same semantics the footer flow had
// (multi-step chords, fragile-chord warning, conflict confirm) in a dialog.
type keymapCapture struct {
	page *KeymapPage
	host SubPanelHost
	row  keymapRow

	steps    []keymap.Key
	conflict string
	warn     string
	// other is the colliding binding behind conflict (0460, #1312): its
	// context decides whether "keep both, resolve by context" is possible.
	other keymap.Binding
	// langMode is the "keep both, limit to file type" input (#1876): the typed
	// language id scopes the new binding to editor[<lang>], leaving the
	// colliding command in charge everywhere else.
	langMode  bool
	langField textField
	langErr   string
}

func newKeymapCapture(page *KeymapPage, host SubPanelHost, row keymapRow) *keymapCapture {
	return &keymapCapture{page: page, host: host, row: row}
}

func (c *keymapCapture) Title() string   { return "Rebind " + c.row.Command }
func (c *keymapCapture) Capturing() bool { return true }

func (c *keymapCapture) Buttons() []Button {
	if c.langMode {
		return []Button{
			{Label: "Apply", Key: "enter", Do: c.commitLang},
			{Label: "Back", Do: func() tea.Cmd { c.leaveLangMode(); return nil }},
			{Label: "Cancel", Do: func() tea.Cmd { c.host.Pop(); return nil }},
		}
	}
	if c.conflict != "" {
		// A collision is a decision, not a yes/no (#1298): take the chord
		// from the other command, keep both when their panes never overlap
		// (#1312) or when one file type suffices (#1876), or go back and
		// record a different one.
		btns := []Button{{Label: "Replace & unbind other", Key: "enter", Do: c.commit}}
		if c.canKeepBoth() {
			btns = append(btns, Button{Label: "Keep both, resolve by context", Key: "b", Do: c.keepBoth})
		}
		if c.canKeepByLang() {
			btns = append(btns, Button{Label: "Keep both, limit to file type", Key: "l", Do: c.enterLangMode})
		}
		return append(btns,
			Button{Label: "Pick a different chord", Key: "p", Do: func() tea.Cmd {
				c.steps, c.conflict, c.warn, c.other = nil, "", "", keymap.Binding{}
				return nil
			}},
			Button{Label: "Cancel", Do: func() tea.Cmd { c.host.Pop(); return nil }},
		)
	}
	return []Button{
		{Label: "Apply", Do: c.confirm, Disabled: len(c.steps) == 0},
		{Label: "Cancel", Do: func() tea.Cmd { c.host.Pop(); return nil }},
	}
}

func (c *keymapCapture) chord() keymap.Chord { return keymap.Chord{Steps: c.steps} }

// Paste inserts into the language-id input while it is open (#1876); the
// chord capture itself has nothing to paste into.
func (c *keymapCapture) Paste(text string) bool {
	if !c.langMode {
		return false
	}
	if !c.langField.Paste(text) {
		return false
	}
	c.langErr = ""
	return true
}

func (c *keymapCapture) Update(key tea.KeyPressMsg) tea.Cmd {
	// The language input (#1876) is a text field: enter applies, esc returns
	// to the conflict decision, everything else edits the typed id.
	if c.langMode {
		switch key.Code {
		case tea.KeyEscape:
			c.leaveLangMode()
			return nil
		case tea.KeyEnter:
			return c.commitLang()
		}
		c.langField.Handle(key)
		c.langErr = ""
		return nil
	}
	// A pending conflict waits for an explicit decision (#1298): enter takes
	// the chord, "b"/"l" keep both, "p" records a different one, esc abandons
	// the rebind.
	if c.conflict != "" {
		switch key.String() {
		case "enter":
			return c.commit()
		case "b":
			if c.canKeepBoth() {
				return c.keepBoth()
			}
		case "l":
			if c.canKeepByLang() {
				return c.enterLangMode()
			}
		case "p":
			c.steps, c.conflict, c.warn, c.other = nil, "", "", keymap.Binding{}
			return nil
		}
		c.host.Pop()
		return nil
	}
	switch key.Code {
	case tea.KeyEscape:
		c.host.Pop()
		return nil
	case tea.KeyEnter:
		return c.confirm()
	case tea.KeyBackspace:
		if len(c.steps) > 0 {
			c.steps = c.steps[:len(c.steps)-1]
			c.warn = fragileWarning(c.chord())
			return nil
		}
	}
	kk, ok := keymap.FromKeyMsg(key)
	if !ok {
		// A press the chord format cannot carry used to vanish without a trace
		// (#1331) — the field simply stayed empty. Say so instead.
		c.warn = "cannot record " + strconv.Quote(key.String()) + " as a chord"
		return nil
	}
	c.steps = append(c.steps, keymap.NormalizeKey(kk, keymap.GOOS))
	c.warn = fragileWarning(c.chord())
	// A recorded step must survive the round-trip through the config's chord
	// string, or the binding would be written and never read back (#1331).
	if _, err := keymap.ParseChord(c.chord().String()); err != nil {
		c.steps = c.steps[:len(c.steps)-1]
		c.warn = "cannot record " + strconv.Quote(key.String()) + " as a chord"
	}
	return nil
}

// confirm runs the conflict check, then commits.
func (c *keymapCapture) confirm() tea.Cmd {
	if len(c.steps) == 0 {
		c.host.Pop()
		return nil
	}
	if other, found := c.page.conflictWith(c.chord(), c.row); found {
		c.conflict, c.other = other.Command, other
		return nil
	}
	return c.commit()
}

// canKeepBoth reports whether the collision can be resolved by context rather
// than by taking the chord away (#1312): both commands must be pane-scoped, in
// different panes, so neither ever shadows the other.
func (c *keymapCapture) canKeepBoth() bool {
	return c.conflict != "" && separableContexts(c.row.Context, c.other.Context)
}

// canKeepByLang reports whether the collision can be narrowed to one file type
// (#1876): the rebound command must be able to live under editor[<lang>] — its
// row is Global or editor-scoped and not language-scoped already. The other
// binding stays put; in editors of the chosen language it is shadowed (the
// point of the narrowing), everywhere else it keeps the chord.
func (c *keymapCapture) canKeepByLang() bool {
	base := c.row.Context.Base()
	return c.conflict != "" && c.row.Context.Lang() == "" &&
		(base == keymap.Global || base == keymap.Editor)
}

// enterLangMode opens the language-id input for "keep both, limit to file type".
func (c *keymapCapture) enterLangMode() tea.Cmd {
	c.langMode, c.langField, c.langErr = true, newTextField(""), ""
	return nil
}

// leaveLangMode returns to the conflict decision without writing anything.
func (c *keymapCapture) leaveLangMode() {
	c.langMode, c.langErr = false, ""
}

// commitLang validates the typed language id and writes the chord as an
// editor[<lang>]-scoped override (#1876). Bad input stays in the panel with a
// clear message: the id must have the qualifier shape and name a registered
// language, or the binding could never match a buffer.
func (c *keymapCapture) commitLang() tea.Cmd {
	id := strings.TrimSpace(c.langField.Text)
	if !keymap.ValidLangQualifier(id) {
		c.langErr = "language id: lowercase letters, digits and -_+# only"
		return nil
	}
	if _, ok := lang.ByID(id); !ok {
		c.langErr = "unknown language id " + strconv.Quote(id)
		return nil
	}
	c.host.Pop()
	return c.page.commitRebindLang(c.row, c.chord(), id)
}

func (c *keymapCapture) commit() tea.Cmd {
	c.host.Pop()
	return c.page.commitRebindChord(c.row, c.chord(), false)
}

// keepBoth writes the chord as a context-qualified override, leaving the
// colliding command bound in its own context. Both bindings stay; the resolver
// picks the one whose pane is focused.
func (c *keymapCapture) keepBoth() tea.Cmd {
	c.host.Pop()
	return c.page.commitRebindChord(c.row, c.chord(), true)
}

func (c *keymapCapture) View(w, h int) string {
	pal := c.page.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	warn := lipgloss.NewStyle().Foreground(pal.Error)
	if c.langMode {
		lines := []string{
			sec.Render(" Limit " + c.row.Command + " on " + c.chord().String() + " to one file type."),
			sec.Render(" Language id (e.g. http, go, markdown):"),
			" " + c.langField.View(),
		}
		if c.langErr != "" {
			lines = append(lines, warn.Render(" ✗ "+c.langErr))
		}
		lines = append(lines, sec.Render(" enter apply · esc back"))
		return strings.Join(lines, "\n")
	}
	shown := c.chord().String()
	if shown == "" {
		shown = "…"
	}
	lines := []string{
		sec.Render(" Press the new chord for " + c.row.Command + ":"),
		" " + lipgloss.NewStyle().Bold(true).Render(shown),
	}
	if c.warn != "" {
		lines = append(lines, warn.Render(" ⚠ "+c.warn))
	}
	if c.conflict != "" {
		where := " (" + keymap.ContextName(c.other.Context) + ")"
		lines = append(lines, warn.Render(" ⚠ already runs "+c.conflict+where))
		hint := " enter replace & unbind it · p pick a different chord · esc cancel"
		if c.canKeepBoth() {
			lines = append(lines, sec.Render(" b keeps both: "+c.row.Command+" in "+
				keymap.ContextName(c.row.Context)+", "+c.conflict+" in "+keymap.ContextName(c.other.Context)))
			hint = " enter replace · b keep both by context · p different chord · esc cancel"
		}
		if c.canKeepByLang() {
			lines = append(lines, sec.Render(" l keeps both, limiting "+c.row.Command+" to one file type (editor[lang])"))
			if c.canKeepBoth() {
				hint = " enter replace · b by context · l by file type · p different chord · esc cancel"
			} else {
				hint = " enter replace · l keep both by file type · p different chord · esc cancel"
			}
		}
		lines = append(lines, sec.Render(hint))
		if free := c.page.suggestChords(2); len(free) > 0 {
			lines = append(lines, sec.Render(" free: "+strings.Join(free, " · ")))
		}
	} else {
		lines = append(lines, sec.Render(" enter apply · backspace undo a step · esc cancel"))
	}
	return strings.Join(lines, "\n")
}

// --- keymap JetBrains import ---

// keymapImport is the import-path prompt (#677) as a sub-panel: a cursor
// input with clickable completion suggestions.
type keymapImport struct {
	page    *KeymapPage
	host    SubPanelHost
	path    textField
	suggest pathSuggest
}

func newKeymapImport(page *KeymapPage, host SubPanelHost) *keymapImport {
	return &keymapImport{page: page, host: host}
}

func (i *keymapImport) Title() string   { return "Import JetBrains Keymap" }
func (i *keymapImport) Capturing() bool { return true }

func (i *keymapImport) Buttons() []Button {
	return []Button{
		{Label: "Import", Do: i.commit},
		{Label: "Cancel", Do: func() tea.Cmd { i.host.Pop(); return nil }},
	}
}

func (i *keymapImport) Update(key tea.KeyPressMsg) tea.Cmd {
	switch key.Code {
	case tea.KeyEscape:
		i.host.Pop()
		return nil
	case tea.KeyEnter:
		return i.commit()
	case tea.KeyTab:
		i.path.Set(i.suggest.complete(i.path.Text))
		return nil
	}
	if _, changed := i.path.Handle(key); changed {
		i.suggest.refresh(i.path.Text)
	}
	return nil
}

// Paste inserts into the path input at its cursor (#2002) and refreshes the
// completion candidates, exactly like a typed rune does.
func (i *keymapImport) Paste(text string) bool {
	if !i.path.Paste(text) {
		return false
	}
	i.suggest.refresh(i.path.Text)
	return true
}

func (i *keymapImport) commit() tea.Cmd {
	i.host.Pop()
	return i.page.commitImportPath(i.path.Text)
}

// Click takes a completion suggestion (line 2 onward).
func (i *keymapImport) Click(_, y int) tea.Cmd {
	if idx := y - 2; idx >= 0 && idx < len(i.suggest.candidates) && idx < maxSuggestLines {
		i.path.Set(i.suggest.candidates[idx])
		i.suggest.refresh(i.path.Text)
	}
	return nil
}

func (i *keymapImport) View(w, h int) string {
	pal := i.page.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	clip := lipgloss.NewStyle().MaxWidth(w)
	lines := []string{
		clip.Render(sec.Render(" Path to the exported keymap XML (tab completes):")),
		clip.Render(" " + i.path.View()),
	}
	for _, s := range i.suggest.lines() {
		lines = append(lines, clip.Render(sec.Render(s)))
	}
	return strings.Join(lines, "\n")
}

// --- LSP override editor ---

// lspOverrideForm edits one server override (command / args / options JSON)
// in a dialog; empty input resets the override.
type lspOverrideForm struct {
	page  *LSPPage
	host  SubPanelHost
	lang  string
	kind  lspEditField
	input textField
	note  string
}

func newLSPOverrideForm(page *LSPPage, host SubPanelHost, lang string, kind lspEditField, initial string) *lspOverrideForm {
	return &lspOverrideForm{page: page, host: host, lang: lang, kind: kind, input: newTextField(initial)}
}

func (f *lspOverrideForm) Title() string {
	switch f.kind {
	case lspEditCommand:
		return "Edit Command"
	case lspEditArgs:
		return "Edit Args"
	default:
		return "Edit Options JSON"
	}
}

func (f *lspOverrideForm) Capturing() bool { return true }

func (f *lspOverrideForm) Buttons() []Button {
	return []Button{
		{Label: "Save", Do: f.commit},
		{Label: "Cancel", Do: func() tea.Cmd { f.host.Pop(); return nil }},
	}
}

func (f *lspOverrideForm) Update(key tea.KeyPressMsg) tea.Cmd {
	switch key.Code {
	case tea.KeyEscape:
		f.host.Pop()
		return nil
	case tea.KeyEnter:
		return f.commit()
	}
	f.input.Handle(key)
	return nil
}

// Paste inserts into the override input at its cursor (#2002).
func (f *lspOverrideForm) Paste(text string) bool { return f.input.Paste(text) }

func (f *lspOverrideForm) commit() tea.Cmd {
	if f.kind == lspEditSettings {
		if t := strings.TrimSpace(f.input.Text); t != "" && !json.Valid([]byte(t)) {
			f.note = "not valid JSON"
			return nil
		}
	}
	f.host.Pop()
	return f.page.commitOverride(f.lang, f.kind, f.input.Text)
}

func (f *lspOverrideForm) View(w, h int) string {
	pal := f.page.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	clip := lipgloss.NewStyle().MaxWidth(w)
	prompt := map[lspEditField]string{
		lspEditCommand:  "command",
		lspEditArgs:     "args (space-separated)",
		lspEditSettings: "settings (JSON object)",
	}[f.kind]
	lines := []string{
		clip.Render(sec.Render(" " + f.lang + " · " + prompt + "  (empty = reset)")),
		clip.Render(" " + f.input.View()),
	}
	if f.note != "" {
		lines = append(lines, clip.Render(lipgloss.NewStyle().Foreground(pal.Error).Render(" ✗ "+f.note)))
	}
	return strings.Join(lines, "\n")
}

// --- uv-install Python picker ---

// uvPickerPanel picks a downloadable Python for `uv python install`.
type uvPickerPanel struct {
	navRows  // last rendered height, the pgup/pgdn page (#1666)
	page     *ToolchainPage
	host     SubPanelHost
	versions []string
	pick     int
	off      int
}

func newUvPicker(page *ToolchainPage, host SubPanelHost, versions []string) *uvPickerPanel {
	return &uvPickerPanel{page: page, host: host, versions: versions}
}

func (u *uvPickerPanel) Title() string   { return "Install Python (uv)" }
func (u *uvPickerPanel) Capturing() bool { return false }

func (u *uvPickerPanel) Buttons() []Button {
	return []Button{
		{Label: "Install", Key: "enter", Do: u.install},
		{Label: "Cancel", Do: func() tea.Cmd { u.host.Pop(); return nil }},
	}
}

func (u *uvPickerPanel) Update(key tea.KeyPressMsg) tea.Cmd {
	listNav(key.String(), &u.pick, len(u.versions), u.navPageSize())
	return nil
}

func (u *uvPickerPanel) Wheel(delta int) {
	u.pick = clamp(u.pick+delta, 0, len(u.versions)-1)
}

// Click selects a row; a press on the selection installs.
func (u *uvPickerPanel) Click(_, y int) tea.Cmd {
	if idx := y - 1 + u.off; idx >= 0 && idx < len(u.versions) {
		if idx == u.pick {
			return u.install()
		}
		u.pick = idx
	}
	return nil
}

func (u *uvPickerPanel) install() tea.Cmd {
	if u.pick < 0 || u.pick >= len(u.versions) {
		return nil
	}
	version := u.versions[u.pick]
	u.host.Pop()
	u.page.envState = envBusy
	return uvInstall(version, u.page.run)
}

func (u *uvPickerPanel) View(w, h int) string {
	u.setRows(h)
	pal := u.page.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	sel := lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	clip := lipgloss.NewStyle().MaxWidth(w)
	lines := []string{clip.Render(sec.Render(" Downloadable versions (uv fetches on install):"))}
	u.off = follow(u.off, u.pick, u.pick, len(u.versions), h-1)
	end := u.off + h - 1
	if end > len(u.versions) {
		end = len(u.versions)
	}
	for i := u.off; i < end; i++ {
		line := " python " + u.versions[i]
		if i == u.pick {
			lines = append(lines, clip.Render(sel.Render(line)))
		} else {
			lines = append(lines, clip.Render(line))
		}
	}
	return strings.Join(lines, "\n")
}

// --- PHP debug path-mapping form ---

// debugMapForm is the add/edit mapping form as a sub-panel (#892).
type debugMapForm struct {
	page *DebugMapPage
	host SubPanelHost
	idx  int

	fieldNav // focused field + cursor within it (#888, #2466)
	form     [mapFieldCount]string
	note     string
}

func newDebugMapForm(page *DebugMapPage, host SubPanelHost, idx int) *debugMapForm {
	f := &debugMapForm{page: page, host: host, idx: idx}
	f.fieldNav = newFieldNav(mapFieldCount, func(i int) string { return f.form[i] })
	if idx >= 0 {
		e := page.entries()[idx]
		f.form = [mapFieldCount]string{e.Server, e.Local}
	}
	return f
}

func (f *debugMapForm) Title() string {
	if f.idx < 0 {
		return "New Mapping"
	}
	return "Edit Mapping"
}

func (f *debugMapForm) Capturing() bool { return true }

func (f *debugMapForm) Buttons() []Button {
	return []Button{
		{Label: "Save", Do: f.save},
		{Label: "Cancel", Do: func() tea.Cmd { f.host.Pop(); return nil }},
	}
}

func (f *debugMapForm) Update(key tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Code == tea.KeyEscape:
		f.host.Pop()
	case key.Code == tea.KeyEnter:
		return f.save()
	case f.fieldNav.Update(key): // shared field motion (#2466)
	default:
		tf := newTextFieldAt(f.form[f.field], f.cur)
		if handled, _ := tf.Handle(key); handled {
			f.form[f.field], f.cur = tf.Text, tf.Cur
		}
	}
	return nil
}

// Click focuses a field row.
func (f *debugMapForm) Click(_, y int) tea.Cmd {
	f.fieldNav.Focus(y)
	return nil
}

func (f *debugMapForm) validate() string {
	server := strings.TrimSpace(f.form[0])
	if server == "" {
		return "server path is required"
	}
	if strings.TrimSpace(f.form[1]) == "" {
		return "local path is required"
	}
	for i, e := range f.page.entries() {
		if i != f.idx && e.Server == server {
			return "a mapping for " + server + " already exists"
		}
	}
	return ""
}

// Paste inserts into the focused field at its cursor (#2002).
func (f *debugMapForm) Paste(text string) bool {
	tf := newTextFieldAt(f.form[f.field], f.cur)
	if !tf.Paste(text) {
		return false
	}
	f.form[f.field], f.cur = tf.Text, tf.Cur
	return true
}

func (f *debugMapForm) save() tea.Cmd {
	if msg := f.validate(); msg != "" {
		f.note = msg
		return nil
	}
	entry := config.DebugPathMap{
		Server: strings.TrimSpace(f.form[0]),
		Local:  strings.TrimSpace(f.form[1]),
	}
	entries := append([]config.DebugPathMap(nil), f.page.entries()...)
	if f.idx >= 0 && f.idx < len(entries) {
		entries[f.idx] = entry
	} else {
		entries = append(entries, entry)
	}
	f.host.Pop()
	return writeDebugMappings(f.page.opts, entries)
}

func (f *debugMapForm) View(w, h int) string {
	pal := f.page.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	clip := lipgloss.NewStyle().MaxWidth(w)
	lines := make([]string, 0, h)
	for i, name := range mapFieldNames {
		marker := "  "
		style := lipgloss.NewStyle()
		text := f.form[i]
		if i == f.field {
			marker = "▸ "
			style = style.Bold(true)
			text = newTextFieldAt(f.form[i], f.cur).View()
		}
		lines = append(lines, clip.Render(style.Render(" "+marker+pad(name, 8)+text)))
	}
	lines = append(lines, "")
	if f.note != "" {
		lines = append(lines, clip.Render(lipgloss.NewStyle().Foreground(pal.Error).Render(" ✗ "+f.note)))
	} else {
		lines = append(lines, clip.Render(sec.Render(" tab next field · enter saves · esc cancels")))
	}
	return strings.Join(lines, "\n")
}
