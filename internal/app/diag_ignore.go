package app

import (
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// diag_ignore.go — the app-level diagnostic ignore filter (#1259). Every
// published diagnostic set passes through applyDiagnostics before any consumer
// sees it: the raw set is cached per path, the lsp.diagnostics_ignore rules
// drop suppressed entries, and only the filtered set reaches the Problems
// store and the editors. Keeping the raw cache lets a rule edit (settings
// panel, the ignore action, a manual TOML edit) re-filter everything already
// on screen without waiting for the servers to republish.

// compileDiagIgnore refreshes the compiled rule set from the live config and
// reports whether the rules changed.
func (m *Model) compileDiagIgnore() bool {
	raw := config.Get().LSP.DiagnosticsIgnore
	if slices.Equal(raw, m.diagIgnoreRaw) {
		return false
	}
	m.diagIgnoreRaw = slices.Clone(raw)
	m.diagIgnore = ilsp.CompileIgnoreRules(raw)
	return true
}

// applyDiagnostics is the single entry point for a published diagnostic set:
// cache the raw set, filter it and fan it out to the Problems store and the
// path's editor views. An empty raw set drops the cache entry.
func (m *Model) applyDiagnostics(path string, diags []ilsp.Diagnostic) tea.Cmd {
	if m.rawDiags == nil {
		m.rawDiags = map[string][]ilsp.Diagnostic{}
	}
	if len(diags) == 0 {
		delete(m.rawDiags, path)
	} else {
		m.rawDiags[path] = diags
	}
	filtered := m.diagIgnore.Filter(diags)
	m.probStore.Set(path, filtered)
	return m.routeToEditor(path, ilsp.DiagnosticsMsg{Path: path, Diagnostics: filtered})
}

// refilterDiagnostics re-applies the (changed) rules to every cached raw set —
// the live-apply path for rule edits.
func (m *Model) refilterDiagnostics() []tea.Cmd {
	var cmds []tea.Cmd
	for path, raw := range m.rawDiags {
		filtered := m.diagIgnore.Filter(raw)
		m.probStore.Set(path, filtered)
		if cmd := m.routeToEditor(path, ilsp.DiagnosticsMsg{Path: path, Diagnostics: filtered}); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	m.refreshProblemsPanel()
	return cmds
}

// dropRawDiags evicts a deleted path (or directory subtree) from the raw
// cache, mirroring problems.Store.Drop.
func (m *Model) dropRawDiags(path string, isDir bool) {
	if !isDir {
		delete(m.rawDiags, path)
		return
	}
	prefix := path + string(filepath.Separator)
	for p := range m.rawDiags {
		if strings.HasPrefix(p, prefix) {
			delete(m.rawDiags, p)
		}
	}
}

// ignoreDiagnostic persists the rule suppressing d (#1259): appended to
// lsp.diagnostics_ignore in the project config; the reload then re-filters
// everything on screen through refilterDiagnostics.
func (m *Model) ignoreDiagnostic(d ilsp.Diagnostic) tea.Cmd {
	rule := ilsp.IgnoreRuleFor(d)
	if rule == "" {
		m.host.Notify(host.Warn, "diagnostic carries no source, code or message to match on")
		return nil
	}
	list := slices.Clone(config.Get().LSP.DiagnosticsIgnore)
	if slices.Contains(list, rule) {
		m.host.Notify(host.Info, "already ignored: "+rule)
		return nil
	}
	list = append(list, rule)
	m.host.Notify(host.Info, "ignoring diagnostics matching "+rule)
	return config.WriteAndReload(m.cfgOpts, config.ProjectScope, "lsp.diagnostics_ignore", list)
}
