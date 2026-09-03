package app

// remote.go wires the SFTP remote browser (#1997) into the root model: the
// remote.browse command opens the same ~/.ssh/config host list the SSH
// terminal picker uses (#1938), a pick opens (or refocuses) the host's
// browser pane, the pane's background results route back by key, and opening
// a remote file downloads it into the local cache — a viewer-claimed file
// (archive, gz, image, database) opens through the normal handler dispatch on
// the local copy, everything else lands in a read-only editor buffer under
// the virtual path "sftp://<alias><path>". Saves are blocked by the buffer
// being read-only (E45), never silently redirected to the cache: write-back
// is out of scope, and the [RO] title says so.

import (
	"os"
	"path"
	"path/filepath"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/remote"
	"ike/internal/sshconf"
	"ike/internal/textenc"
)

// remotePrefix selects the remote-host picker mode inside the palette; opened
// locked only, like the ssh terminal picker's ','.
const remotePrefix = '<'

// RemoteBrowseMsg opens the host picker (remote.browse).
type RemoteBrowseMsg struct{}

// RemoteHostPickedMsg opens the SFTP browser of one picked host.
type RemoteHostPickedMsg struct{ Host string }

// remoteFetchedMsg is one finished background download: the remote file's
// bytes are in the cache at Local, ready to open.
type remoteFetchedMsg struct {
	Alias string
	Path  string
	Local string
	Data  []byte
	Err   error
}

// remoteMode is the palette Mode listing the known hosts for browsing — the
// sshMode list with a browse pick, filled before each locked open.
type remoteMode struct {
	entries []sshconf.Host
}

func newRemoteMode() *remoteMode { return &remoteMode{} }

// Prefix implements palette.Mode.
func (r *remoteMode) Prefix() rune { return remotePrefix }

// Placeholder implements palette.Mode, doubling as the empty-state hint like
// the ssh picker's.
func (r *remoteMode) Placeholder() string {
	if len(r.entries) == 0 {
		return "No SSH hosts — add them to ~/.ssh/config or the terminal.ssh_hosts setting"
	}
	return "Browse SSH host…"
}

// Results implements palette.Mode: rows fuzzy-matched over the alias.
func (r *remoteMode) Results(query string, _ palette.Context) []palette.Item {
	return palette.FuzzyItems(query, r.entries,
		func(h sshconf.Host) string { return h.Alias },
		func(h sshconf.Host) palette.Item {
			return palette.Item{
				Title:  h.Alias,
				Detail: h.Detail(),
				Msg:    RemoteHostPickedMsg{Host: h.Alias},
			}
		})
}

// openRemotePicker fills and opens the locked host picker (remote.browse).
func (m *Model) openRemotePicker() {
	m.remote.entries = m.sshHosts()
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, remotePrefix)
}

// openRemotePane opens (or refocuses) the browser for the picked host, split
// off the viewerSplitTarget leaf like the other viewer panes. The pane
// appears at once; dialing the host is the returned background command.
func (m *Model) openRemotePane(alias string) tea.Cmd {
	if alias == "" {
		return nil
	}
	if _, _, inst, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindRemote && c.Remote().Alias() == alias
	}); ok {
		m.setFocus(inst.Key())
		return nil
	}
	return m.splitViewerPane(func() string { return m.activeWS().Panes.AddRemote(alias) })
}

// remoteResult routes one background browser result to the pane that asked
// for it, matched by the model's own key; an unroutable result is discarded
// (closing a late-landing connection).
func (m *Model) remoteResult(msg remote.ResultMsg) tea.Cmd {
	return m.routeResult(msg, func(c *pane.Instance) bool {
		return c.Kind() == pane.KindRemote && c.Remote().Key() == msg.Key
	}, msg.Discard)
}

// initRemotePanes starts the background dial of every browser that has not
// begun one — the restore paths build their panes without a command of their
// own, exactly like initESPanes.
func (m Model) initRemotePanes() []tea.Cmd {
	var cmds []tea.Cmd
	m.contentInstances(func(_ string, _ int, inst *pane.Instance) bool {
		if inst.Kind() == pane.KindRemote {
			if cmd := inst.Init(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return true
	})
	return cmds
}

// remoteFetchLimit is the download byte cap (remote.max_fetch_mb) — the guard
// against pulling a huge file over a slow link just to preview it.
func (m *Model) remoteFetchLimit() int64 {
	const fallback = 64
	mb := int64(fallback)
	if cfg := m.host.Config(); cfg != nil {
		if v, ok := cfg.Get("remote.max_fetch_mb"); ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				mb = int64(n)
			}
		}
	}
	return mb << 20
}

