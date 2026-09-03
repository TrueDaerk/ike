package app

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/deps"
	"ike/internal/depspanel"
	"ike/internal/editor/buffer"
	"ike/internal/host"
	"ike/internal/intention"
	"ike/internal/layout"
	ilsp "ike/internal/lsp"
	"ike/internal/pane"
	"ike/internal/ui"
)

// deps_panel.go wires the Dependencies tool window (#2419): a singleton
// bottom-split pane listing the project's declared dependencies per manifest
// with the latest versions and vulnerabilities from the toolchain
// (internal/deps). Scans run in a background tea.Cmd with a status-line
// segment while in flight; results land in the pane, the global snapshot
// (hover + intention read it synchronously) and the Problems store
// (vulnerable entries as warnings under the "deps" task source). Installs
// and updates never run without an explicit confirmation.

// DepsToggleMsg runs deps.toggle.
type DepsToggleMsg struct{}

// DepsRefreshMsg runs deps.refresh: a forced rescan past the mtime cache.
type DepsRefreshMsg struct{}

// DepsAuditMsg runs deps.audit: the same forced rescan, kept as its own
// command so the palette names the security angle.
type DepsAuditMsg struct{}

// DepsUpdateLatestMsg runs deps.updateLatest, the manifest code action:
// bump the dependency under the caret to its latest version.
type DepsUpdateLatestMsg struct{}

// DepsScanStartedMsg marks a background scan as in flight — emitted next to
// the scan command itself so the status-line segment also works for the
// Init-time auto-scan, where the command builder's receiver copy is
// discarded.
type DepsScanStartedMsg struct{}

// DepsScanMsg is a finished background scan. Manual marks a user-initiated
// run (deps.refresh / deps.audit / 'r'), which reports missing tools in the
// centered dialog rather than a quiet notification.
type DepsScanMsg struct {
	Result deps.Result
	Manual bool
}

// DepsInstallDoneMsg is a finished install step after a version bump.
type DepsInstallDoneMsg struct {
	Argv []string
	Err  error
}

// depsScanTimeout bounds one background scan: the toolchain calls behind it
// (go list -u, npm outdated, audits) all talk to the network.
const depsScanTimeout = 3 * time.Minute

// toggleDepsPanel is the deps.toggle state machine, mirroring
// toggleProblemsPanel: no panel → open at the bottom; unfocused → focus it;
// focused → return focus to the remembered pane.
func (m *Model) toggleDepsPanel() tea.Cmd {
	return m.togglePanel(pane.DepsKey, m.openDepsPanel)
}

// depsPanel returns the singleton panel model, or nil when it is not open.
func (m Model) depsPanel() *depspanel.Model {
	if !m.activeWS().Panes.Has(pane.DepsKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.DepsKey).Deps()
}

// openDepsPanel splits the active editor (fallback: focused leaf) at the
// bottom with the singleton panel, seeded from the last snapshot; opening
// with no completed scan yet starts one.
func (m *Model) openDepsPanel() tea.Cmd {
	if !m.openToolPane(m.activeWS().Panes.AddDeps, fixedZone(layout.ZoneBottom), func(key string) {
		p := m.activeWS().Panes.Get(key).Deps()
		p.SetDisplayPath(displayPath)
		p.Set(deps.Snapshot())
		p.SetScanning(m.depsScanning)
	}) {
		return nil
	}
	if len(deps.Snapshot().Manifests) == 0 && !m.depsScanning {
		return m.depsScanCmd(false, false)
	}
	return nil
}

// depsAutoScanCmd is the scan-on-open hook (Init/#2419): a background scan
// when the setting is on and the project root holds any manifest.
func (m *Model) depsAutoScanCmd() tea.Cmd {
	cfg := config.Get()
	if cfg == nil || !cfg.Deps.AutoScan {
		return nil
	}
	root := m.activeWS().Root
	if root == "" || len(deps.DetectManifests(root)) == 0 {
		return nil
	}
	return m.depsScanCmd(false, false)
}

