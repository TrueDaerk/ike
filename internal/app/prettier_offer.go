package app

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/configcmd"
	"ike/internal/format"
	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/toolcatalog"
)

// prettier_offer.go is the first-reformat install prompt (#3023): JSON, the
// web languages, YAML, Ansible and Markdown prefer prettier, and without it a
// reformat falls back to a tool that may only re-indent. When a reformat of
// such a buffer finds no tool of the language's chain installed, a small
// locked palette opens centered in the editor pane that asked, offering to
// install prettier globally (npm install -g prettier) and reformat with it,
// or to reformat with the rest of the chain and never ask again — a decision
// stored at user scope (editor.prettier_install_prompt = false), so it holds
// in every project and across restarts. Without npm on PATH no install is
// offered: the popup names the command to run by hand and reformats without
// prettier. The install itself is toolcatalog.Install, the runner the tool
// panes and the Formatters settings page share.

// prettierOfferPrefix selects the prompt inside the palette. The root model
// only ever opens it locked, so the rune merely has to be unique.
const prettierOfferPrefix = '¦'

// prettierPromptKey is the persisted "ask on reformat" switch.
const prettierPromptKey = "editor.prettier_install_prompt"

// prettierOfferWidth is the prompt's outer width before it is clamped to the
// editor pane.
const prettierOfferWidth = 64

// prettierChoice is one answer to the prompt.
type prettierChoice int

const (
	// prettierInstall installs prettier, then reformats with it.
	prettierInstall prettierChoice = iota
	// prettierDecline reformats without prettier and never asks again.
	prettierDecline
	// prettierSkip reformats without prettier this once (npm missing: the
	// prompt cannot install, and not answering is no decision).
	prettierSkip
)

// prettierOfferMsg is a picked prompt row.
type prettierOfferMsg struct{ choice prettierChoice }

// prettierReformat is the reformat request the prompt interrupted: the
// buffer (its editor.ParseKey, so an unsaved buffer is found again too,
// #3029), its file ("" when unsaved) and whether it was the explicit
// Reformat Selection command.
type prettierReformat struct {
	key    string
	path   string
	ranged bool
}

// prettierOfferMode is the palette Mode listing the prompt's answers. It is
// stateless: whether npm is available decides the rows at open time.
type prettierOfferMode struct{}

// Prefix implements palette.Mode.
func (prettierOfferMode) Prefix() rune { return prettierOfferPrefix }

// Placeholder implements palette.Mode: the question, shown in the query line.
func (prettierOfferMode) Placeholder() string {
	if !toolcatalog.NpmAvailable() {
		return "prettier is not installed, and npm was not found"
	}
	return "prettier is not installed — install it now?"
}

// DigitShortcuts implements palette.DigitPicker: 1 and 2 answer directly.
func (prettierOfferMode) DigitShortcuts() bool { return true }

// Results implements palette.Mode: the fixed answers (the query does not
// filter them — there is nothing to search). Without npm the install row is
// replaced by a reformat-once row and an inert line naming the command.
func (prettierOfferMode) Results(string, palette.Context) []palette.Item {
	if !toolcatalog.NpmAvailable() {
		return []palette.Item{
			{Hint: "1", Title: "Reformat without prettier", Detail: "ask next time", Msg: prettierOfferMsg{choice: prettierSkip}},
			{Hint: "2", Title: "Never ask again", Detail: "reformat without it", Msg: prettierOfferMsg{choice: prettierDecline}},
			{Title: "npm not found — run: " + toolcatalog.PrettierInstall, Inert: true},
		}
	}
	return []palette.Item{
		{Hint: "1", Title: "Yes, install and reformat", Detail: toolcatalog.PrettierInstall, Msg: prettierOfferMsg{choice: prettierInstall}},
		{Hint: "2", Title: "No, never ask again", Detail: "reformat without it", Msg: prettierOfferMsg{choice: prettierDecline}},
	}
}

// prettierPreferred reports whether the language's default chain leads with
// prettier — the languages the offer is about.
func prettierPreferred(langID string) bool {
	chain := format.ExternalDefaultChain(langID)
	return len(chain) > 0 && filepath.Base(chain[0].Command) == "prettier"
}

