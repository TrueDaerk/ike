package manager

import (
	"os/exec"

	"ike/internal/lsp"
)

// lookPath probes PATH for a companion binary; a package var so tests can
// substitute a fake filesystem-free probe.
var lookPath = exec.LookPath

// hintCompanions probes PATH for the spec's optional companion tools (#1067)
// and raises a warn status for each missing one — once per language for the
// manager's lifetime, so opening more files (or more roots) of the same
// language never repeats the hint. Called when a server first becomes ready:
// the server itself runs fine, but a missing companion silently disables a
// capability (bash-language-server without shellcheck produces no
// diagnostics), and nothing else tells the user why.
func (m *Manager) hintCompanions(langID string, spec lsp.ServerSpec) {
	if len(spec.Companions) == 0 {
		return
	}
	m.mu.Lock()
	if m.companionsHinted[langID] {
		m.mu.Unlock()
		return
	}
	m.companionsHinted[langID] = true
	m.mu.Unlock()
	for _, c := range spec.Companions {
		if c.Binary == "" {
			continue
		}
		if _, err := lookPath(c.Binary); err == nil {
			continue
		}
		text := c.Binary + " not found — " + c.Purpose + " disabled"
		if c.Install != "" {
			text += " (" + c.Install + ")"
		}
		m.status(langID, text, lsp.ServerEventWarn)
	}
}
