package settings

import (
	"os"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/lang"
	"ike/internal/theme"
)

// toolchain_page.go is the toolchain settings page (#94): one row per
// registered language with a toolchain or server, showing the effective
// interpreter (explicit config beats detection — lang.Interpreter is the
// single source of truth 0170's shims reuse) and its probed version. Enter
// opens a discovery picker (venv/uv/pyenv/PATH for Python, PATH + install
// locations for PHP) or a custom path input; the choice lands in the project
// config ([lang.<id>] interpreter) and offers an LSP restart.

// VersionMsg delivers an async interpreter version probe result. The root
// model forwards it to the settings panel (Model.Deliver).
type VersionMsg struct {
	Path    string
	Version string
}

// MsgReceiver is an optional PageModel extension for pages that consume
// non-key messages (async probe results).
type MsgReceiver interface {
	Receive(msg tea.Msg)
}

// ToolchainPage implements PageModel (and MsgReceiver).
type ToolchainPage struct {
	opts    config.Options
	root    string
	restart func() tea.Cmd // dispatches lsp.restart after an interpreter change
	run     runCommand
	look    lookPath
	pal     *theme.Palette

	sel      int
	off      int               // list scroll offset (#537)
	versions map[string]string // interpreter path -> probed version line

	picking    bool
	candidates []string
	pick       int
	custom     bool
	input      string
	invalid    string

	// Python environment actions (#132): the uv-version picker and the
	// in-flight/busy marker the view shows while an action runs.
	uvPicking  bool
	uvVersions []string
	uvPick     int
	envState   string
}

// NewToolchainPage builds the page. restart may be nil (no LSP integration).
func NewToolchainPage(opts config.Options, root string, restart func() tea.Cmd) *ToolchainPage {
	return &ToolchainPage{
		opts:     opts,
		root:     root,
		restart:  restart,
		run:      execRun,
		look:     execLook,
		versions: map[string]string{},
	}
}

// SetPalette implements PageModel.
func (t *ToolchainPage) SetPalette(p *theme.Palette) { t.pal = p }

// Capturing implements PageModel: the pickers and the custom-path input need
// keys verbatim.
func (t *ToolchainPage) Capturing() bool { return t.picking || t.custom || t.uvPicking }

// Receive implements MsgReceiver: async version probes land in the cache,
// environment results clear the busy marker.
func (t *ToolchainPage) Receive(msg tea.Msg) {
	switch v := msg.(type) {
	case VersionMsg:
		if v.Version != "" {
			t.versions[v.Path] = v.Version
		}
	case EnvMsg:
		if v.Err != nil {
			t.envState = "✗ " + v.Err.Error()
		} else {
			t.envState = "✓ " + v.Label
		}
	}
}

