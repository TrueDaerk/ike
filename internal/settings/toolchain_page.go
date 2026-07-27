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
	glob    globList    // versioned-directory discovery (#675)
	pal     *theme.Palette

	sel      int
	off      int               // list scroll offset (#537)
	versions map[string]string // interpreter path -> probed version line

	picking    bool
	candidates []string
	pick       int
	custom     bool
	inputField textField // shared cursor input (#888)
	invalid    string
	suggest    pathSuggest // live path completion for the custom input (#541)

	// Python environment actions (#132): the uv-version picker and the
	// in-flight/busy marker the view shows while an action runs. The guided
	// create wizard is a SubPanel now (#884, venv_wizard.go) pushed through
	// host.
	envState string
	host     SubPanelHost

	// Package listing (#569) and management (#571): `i` fetches the
	// effective interpreter's installed packages plus available upgrades
	// asynchronously; j/k move the selection, +/-/u install/uninstall/
	// upgrade.
	pkgViewing bool
	pkgs       []pkgInfo
	pkgErr     string
	pkgOff     int
	pkgSel     int
	pkgLatest  map[string]string // normalized name -> latest version (#571)
	pkgMode    string            // "" | "input" (install name) | "confirm" (uninstall)
	pkgInput   textField
	pkgBusy    string // in-flight action phrase ("" when idle)
	pkgState   string // last action result for the status line
	runErr     runCtx // error-aware runner for package actions (#571)

	prov map[string]string // interpreter path -> provenance (cached stats)

	listH int // list-window height of the last render (mouse hit-testing, #674)
	listW int // settings-column width of the last render (#1299), 0 = full width
	// candTop is the detail column's first candidate line, detailHead the
	// lines above the candidate list (mouse hit-testing, #1299).
	candTop, detailHead int
	// showMissing unfolds the not-installed group (#1299), per session;
	// selKey remembers the selection by language so a regrouping keeps it.
	showMissing bool
	selKey      string
}

// NewToolchainPage builds the page. restart may be nil (no LSP integration).
func NewToolchainPage(opts config.Options, root string, restart func() tea.Cmd) *ToolchainPage {
	return &ToolchainPage{
		opts:     opts,
		root:     root,
		restart:  restart,
		run:      execRun,
		runErr:   execRunCtx,
		look:     execLook,
		resolve:  lang.ResolveShim,
		glob:     execGlob,
		versions: map[string]string{},
	}
}

// SetPalette implements PageModel.
func (t *ToolchainPage) SetPalette(p *theme.Palette) { t.pal = p }

// SetSubPanelHost implements the hostAware injection seam (#883).
func (t *ToolchainPage) SetSubPanelHost(h SubPanelHost) { t.host = h }

// pushWizard opens the create-environment wizard (#884); without a host
// (tests, degraded wiring) it reports instead of failing silently.
func (t *ToolchainPage) pushWizard() {
	if t.look("uv") == "" && t.look("python3") == "" && t.look("python") == "" {
		t.envState = "✗ neither uv nor python found on PATH"
		return
	}
	if t.host != nil {
		t.host.Push(newVenvWizard(t, t.host))
	}
}

// Capturing implements PageModel: the pickers and the custom-path input need
// keys verbatim.
func (t *ToolchainPage) Capturing() bool {
	return t.picking || t.custom || t.pkgViewing
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
			t.clampPkgSel()
		}
	case OutdatedMsg:
		t.pkgLatest = v.Latest
	case PkgActionMsg:
		t.pkgBusy = ""
		if v.Err != nil {
			line := v.Output
			if line == "" {
				line = v.Err.Error()
			}
			t.pkgState = "✗ " + v.Action + ": " + line
			return
		}
		t.pkgState = "✓ " + v.Action
		if v.Pkgs != nil {
			t.pkgs, t.pkgErr = v.Pkgs, ""
			t.clampPkgSel()
		}
		// The acted-on package's cached latest marker is stale now.
		delete(t.pkgLatest, normalizePkg(pkgBaseName(v.Name)))
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

