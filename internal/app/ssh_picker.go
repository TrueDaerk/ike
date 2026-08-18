package app

import (
	"strings"

	"ike/internal/fuzzy"
	"ike/internal/layout"
	"ike/internal/palette"
	"ike/internal/sshconf"
	"ike/internal/terminal"
)

// ssh_picker.go is the SSH host picker (#1938, terminal.ssh): the aliases
// ~/.ssh/config declares — including the files it Includes, wildcard patterns
// excluded — plus the extra hosts terminal.ssh_hosts lists, offered in a
// locked palette mode. Picking one opens a terminal running `ssh <alias>`,
// labelled with the host so several remote terminals stay apart; everything
// beyond the alias (keys, jump hosts, per-host options) is the user's own ssh
// resolving its config, IKE never re-implements that.

// sshPrefix selects the picker mode inside the palette; opened locked only,
// so the rune has no user-facing prefix story.
const sshPrefix = ','

// SSHHostPickedMsg opens a terminal connected to one picked host.
type SSHHostPickedMsg struct{ Host string }

// SSHPickerMsg opens the host picker (terminal.ssh).
type SSHPickerMsg struct{}

// sshMode is the palette Mode listing the known hosts; the model fills
// entries before each locked open (the runConfigsMode pattern).
type sshMode struct {
	entries []sshconf.Host
}

func newSSHMode() *sshMode { return &sshMode{} }

// Prefix implements palette.Mode.
func (s *sshMode) Prefix() rune { return sshPrefix }

// Placeholder implements palette.Mode. With nothing to list the placeholder
// is the hint: a missing, unreadable or wildcard-only ssh config leaves an
// empty picker that explains where hosts come from instead of an error.
func (s *sshMode) Placeholder() string {
	if len(s.entries) == 0 {
		return "No SSH hosts — add them to ~/.ssh/config or the terminal.ssh_hosts setting"
	}
	return "SSH host…"
}

// Results implements palette.Mode: rows fuzzy-matched over the alias,
// detailing the block's user@hostname:port where the config spells it out.
func (s *sshMode) Results(query string, _ palette.Context) []palette.Item {
	var items []palette.Item
	for _, h := range s.entries {
		res, ok := fuzzy.Match(query, h.Alias)
		if !ok {
			continue
		}
		items = append(items, palette.Item{
			Title:  h.Alias,
			Spans:  res.Positions,
			Score:  res.Score,
			Detail: h.Detail(),
			Msg:    SSHHostPickedMsg{Host: h.Alias},
		})
	}
	return items
}

// sshArgv assembles the command a picked host runs. The alias goes to ssh
// verbatim — ssh reads the same config the picker listed it from, so no
// user, port or identity is duplicated onto the command line.
func sshArgv(host string) []string { return []string{"ssh", host} }

// sshLabel is the terminal's chrome label: the host name, prefixed so an ssh
// terminal reads as one among the file tabs it sits next to.
func sshLabel(host string) string { return "ssh: " + host }

// sshHosts collects the picker's list: the ssh config's aliases first, then
// the terminal.ssh_hosts entries the config file does not know about.
// Duplicates fold into the ssh-config entry, which carries the detail line.
func (m *Model) sshHosts() []sshconf.Host {
	hosts := sshconf.Hosts()
	seen := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		seen[h.Alias] = true
	}
	for _, extra := range m.configuredSSHHosts() {
		if extra == "" || seen[extra] {
			continue
		}
		seen[extra] = true
		hosts = append(hosts, sshconf.Host{Alias: extra})
	}
	return hosts
}

// configuredSSHHosts reads the terminal.ssh_hosts list setting (extra hosts
// for machines no ssh config declares).
func (m *Model) configuredSSHHosts() []string {
	v, ok := m.host.Config().Get("terminal.ssh_hosts")
	if !ok {
		return nil
	}
	var out []string
	for _, e := range strings.Split(v, ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

// openSSHPicker fills and opens the locked host picker (terminal.ssh). An
// empty list still opens — the placeholder says where hosts come from.
func (m *Model) openSSHPicker() {
	m.ssh.entries = m.sshHosts()
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, sshPrefix)
}

// openSSHTerminal opens a terminal connected to host: a tab of the active
// editor pane when there is one (the terminal.newTab placement, so the remote
// shell sits next to the files), otherwise a fresh split like terminal.new.
// The session is a command session, so ssh ending leaves the pane showing its
// exit status instead of closing — the existing terminal semantics for a
// program spawned directly on the PTY, and where a failed connection's error
// stays readable.
func (m *Model) openSSHTerminal(host string) {
	if host == "" {
		return
	}
	term := m.newSSHTerminal(host)
	if target := m.activeEditorKey(); target != "" {
		m.activeWS().Panes.Get(target).AddTerminalTab(term)
		m.setFocus(target)
		saveLayout(m.activeWS().Tree, m.activeWS().Panes)
		return
	}
	target := m.activeWS().Panes.Focused()
	if target == "" || m.activeWS().Tree == nil {
		return
	}
	key := m.activeWS().Panes.AddTerminalPaneFrom(term)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, m.auxZone(target))
	if !ok {
		m.activeWS().Panes.Close(key)
		return
	}
	m.activeWS().Tree = tree
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// newSSHTerminal spawns the `ssh <host>` command session, labelled with the
// host so the tab and pane title name it.
func (m *Model) newSSHTerminal(host string) terminal.Model {
	key := m.activeWS().Panes.MintTerminalKey()
	t := terminal.NewCommand(key, sshArgv(host), ".", 80, 24, terminalEnv(), m.host.Send)
	t.SetLabel(sshLabel(host))
	return t
}
