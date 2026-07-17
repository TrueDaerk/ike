package settings

import (
	"os"
	"sort"
	"strconv"
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
	resolve resolveShim // version-manager shim resolution (#650)
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
	suggest    pathSuggest // live path completion for the custom input (#541)

	// Python environment actions (#132): the uv-version picker and the
	// in-flight/busy marker the view shows while an action runs.
	uvPicking  bool
	uvVersions []string
	uvPick     int
	envState   string

	// Create-environment target input (#547): pre-filled with ".venv",
	// path-completed, resolved against the project root on enter. Since #569
	// it is the last step of the guided create wizard.
	envInput   bool
	envPath    string
	envSuggest pathSuggest

	// Guided create wizard (#569): step 1 picks the tool (uv / venv), step 2
	// the Python (uv: version, venv: base interpreter), step 3 is envInput.
	wizStep   int // 0 = inactive
	wizTools  []string
	wizPick   int
	wizTool   string
	wizPys    []string
	wizPyPick int
	wizPython string

	// Package listing (#569): `i` fetches the effective interpreter's
	// installed packages asynchronously; j/k scroll the inline window.
	pkgViewing bool
	pkgs       []pkgInfo
	pkgErr     string
	pkgOff     int

	prov map[string]string // interpreter path -> provenance (cached stats)

	listH int // list-window height of the last render (mouse hit-testing, #674)
}

// NewToolchainPage builds the page. restart may be nil (no LSP integration).
func NewToolchainPage(opts config.Options, root string, restart func() tea.Cmd) *ToolchainPage {
	return &ToolchainPage{
		opts:     opts,
		root:     root,
		restart:  restart,
		run:      execRun,
		look:     execLook,
		resolve:  lang.ResolveShim,
		versions: map[string]string{},
	}
}

// SetPalette implements PageModel.
func (t *ToolchainPage) SetPalette(p *theme.Palette) { t.pal = p }