// depsScanCmd starts one background scan; force bypasses the per-manifest
// mtime cache. A scan already in flight is not doubled.
func (m *Model) depsScanCmd(force, manual bool) tea.Cmd {
	if m.depsScanning {
		return nil
	}
	root := m.activeWS().Root
	if root == "" {
		root = "."
	}
	m.depsScanning = true
	if p := m.depsPanel(); p != nil {
		p.SetScanning(true)
	}
	scanner := m.depsScanner
	return tea.Batch(
		func() tea.Msg { return DepsScanStartedMsg{} },
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), depsScanTimeout)
			defer cancel()
			return DepsScanMsg{Result: scanner.Scan(ctx, root, force), Manual: manual}
		})
}

// handleDepsScanStarted lands the in-flight flag on the model the runtime
// actually keeps (the Init-time copy discards the builder's mutation).
func (m Model) handleDepsScanStarted() (tea.Model, tea.Cmd) {
	m.depsScanning = true
	if p := m.depsPanel(); p != nil {
		p.SetScanning(true)
	}
	return m, nil
}

// handleDepsScan lands a finished scan: snapshot, panel, Problems store, and
// the missing-tool report.
func (m Model) handleDepsScan(msg DepsScanMsg) (tea.Model, tea.Cmd) {
	m.depsScanning = false
	deps.SetSnapshot(msg.Result)
	if p := m.depsPanel(); p != nil {
		p.SetScanning(false)
		p.Set(msg.Result)
	}
	m.feedDepsProblems(msg.Result)
	for _, e := range msg.Result.Errors {
		m.host.Notify(host.Warn, "deps: "+e)
	}
	if len(msg.Result.Missing) > 0 {
		if msg.Manual {
			m.openDepsMissingDialog(msg.Result.Missing)
		} else {
			m.host.Notify(host.Info, "deps: "+strconv.Itoa(len(msg.Result.Missing))+
				" toolchain tools missing — deps.refresh shows install hints")
		}
	}
	return m, nil
}

// feedDepsProblems mirrors the scan's vulnerable entries into the Problems
// store as warnings under the "deps" task source (#2419): one warning per
// vulnerability, anchored at the dependency's manifest line, replaced
// wholesale on every scan.
func (m *Model) feedDepsProblems(res deps.Result) {
	byPath := map[string][]ilsp.Diagnostic{}
	for _, md := range res.Manifests {
		for _, d := range md.Deps {
			line := d.Line - 1
			if line < 0 {
				line = 0
			}
			for _, v := range d.Vulns {
				text := d.Name + ": " + v.Summary
				if v.FixedIn != "" {
					text += " (fixed in " + v.FixedIn + ")"
				}
				byPath[md.Path] = append(byPath[md.Path], ilsp.Diagnostic{
					Range:    buffer.Range{Start: buffer.Position{Line: line}, End: buffer.Position{Line: line}},
					Severity: 2,
					Message:  text,
					Source:   "deps",
					Code:     v.ID,
				})
			}
		}
	}
	m.probStore.SetTaskSource("deps", byPath)
	m.refreshProblemsPanel()
}

// depsSegment is the status-line segment while a scan is in flight.
func (m Model) depsSegment() string {
	if !m.depsScanning {
		return ""
	}
	return "⟳ deps: scanning"
}