// openRemoteFile downloads one remote file into the cache in the background.
// The size from the listing refuses an over-cap file before any bytes move;
// the fetch itself is capped again, so a file that grew since the scan cannot
// slip past.
func (m *Model) openRemoteFile(msg remote.OpenFileMsg) tea.Cmd {
	_, _, inst, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindRemote && c.Remote().Key() == msg.Key
	})
	if !ok {
		return nil
	}
	conn := inst.Remote().Conn()
	if conn == nil {
		m.host.Notify(host.Warn, "sftp: not connected")
		return nil
	}
	limit := m.remoteFetchLimit()
	if msg.Size > limit {
		m.host.Notify(host.Warn, "remote file exceeds remote.max_fetch_mb — not downloaded: "+msg.Path)
		return nil
	}
	local := remote.CachePath(remote.DefaultCacheRoot(), msg.Alias, msg.Path)
	alias, rpath := msg.Alias, msg.Path
	return func() tea.Msg {
		out := remoteFetchedMsg{Alias: alias, Path: rpath, Local: local}
		out.Data, out.Err = conn.Fetch(rpath, limit)
		if out.Err == nil {
			if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
				out.Err = err
			} else {
				out.Err = os.WriteFile(local, out.Data, 0o600)
			}
		}
		return out
	}
}

// remoteFetched lands one download: a viewer-claimed file (archive, gz,
// image, database) opens through the normal handler dispatch on the cached
// local copy — those panes are read-only by construction — and text lands in
// a read-only editor buffer under the sftp:// virtual path. Binary content
// nothing claims is refused like a binary archive entry.
func (m *Model) remoteFetched(msg remoteFetchedMsg) tea.Cmd {
	if msg.Err != nil {
		m.host.Notify(host.Error, "sftp: cannot open "+msg.Alias+":"+msg.Path+": "+msg.Err.Error())
		return nil
	}
	head := msg.Data
	if len(head) > 512 {
		head = head[:512]
	}
	if h, ok := m.reg.ResolveHandler(msg.Local, head); ok {
		m.viewerTabHost = m.fileEditorKey()
		return h.Open(m.host, msg.Local)
	}
	if isBinary(msg.Data) {
		m.host.Notify(host.Warn, "binary remote file — no preview: "+msg.Path)
		return nil
	}
	text, _, err := textenc.Decode(msg.Data, textenc.UTF8)
	if err != nil {
		m.host.Notify(host.Error, "cannot decode "+msg.Path+": "+err.Error())
		return nil
	}
	return m.showRemoteBuffer(remote.VPath(msg.Alias, msg.Path), text)
}

// showRemoteBuffer installs text as a read-only editor tab under the sftp://
// virtual path — the archive-entry recipe: tab dedupe, the empty-scratch-tab
// fill, reparse for highlighting.
func (m *Model) showRemoteBuffer(vpath, text string) tea.Cmd {
	key := m.fileEditorKey()
	if key == "" {
		key = m.spawnEditor()
	}
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		return nil
	}
	if idx := inst.TabForPath(vpath); idx >= 0 {
		// The same remote file again: refresh the preview in place.
		m.activateTab(inst, idx)
		m.setFocus(key)
		if ed := inst.EditorForPath(vpath); ed != nil {
			ed.ShowReadOnly(vpath, text)
			return ed.Reparse()
		}
		return nil
	}
	if ed := inst.Editor(); ed == nil || !ed.IsEmpty() {
		inst.AddTab()
		m.installEmitter(key)
	}
	ed := inst.Editor()
	if ed == nil {
		return nil
	}
	ed.ShowReadOnly(vpath, text)
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	return ed.Reparse()
}

// remoteEntryTitle renders the tab/status label of a read-only remote buffer:
// "app.log (web01)". A path without the sftp:// prefix is not one of ours.
func remoteEntryTitle(vpath string) (string, bool) {
	alias, rpath, ok := remote.ParseVPath(vpath)
	if !ok {
		return "", false
	}
	return path.Base(rpath) + " (" + alias + ")", true
}
