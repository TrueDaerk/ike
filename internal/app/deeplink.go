package app

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/deeplink"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/project"
	"ike/internal/ui"
)

// deeplink.go is the root-model side of ike:// links (#2396): the socket
// endpoint that receives clicks from the OS handler, the resolution pipeline
// (history → projects directory → clone dialog), the multiple-matches chooser,
// and the post-switch payload (open file at line, show tool window). The
// parsing and matching itself lives in internal/deeplink; this file only
// routes its verdicts through the existing switch/clone/open machinery.
//
// Trust rule (#2396): a link may silently switch to an already known local
// project, nothing more. The clone path always goes through the user-confirmed
// dialog showing the linked URL verbatim, and every incoming URL is re-parsed
// before anything acts on it.

// DeepLinkMsg carries one raw ike:// URL into the Update loop — from the
// socket endpoint, the CLI argument, or the project.open_link prompt.
type DeepLinkMsg struct{ URL string }

// deepLinkResolvedMsg is the async resolution verdict for a parsed link.
type deepLinkResolvedMsg struct {
	link deeplink.Link
	res  deeplink.Resolution
	// group is the group a `group=` link addresses (0510, #2576); the zero
	// Group for a single-project link.
	group project.Group
	// err refuses the link off the loop — an unknown group, a landing that
	// is not a member. The handler only notifies it.
	err string
}

// deepLinkPending is a link's payload parked until the switch it caused
// lands: the SwitchedMsg handler finishes the job, like allPendingOpen.
type deepLinkPending struct {
	root string
	file string
	line int
	tool string
}

// deepLinkChooser is the open multiple-matches dialog: the link (for the
// payload) and its candidates, most recently opened first.
type deepLinkChooser struct {
	link    deeplink.Link
	choices []deeplink.Candidate
	// group is set for a `group=` link (0510, #2576): the pick is the
	// member the group's open chain lands on, not a plain switch target.
	group project.Group
}

// pendingFor extracts a link's post-switch payload for root.
func pendingFor(link deeplink.Link, root string) *deepLinkPending {
	return &deepLinkPending{root: root, file: link.File, line: link.Line, tool: link.Tool}
}

// StartDeepLink opens this instance's ike:// socket endpoint and returns the
// model holding it. Delivery goes through host.Send, the one bridge a
// background goroutine may use. A failure to listen (exotic platform, no
// home) degrades silently — links then only work by starting `ike ike://…`.
func (m Model) StartDeepLink() Model {
	h := m.host
	srv, err := deeplink.Serve(deeplink.DefaultDir(), func(url string) {
		h.Send(DeepLinkMsg{URL: url})
	})
	if err != nil {
		return m
	}
	m.dlServer = srv
	return m
}

// CloseDeepLink removes the socket endpoint; cmd/ike defers it around Run.
func (m Model) CloseDeepLink() { m.dlServer.Close() }

// handleDeepLink parses one incoming URL and starts the resolution. Malformed
// links produce a notification and nothing else.
func (m Model) handleDeepLink(url string) (tea.Model, tea.Cmd) {
	link, err := deeplink.Parse(url)
	if err != nil {
		m.host.Notify(host.Warn, "ike link: "+err.Error())
		return m, nil
	}
	return m, func() tea.Msg {
		// History and projects-dir matching stat directories and read git
		// configs — off the Update loop like every other disk walk.
		cfg := config.Get()
		var candidates []deeplink.Candidate
		for _, e := range project.History(cfg) {
			candidates = append(candidates, deeplink.Candidate{
				Path: e.Path, Name: e.Name, LastOpened: e.LastOpened, Remotes: e.Remotes,
			})
		}
		dir, _ := project.ProjectsDir()
		if link.Group != "" {
			return resolveGroupLink(link, cfg, candidates, dir)
		}
		return deepLinkResolvedMsg{link: link, res: deeplink.Resolve(link, candidates, dir)}
	}
}

