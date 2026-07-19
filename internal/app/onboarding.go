package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/lang"
	"ike/internal/ui"
)

// onboarding.go is the first-start LSP onboarding dialog (#301). On the very
// first launch — the user settings file does not exist yet — a one-time
// floating prompt lists every registered language whose server ships an
// install recipe (0180), each with a checkbox. Enter installs the checked
// servers as a batch through the existing lsp.installMissing command (the 0180
// recipes and progress/result notifications verbatim) and writes the unchecked
// ones off ([lsp.servers.<id>] enabled = false, user scope) so auto-install
// leaves them alone; esc skips without touching any server. Either way
// lsp.onboarded = true persists and the dialog never returns — the Language
// Servers settings page stays the ongoing management surface. With
// lsp.auto_install = false the dialog does not open at all ("ask me nothing,
// install nothing").

// onboardingState is the open dialog: the offered languages, the per-language
// checkbox state, and the cursor.
type onboardingState struct {
	items   []lang.Language
	checked map[string]bool
	cursor  int
}

// onboardingLangs lists the registered languages whose server carries an
// install recipe — the only ones the dialog can act on.
func onboardingLangs() []lang.Language {
	var out []lang.Language
	for _, l := range lang.All() {
		if l.Server != nil && len(l.Server.Install) > 0 {
			out = append(out, l)
		}
	}
	return out
}

// scanOnboarding decides at startup whether the dialog is due: the LSP
// subsystem and auto-install on, not onboarded yet, and at least one server
// to offer. The gate is the lsp.onboarded flag alone, NOT the settings file's
// existence (#658): the welcome tour writes ui.onboarded when it opens, so on
// a launch after a mid-tour quit the file already exists while this dialog
// still hasn't had its say. The prompt itself waits for the first window size
// (maybeOpenOnboarding).
func (m *Model) scanOnboarding() {
	if m.cfgOpts.UserPath == "" {
		return
	}
	c := config.Get()
	if c == nil || !c.LSP.Enabled || !c.LSP.AutoInstall || c.LSP.Onboarded {
		return
	}
	if len(onboardingLangs()) == 0 {
		return
	}
	m.onboardingPending = true
}

// maybeOpenOnboarding shows the dialog once the window is sized, if startup
// flagged it and no other startup prompt (crash recovery) holds the shell.
func (m *Model) maybeOpenOnboarding() {
	if !m.onboardingPending || m.onboarding != nil || m.width == 0 || m.height == 0 {
		return
	}
	if m.recovery != nil || len(m.recoveryPending) > 0 || m.shell.IsOpen() {
		return
	}
	m.onboardingPending = false
	m.openOnboardingDialog()
}

// openOnboardingDialog shows the server picker unconditionally (no pending /
// onboarded gate) — the first-run path above and the post-tour setup flow
// (#713) both land here. False when LSP is off or no server ships a recipe.
func (m *Model) openOnboardingDialog() bool {
	if c := config.Get(); c == nil || !c.LSP.Enabled {
		return false
	}
	items := onboardingLangs()
	if len(items) == 0 {
		return false
	}
	checked := make(map[string]bool, len(items))
	for _, l := range items {
		checked[l.ID] = true
	}
	m.onboarding = &onboardingState{items: items, checked: checked}
	m.shell.SetContent(ui.ModelContent{Heading: "Set up language servers", Body: m.onboardingBody})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
	return true
}

// onboardingOpen reports whether the dialog is showing.
func (m Model) onboardingOpen() bool { return m.onboarding != nil && m.shell.IsOpen() }

// onboardingBody renders the checkbox list with the cursor and the key legend.
func (m Model) onboardingBody() string {
	ob := m.onboarding
	var b strings.Builder
	b.WriteString("IKE can install language servers for the languages it supports.\n")
	b.WriteString("Pick the ones you want — unchecked servers are disabled and never\n")
	b.WriteString("auto-installed (change either later in Settings → Language Servers).\n\n")
	for i, l := range ob.items {
		marker := "  "
		if i == ob.cursor {
			marker = "▸ "
		}
		box := "[ ]"
		if ob.checked[l.ID] {
			box = "[x]"
		}
		b.WriteString(marker + box + " " + l.ID + " — " + l.Server.Command +
			" (" + strings.Join(l.Server.Install, " ") + ")\n")
	}
	b.WriteString("\n")
	b.WriteString("  [space] toggle   [a] all   [n] none   [j/k] move   [enter] install checked   [esc] skip")
	return b.String()
}

// updateOnboarding consumes every key while the dialog is open. space toggles
// the highlighted checkbox, a/n check/uncheck all, enter commits, esc skips;
// everything else is swallowed so nothing leaks past the modal.
func (m Model) updateOnboarding(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ob := m.onboarding
	switch msg.String() {
	case "j", "down":
		if ob.cursor < len(ob.items)-1 {
			ob.cursor++
		}
	case "k", "up":
		if ob.cursor > 0 {
			ob.cursor--
		}
	case "space", " ":
		id := ob.items[ob.cursor].ID
		ob.checked[id] = !ob.checked[id]
	case "a":
		for _, l := range ob.items {
			ob.checked[l.ID] = true
		}
	case "n":
		for _, l := range ob.items {
			ob.checked[l.ID] = false
		}
	case "enter":
		return m.onboardingConfirm()
	case "esc":
		// Skip: install nothing, disable nothing — only remember the dialog
		// has had its say (the write also creates the user settings file).
		return m.closeOnboarding(), config.WriteAndReload(m.cfgOpts, config.UserScope, "lsp.onboarded", true)
	}
	return m, nil
}

// onboardingConfirm persists the selection and kicks the batch install. The
// writes run inside the returned command (#123 rules): unchecked servers get
// [lsp.servers.<id>] enabled = false in the user layer, plus the onboarded
// flag, followed by one reload. The install reuses lsp.installMissing, which
// re-reads the config off disk — tea.Sequence guarantees the writes land
// first, so exactly the checked (still enabled) missing servers install, with
// the 0180 progress/result notifications unchanged.
func (m Model) onboardingConfirm() (tea.Model, tea.Cmd) {
	ob, opts := m.onboarding, m.cfgOpts
	var disabled []string
	anyChecked := false
	for _, l := range ob.items {
		if ob.checked[l.ID] {
			anyChecked = true
		} else {
			disabled = append(disabled, l.ID)
		}
	}
	write := func() tea.Msg {
		for _, id := range disabled {
			_ = config.WriteKey(opts, config.UserScope, "lsp.servers."+id+".enabled", false)
		}
		_ = config.WriteKey(opts, config.UserScope, "lsp.onboarded", true)
		c, diags := config.Load(opts)
		return config.ConfigReloadedMsg{Config: c, Diags: diags}
	}
	tm := m.closeOnboarding()
	if anyChecked {
		if c, ok := m.reg.Command("lsp.installMissing"); ok {
			return tm, tea.Sequence(write, c.Run(m.host))
		}
	}
	return tm, write
}

// closeOnboarding dismisses the dialog.
func (m Model) closeOnboarding() tea.Model {
	m.onboarding = nil
	m.shell.Close()
	// Continue the post-tour setup flow (#713); a no-op outside it.
	m.advanceSetup()
	return m
}