// tcRow is one visible list row: a language, or an action row (#884 — the
// create-environment entry point lives in the list, not behind a letter).
type tcRow struct {
	lang   lang.Language
	action string // "" = language row; "newenv" = create environment
	// header is a non-selectable group caption (#1299): "configured",
	// "detected · not configured", "not installed · 12  (z)".
	header string
}

// rows lists the visible rows grouped by state (#1299): configured first,
// then detected-but-unconfigured, then the not-installed languages folded
// behind a counted caption — twelve "(not found)" lines are noise, not
// information. The new-environment action row stays under python.
func (t *ToolchainPage) rows() []tcRow {
	byState := map[tcState][]lang.Language{}
	for _, l := range t.languages() {
		st := t.stateOf(l)
		byState[st] = append(byState[st], l)
	}
	var out []tcRow
	for _, st := range []tcState{tcConfigured, tcDetected, tcMissing} {
		langs := byState[st]
		if len(langs) == 0 {
			continue
		}
		caption := groupTitles[st] + " · " + strconv.Itoa(len(langs))
		if st == tcMissing && !t.showMissing {
			out = append(out, tcRow{header: caption + "   z to unfold"})
			continue
		}
		if st == tcMissing {
			caption += "   z to fold"
		}
		out = append(out, tcRow{header: caption})
		for _, l := range langs {
			out = append(out, tcRow{lang: l})
			if l.ID == "python" {
				out = append(out, tcRow{action: "newenv"})
			}
		}
	}
	// Python may live in a folded group; its action row must stay reachable.
	if !hasNewEnv(out) {
		out = append(out, tcRow{action: "newenv"})
	}
	return out
}

// hasNewEnv reports whether the create-environment row is already present.
func hasNewEnv(rows []tcRow) bool {
	for _, r := range rows {
		if r.action == "newenv" {
			return true
		}
	}
	return false
}

// skipHeader moves the selection off a group caption in the direction of
// travel (#1299): captions are structure, never a destination.
func (t *ToolchainPage) skipHeader(dir string) {
	rows := t.rows()
	step := 1
	if dir == "up" || dir == "k" || dir == "pgup" || dir == "home" {
		step = -1
	}
	for t.sel >= 0 && t.sel < len(rows) && rows[t.sel].header != "" {
		next := t.sel + step
		if next < 0 || next >= len(rows) {
			step = -step
			next = t.sel + step
		}
		if next < 0 || next >= len(rows) {
			return
		}
		t.sel = next
	}
}

// current returns the selected language row (ok=false on action rows).
func (t *ToolchainPage) current() (lang.Language, bool) {
	rows := t.rows()
	if t.sel < 0 || t.sel >= len(rows) || rows[t.sel].action != "" || rows[t.sel].header != "" {
		return lang.Language{}, false
	}
	return rows[t.sel].lang, true
}

// interpreter resolves the effective interpreter for a language, explicit
// config first.
func (t *ToolchainPage) interpreter(id string) (path, source string) {
	explicit := config.Get().Lang[id]["interpreter"]
	return lang.Interpreter(id, t.root, explicit)
}

// selectRow positions the page on the first row matching want (no-op when
// nothing matches), the seam search results and tests use — a row index is
// not a languages() index once the list groups (#1299).
func (t *ToolchainPage) selectRow(want func(tcRow) bool) bool {
	for i, r := range t.rows() {
		if want(r) {
			t.sel = i
			t.rememberSel()
			return true
		}
	}
	return false
}

// rememberSel records what the selection *is*, so a regrouping can restore it.
func (t *ToolchainPage) rememberSel() {
	rows := t.rows()
	if t.sel < 0 || t.sel >= len(rows) {
		return
	}
	switch r := rows[t.sel]; {
	case r.action != "":
		t.selKey = "\x00" + r.action
	case r.header == "":
		t.selKey = r.lang.ID
	}
}