// languages lists the page's rows: registered languages carrying a toolchain
// or a server, sorted by id.
func (t *ToolchainPage) languages() []lang.Language {
	var out []lang.Language
	for _, l := range lang.All() {
		if l.Toolchain != nil || l.Server != nil {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// current returns the selected language row.
func (t *ToolchainPage) current() (lang.Language, bool) {
	rows := t.languages()
	if t.sel < 0 || t.sel >= len(rows) {
		return lang.Language{}, false
	}
	return rows[t.sel], true
}

// interpreter resolves the effective interpreter for a language, explicit
// config first.
func (t *ToolchainPage) interpreter(id string) (path, source string) {
	explicit := config.Get().Lang[id]["interpreter"]
	return lang.Interpreter(id, t.root, explicit)
}

// Update implements PageModel.
func (t *ToolchainPage) Update(key tea.KeyPressMsg) tea.Cmd {
	if t.custom {
		return t.updateCustom(key)
	}
	if t.picking {
		return t.updatePicker(key)
	}
	if t.uvPicking {
		return t.updateUvPicker(key)
	}
	switch key.String() {
	case "up", "k":
		if t.sel > 0 {
			t.sel--
		}
	case "down", "j":
		if t.sel < len(t.languages())-1 {
			t.sel++
		}
	case "enter":
		if l, ok := t.current(); ok {
			t.openPicker(l)
		}
	case "r":
		if l, ok := t.current(); ok {
			return tea.Batch(config.RemoveAndReload(t.opts, config.ProjectScope, "lang."+l.ID+".interpreter"), t.restartCmd())
		}
	case "p":
		// Probe the effective interpreter's version asynchronously.
		if l, ok := t.current(); ok {
			if path, _ := t.interpreter(l.ID); path != "" {
				return t.probe(l.ID, path)
			}
		}
	case "n":
		// Create a project environment (#132): uv venv, python -m venv fallback.
		if l, ok := t.current(); ok && l.ID == "python" {
			t.envState = envBusy
			return createEnv(t.root, t.run, t.look)
		}
	case "u":
		// Install a managed Python via uv (#132): pick from `uv python list`.
		if l, ok := t.current(); ok && l.ID == "python" {
			if t.look("uv") == "" {
				t.envState = "✗ uv not found on PATH"
				return nil
			}
			t.uvVersions = uvInstallable(t.run("uv", "python", "list"))
			if len(t.uvVersions) == 0 {
				t.envState = "✗ uv offers no downloadable versions"
				return nil
			}
			t.uvPicking, t.uvPick = true, 0
		}
	}
	return nil
}

// updateUvPicker handles keys inside the uv version picker.
func (t *ToolchainPage) updateUvPicker(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		t.uvPicking = false
	case "up", "k":
		if t.uvPick > 0 {
			t.uvPick--
		}
	case "down", "j":
		if t.uvPick < len(t.uvVersions)-1 {
			t.uvPick++
		}
	case "enter":
		t.uvPicking = false
		version := t.uvVersions[t.uvPick]
		t.envState = envBusy
		return uvInstall(version, t.run)
	}
	return nil
}

// openPicker builds the language's candidate list.
func (t *ToolchainPage) openPicker(l lang.Language) {
	switch l.ID {
	case "python":
		t.candidates = pythonCandidates(t.root, t.run, t.look)
	case "php":
		t.candidates = phpCandidates(t.look)
	default:
		// No specific discovery: PATH lookup by language id, then the
		// well-known install directories (#538).
		t.candidates = defaultCandidates(l.ID, t.look)
	}
	t.picking, t.pick, t.invalid = true, 0, ""
}

// updatePicker handles keys inside the candidate picker.
func (t *ToolchainPage) updatePicker(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		t.picking = false
	case "up", "k":
		if t.pick > 0 {
			t.pick--
		}
	case "down", "j":
		if t.pick < len(t.candidates) { // one past the end = "custom path…"
			t.pick++
		}
	case "enter":
		if t.pick >= len(t.candidates) {
			t.picking, t.custom, t.input = false, true, ""
			return nil
		}
		if len(t.candidates) == 0 {
			return nil
		}
		return t.choose(t.candidates[t.pick])
	}
	return nil
}

// updateCustom handles the custom-path input.
func (t *ToolchainPage) updateCustom(key tea.KeyPressMsg) tea.Cmd {
	switch key.Code {
	case tea.KeyEscape:
		t.custom, t.invalid = false, ""
	case tea.KeyEnter:
		p := strings.TrimSpace(t.input)
		if p == "" {
			t.custom = false
			return nil
		}
		if !fileExists(expandHome(p)) {
			t.invalid = "path does not exist"
			return nil
		}
		t.custom = false
		return t.choose(p)
	case tea.KeyBackspace:
		if t.input != "" {
			t.input = t.input[:len(t.input)-1]
		}
	default:
		if key.Text != "" {
			t.input += key.Text
		}
	}
	return nil
}

// choose persists the interpreter to the project config, probes its version
// and offers the LSP restart so the server respawns against it.
func (t *ToolchainPage) choose(path string) tea.Cmd {
	l, ok := t.current()
	if !ok {
		return nil
	}
	t.picking = false
	return tea.Batch(
		config.WriteAndReload(t.opts, config.ProjectScope, "lang."+l.ID+".interpreter", path),
		t.probe(l.ID, path),
		t.restartCmd(),
	)
}

// restartCmd wires the injected LSP restart (nil-safe).
func (t *ToolchainPage) restartCmd() tea.Cmd {
	if t.restart == nil {
		return nil
	}
	return t.restart()
}

// probe runs the interpreter's version command asynchronously and delivers a
// VersionMsg.
func (t *ToolchainPage) probe(langID, path string) tea.Cmd {
	run := t.run
	args := versionArgs(langID)
	return func() tea.Msg {
		out := strings.TrimSpace(run(path, args...))
		if i := strings.IndexByte(out, '\n'); i >= 0 {
			out = out[:i]
		}
		return VersionMsg{Path: path, Version: out}
	}
}

// theme returns the active palette, defaulting when none was threaded in.
func (t *ToolchainPage) theme() *theme.Palette {
	if t.pal != nil {
		return t.pal
	}
	return theme.DefaultPalette()
}

// View implements PageModel. The header is pinned on top and the key hints /
// env status render in a constant-height footer pinned to the bottom (#537),
// so moving the selection never shifts the rows; only the pickers and the
// custom-path input still expand inline (explicit actions).
func (t *ToolchainPage) View(w, h int) string {
	pal := t.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	head := sec.Render(" language · interpreter · source · version")
	var list []string
	selStart, selEnd := 0, 0
	for i, l := range t.languages() {
		if i == t.sel {
			selStart = len(list)
		}
		list = append(list, t.renderLang(l, i == t.sel))
		if i == t.sel {
			switch {
			case t.custom:
				detail := "   custom path: " + t.input + "▌"
				if t.invalid != "" {
					detail += "  ✗ " + t.invalid
				}
				list = append(list, sec.Render(detail))
			case t.picking:
				list = append(list, t.renderPicker()...)
			case t.uvPicking:
				for i, v := range t.uvVersions {
					line := "   install python " + v
					style := lipgloss.NewStyle().Foreground(pal.Secondary)
					if i == t.uvPick {
						style = lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText)
					}
					list = append(list, style.Render(line))
				}
			}
			selEnd = len(list) - 1
		}
	}
	return head + "\n" + pinFooter(list, t.footer(sec), selStart, selEnd, h-1, &t.off)
}

