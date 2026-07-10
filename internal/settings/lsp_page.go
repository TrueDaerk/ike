package settings

import (
	"encoding/json"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/theme"
)

// lsp_page.go is the Language Servers settings page (Roadmap 0180, #130): one
// row per registered language carrying a server, showing live status (0130
// classification), the effective command line and the config layer supplying
// it. Controls toggle the lsp.enabled master switch and the per-server enable,
// edit command/args/settings overrides (written to [lsp.servers.<id>] in the
// project config via write-back), and restart servers asynchronously (#123
// rules: the work happens inside the returned tea.Cmd, never on Update).

// lspEditField names the override the inline input edits.
type lspEditField int

const (
	lspEditNone lspEditField = iota
	lspEditCommand
	lspEditArgs
	lspEditSettings
)

// lspStatus is the last language-tagged status update, cached per language.
type lspStatus struct {
	text string
	kind ilsp.ServerStatusKind
}

// LSPPage implements PageModel (and MsgReceiver). The restart closures come
// from the LSP plugin; running lists the languages with a live server.
type LSPPage struct {
	opts        config.Options
	running     func() []string
	restartAll  func() tea.Cmd
	restartLang func(langID string) tea.Cmd
	pal         *theme.Palette

	sel     int
	status  map[string]lspStatus
	editing lspEditField
	input   string
	invalid string
}

// NewLSPPage builds the page. The closures may be nil (no restart wiring).
func NewLSPPage(opts config.Options, running func() []string, restartAll func() tea.Cmd, restartLang func(string) tea.Cmd) *LSPPage {
	return &LSPPage{
		opts:        opts,
		running:     running,
		restartAll:  restartAll,
		restartLang: restartLang,
		status:      map[string]lspStatus{},
	}
}

// SetPalette implements PageModel.
func (p *LSPPage) SetPalette(pal *theme.Palette) { p.pal = pal }

// Capturing implements PageModel: the override input needs keys verbatim.
func (p *LSPPage) Capturing() bool { return p.editing != lspEditNone }

// Receive implements MsgReceiver: language-tagged server status updates land
// in the per-language cache the status column reads.
func (p *LSPPage) Receive(msg tea.Msg) {
	if s, ok := msg.(ilsp.ServerStatusMsg); ok && s.Lang != "" {
		p.status[s.Lang] = lspStatus{text: s.Text, kind: s.Kind}
	}
}