// resolveGroupLink resolves an `ike://open?group=` link (0510, #2576): the
// group is looked up by name (case-insensitively), and a `project`/`remote`
// beside it must resolve to one of its members — the landing. A group alone
// leaves the landing to the chain (member 1). Nothing is ever cloned for a
// group link: the set it addresses is checkouts the user already has.
func resolveGroupLink(link deeplink.Link, cfg *config.Config, candidates []deeplink.Candidate, dir string) deepLinkResolvedMsg {
	g, ok := project.FindGroup(cfg, link.Group)
	if !ok {
		return deepLinkResolvedMsg{link: link, err: "no group named \"" + link.Group + "\""}
	}
	if link.Project == "" && link.RemoteKey == "" {
		return deepLinkResolvedMsg{link: link, group: g}
	}
	present, _ := project.ResolveGroupRoots(g)
	res := deeplink.ResolveIn(link, candidates, dir, present)
	if res.Kind == deeplink.KindNotFound {
		return deepLinkResolvedMsg{link: link,
			err: "\"" + deepLinkTarget(link) + "\" is not in group \"" + g.Name + "\""}
	}
	return deepLinkResolvedMsg{link: link, group: g, res: res}
}

// deepLinkTarget names the link's landing target the way the user wrote it —
// the project name, else the remote verbatim — for the refusal notification.
func deepLinkTarget(link deeplink.Link) string {
	if link.Project != "" {
		return link.Project
	}
	return link.RemoteRaw
}

// handleDeepLinkResolved acts on the pipeline's verdict.
func (m Model) handleDeepLinkResolved(msg deepLinkResolvedMsg) (tea.Model, tea.Cmd) {
	link := msg.link
	if msg.err != "" {
		m.host.Notify(host.Warn, "ike link: "+msg.err)
		return m, nil
	}
	if link.Group != "" {
		// A group link (0510, #2576): several members answering the landing
		// still ask; everything else runs the open chain straight away, with
		// an empty landing meaning "member 1".
		if msg.res.Kind == deeplink.KindChoose {
			m.openDeepLinkChooser(link, msg.res.Choices, msg.group)
			return m, nil
		}
		return m.deepLinkOpenGroup(link, msg.group, msg.res.Path)
	}
	switch msg.res.Kind {
	case deeplink.KindSwitch:
		return m.deepLinkSwitch(link, msg.res.Path)
	case deeplink.KindChoose:
		m.openDeepLinkChooser(link, msg.res.Choices, project.Group{})
		return m, nil
	case deeplink.KindClone:
		// Nothing local: the clone dialog, pre-filled with the linked URL
		// verbatim — cloning only ever happens after the user confirms here.
		m.dlAfterClone = &link
		m.startClonePromptURL(link.RemoteRaw)
		return m, nil
	default: // KindNotFound
		m.host.Notify(host.Warn, "ike link: no project named "+link.Project+
			" in the history or the projects directory")
		return m, nil
	}
}

// deepLinkSwitch heads for root: already there, the payload applies directly;
// otherwise the payload parks and the seamless switch runs, finished by the
// SwitchedMsg handler.
func (m Model) deepLinkSwitch(link deeplink.Link, root string) (tea.Model, tea.Cmd) {
	if cwd, err := os.Getwd(); err == nil && cwd == root {
		if link.File == "" && link.Tool == "" {
			// A bare link to the current project is a no-op; without this the
			// user cannot tell whether the click arrived at all (#2518).
			m.host.Notify(host.Info, "ike link: already in project "+filepath.Base(root))
			return m, nil
		}
		return m.finishDeepLink(*pendingFor(link, root))
	}
	m.dlPending = pendingFor(link, root)
	return m, project.SwitchTo(root)
}

// deepLinkOpenGroup runs a group link's open chain (0510, #2576): every
// member is warmed, the chain lands on landing ("" — member 1), and the
// link's file/tool payload parks for that landing, applied by the chain's
// finish exactly as a single-project link's SwitchedMsg applies it.
func (m Model) deepLinkOpenGroup(link deeplink.Link, g project.Group, landing string) (tea.Model, tea.Cmd) {
	var pending *deepLinkPending
	if link.File != "" || link.Tool != "" {
		pending = pendingFor(link, landing)
	}
	return m.openGroupChain(g.Name, landing, pending)
}

