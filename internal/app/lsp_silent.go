package app

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/pane"
)

// lsp_silent.go remembers language servers that never publish (#3080). A
// project switch whose warm-up wait ends "quiet" marks every server it waited
// for as known-silent in the state store (ilsp.SilentStore,
// lsp-silent.json); the next switch into the project does not wait for those
// servers at all — no two-minute fallback timer, no silent-server notice,
// and the "lsp" phase reads skipped=silent_known when nothing else was left
// to wait for. The first publish of a known-silent server clears its marker,
// so an upgraded or fixed server warms up normally again; lsp.forgetSilent
// and the server's row in Settings › Language Servers clear it by hand.

// LSPForgetSilentMsg runs lsp.forgetSilent (#3080): forget every known-silent
// server of the active project.
type LSPForgetSilentMsg struct{}

// lspSilentState caches the known-silent server ids of one project root, so
// the per-publish check costs a map lookup rather than a file read. The cache
// is only a hint: every write is a fresh read-modify-write of the file
// (ilsp.UpdateSilent), so a reset done on the settings page is never undone.
type lspSilentState struct {
	root   string
	ids    map[string]bool
	loaded bool
}

// serverDoc names a server-backed language open after a switch: the
// document's language (what the notice names) and the id of the server that
// serves it (what the silent marker is keyed by — a language riding another
// language's server shares that server's id).
type serverDoc struct {
	lang, server string
}

// serverIDOf is the id of the server serving l, "" when none does.
func serverIDOf(l lang.Language) string {
	switch {
	case l.Server != nil:
		return l.ID
	case l.ServerLanguage != "":
		if d, ok := lang.ByID(l.ServerLanguage); ok && d.Server != nil {
			return d.ID
		}
	}
	return ""
}

// silentRoot is the project root the markers are keyed by, "" without one.
func (m Model) silentRoot() string {
	return m.projectRootTag()
}

// silentIDs returns the cached known-silent ids of the active project,
// reading the file the first time (and again after a root change).
func (m *Model) silentIDs() map[string]bool {
	root := m.silentRoot()
	if m.lspSilent == nil || root == "" {
		return nil
	}
	st := m.lspSilent
	if !st.loaded || st.root != root {
		s, _ := ilsp.LoadSilent(ilsp.SilentPath(root))
		st.setFrom(root, s)
	}
	return st.ids
}

// setFrom refreshes the cache from a freshly read or written store.
func (st *lspSilentState) setFrom(root string, s *ilsp.SilentStore) {
	st.root, st.loaded = root, true
	st.ids = map[string]bool{}
	for _, id := range s.IDs(root) {
		st.ids[id] = true
	}
}

// updateSilent applies fn to the active project's markers on disk and
// refreshes the cache.
func (m *Model) updateSilent(fn func(s *ilsp.SilentStore, root string) bool) error {
	root := m.silentRoot()
	if m.lspSilent == nil || root == "" {
		return nil
	}
	s, err := ilsp.UpdateSilent(ilsp.SilentPath(root), func(s *ilsp.SilentStore) bool { return fn(s, root) })
	m.lspSilent.setFrom(root, s)
	return err
}

// switchServerDocs lists the server-backed languages of the freshly built
// model's open editor documents (#2492, #3080), one entry per server id in
// pane/tab order — the same walk Init's EventFileOpened announcement does.
// When it is empty no didOpen will ever fire and no publish can close the
// warm-up wait, so the switch reports the "lsp" phase as skipped instead of
// arming it.
func (m *Model) switchServerDocs() []serverDoc {
	var out []serverDoc
	seen := map[string]bool{}
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if !ed.HasFile() {
				continue
			}
			l, ok := lang.ByPath(ed.Path())
			if !ok {
				continue
			}
			if id := serverIDOf(l); id != "" && !seen[id] {
				seen[id] = true
				out = append(out, serverDoc{lang: l.ID, server: id})
			}
		}
	}
	return out
}