// handleDepsBump applies the 'u' update (or the deps.updateLatest code
// action): rewrite the version in the manifest, then offer the provider's
// install step behind a confirmation. A dirty open editor blocks the bump —
// the on-disk rewrite would race the unsaved buffer.
func (m Model) handleDepsBump(msg depspanel.BumpMsg) (tea.Model, tea.Cmd) {
	p := deps.ProviderByID(msg.Provider)
	if p == nil || !msg.Dep.Outdated() {
		return m, nil
	}
	if ed := m.editorForPath(msg.Path); ed != nil && ed.Dirty() {
		m.host.Notify(host.Warn, "deps: "+displayPath(msg.Path)+" has unsaved changes — save before updating")
		return m, nil
	}
	if err := p.Bump(msg.Path, msg.Dep, msg.Dep.Latest); err != nil {
		m.host.Notify(host.Error, "deps: "+err.Error())
		return m, nil
	}
	m.depsScanner.Invalidate(msg.Path)
	m.host.Notify(host.Info, "deps: "+msg.Dep.Name+" → "+msg.Dep.Latest+" in "+displayPath(msg.Path))
	// A clean open editor picks the rewrite up through the external-change
	// watcher (reload.go); a dirty one was refused above.
	m.openDepsInstallPrompt(p.InstallCmd(filepath.Dir(msg.Path)), filepath.Dir(msg.Path))
	return m, nil
}

// handleDepsUpdateLatest resolves the caret in the focused manifest editor
// to a scanned dependency and runs the same bump flow the pane's 'u' uses.
func (m Model) handleDepsUpdateLatest() (tea.Model, tea.Cmd) {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		return m, nil
	}
	line, _ := ed.Cursor() // 1-based, matching Dep.Line
	d, prov, ok := deps.DepAt(ed.Path(), line)
	if !ok || !d.Outdated() {
		m.host.Notify(host.Info, "deps: no outdated dependency on this line")
		return m, nil
	}
	return m.handleDepsBump(depspanel.BumpMsg{Path: ed.Path(), Provider: prov, Dep: d})
}

// openDepsInstallPrompt asks before running the provider's install step —
// updates never install without consent (#2419).
func (m *Model) openDepsInstallPrompt(argv []string, dir string) {
	if len(argv) == 0 {
		return
	}
	m.depsInstallPending = argv
	m.depsInstallDir = dir
	m.shell.SetContent(ui.ModelContent{
		Heading: "Run the install step?",
		Body: func() string {
			return "The manifest is updated. Apply it to the environment with\n\n    " +
				strings.Join(argv, " ") + "\n\n" +
				"  [enter] run — apply the update now\n" +
				"  [esc]   skip — the manifest edit stays"
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// updateDepsInstallPrompt consumes every key while the install confirmation
// is open.
func (m Model) updateDepsInstallPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "y":
		argv, dir := m.depsInstallPending, m.depsInstallDir
		m.depsInstallPending, m.depsInstallDir = nil, ""
		m.shell.Close()
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), depsScanTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
			cmd.Dir = dir
			_, err := cmd.CombinedOutput()
			return DepsInstallDoneMsg{Argv: argv, Err: err}
		}
	case "esc":
		m.depsInstallPending, m.depsInstallDir = nil, ""
		m.shell.Close()
		return m, nil
	}
	return m, nil
}

// depsInstallPromptOpen reports whether the shell shows the install
// confirmation.
func (m Model) depsInstallPromptOpen() bool {
	return len(m.depsInstallPending) > 0 && m.shell.IsOpen()
}

// handleDepsInstallDone reports the install result and rescans so the pane
// reflects the new state.
func (m Model) handleDepsInstallDone(msg DepsInstallDoneMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.host.Notify(host.Error, "deps: "+strings.Join(msg.Argv, " ")+": "+msg.Err.Error())
		return m, nil
	}
	m.host.Notify(host.Info, "deps: "+strings.Join(msg.Argv, " ")+" finished")
	return m, m.depsScanCmd(true, false)
}

