package lsp

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/lsp/manager"
)

// showlog.go implements lsp.showLog (#715): open the most recently modified
// server log (the crashed server's, in the common case) in a new editor pane.
// The transport tees every server's stderr into manager.LogPath(lang), so the
// file holds the full crash output plus the manager's lifecycle markers.

// showLog runs the palette command.
func showLog(h host.API) tea.Cmd {
	path, others := latestLog(manager.LogDir())
	if path == "" {
		h.Notify(host.Info, "no language-server logs yet ("+manager.LogDir()+")")
		return nil
	}
	if others > 0 {
		h.Notify(host.Info, "opening "+filepath.Base(path)+" — more logs in "+manager.LogDir())
	}
	return h.OpenFileIn(path, true)
}

// latestLog picks the most recently modified lsp-*.log in dir and counts the
// remaining ones.
func latestLog(dir string) (path string, others int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0
	}
	type cand struct {
		path string
		mod  int64
	}
	var cands []cand
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "lsp-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		cands = append(cands, cand{path: filepath.Join(dir, name), mod: info.ModTime().UnixNano()})
	}
	if len(cands) == 0 {
		return "", 0
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].mod > cands[j].mod })
	return cands[0].path, len(cands) - 1
}
