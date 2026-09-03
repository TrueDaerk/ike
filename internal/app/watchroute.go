package app

import (
	"os"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/watch"
)

// watchroute.go routes external file changes (Roadmap 0140) into the app. The
// watcher flushes one EventBatchMsg per debounce window (#2176); the per-event
// logic lives in routeWatchEvent so a batch of 300 checkout events shares one
// Update pass — and one render — instead of costing one full pass each.

// fixRemovedWatchKind reclassifies a remove whose file is back on disk: a
// replace-in-place (write temp + rename, git checkout) coalesced remove over
// create — a content change, not a deletion.
func fixRemovedWatchKind(msg watch.EventMsg) watch.EventMsg {
	if msg.Kind == watch.FileRemoved {
		if _, err := os.Stat(msg.Path); err == nil {
			msg.Kind = watch.FileChanged
		}
	}
	return msg
}

// routeWatchEvent applies one (kind-fixed) watcher event: directory events
// refresh the explorer, file events go to the editor leaf owning the path.
// Every event also invalidates the git status snapshot (Roadmap 0320); the
// debounce collapses bursts into one refresh. The change-feed capture is the
// caller's business — it must run before any event of the batch routes.
func (m *Model) routeWatchEvent(msg watch.EventMsg) tea.Cmd {
	vcsCmd := m.scheduleVCSRefresh()
	// On-disk changes refresh the symbol completion index (#853); repo
	// metadata and settings files are not index material.
	if m.completeEngine != nil && msg.Kind != watch.GitChanged && msg.Kind != watch.ConfigChanged {
		m.completeEngine.NotifyFileChanged(msg.Path)
	}
	if msg.Kind == watch.ConfigChanged {
		// The project settings file changed externally (0380, #795):
		// re-run the reload pipeline — theme, keymap, editor behavior
		// re-apply live, diagnostics toast via the normal path. No VCS
		// refresh: .ike is not part of the working tree view.
		return config.Reload(m.cfgOpts)
	}
	if msg.Kind == watch.GitChanged {
		// Repository metadata changed under .git (#738): an external commit,
		// branch switch, staging or pull — e.g. inside a lazygit tool pane.
		// Only the snapshot refresh; there is no project file to route.
		return vcsCmd
	}
	if msg.Kind == watch.DirChanged {
		if m.activeWS().Panes.Has(pane.ExplorerKey) {
			return tea.Batch(m.activeWS().Panes.Get(pane.ExplorerKey).Update(msg), vcsCmd)
		}
		return vcsCmd
	}
	// Announce the file event to hook subscribers (#1144): the LSP bridge
	// forwards it to the servers as workspace/didChangeWatchedFiles, so
	// Intelephense re-indexes externally created/changed/deleted files.
	hookCmds := m.fireHooks(plugin.EventExternalFileChange, plugin.FileChange{
		Path: msg.Path,
		Kind: fileChangeKind(msg.Kind),
	})
	if msg.Kind == watch.FileRemoved {
		if ed := m.editorForPath(msg.Path); ed != nil && ed.Following() {
			// A followed file disappeared (#1928): rotation in progress,
			// not a close — keep the pane, re-stamp the poll tracker so
			// the replacement file is picked up (Poll dropped the entry
			// when it reported the removal), and let the editor mark the
			// pending rotation.
			if m.watcher != nil {
				m.watcher.Track(msg.Path)
			}
		} else if ed != nil && !ed.Dirty() {
			// Externally deleted, nothing unsaved: same as the
			// explorer's delete flow — close the pane (#83). A dirty
			// buffer instead stays open, marked stale by the editor.
			// A playground following the file is told first (#2356): it
			// reads the buffer that is about to be closed.
			playCmd := m.playWatchEvent(msg)
			m.closeEditorsForPath(msg.Path, false)
			return tea.Batch(append(hookCmds, playCmd, vcsCmd)...)
		}
	}
	if msg.Kind == watch.FileChanged || msg.Kind == watch.FileCreated {
		// A gz preview's buffer path names content *inside* the archive
		// (#1763), so the editor's own reload never matches the file that
		// changed: re-decompress it here. The command it returns re-runs
		// the parse the fresh content needs (#1853).
		hookCmds = append(hookCmds, m.refreshGzipBuffers(msg.Path))
		// A notebook viewer holds no buffer either (#2425): a kernel writing
		// the .ipynb re-renders the pane here, or nowhere.
		m.refreshNotebooks(msg.Path)
	}
	// A merged rotation set's buffer path names no file either (#1996): its
	// followers tail the set's newest member, so the event is routed by
	// follow source rather than by path.
	hookCmds = append(hookCmds, m.routeMergedLogFollow(msg))
	// The editor first, the playground after it: an open playground re-reads
	// its input off the buffer (#2356), which the routing above has just
	// reloaded from disk. Both steps are sequenced here rather than left to
	// tea.Batch's argument order.
	edCmd := m.routeToEditor(msg.Path, msg)
	playCmd := m.playWatchEvent(msg)
	return tea.Batch(append(hookCmds, edCmd, playCmd, vcsCmd)...)
}