// openDepsVulnsDialog shows one dependency's vulnerability details in the
// centered dialog ('v' in the pane).
func (m *Model) openDepsVulnsDialog(d deps.Dep) {
	m.depsVulnsOpen = true
	m.shell.SetContent(ui.ModelContent{
		Heading: "Vulnerabilities — " + d.Name + " " + d.Current,
		Body: func() string {
			var b strings.Builder
			for _, v := range d.Vulns {
				b.WriteString("  ▲ ")
				if v.ID != "" {
					b.WriteString(v.ID + ": ")
				}
				b.WriteString(v.Summary)
				if v.Severity != "" {
					b.WriteString(" [" + v.Severity + "]")
				}
				b.WriteString("\n")
				if v.FixedIn != "" {
					b.WriteString("      fixed in " + v.FixedIn + "\n")
				}
				if v.URL != "" {
					b.WriteString("      " + v.URL + "\n")
				}
			}
			b.WriteString("\n  [esc] close")
			return b.String()
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// updateDepsVulnsDialog consumes keys while the details dialog is open.
func (m Model) updateDepsVulnsDialog(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter", "q":
		m.depsVulnsOpen = false
		m.shell.Close()
	}
	return m, nil
}

// depsVulnsDialogOpen reports whether the shell shows vulnerability details.
func (m Model) depsVulnsDialogOpen() bool { return m.depsVulnsOpen && m.shell.IsOpen() }

// openDepsMissingDialog reports absent toolchain binaries with their install
// hints — the prominent-action dialog the missing tools convention asks for,
// never a crash (#2419).
func (m *Model) openDepsMissingDialog(missing []deps.MissingTool) {
	m.depsMissingOpen = true
	m.shell.SetContent(ui.ModelContent{
		Heading: "Dependency toolchain — missing tools",
		Body: func() string {
			var b strings.Builder
			for _, mt := range missing {
				b.WriteString("  " + mt.Provider + ": " + mt.Tool.Name)
				if mt.Tool.Optional {
					b.WriteString(" (optional)")
				}
				b.WriteString("\n      install: " + mt.Tool.Hint + "\n")
			}
			b.WriteString("\n  [esc] close")
			return b.String()
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// updateDepsMissingDialog consumes keys while the missing-tools dialog is
// open.
func (m Model) updateDepsMissingDialog(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter", "q":
		m.depsMissingOpen = false
		m.shell.Close()
	}
	return m, nil
}

// depsMissingDialogOpen reports whether the shell shows the missing-tools
// report.
func (m Model) depsMissingDialogOpen() bool { return m.depsMissingOpen && m.shell.IsOpen() }

// depsIntention is the manifest code-action provider (#2419): on a line
// declaring an outdated dependency of a scanned manifest it offers "Update
// to <latest>", delegating to the registered deps.updateLatest command.
func depsIntention() intention.Provider {
	return intention.Provider{
		ID: "deps",
		Items: func(cx intention.Context) []intention.Item {
			if cx.Path == "" || cx.ReadOnly {
				return nil
			}
			d, _, ok := deps.DepAt(cx.Path, cx.Line+1)
			if !ok || !d.Outdated() {
				return nil
			}
			return []intention.Item{{
				Title:     "Update " + d.Name + " to " + d.Latest,
				Kind:      "deps",
				CommandID: "deps.updateLatest",
			}}
		},
	}
}

// depsLocalHover claims the hover in scanned manifest files (#2419): the
// dependency's current → latest state and its vulnerabilities, served from
// the snapshot — the hover seam is synchronous, so no toolchain call.
func depsLocalHover(path string, line, col int, lines []string) (string, bool) {
	if !deps.IsManifestName(filepath.Base(path)) {
		return "", false
	}
	d, _, ok := deps.DepAt(path, line+1)
	if !ok {
		return "", false
	}
	var b strings.Builder
	b.WriteString("**" + d.Name + "**  " + d.Current)
	if d.Outdated() {
		b.WriteString(" → " + d.Latest + " _(update available)_")
	} else if d.Current != "" {
		b.WriteString(" _(up to date)_")
	}
	for _, v := range d.Vulns {
		b.WriteString("\n\n▲ ")
		if v.ID != "" {
			b.WriteString(v.ID + ": ")
		}
		b.WriteString(v.Summary)
		if v.FixedIn != "" {
			b.WriteString(" — fixed in " + v.FixedIn)
		}
	}
	return b.String(), true
}

func init() { ilsp.RegisterLocalHover(depsLocalHover) }