// applyGroupLinkPayload finishes a group link when its chain lands: the
// parked payload applies in the landing member. A chain that never reached
// the landing (its hop failed) drops the payload with one notification —
// the file it names lives in a member that is not open.
func (m Model) applyGroupLinkPayload() (Model, tea.Cmd) {
	dp := m.dlPending
	if dp == nil {
		return m, nil
	}
	m.dlPending = nil
	if !sameGroupRoot(dp.root, m.currentRoot()) {
		m.host.Notify(host.Warn, "ike link: "+filepath.Base(dp.root)+" did not open — nothing to show")
		return m, nil
	}
	next, cmd := m.finishDeepLink(*dp)
	return next.(Model), cmd
}

// finishDeepLink applies a link's payload in the (now current) project: the
// tool window first, then the file — opened last so focus lands on it when
// both are given. A missing file is a notification, never a rollback: the
// switch itself stands.
func (m Model) finishDeepLink(p deepLinkPending) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if p.tool != "" {
		next, cmd := m.deepLinkOpenTool(p.tool)
		m = next
		cmds = append(cmds, cmd)
	}
	if p.file != "" {
		abs := filepath.Join(p.root, filepath.FromSlash(p.file))
		if fi, err := os.Stat(abs); err != nil || fi.IsDir() {
			m.host.Notify(host.Warn, "ike link: no file "+p.file+" in this project")
		} else {
			next, cmd := m.openPathAt(abs, p.line-1, -1)
			m = next.(Model)
			cmds = append(cmds, cmd)
		}
	}
	return m, tea.Batch(cmds...)
}

// deepLinkTools maps the built-in tool names of the URL scheme to their
// singleton pane key (the is-it-open check) and the message their toggle
// command dispatches (the open path). terminal is special-cased — its pane is
// not a singleton — and any other name is tried as a [[tools.custom]] entry.
var deepLinkTools = map[string]struct {
	key  string
	open func() tea.Msg
}{
	"explorer":    {pane.ExplorerKey, func() tea.Msg { return ToggleExplorerFocusMsg{} }},
	"vcs":         {pane.VCSKey, func() tea.Msg { return VCSPanelToggleMsg{} }},
	"problems":    {pane.ProblemsKey, func() tea.Msg { return ProblemsToggleMsg{} }},
	"structure":   {pane.StructureKey, func() tea.Msg { return StructureToggleMsg{} }},
	"usages":      {pane.UsagesKey, func() tea.Msg { return UsagesToggleMsg{} }},
	"breakpoints": {pane.BreakpointsKey, func() tea.Msg { return BreakpointsToggleMsg{} }},
	"terminal":    {"", func() tea.Msg { return TerminalToggleMsg{} }},
}

// deepLinkOpenTool shows the named tool window, but only when it is not
// already open — a link never spawns a second instance and never toggles an
// open one away.
func (m Model) deepLinkOpenTool(name string) (Model, tea.Cmd) {
	if t, ok := deepLinkTools[name]; ok {
		open := t.key != "" && m.activeWS().Panes.Has(t.key)
		if name == "terminal" {
			open = m.shellTerminalOpen()
		}
		if open {
			return m, nil
		}
		// Route through the tool's own toggle handler synchronously, so a
		// following file open still lands after it in this very Update pass.
		next, cmd := m.Update(t.open())
		return next.(Model), cmd
	}
	switch name {
	case "http":
		if !m.activeWS().Panes.Has(pane.HTTPKey) {
			m.openHTTPPanel()
		}
		return m, nil
	case "debug":
		if !m.activeWS().Panes.Has(pane.DebugKey) {
			m.openDebugPanel()
		}
		return m, nil
	}
	if _, ok := toolEntry(name); ok {
		if len(m.toolLocations(name)) == 0 {
			m.openTool(name, false)
		}
		return m, nil
	}
	m.host.Notify(host.Warn, "ike link: unknown tool "+name)
	return m, nil
}

