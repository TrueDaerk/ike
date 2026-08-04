package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/plugin"
)

// workspace_idle.go implements the background LSP idle shutdown (0490 M2,
// #1521): a parked workspace's language servers are the dominant per-workspace
// memory block and hold no user-visible live state, so after a configurable
// idle period in the background they stop via the LSP manager's CloseRoot.
// Respawn is lazy: performSwitch's fresh Init re-announces every open file
// (EventFileOpened), so the first document event after resume restarts them.

// defaultBackgroundLSPTimeout applies when project.background_lsp_timeout is
// unset or unparsable: long enough that quick A→B→A hops never pay a
// re-index, short enough that a workspace parked for the afternoon frees its
// servers.
const defaultBackgroundLSPTimeout = 5 * time.Minute

// backgroundLSPTimeout resolves project.background_lsp_timeout: a Go duration
// string ("5m", "90s"); empty or unparsable selects the default, "off"/"0"/
// "false" (or any non-positive duration) disables the shutdown entirely
// (returned as 0).
func backgroundLSPTimeout() time.Duration {
	c := config.Get()
	if c == nil {
		return defaultBackgroundLSPTimeout
	}
	raw := strings.TrimSpace(c.Project.BackgroundLSPTimeout)
	switch raw {
	case "":
		return defaultBackgroundLSPTimeout
	case "off", "false", "0":
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return defaultBackgroundLSPTimeout
	}
	if d <= 0 {
		return 0
	}
	return d
}

// workspaceIdleMsg wakes the model when the workspace parked under root may
// have sat idle past the background LSP timeout. The handler re-validates:
// the timer is not cancelled on resume, it is simply ignored when it fires.
type workspaceIdleMsg struct{ root string }

// armWorkspaceIdle schedules the idle check for the workspace just parked
// under root, or nil when the shutdown is disabled. Each park arms a fresh
// timer; stale timers from an earlier park of the same root fall through the
// ParkedAt re-check in handleWorkspaceIdle.
func armWorkspaceIdle(root string) tea.Cmd {
	timeout := backgroundLSPTimeout()
	if timeout <= 0 || root == "" {
		return nil
	}
	return tea.Tick(timeout, func(time.Time) tea.Msg { return workspaceIdleMsg{root: root} })
}

// handleWorkspaceIdle fires the workspace-idle hooks (the LSP bridge stops
// the root's servers) when the workspace is still parked and its current park
// began at least a full timeout ago. A workspace resumed meanwhile is gone
// from the background set; one resumed and re-parked reads as freshly parked
// (ParkedAt moved) and is covered by the newer timer its re-park armed. The
// timeout is re-read at expiry, so a config change applies to already-armed
// timers too.
func (m Model) handleWorkspaceIdle(msg workspaceIdleMsg) (tea.Model, tea.Cmd) {
	timeout := backgroundLSPTimeout()
	if timeout <= 0 {
		return m, nil
	}
	w := m.ws.Peek(msg.root)
	if w == nil || time.Since(w.ParkedAt) < timeout {
		return m, nil
	}
	return m, tea.Batch(m.fireHooks(plugin.EventWorkspaceIdle, msg.root)...)
}