// normalizeSel keeps the selection on a real row: grouping (#1299) inserts
// captions and reorders on state changes, and a page opened cold would
// otherwise start on a caption.
func (t *ToolchainPage) normalizeSel() {
	rows := t.rows()
	if len(rows) == 0 {
		return
	}
	if t.selKey != "" {
		for i, r := range rows {
			if (r.action != "" && "\x00"+r.action == t.selKey) ||
				(r.header == "" && r.action == "" && r.lang.ID == t.selKey) {
				t.sel = i
				return
			}
		}
	}
	t.sel = clamp(t.sel, 0, len(rows)-1)
	if rows[t.sel].header != "" {
		t.skipHeader("down")
	}
}

// Update implements PageModel. The selection is remembered by language, not
// by index (#1299): choosing an interpreter moves the row into another group,
// and the cursor must follow the language rather than stay on a line number.
func (t *ToolchainPage) Update(key tea.KeyPressMsg) tea.Cmd {
	t.normalizeSel()
	cmd := t.update(key)
	t.rememberSel()
	return cmd
}

func (t *ToolchainPage) update(key tea.KeyPressMsg) tea.Cmd {
	if t.custom {
		return t.updateCustom(key)
	}
	if t.picking {
		return t.updatePicker(key)
	}
	if t.pkgViewing {
		return t.updatePkgView(key)
	}
	if listNav(key.String(), &t.sel, len(t.rows()), navPage) {
		t.skipHeader(key.String())
		return nil
	}
	switch key.String() {
	case "z":
		// Fold or unfold the not-installed group (#1299).
		t.showMissing = !t.showMissing
		t.sel = clamp(t.sel, 0, len(t.rows())-1)
		t.skipHeader("down")
		return nil
	case "a":
		// Take every detected toolchain at once — the first run in one key.
		return t.acceptAll()
	case "enter":
		if rows := t.rows(); t.sel >= 0 && t.sel < len(rows) && rows[t.sel].action == "newenv" {
			t.pushWizard()
			return nil
		}
		if l, ok := t.current(); ok {
			return t.openPicker(l)
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
		// Create a project environment via the guided wizard (#884).
		if l, ok := t.current(); ok && l.ID == "python" {
			t.pushWizard()
		}
	case "i":
		// List the effective interpreter's installed packages (#569) plus
		// the available upgrades for the latest column (#571).
		if l, ok := t.current(); ok && l.ID == "python" {
			if path, _ := t.interpreter(l.ID); path != "" {
				t.pkgViewing, t.pkgs, t.pkgErr, t.pkgOff, t.pkgSel = true, nil, "", 0, 0
				t.pkgMode, t.pkgState, t.pkgLatest = "", "", nil
				return tea.Batch(
					listPackages(path, t.run, t.look),
					listOutdated(path, t.run, t.look),
				)
			}
		}
	case "u":
		// Install a managed Python via uv (#132) — the picker is a sub-panel
		// now (#892).
		if l, ok := t.current(); ok && l.ID == "python" && t.host != nil {
			if t.look("uv") == "" {
				t.envState = "✗ uv not found on PATH"
				return nil
			}
			versions := uvInstallable(t.run("uv", "python", "list"))
			if len(versions) == 0 {
				t.envState = "✗ uv offers no downloadable versions"
				return nil
			}
			t.host.Push(newUvPicker(t, t.host, versions))
		}
	}
	return nil
}

// updatePkgView handles keys in the package view: j/k move the selection,
// `+` installs (name input), `-` uninstalls (confirm), `u` upgrades the
// selection, esc closes (#571). While an action runs, only navigation and
// closing work.
func (t *ToolchainPage) updatePkgView(key tea.KeyPressMsg) tea.Cmd {
	switch t.pkgMode {
	case "input":
		return t.updatePkgInput(key)
	case "confirm":
		return t.updatePkgConfirm(key)
	}
	switch key.String() {
	case "esc", "i", "q":
		t.pkgViewing = false
	case "up", "k":
		if t.pkgSel > 0 {
			t.pkgSel--
			t.followPkgSel()
		}
	case "down", "j":
		if t.pkgSel < len(t.pkgs)-1 {
			t.pkgSel++
			t.followPkgSel()
		}
	case "+":
		if t.pkgBusy == "" {
			t.pkgMode, t.pkgInput.text, t.pkgState = "input", "", ""
			t.pkgInput.Set("")
		}
	case "-":
		if t.pkgBusy == "" && t.selectedPkg() != "" {
			t.pkgMode = "confirm"
		}
	case "u":
		if t.pkgBusy == "" {
			if name := t.selectedPkg(); name != "" {
				return t.pkgAction(pkgUpgrade, name)
			}
		}
	}
	return nil
}

// selectedPkg returns the selected row's package name ("" on empty lists).
func (t *ToolchainPage) selectedPkg() string {
	if t.pkgSel < 0 || t.pkgSel >= len(t.pkgs) {
		return ""
	}
	return t.pkgs[t.pkgSel].Name
}

// clampPkgSel re-bounds the selection after the listing changed.
func (t *ToolchainPage) clampPkgSel() {
	if t.pkgSel >= len(t.pkgs) {
		t.pkgSel = len(t.pkgs) - 1
	}
	if t.pkgSel < 0 {
		t.pkgSel = 0
	}
	t.followPkgSel()
}

// followPkgSel keeps the selected row inside the visible window.
func (t *ToolchainPage) followPkgSel() {
	if t.pkgSel < t.pkgOff {
		t.pkgOff = t.pkgSel
	}
	if t.pkgSel >= t.pkgOff+pkgWindow {
		t.pkgOff = t.pkgSel - pkgWindow + 1
	}
}

// updatePkgInput handles the install-name input (`name` or `name==version`).
func (t *ToolchainPage) updatePkgInput(key tea.KeyPressMsg) tea.Cmd {
	switch key.Code {
	case tea.KeyEscape:
		t.pkgMode = ""
	case tea.KeyEnter:
		name := strings.TrimSpace(t.pkgInput.text)
		t.pkgMode = ""
		if name == "" {
			return nil
		}
		return t.pkgAction(pkgInstall, name)
	default:
		t.pkgInput.Handle(key)
	}
	return nil
}

// updatePkgConfirm handles the uninstall confirmation.
func (t *ToolchainPage) updatePkgConfirm(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "y", "enter":
		t.pkgMode = ""
		if name := t.selectedPkg(); name != "" {
			return t.pkgAction(pkgUninstall, name)
		}
	case "n", "esc":
		t.pkgMode = ""
	}
	return nil
}