// shellTerminalOpen reports whether a plain shell terminal pane (not a
// custom-tool session) is part of the active workspace.
func (m Model) shellTerminalOpen() bool {
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst != nil && inst.Kind() == pane.KindTerminal && inst.Terminal().Tool() == "" {
			return true
		}
	}
	return false
}

// openDeepLinkChooser shows the multiple-matches dialog: several clones or
// worktrees answer the link; the most recently opened one is the default.
func (m *Model) openDeepLinkChooser(link deeplink.Link, choices []deeplink.Candidate, g project.Group) {
	if len(choices) > 9 {
		choices = choices[:9] // one digit each; more clones than that is noise
	}
	m.dlChoose = &deepLinkChooser{link: link, choices: choices, group: g}
	m.shell.SetContent(ui.ModelContent{
		Heading: "Several projects match the link",
		Body: func() string {
			body := ""
			for i, c := range choices {
				line := project.CompactPath(c.Path)
				if ago := project.RelTime(c.LastOpened, time.Now()); ago != "" {
					line += "  (" + ago + ")"
				}
				body += guardLine(strconv.Itoa(i+1), line, i == 0)
			}
			return body + guardCancel("cancel — stay in the current project")
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// startOpenLinkPrompt opens the one-line project.open_link prompt: paste an
// ike:// URL by hand when no OS handler delivered it.
func (m *Model) startOpenLinkPrompt() {
	m.dlLinkOpen = true
	m.dlLinkText.Clear()
	m.renderOpenLinkPrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// openLinkPromptOpen reports whether the paste prompt owns the keyboard.
func (m Model) openLinkPromptOpen() bool { return m.dlLinkOpen && m.shell.IsOpen() }

// renderOpenLinkPrompt (re)fills the shell for the current input.
func (m *Model) renderOpenLinkPrompt() {
	avail := m.width - 20
	if avail < 20 {
		avail = 20
	}
	text := m.dlLinkText
	m.shell.SetContent(ui.ModelContent{
		Heading: "Open ike:// Link",
		Body: func() string {
			return "> URL : " + windowedInput(text.Text, text.Cur, avail) +
				"\n\nenter open · esc cancel"
		},
	})
}

// updateOpenLinkPrompt consumes every key while the prompt is open: enter
// resolves the pasted link, esc cancels, everything else is line editing.
func (m Model) updateOpenLinkPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.Code {
	case tea.KeyEscape:
		m.dlLinkOpen = false
		m.shell.Close()
		return m, nil
	case tea.KeyEnter:
		url := m.dlLinkText.Text
		m.dlLinkOpen = false
		m.shell.Close()
		return m.handleDeepLink(url)
	}
	if handled, _ := m.dlLinkText.Key(msg); handled {
		m.renderOpenLinkPrompt()
	}
	return m, nil
}

// pasteOpenLinkPrompt inserts a paste at the cursor — the common way an
// ike:// URL arrives in this prompt.
func (m *Model) pasteOpenLinkPrompt(text string) bool {
	if !m.dlLinkText.Paste(text) {
		return false
	}
	m.renderOpenLinkPrompt()
	return true
}

// deepLinkChooserOpen reports whether the chooser owns the keyboard.
func (m Model) deepLinkChooserOpen() bool { return m.dlChoose != nil && m.shell.IsOpen() }

// updateDeepLinkChooser consumes every key while the chooser is open: a digit
// picks that project (enter stands in for 1, the most recent), esc cancels.
func (m Model) updateDeepLinkChooser(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ch := m.dlChoose
	switch key := guardAnswer(msg, "1"); key {
	case "esc":
		m.dlChoose = nil
		m.shell.Close()
		return m, nil
	default:
		n, err := strconv.Atoi(key)
		if err != nil || n < 1 || n > len(ch.choices) {
			return m, nil
		}
		m.dlChoose = nil
		m.shell.Close()
		if ch.group.Name != "" {
			// A group link's chooser picks the landing member (#2576); the
			// open chain still warms the whole group.
			return m.deepLinkOpenGroup(ch.link, ch.group, ch.choices[n-1].Path)
		}
		return m.deepLinkSwitch(ch.link, ch.choices[n-1].Path)
	}
}
