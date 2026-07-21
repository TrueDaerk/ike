package settings

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/keymap"
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
}

func newKeymapCapture(page *KeymapPage, host SubPanelHost, row keymapRow) *keymapCapture {
	return &keymapCapture{page: page, host: host, row: row}
}

func (c *keymapCapture) Title() string   { return "Rebind " + c.row.Command }
func (c *keymapCapture) Capturing() bool { return true }

func (c *keymapCapture) Buttons() []Button {
	return []Button{
		{Label: "Apply", Do: c.confirm, Disabled: len(c.steps) == 0},
		{Label: "Cancel", Do: func() tea.Cmd { c.host.Pop(); return nil }},
	}
}

func (c *keymapCapture) chord() keymap.Chord { return keymap.Chord{Steps: c.steps} }

func (c *keymapCapture) Update(key tea.KeyPressMsg) tea.Cmd {
	// A pending conflict waits for an explicit confirm/cancel.
	if c.conflict != "" {
		if key.Code == tea.KeyEnter {
			return c.commit()
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
	if kk, ok := keymap.FromKeyMsg(key); ok {
		c.steps = append(c.steps, keymap.NormalizeKey(kk, keymap.GOOS))
		c.warn = fragileWarning(c.chord())
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
		c.conflict = other
		return nil
	}
	return c.commit()
}

func (c *keymapCapture) commit() tea.Cmd {
	c.host.Pop()
	return c.page.commitRebindChord(c.row, c.chord())
}

func (c *keymapCapture) View(w, h int) string {
	pal := c.page.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	warn := lipgloss.NewStyle().Foreground(pal.Error)
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
		lines = append(lines, warn.Render(" conflicts with "+c.conflict+" — enter overrides, any other key cancels"))
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
		i.path.Set(i.suggest.complete(i.path.text))
		return nil
	}
	if _, changed := i.path.Handle(key); changed {
		i.suggest.refresh(i.path.text)
	}
	return nil
}

func (i *keymapImport) commit() tea.Cmd {
	i.host.Pop()
	return i.page.commitImportPath(i.path.text)
}

// Click takes a completion suggestion (line 2 onward).
func (i *keymapImport) Click(_, y int) tea.Cmd {
	if idx := y - 2; idx >= 0 && idx < len(i.suggest.candidates) && idx < maxSuggestLines {
		i.path.Set(i.suggest.candidates[idx])
		i.suggest.refresh(i.path.text)
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

func (f *lspOverrideForm) commit() tea.Cmd {
	if f.kind == lspEditSettings {
		if t := strings.TrimSpace(f.input.text); t != "" && !json.Valid([]byte(t)) {
			f.note = "not valid JSON"
			return nil
		}
	}
	f.host.Pop()
	return f.page.commitOverride(f.lang, f.kind, f.input.text)
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
	listNav(key.String(), &u.pick, len(u.versions), navPage)
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

	field int
	cur   int
	form  [mapFieldCount]string
	note  string
}

func newDebugMapForm(page *DebugMapPage, host SubPanelHost, idx int) *debugMapForm {
	f := &debugMapForm{page: page, host: host, idx: idx}
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
	case key.Code == tea.KeyTab && key.Mod&tea.ModShift != 0, key.Code == tea.KeyUp:
		f.field = (f.field + mapFieldCount - 1) % mapFieldCount
		f.cur = len([]rune(f.form[f.field]))
	case key.Code == tea.KeyTab, key.Code == tea.KeyDown:
		f.field = (f.field + 1) % mapFieldCount
		f.cur = len([]rune(f.form[f.field]))
	default:
		tf := newTextFieldAt(f.form[f.field], f.cur)
		if handled, _ := tf.Handle(key); handled {
			f.form[f.field], f.cur = tf.text, tf.cur
		}
	}
	return nil
}

// Click focuses a field row.
func (f *debugMapForm) Click(_, y int) tea.Cmd {
	if y >= 0 && y < mapFieldCount {
		f.field = y
		f.cur = len([]rune(f.form[f.field]))
	}
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