// servers lists the page's rows: registered languages carrying a server.
func (p *LSPPage) servers() []lang.Language {
	var out []lang.Language
	for _, l := range lang.All() {
		if l.Server != nil {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// current returns the selected row.
func (p *LSPPage) current() (lang.Language, bool) {
	rows := p.servers()
	if p.sel < 0 || p.sel >= len(rows) {
		return lang.Language{}, false
	}
	return rows[p.sel], true
}

// overlay returns the language's raw [lsp.servers.<id>] config entry.
func overlay(id string) map[string]any {
	if c := config.Get(); c != nil {
		return c.LSP.Servers[id]
	}
	return nil
}

// masterEnabled reads the lsp.enabled switch live.
func masterEnabled() bool {
	c := config.Get()
	return c == nil || c.LSP.Enabled
}

// serverOn reads the per-server enable (absent means enabled).
func serverOn(id string) bool {
	if b, ok := overlay(id)["enabled"].(bool); ok {
		return b
	}
	return true
}

// effective resolves the display command line: the config overlay wins over
// the language plugin's baseline, field by field (mirroring the launch path).
func effective(l lang.Language) (command string, args []string) {
	command, args = l.Server.Command, l.Server.Args
	if ov, ok := ilsp.Overlay(config.Get().LSP.Servers, l.ID); ok {
		if ov.Command != "" {
			command = ov.Command
		}
		if ov.Args != nil {
			args = ov.Args
		}
	}
	return command, args
}

// source reports the strongest layer supplying an override for the server:
// project beats user beats built-in (no override at all).
func (p *LSPPage) source(id string) string {
	strongest := "built-in"
	for _, k := range []string{"command", "args", "settings", "enabled"} {
		switch config.Origin(p.opts, "lsp.servers."+id+"."+k) {
		case "project":
			return "project"
		case "user":
			strongest = "user"
		}
	}
	return strongest
}

// isRunning reports whether the language has a live server.
func (p *LSPPage) isRunning(id string) bool {
	if p.running == nil {
		return false
	}
	for _, l := range p.running() {
		if l == id {
			return true
		}
	}
	return false
}

// rowStatus derives one row's status label and, for failures, the detail text.
func (p *LSPPage) rowStatus(id string) (label, detail string) {
	switch {
	case !masterEnabled():
		return "off (master)", ""
	case !serverOn(id):
		return "disabled", ""
	}
	last, has := p.status[id]
	if has && strings.Contains(last.text, "not found") {
		// Missing binary: the launch-failure reason plus the install hint
		// (the automatic install helper is 0180/20, #131).
		return "missing", last.text + " — install it manually (install helper: #131)"
	}
	if p.isRunning(id) {
		return "ready", ""
	}
	if has {
		switch last.kind {
		case ilsp.ServerEventWarn, ilsp.ServerEventError:
			return "crashed", last.text
		}
	}
	return "idle", ""
}

// Update implements PageModel.
func (p *LSPPage) Update(key tea.KeyPressMsg) tea.Cmd {
	if p.editing != lspEditNone {
		return p.updateInput(key)
	}
	l, hasRow := p.current()
	switch key.String() {
	case "up", "k":
		if p.sel > 0 {
			p.sel--
		}
	case "down", "j":
		if p.sel < len(p.servers())-1 {
			p.sel++
		}
	case "E":
		// Master switch: flips the whole subsystem, its conventional layer.
		v := !masterEnabled()
		return config.WriteAndReload(p.opts, config.DefaultScope("lsp.enabled"), "lsp.enabled", v)
	case "e":
		if hasRow {
			return p.write(l.ID, "enabled", !serverOn(l.ID))
		}
	case "c":
		if hasRow {
			cmd, _ := effective(l)
			p.startEdit(lspEditCommand, cmd)
		}
	case "a":
		if hasRow {
			_, args := effective(l)
			p.startEdit(lspEditArgs, strings.Join(args, " "))
		}
	case "s":
		if hasRow {
			p.startEdit(lspEditSettings, settingsJSON(l.ID))
		}
	case "r":
		if hasRow && p.restartLang != nil {
			return p.restartLang(l.ID)
		}
	case "R":
		if p.restartAll != nil {
			return p.restartAll()
		}
	case "x":
		if hasRow {
			// Reset every override of this server back to the baseline: all
			// removals in one command with a single reload at the end, so
			// the writes never race each other.
			opts, id := p.opts, l.ID
			return func() tea.Msg {
				for _, k := range []string{"command", "args", "settings", "enabled"} {
					_ = config.RemoveKey(opts, config.ProjectScope, "lsp.servers."+id+"."+k)
				}
				c, diags := config.Load(opts)
				return config.ConfigReloadedMsg{Config: c, Diags: diags}
			}
		}
	}
	return nil
}

// startEdit opens the inline input prefilled with the current value.
func (p *LSPPage) startEdit(field lspEditField, prefill string) {
	p.editing, p.input, p.invalid = field, prefill, ""
}

// settingsJSON renders the server's settings override as compact JSON for the
// inline editor ("" when none is set).
func settingsJSON(id string) string {
	m, ok := overlay(id)["settings"].(map[string]any)
	if !ok || len(m) == 0 {
		return ""
	}
	data, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(data)
}

// updateInput handles the inline override editor. Enter commits (an empty
// input removes the override), esc cancels.
func (p *LSPPage) updateInput(key tea.KeyPressMsg) tea.Cmd {
	switch key.Code {
	case tea.KeyEscape:
		p.editing, p.invalid = lspEditNone, ""
	case tea.KeyEnter:
		return p.commitInput()
	case tea.KeyBackspace:
		if p.input != "" {
			p.input = p.input[:len(p.input)-1]
		}
	default:
		if key.Text != "" {
			p.input += key.Text
		}
	}
	return nil
}

// commitInput writes the edited override through write-back (project scope).
func (p *LSPPage) commitInput() tea.Cmd {
	l, ok := p.current()
	if !ok {
		p.editing = lspEditNone
		return nil
	}
	field, raw := p.editing, strings.TrimSpace(p.input)
	var key string
	var value any
	switch field {
	case lspEditCommand:
		key, value = "command", raw
	case lspEditArgs:
		key = "args"
		if raw != "" {
			value = strings.Fields(raw)
		}
	case lspEditSettings:
		key = "settings"
		if raw != "" {
			m := map[string]any{}
			if err := json.Unmarshal([]byte(raw), &m); err != nil {
				p.invalid = "not a JSON object"
				return nil
			}
			value = m
		}
	}
	p.editing, p.invalid = lspEditNone, ""
	if raw == "" {
		return config.RemoveAndReload(p.opts, config.ProjectScope, "lsp.servers."+l.ID+"."+key)
	}
	return p.write(l.ID, key, value)
}

// write persists one [lsp.servers.<id>] override to the project config.
func (p *LSPPage) write(id, key string, value any) tea.Cmd {
	return config.WriteAndReload(p.opts, config.ProjectScope, "lsp.servers."+id+"."+key, value)
}

// theme returns the active palette, defaulting when none was threaded in.
func (p *LSPPage) theme() *theme.Palette {
	if p.pal != nil {
		return p.pal
	}
	return theme.DefaultPalette()
}

// View implements PageModel.
func (p *LSPPage) View(w, h int) string {
	pal := p.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	master := "on"
	if !masterEnabled() {
		master = "off"
	}
	lines := []string{
		sec.Render(" LSP master switch: " + master + "  (E toggles)"),
		sec.Render(" language · status · command · source"),
	}
	for i, l := range p.servers() {
		lines = append(lines, p.renderRow(l, i == p.sel))
		if i != p.sel {
			continue
		}
		_, detail := p.rowStatus(l.ID)
		switch {
		case p.editing != lspEditNone:
			prompt := map[lspEditField]string{
				lspEditCommand:  "command",
				lspEditArgs:     "args (space-separated)",
				lspEditSettings: "settings (JSON object)",
			}[p.editing]
			line := "   " + prompt + ": " + p.input + "▌  (empty = reset)"
			if p.invalid != "" {
				line += "  ✗ " + p.invalid
			}
			lines = append(lines, sec.Render(line))
		default:
			if detail != "" {
				lines = append(lines, lipgloss.NewStyle().Foreground(pal.Error).Render("   "+detail))
			}
			lines = append(lines, sec.Render("   e enable · c command · a args · s settings · r restart · R restart all · x reset"))
		}
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// renderRow renders one language row.
func (p *LSPPage) renderRow(l lang.Language, selected bool) string {
	pal := p.theme()
	status, _ := p.rowStatus(l.ID)
	cmd, args := effective(l)
	cmdline := strings.TrimSpace(cmd + " " + strings.Join(args, " "))
	src := p.source(l.ID)
	label := " " + pad(l.ID, 10) + pad(status, 14) + pad(cmdline, 44) + "@" + src
	style := lipgloss.NewStyle()
	switch {
	case selected:
		style = style.Background(pal.Selection).Bold(true)
	case status == "missing" || status == "crashed":
		style = style.Foreground(pal.Error)
	case src != "built-in":
		style = style.Foreground(pal.Info)
	}
	return style.Render(label)
}