// newSwitchLSPWait sets the switch's warm-up wait (#2403) for the servers of
// docs that are not already known to stay silent in this project (#3080),
// and returns how many were skipped as known-silent. The notice names the
// first waited-for document's language.
func (m *Model) newSwitchLSPWait(start time.Time, docs []serverDoc) (knownSilent int) {
	ids := m.silentIDs()
	w := &switchLSPWait{start: start, root: m.projectRootTag()}
	for _, d := range docs {
		if ids[d.server] {
			knownSilent++
			continue
		}
		if w.lang == "" {
			w.lang = d.lang
		}
		w.servers = append(w.servers, d.server)
	}
	m.switchLSPWait = w
	return knownSilent
}

// armSwitchLSPWait schedules the timers of the wait newSwitchLSPWait set, or
// closes it on the spot (#2492): skipped=silent_known when every open
// document's server is known to stay silent (#3080), skipped=no_server_docs
// when no server-language document is open at all. Called after the switch's
// "ok", so the phase always follows it.
func (m *Model) armSwitchLSPWait(knownSilent int) tea.Cmd {
	w := m.switchLSPWait
	switch {
	case w != nil && len(w.servers) > 0:
		// A server that never publishes leaves the editor diagnostic-blind
		// without saying so (#2629): the notice timer speaks up long before
		// the two-minute telemetry fallback closes the wait.
		return tea.Batch(armSwitchLSPQuiet(w), armSwitchLSPNotice(w))
	case knownSilent > 0:
		m.noteSwitchLSPSkipped("silent_known")
	default:
		m.noteSwitchLSPSkipped("no_server_docs")
	}
	return nil
}

// markSwitchSilent persists the servers a quiet warm-up waited for as
// known-silent (#3080). With language servers switched off nothing was
// meant to answer, so nothing is recorded.
func (m *Model) markSwitchSilent(w *switchLSPWait) {
	if w == nil || len(w.servers) == 0 {
		return
	}
	if c := config.Get(); c != nil && !c.LSP.Enabled {
		return
	}
	_ = m.updateSilent(func(s *ilsp.SilentStore, root string) bool {
		changed := false
		for _, id := range w.servers {
			changed = s.Mark(root, id) || changed
		}
		return changed
	})
}

// noteLSPPublish clears the marker of a known-silent server that just
// published for one of paths (#3080): it is not silent after all — an
// upgrade or a config fix brought it to life — so the next switch waits for
// it normally again. Projects without markers return after one map check.
func (m *Model) noteLSPPublish(paths ...string) {
	ids := m.silentIDs()
	if len(ids) == 0 {
		return
	}
	var cleared []string
	for _, p := range paths {
		l, ok := lang.ByPath(p)
		if !ok {
			continue
		}
		if id := serverIDOf(l); ids[id] {
			cleared = append(cleared, id)
			delete(ids, id)
		}
	}
	if len(cleared) == 0 {
		return
	}
	_ = m.updateSilent(func(s *ilsp.SilentStore, root string) bool {
		changed := false
		for _, id := range cleared {
			changed = s.Clear(root, id) || changed
		}
		return changed
	})
}

// forgetSilent runs lsp.forgetSilent (#3080): every known-silent server of
// the active project is forgotten, so the next switch into it waits for all
// of them again.
func (m *Model) forgetSilent() tea.Cmd {
	root := m.silentRoot()
	if root == "" {
		m.host.Notify(host.Warn, "no project to forget silent language servers for")
		return nil
	}
	// Re-read: the settings page may have reset flags behind the cache.
	if m.lspSilent != nil {
		m.lspSilent.loaded = false
	}
	ids := m.silentIDs()
	if len(ids) == 0 {
		m.host.Notify(host.Info, "no language server is marked silent for "+filepath.Base(root))
		return nil
	}
	names := make([]string, 0, len(ids))
	for id := range ids {
		names = append(names, id)
	}
	if err := m.updateSilent(func(s *ilsp.SilentStore, root string) bool { return s.Clear(root, "") }); err != nil {
		m.host.Notify(host.Error, "forget silent language servers: "+err.Error())
		return nil
	}
	sort.Strings(names)
	m.host.Notify(host.Info, "silent flag reset for "+strings.Join(names, ", ")+" in "+filepath.Base(root))
	return nil
}