// pkgAction builds and launches one asynchronous package action against the
// effective interpreter, marking the view busy until its PkgActionMsg lands.
func (t *ToolchainPage) pkgAction(action pkgAction, name string) tea.Cmd {
	l, ok := t.current()
	if !ok {
		return nil
	}
	interp, _ := t.interpreter(l.ID)
	if interp == "" {
		return nil
	}
	backend := detectPkgBackend(t.root, interp, t.look)
	cmds := pkgCommands(backend, action, name, t.root, interp)
	t.pkgBusy = action.verb() + " " + name + "…"
	t.pkgState = ""
	return runPkgAction(interp, action, name, cmds, t.runErr, t.run, t.look)
}

// openPicker builds the language's candidate list, pre-selects the currently
// effective interpreter (#675) and returns eager version probes for every
// candidate the cache does not know yet — the list shows versions without
// pressing p.
func (t *ToolchainPage) openPicker(l lang.Language) tea.Cmd {
	switch l.ID {
	case "python":
		t.candidates = pythonCandidates(t.root, t.run, t.look, t.resolve, t.glob)
	case "php":
		t.candidates = phpCandidates(t.root, t.look, t.resolve, t.glob)
	default:
		// No specific discovery: PATH lookup by language id, then the
		// well-known install directories (#538) and versioned installs (#675).
		t.candidates = defaultCandidates(l.ID, t.root, t.look, t.resolve, t.glob)
	}
	t.picking, t.pick, t.invalid = true, 0, ""
	if cur, _ := t.interpreter(l.ID); cur != "" {
		key := resolvedKey(cur)
		for i, c := range t.candidates {
			if c == cur || resolvedKey(c) == key {
				t.pick = i
				break
			}
		}
	}
	var probes []tea.Cmd
	for _, c := range t.candidates {
		if _, known := t.versions[c]; !known {
			probes = append(probes, t.probe(l.ID, c))
		}
	}
	return tea.Batch(probes...)
}