// Capturing implements PageModel: the pickers and the custom-path input need
// keys verbatim.
func (t *ToolchainPage) Capturing() bool {
	return t.picking || t.custom || t.uvPicking || t.envInput || t.wizStep > 0 || t.pkgViewing
}

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
	case PackagesMsg:
		if !t.pkgViewing {
			return
		}
		if v.Err != nil {
			t.pkgErr = v.Err.Error()
		} else {
			t.pkgs, t.pkgErr = v.Pkgs, ""
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
	if t.wizStep == 1 || t.wizStep == 2 {
		return t.updateWizard(key)
	}
	if t.envInput {
		return t.updateEnvInput(key)
	}
	if t.pkgViewing {
		return t.updatePkgView(key)
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
		// Create a project environment via the guided wizard (#569): tool,
		// Python, then target directory.
		if l, ok := t.current(); ok && l.ID == "python" {
			t.startWizard()
		}
	case "i":
		// List the effective interpreter's installed packages (#569).
		if l, ok := t.current(); ok && l.ID == "python" {
			if path, _ := t.interpreter(l.ID); path != "" {
				t.pkgViewing, t.pkgs, t.pkgErr, t.pkgOff = true, nil, "", 0
				return listPackages(path, t.run, t.look)
			}
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

// startWizard opens the guided create wizard (#569): the tool step is
// skipped when only one tool is available.
func (t *ToolchainPage) startWizard() {
	t.wizTools = nil
	if t.look("uv") != "" {
		t.wizTools = append(t.wizTools, "uv")
	}
	if t.look("python3") != "" || t.look("python") != "" {
		t.wizTools = append(t.wizTools, "venv")
	}
	if len(t.wizTools) == 0 {
		t.envState = "✗ neither uv nor python found on PATH"
		return
	}
	t.wizPick = 0
	if len(t.wizTools) == 1 {
		t.chooseTool(t.wizTools[0])
		return
	}
	t.wizStep = 1
}

// chooseTool advances to the Python step: uv offers its versions (default
// first), venv offers the discovered base interpreters. With nothing to
// choose the step is skipped.
func (t *ToolchainPage) chooseTool(tool string) {
	t.wizTool, t.wizPyPick = tool, 0
	if tool == "uv" {
		t.wizPys = append([]string{"default"}, uvVersionsAll(t.run("uv", "python", "list"))...)
	} else {
		t.wizPys = pythonCandidates(t.root, t.run, t.look, t.resolve)
	}
	if len(t.wizPys) <= 1 {
		python := ""
		if tool == "venv" && len(t.wizPys) == 1 {
			python = t.wizPys[0]
		}
		t.choosePython(python)
		return
	}
	t.wizStep = 2
}

// choosePython advances to the target-directory step (the pre-#569 input).
func (t *ToolchainPage) choosePython(python string) {
	t.wizPython = python
	t.wizStep = 3
	t.envInput, t.envPath = true, ".venv"
	t.envSuggest.clear()
}

// updateWizard handles keys in the wizard's tool and Python steps.
func (t *ToolchainPage) updateWizard(key tea.KeyPressMsg) tea.Cmd {
	items, pick := t.wizTools, &t.wizPick
	if t.wizStep == 2 {
		items, pick = t.wizPys, &t.wizPyPick
	}
	switch key.String() {
	case "esc":
		t.wizStep = 0
	case "up", "k":
		if *pick > 0 {
			*pick--
		}
	case "down", "j":
		if *pick < len(items)-1 {
			*pick++
		}
	case "enter":
		if t.wizStep == 1 {
			t.chooseTool(t.wizTools[t.wizPick])
			return nil
		}
		python := t.wizPys[t.wizPyPick]
		if t.wizTool == "uv" && python == "default" {
			python = ""
		}
		t.choosePython(python)
	}
	return nil
}

// updatePkgView handles keys in the package listing: j/k scroll, esc closes.
func (t *ToolchainPage) updatePkgView(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc", "i", "q":
		t.pkgViewing = false
	case "up", "k":
		if t.pkgOff > 0 {
			t.pkgOff--
		}
	case "down", "j":
		if t.pkgOff < len(t.pkgs)-pkgWindow {
			t.pkgOff++
		}
	}
	return nil
}

// updateEnvInput handles the create-environment target input (#547), the
// wizard's final step since #569.
func (t *ToolchainPage) updateEnvInput(key tea.KeyPressMsg) tea.Cmd {
	switch key.Code {
	case tea.KeyEscape:
		t.envInput, t.wizStep = false, 0
		t.envSuggest.clear()
	case tea.KeyEnter:
		target := strings.TrimSpace(t.envPath)
		if target == "" {
			target = ".venv"
		}
		t.envInput, t.wizStep = false, 0
		t.envSuggest.clear()
		t.envState = envBusy
		return createEnvWith(t.root, envSpec{Tool: t.wizTool, Python: t.wizPython, Target: target}, t.run, t.look)
	case tea.KeyTab:
		t.envPath = t.envSuggest.complete(t.envPath)
	case tea.KeyBackspace:
		if t.envPath != "" {
			t.envPath = t.envPath[:len(t.envPath)-1]
			t.envSuggest.refresh(t.envPath)
		}
	default:
		if key.Text != "" {
			t.envPath += key.Text
			t.envSuggest.refresh(t.envPath)
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
		t.candidates = pythonCandidates(t.root, t.run, t.look, t.resolve)
	case "php":
		t.candidates = phpCandidates(t.root, t.look, t.resolve)
	default:
		// No specific discovery: PATH lookup by language id, then the
		// well-known install directories (#538).
		t.candidates = defaultCandidates(l.ID, t.root, t.look, t.resolve)
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
		t.suggest.clear()
	case tea.KeyEnter:
		p := strings.TrimSpace(t.input)
		if p == "" {
			t.custom = false
			t.suggest.clear()
			return nil
		}
		if !fileExists(expandHome(p)) {
			t.invalid = "path does not exist"
			return nil
		}
		t.custom = false
		t.suggest.clear()
		return t.choose(p)
	case tea.KeyTab:
		t.input = t.suggest.complete(t.input)
	case tea.KeyBackspace:
		if t.input != "" {
			t.input = t.input[:len(t.input)-1]
			t.suggest.refresh(t.input)
		}
	default:
		if key.Text != "" {
			t.input += key.Text
			t.suggest.refresh(t.input)
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
	head := sec.Render(" language · interpreter · source · env · version")
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
				for _, s := range t.suggest.lines() {
					list = append(list, sec.Render(s))
				}
			case t.envInput:
				list = append(list, sec.Render("   create "+t.wizardLabel()+" at: "+t.envPath+"▌"))
				for _, s := range t.envSuggest.lines() {
					list = append(list, sec.Render(s))
				}
			case t.wizStep == 1 || t.wizStep == 2:
				list = append(list, t.renderWizard()...)
			case t.pkgViewing:
				list = append(list, t.renderPackages()...)
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
	foot := t.footer(sec, w)
	t.listH = h - 1 - len(foot)
	return head + "\n" + pinFooter(list, foot, selStart, selEnd, h-1, &t.off)
}

// listLine maps a page-local click to a list-line index (the same indexing
// View uses, so inline expansions line up); ok is false outside the list
// window — the header row and the pinned footer are not row clicks.
func (t *ToolchainPage) listLine(y int) (int, bool) {
	row := y - 1 // header line
	if row < 0 || (t.listH > 0 && row >= t.listH) {
		return 0, false
	}
	return row + t.off, true
}

// Click implements the optional PageClicker seam (#674): in the plain list a
// press selects the row and a press on the selection opens the picker (enter
// semantics); with the picker open a press chooses the candidate under the
// pointer (the trailing entry is "custom path…") and a press anywhere else
// closes it. The other modal flows (custom input, wizard, package view) stay
// keyboard-driven.
func (t *ToolchainPage) Click(x, y int) tea.Cmd {
	if t.picking {
		idx, ok := t.listLine(y)
		if !ok {
			t.picking = false
			return nil
		}
		// Candidate i renders on list line t.sel+1+i (rows above the
		// selection are 1:1 with lines; expansions only happen there).
		opt := idx - t.sel - 1
		switch {
		case opt < 0 || opt > len(t.candidates):
			t.picking = false
		case opt == len(t.candidates): // "custom path…"
			t.picking, t.custom, t.input = false, true, ""
		default:
			t.pick = opt
			return t.choose(t.candidates[opt])
		}
		return nil
	}
	if t.Capturing() { // custom input / wizard / env input / package view
		return nil
	}
	idx, ok := t.listLine(y)
	if !ok || idx >= len(t.languages()) {
		return nil
	}
	if idx == t.sel {
		if l, hit := t.current(); hit {
			t.openPicker(l)
		}
		return nil
	}
	t.sel = idx
	return nil
}

// Wheel implements the optional PageWheeler seam (#674): the plain list moves
// its selection (it follows, like j/k), the picker moves its highlight and
// the package view scrolls its window.
func (t *ToolchainPage) Wheel(delta int) {
	switch {
	case t.picking:
		t.pick = clamp(t.pick+delta, 0, len(t.candidates)) // one past = custom
	case t.pkgViewing:
		max := len(t.pkgs) - pkgWindow
		if max < 0 {
			max = 0
		}
		t.pkgOff = clamp(t.pkgOff+delta, 0, max)
	case t.Capturing(): // other modal flows: inert
	default:
		if n := len(t.languages()); n > 0 {
			t.sel = clamp(t.sel+delta, 0, n-1)
		}
	}
}

// footer renders the pinned footer: key hints for the current mode (wrapped
// to the column width over up to two lines, #553) plus the python
// environment status — a constant three lines so the list never shifts.
func (t *ToolchainPage) footer(sec lipgloss.Style, w int) []string {
	l, ok := t.current()
	if !ok {
		return nil
	}
	hint := " enter pick interpreter · p probe version · r reset to detection"
	switch {
	case t.custom:
		hint = " tab complete path · enter apply · esc cancel"
	case t.wizStep == 1:
		hint = " ↑↓ choose tool · enter next · esc cancel"
	case t.wizStep == 2 && t.wizTool == "uv":
		hint = " ↑↓ python version (uv downloads missing ones) · enter next · esc cancel"
	case t.wizStep == 2:
		hint = " ↑↓ base interpreter · enter next · esc cancel"
	case t.envInput:
		hint = " target directory (relative to project root) · tab complete · enter create · esc cancel"
	case t.pkgViewing:
		hint = " j/k scroll packages · esc close"
	case t.picking, t.uvPicking:
		hint = " ↑↓ choose · enter apply · esc cancel"
	case l.ID == "python":
		hint += " · n new env · i packages · u uv install"
	}
	status := ""
	if l.ID == "python" && t.envState != "" {
		status = " " + t.envState
	}
	out := wrapFooter([]footerLine{{text: hint, style: sec}}, w, 2)
	return append(out, sec.Render(status))
}

// provenance classifies (and caches) how an interpreter's environment was
// created (#569); only meaningful for python paths.
func (t *ToolchainPage) provenance(path string) string {
	if path == "" {
		return ""
	}
	if t.prov == nil {
		t.prov = map[string]string{}
	}
	if p, ok := t.prov[path]; ok {
		return p
	}
	p := envProvenance(path)
	t.prov[path] = p
	return p
}

// wizardLabel phrases the wizard's tool + Python choice for the target step.
func (t *ToolchainPage) wizardLabel() string {
	label := t.wizTool
	if label == "" {
		label = "env"
	}
	if t.wizPython != "" {
		if t.wizTool == "uv" {
			label += " (python " + t.wizPython + ")"
		} else {
			label += " (" + t.wizPython + ")"
		}
	}
	return label
}

// renderWizard renders the tool / Python pick steps.
func (t *ToolchainPage) renderWizard() []string {
	pal := t.theme()
	items, pick := t.wizTools, t.wizPick
	describe := func(s string) string {
		if s == "uv" {
			return "uv — fast, manages pyproject.toml + uv.lock"
		}
		return "venv — stdlib python -m venv"
	}
	if t.wizStep == 2 {
		items, pick = t.wizPys, t.wizPyPick
		if t.wizTool == "uv" {
			describe = func(s string) string { return "python " + s }
		} else {
			describe = func(s string) string { return s + "  " + t.provenance(s) }
		}
	}
	var out []string
	for i, item := range items {
		style := lipgloss.NewStyle().Foreground(pal.Secondary)
		if i == pick {
			style = lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText)
		}
		out = append(out, style.Render("   "+describe(item)))
	}
	return out
}

// pkgWindow is how many package rows render inline at once.
const pkgWindow = 12

// renderPackages renders the inline package listing window.
func (t *ToolchainPage) renderPackages() []string {
	pal := t.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	switch {
	case t.pkgErr != "":
		return []string{sec.Render("   ✗ " + t.pkgErr)}
	case t.pkgs == nil:
		return []string{sec.Render("   loading packages…")}
	}
	out := []string{sec.Render("   packages (" + strconv.Itoa(len(t.pkgs)) + ")")}
	end := t.pkgOff + pkgWindow
	if end > len(t.pkgs) {
		end = len(t.pkgs)
	}
	for _, p := range t.pkgs[t.pkgOff:end] {
		out = append(out, sec.Render("   "+pad(p.Name, 32)+p.Version))
	}
	if end < len(t.pkgs) {
		out = append(out, sec.Render("   … "+strconv.Itoa(len(t.pkgs)-end)+" more (j to scroll)"))
	}
	return out
}

// renderLang renders one language row.
func (t *ToolchainPage) renderLang(l lang.Language, selected bool) string {
	pal := t.theme()
	path, source := t.interpreter(l.ID)
	display := path
	if display == "" {
		display, source = "(not found)", "-"
	}
	env := ""
	if l.ID == "python" {
		env = t.provenance(path)
	}
	version := t.versions[path]
	label := " " + pad(l.ID, 10) + pad(display, 52) + pad("@"+source, 12) + pad(env, 12) + version
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
	python := false
	if l, ok := t.current(); ok {
		python = l.ID == "python"
	}
	var out []string
	for i, c := range t.candidates {
		line := "   " + c
		if python {
			if p := t.provenance(c); p != "" {
				line += "  [" + p + "]"
			}
		}
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
