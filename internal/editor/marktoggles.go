package editor

import "ike/internal/vcs"

// marktoggles.go — the per-source, per-severity decoration toggles (#1259).
// The editor.marks.* config switches gate which LSP severities and git change
// kinds decorate the buffer: scrollbar stripe (scrollbar.go), gutter colouring
// (view.go) and inline underlines (view.go) all consult sevVisible/gitVisible.
// The diagnostic data itself is untouched — the details popup (#739), the
// diagnostic jump (#369) and the Problems window keep the full set, so a
// hidden severity is still reachable, just not painted.

// applyMarkToggles refreshes the toggle state from the retained config and
// invalidates the scrollbar memo when anything changed.
func (m *Model) applyMarkToggles() {
	sev := [5]bool{
		false,
		boolOr(m.cfg, "editor.marks.lsp_errors", m.sevShow[1]),
		boolOr(m.cfg, "editor.marks.lsp_warnings", m.sevShow[2]),
		boolOr(m.cfg, "editor.marks.lsp_info", m.sevShow[3]),
		boolOr(m.cfg, "editor.marks.lsp_hints", m.sevShow[4]),
	}
	git := map[vcs.LineMark]bool{
		vcs.LineAdded:   boolOr(m.cfg, "editor.marks.git_added", m.gitShow[vcs.LineAdded]),
		vcs.LineChanged: boolOr(m.cfg, "editor.marks.git_changed", m.gitShow[vcs.LineChanged]),
		vcs.LineDeleted: boolOr(m.cfg, "editor.marks.git_deleted", m.gitShow[vcs.LineDeleted]),
	}
	if sev != m.sevShow {
		m.sevShow = sev
		m.diagsEpoch++ // invalidate the scrollbar stripe memo (#1097)
	}
	for mk, v := range git {
		if m.gitShow[mk] != v {
			m.gitShow = git
			m.marksEpoch++
			break
		}
	}
}

// sevVisible reports whether marks of the given LSP severity render;
// unspecified/out-of-range severities count as errors, like everywhere else.
func (m Model) sevVisible(sev int) bool {
	if sev < 1 || sev > 4 {
		sev = 1
	}
	return m.sevShow[sev]
}

// gitVisible reports whether the git change kind renders. A nil map (zero
// Model) shows everything.
func (m Model) gitVisible(mk vcs.LineMark) bool {
	if m.gitShow == nil {
		return true
	}
	return m.gitShow[mk]
}