// resolvedKey is the symlink-resolved identity of a path (the path itself
// when resolution fails), matching candidateSet's deduplication (#675).
func resolvedKey(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
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
			t.picking, t.custom, t.inputField.text = false, true, ""
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
		p := strings.TrimSpace(t.inputField.text)
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
		t.inputField.Set(t.suggest.complete(t.inputField.text))
	default:
		// Shared cursor input (#888).
		if _, changed := t.inputField.Handle(key); changed {
			t.suggest.refresh(t.inputField.text)
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
	t.normalizeSel()
	listW, detailW, side := splitGrid(w)
	t.listW = 0
	list := t.renderList(listW, h)
	if !side {
		return list
	}
	t.listW = listW
	return lipgloss.JoinHorizontal(lipgloss.Top, list, columnRule(h), t.renderDetail(detailW, h))
}

// renderList renders the grouped language list.
func (t *ToolchainPage) renderList(w, h int) string {
	pal := t.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	head := sec.Render(" language · interpreter · version")
	clip := lipgloss.NewStyle().MaxWidth(w)
	var list []string
	selStart, selEnd := 0, 0
	for i, r := range t.rows() {
		if i == t.sel {
			selStart = len(list)
		}
		switch {
		case r.header != "":
			list = append(list, clip.Render(sec.Faint(true).Render(" "+r.header)))
		case r.action != "":
			list = append(list, clip.Render(t.renderAction(i == t.sel)))
		default:
			list = append(list, clip.Render(t.renderLang(r.lang, i == t.sel, w)))
			// The package view is the one flow that still expands inline; the
			// candidate picker moved into the detail column (#1299).
			if i == t.sel && t.pkgViewing {
				list = append(list, t.renderPackages()...)
			}
		}
		if i == t.sel {
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
	defer t.rememberSel()
	if t.listW > 0 && x >= t.listW {
		// The detail column: a press picks the candidate under the pointer
		// while the picker is open (#1299), and is inert otherwise.
		if !t.picking || t.candTop == 0 {
			return nil
		}
		opt := y - t.candTop
		switch {
		case opt < 0 || opt > len(t.candidates):
			return nil
		case opt == len(t.candidates): // "enter a path manually…"
			t.picking, t.custom, t.inputField.text = false, true, ""
		default:
			t.pick = opt
			return t.choose(t.candidates[opt])
		}
		return nil
	}
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
			t.picking, t.custom, t.inputField.text = false, true, ""
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
	if !ok || idx >= len(t.rows()) {
		return nil
	}
	if rows := t.rows(); rows[idx].header != "" {
		return nil // group captions are structure, not targets
	}
	if idx == t.sel {
		if rows := t.rows(); rows[idx].action == "newenv" {
			t.pushWizard()
			return nil
		}
		if l, hit := t.current(); hit {
			return t.openPicker(l)
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
		if n := len(t.pkgs); n > 0 {
			t.pkgSel = clamp(t.pkgSel+delta, 0, n-1)
			t.followPkgSel()
		}
	case t.Capturing(): // other modal flows: inert
	default:
		if n := len(t.rows()); n > 0 {
			t.sel = clamp(t.sel+delta, 0, n-1)
		}
	}
}

// footer renders the pinned footer: key hints for the current mode (wrapped
// to the column width over up to two lines, #553) plus the python
// environment status — a constant three lines so the list never shifts.
func (t *ToolchainPage) footer(sec lipgloss.Style, w int) []string {
	if rows := t.rows(); t.sel >= 0 && t.sel < len(rows) && rows[t.sel].action == "newenv" {
		out := wrapFooter([]footerLine{{text: " enter/click opens the guided create wizard", style: sec}}, w, 2)
		status := ""
		if t.envState != "" {
			status = " " + t.envState
		}
		return append(out, sec.Render(status))
	}
	l, ok := t.current()
	if !ok {
		return nil
	}
	hint := " enter pick interpreter · p probe version · r reset to detection"
	switch {
	case t.custom:
		hint = " tab complete path · enter apply · esc cancel"
	case t.pkgViewing && t.pkgMode == "input":
		hint = " enter install · esc cancel"
	case t.pkgViewing && t.pkgMode == "confirm":
		hint = " y uninstall · n cancel"
	case t.pkgViewing:
		hint = " j/k select · + install · - uninstall · u upgrade · esc close"
	case t.picking:
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

// pkgWindow is how many package rows render inline at once.
const pkgWindow = 12

// renderPackages renders the inline package window: selectable rows, the
// latest-version marker for upgradable packages (#571), and the input /
// confirm / busy lines of the management flows.
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
	for i := t.pkgOff; i < end; i++ {
		p := t.pkgs[i]
		line := "   " + pad(p.Name, 32) + pad(p.Version, 14)
		if latest, ok := t.pkgLatest[normalizePkg(p.Name)]; ok && latest != p.Version {
			line += "↑ " + latest
		}
		style := sec
		if i == t.pkgSel {
			style = lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText)
		}
		out = append(out, style.Render(line))
	}
	if end < len(t.pkgs) {
		out = append(out, sec.Render("   … "+strconv.Itoa(len(t.pkgs)-end)+" more (j to scroll)"))
	}
	switch {
	case t.pkgMode == "input":
		out = append(out, sec.Render("   install: "+t.pkgInput.View()+"  (name or name==version)"))
	case t.pkgMode == "confirm":
		out = append(out, sec.Render("   uninstall "+t.selectedPkg()+"? y/n"))
	case t.pkgBusy != "":
		out = append(out, sec.Render("   ⋯ "+t.pkgBusy))
	case t.pkgState != "":
		out = append(out, sec.Render("   "+t.pkgState))
	}
	return out
}

// renderAction renders the create-environment action row (#884).
func (t *ToolchainPage) renderAction(selected bool) string {
	pal := t.theme()
	label := "   + New environment…"
	style := lipgloss.NewStyle().Foreground(pal.Info)
	if selected {
		style = lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	}
	return style.Render(label)
}

// renderLang renders one language row.
func (t *ToolchainPage) renderLang(l lang.Language, selected bool, w int) string {
	pal := t.theme()
	path, source := t.interpreter(l.ID)
	display := path
	if display == "" {
		display, source = "(not found)", "-"
	}
	// Source, provenance and version live in the detail column now (#1299);
	// the row is the language and the binary it resolves to.
	pathW := maxInt(w-14, 12)
	if i := strings.LastIndexByte(display, '/'); i > 0 && lipgloss.Width(display) > pathW {
		display = "…" + display[i:]
	}
	label := " " + pad(l.ID, 10) + display
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

// KeyHelp implements KeyHelper (#887).
func (t *ToolchainPage) KeyHelp() []string {
	return []string{
		"enter  pick the interpreter · p  probe version · r  reset to detection",
		"a  accept every detected toolchain · z  fold/unfold not-installed",
		"n  new Python environment · i  packages · u  uv-install a Python",
		"packages view: +  install · -  uninstall · u  upgrade selection",
	}
}
