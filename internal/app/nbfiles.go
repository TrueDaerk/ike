package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/nbview"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// nbfiles.go routes .ipynb files into the notebook viewer (#2425) and owns
// the three things the pane deliberately cannot do itself: the clipboard, the
// scratch store, and writing an image output to disk.

// OpenNotebookMsg asks the root model to open a file in a notebook viewer
// pane (#2425). Dispatched by the "notebook" file handler for .ipynb files
// and by the "Open file as…" chooser, so a notebook never lands in a raw JSON
// buffer by accident — "Open file as… → Text editor" still does, on purpose.
type OpenNotebookMsg struct{ Path string }

// notebookProvider is the compile-in plugin claiming .ipynb files.
type notebookProvider struct{}

func (notebookProvider) ID() string { return "notebook" }

func (notebookProvider) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{FileHandlers: []plugin.FileHandler{{
		ID: "notebook.view",
		// By extension only: a notebook is JSON, and no magic distinguishes
		// it from any other JSON document without parsing the whole file.
		Extensions: []string{".ipynb"},
		Open: func(h host.API, path string) tea.Cmd {
			return h.Dispatch(OpenNotebookMsg{Path: path})
		},
	}}}
}

func init() { registry.Register(notebookProvider{}) }

// openNotebookPane opens (or refocuses) the notebook viewer for path as a tab
// in the pane the open asked for — the focused pane for the palette (#1825),
// the last-focused editor for the explorer's default open (#1851) — and
// otherwise split off the leaf viewerSplitTarget picks, the pane the user
// last worked in (#1779).
func (m *Model) openNotebookPane(path string) {
	tabHost := m.takeViewerTabHost()
	if hostKey, tabIdx, _, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindNotebook && c.Notebook().Path() == path
	}); ok {
		m.focusContentAt(hostKey, tabIdx) // may live in a tab (#1778)
		return
	}
	if tabHost != "" {
		if _, ok := m.openContentTab(tabHost, pane.KindNotebook, path); ok {
			m.trackNotebook(path)
			return
		}
	}
	target := m.viewerSplitTarget()
	key := m.activeWS().Panes.AddNotebookView(path)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, layout.ZoneRight)
	if !ok {
		m.activeWS().Panes.Close(key)
		return
	}
	m.activeWS().Tree = tree
	m.layout()
	m.setFocus(key)
	m.trackNotebook(path)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// trackNotebook adds the notebook to the watcher's poll set, so a notebook
// outside the recursive watch (or one the fsnotify cap left out) still
// re-renders when a kernel writes it.
func (m *Model) trackNotebook(path string) {
	if m.watcher != nil {
		m.watcher.Track(path)
	}
}

// refreshNotebooks re-reads every open notebook viewer bound to path after
// the watcher reported it changed (#2425). The pane holds no editor buffer,
// so nothing else in the routing would notice; there is nothing unsaved to
// lose either, the viewer being read-only.
func (m *Model) refreshNotebooks(path string) {
	m.contentInstances(func(_ string, _ int, inst *pane.Instance) bool {
		if inst.Kind() == pane.KindNotebook && inst.Notebook().Path() == path {
			inst.Notebook().Reload()
		}
		return true
	})
}

// notebookScratch opens a cell's source as a scratch file in its own
// language (#2425): the pane resolved the extension off the notebook's
// language_info, the scratch store and the open funnel are the app's.
func (m Model) notebookScratch(msg nbview.ScratchMsg) (tea.Model, tea.Cmd) {
	return m.newScratch(msg.Ext, msg.Content)
}

// saveNotebookImage writes one image output next to the notebook (#2425).
// The suggested name is never overwritten: a taken name gets a "-2", "-3", …
// suffix, so repeatedly saving the outputs of a re-run notebook keeps every
// version instead of silently replacing the last one.
func (m *Model) saveNotebookImage(msg nbview.SaveImageMsg) {
	path, err := freeImagePath(msg.Path)
	if err != nil {
		m.host.Notify(host.Warn, "save image: "+err.Error())
		return
	}
	if err := os.WriteFile(path, msg.Data, 0o644); err != nil {
		m.host.Notify(host.Warn, "save image: "+err.Error())
		return
	}
	m.host.Notify(host.Info, "saved "+displayPath(path))
}

// freeImagePath returns the first unused name at or after the suggestion.
// The cap keeps a directory that cannot be written (or a pathological name
// clash) from spinning forever.
func freeImagePath(suggested string) (string, error) {
	ext := filepath.Ext(suggested)
	base := strings.TrimSuffix(suggested, ext)
	for n := 1; n <= 1000; n++ {
		path := suggested
		if n > 1 {
			path = fmt.Sprintf("%s-%d%s", base, n, ext)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path, nil
		}
	}
	return "", fmt.Errorf("no free name next to %s", filepath.Base(suggested))
}