// prettierOfferApplies decides whether a whole-file reformat of langID should
// ask first: the chain prefers prettier, external formatting is on, no
// [format.<lang>] override command is configured, no tool of the chain is
// installed (a project-local node_modules/.bin/prettier is a chain entry, so
// it counts) and the user has not declined the offer. A running install never
// asks twice.
func (m Model) prettierOfferApplies(langID string) bool {
	if langID == "" || !prettierPreferred(langID) || !format.ExternalEnabled(langID) {
		return false
	}
	if m.prettierDeclined || toolcatalog.Installing(toolcatalog.Prettier.Name) {
		return false
	}
	if v, ok := m.host.Config().Get(prettierPromptKey); ok && v == "false" {
		return false
	}
	if c := config.Get(); c != nil {
		if raw, ok := c.Format[langID]; ok && format.ExternalFromConfig(raw).Command != "" {
			return false
		}
	}
	for _, e := range format.ExternalDefaultChain(langID) {
		if e.Ok() {
			return false
		}
	}
	return true
}

// openPrettierOffer opens the prompt for the interrupted request, centered in
// the editor pane that asked (#3013's pane-relative placement); without a
// laid-out pane, or when the box does not fit there, it falls back to the
// centered palette.
func (m *Model) openPrettierOffer(req prettierReformat) {
	m.prettierPending = &req
	m.palette.SetSize(m.width, m.height)
	cx := m.paletteContext()
	if x, y, w, ok := m.prettierOfferAnchor(cx); ok {
		m.palette.OpenAnchoredWith(cx, prettierOfferPrefix, "", x, y, w)
		if m.paletteOnScreen() {
			return
		}
	}
	m.palette.OpenLocked(cx, prettierOfferPrefix)
}

// prettierOfferAnchor centers the prompt box in the active editor pane: the
// box is rendered once at the pane's origin to learn its height, then placed
// so its middle meets the pane's middle.
func (m *Model) prettierOfferAnchor(cx palette.Context) (x, y, w int, ok bool) {
	key := m.activeEditorKey()
	if key == "" {
		return 0, 0, 0, false
	}
	r, found := m.lay.Panes[key]
	if !found || r.W <= 0 || r.H <= 0 {
		return 0, 0, 0, false
	}
	w = min(prettierOfferWidth, r.W)
	m.palette.OpenAnchoredWith(cx, prettierOfferPrefix, "", r.X, r.Y, w)
	v := m.palette.View()
	bw, bh := lipgloss.Width(v), lipgloss.Height(v)
	x = r.X + max(0, (r.W-bw)/2)
	y = r.Y + max(0, (r.H-bh)/2)
	return x, y, w, true
}

// answerPrettierOffer acts on a picked answer for the interrupted request.
func (m *Model) answerPrettierOffer(msg prettierOfferMsg) tea.Cmd {
	req := m.prettierPending
	m.prettierPending = nil
	if req == nil {
		return nil
	}
	switch msg.choice {
	case prettierInstall:
		install := toolcatalog.Install(toolcatalog.Prettier)
		if install == nil {
			m.host.Notify(host.Info, "prettier is already being installed")
			return nil
		}
		m.prettierRerun = req
		m.host.Notify(host.Info, "installing prettier ("+toolcatalog.PrettierInstall+") — the reformat runs when it is done")
		return install
	case prettierDecline:
		// The session flag bridges the gap until the config reload lands.
		m.prettierDeclined = true
		write := configcmd.WriteAndReload(m.cfgOpts, config.UserScope, prettierPromptKey, false)
		return tea.Batch(write, m.rerunReformat(*req, "ok — IKE won't offer prettier again (Settings → Formatters re-enables it)"))
	}
	return m.rerunReformat(*req, "")
}

// rerunReformat runs the interrupted request again without the prompt, when
// its buffer is still the active editor; otherwise it only says so. note,
// when set, is shown first.
func (m *Model) rerunReformat(req prettierReformat, note string) tea.Cmd {
	if note != "" {
		m.host.Notify(host.Info, note)
	}
	ed := m.activeEditor()
	if ed == nil || ed.ParseKey() != req.key {
		name := "the buffer"
		if req.path != "" {
			name = baseName(req.path)
		}
		m.host.Notify(host.Info, "reformat "+name+" again to format it")
		return nil
	}
	return m.reformat(req.ranged, false)
}

// prettierInstalled finishes a prettier install (#3023): the outcome as a
// toast — the Formatters settings page shows it on its prettier line too —
// and, when the prompt started it, the interrupted reformat again, now with
// prettier.
func (m *Model) prettierInstalled(msg toolcatalog.InstallResultMsg) tea.Cmd {
	req := m.prettierRerun
	m.prettierRerun = nil
	if msg.Err != nil {
		text := "installing prettier failed: " + msg.Err.Error()
		if msg.Detail != "" {
			text += " (" + msg.Detail + ")"
		}
		m.host.Notify(host.Error, text)
		return nil
	}
	if req == nil {
		m.host.Notify(host.Info, "prettier is installed — reformat uses it now")
		return nil
	}
	return m.rerunReformat(*req, "prettier is installed — reformatting with it")
}