// footer renders the pinned two-line footer: key hints for the current mode
// plus the python environment status (empty lines keep the height constant).
func (t *ToolchainPage) footer(sec lipgloss.Style) []string {
	l, ok := t.current()
	if !ok {
		return nil
	}
	hint := " enter pick interpreter · p probe version · r reset to detection"
	switch {
	case t.custom:
		hint = " enter apply · esc cancel"
	case t.picking, t.uvPicking:
		hint = " ↑↓ choose · enter apply · esc cancel"
	case l.ID == "python":
		hint += " · n new venv · u uv install"
	}
	status := ""
	if l.ID == "python" && t.envState != "" {
		status = " " + t.envState
	}
	return []string{sec.Render(hint), sec.Render(status)}
}

// renderLang renders one language row.
func (t *ToolchainPage) renderLang(l lang.Language, selected bool) string {
	pal := t.theme()
	path, source := t.interpreter(l.ID)
	display := path
	if display == "" {
		display, source = "(not found)", "-"
	}
	version := t.versions[path]
	label := " " + pad(l.ID, 10) + pad(display, 52) + pad("@"+source, 12) + version
	style := lipgloss.NewStyle()
	switch {
	case selected:
		style = style.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	case source == "config":
		style = style.Foreground(pal.Info)
	}
	return style.Render(label)
}

// renderPicker renders the candidate list plus the custom-path entry.
func (t *ToolchainPage) renderPicker() []string {
	pal := t.theme()
	var out []string
	for i, c := range t.candidates {
		line := "   " + c
		if v := t.versions[c]; v != "" {
			line += "  " + v
		}
		style := lipgloss.NewStyle().Foreground(pal.Secondary)
		if i == t.pick {
			style = lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText)
		}
		out = append(out, style.Render(line))
	}
	custom := "   custom path…"
	style := lipgloss.NewStyle().Foreground(pal.Secondary)
	if t.pick >= len(t.candidates) {
		style = lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText)
	}
	return append(out, style.Render(custom))
}

// fileExists reports a stat-able non-directory path.
func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
