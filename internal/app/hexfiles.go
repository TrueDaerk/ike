package app

import (
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"ike/internal/hexview"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/watch"
)

// OpenHexMsg asks the root model to open a file in a hex viewer pane (#2420).
// Dispatched by the "hex" file handler when a binary file with no dedicated
// viewer would otherwise land in a text buffer, and by the "Open file as…"
// chooser. Forced marks the explicit chooser pick, which the files.binary_open
// setting must not redirect back to the editor.
type OpenHexMsg struct {
	Path   string
	Forced bool
}

// hexProvider is the compile-in plugin claiming binary files by content
// sniff — a NUL byte in the head and no other viewer's magic — routing them
// to the hex viewer instead of a text buffer.
type hexProvider struct{}

func (hexProvider) ID() string { return "hex" }

func (hexProvider) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{FileHandlers: []plugin.FileHandler{{
		ID: "hex.view",
		// No extensions on purpose: the claim is by content only, so a
		// binary-looking .db or .png still reaches its dedicated viewer —
		// handlers match in ID order and the archive/data/gzip sniffs run
		// before this one; the image sniff runs after, hence the exclusion.
		Match: func(path string, head []byte) bool {
			return isBinary(head) && !sniffImage(head)
		},
		Open: func(h host.API, path string) tea.Cmd {
			return h.Dispatch(OpenHexMsg{Path: path})
		},
	}}}
}

func init() { registry.Register(hexProvider{}) }

// openHexPane opens (or refocuses) the hex viewer for path as a tab in the
// pane the open asked for — the focused pane for the palette (#1825), the
// last-focused editor for the explorer's default open (#1851) — and otherwise
// split off the leaf viewerSplitTarget picks, the pane the user last worked
// in (#1779). Reads are windowed, so the pane appears at once whatever the
// file size.
func (m *Model) openHexPane(path string) {
	m.openViewerPane(pane.KindHex, path, func(c *pane.Instance) bool {
		return c.Kind() == pane.KindHex && c.Hex().Path() == path
	}, func() string { return m.activeWS().Panes.AddHexView(path) })
}

// HexSaveMsg runs hex.save (cmd+s / ctrl+s in the hex viewer, #2876): the
// focused hex pane writes its edited bytes back to the file in place.
type HexSaveMsg struct{}

// saveFocusedHex runs hex.save against the focused hex viewer — a pane of
// its own or a content tab. The returned cmd refreshes the VCS status, the
// one follow-up an in-IDE write owes besides the watcher stamp.
func (m *Model) saveFocusedHex() tea.Cmd {
	c := m.focusedContent()
	if c == nil || c.Kind() != pane.KindHex {
		m.host.Notify(host.Info, "hex: focus a hex viewer first")
		return nil
	}
	hv := c.Hex()
	if !hv.Dirty() {
		m.host.Notify(host.Info, "hex: no unsaved changes in "+filepath.Base(hv.Path()))
		return nil
	}
	n, ok := m.saveHex(hv)
	if !ok {
		return nil
	}
	m.host.Notify(host.Info, fmt.Sprintf("hex: wrote %d changed bytes to %s", n, filepath.Base(hv.Path())))
	return func() tea.Msg { return vcsInvalidateMsg{} }
}

// saveHex writes hv's edit overlay in place, stamping the watcher first so
// the write is known as IKE's own (no reload, no external-change warning).
// A failed write notifies and keeps the edits.
func (m *Model) saveHex(hv *hexview.Model) (int, bool) {
	return saveHexWith(hv, m.watcher, m.host)
}

// saveHexWith is saveHex for the callers without a model at hand — the
// workspace teardown guards save parked workspaces through it.
func saveHexWith(hv *hexview.Model, w *watch.Service, h host.API) (int, bool) {
	if !hv.Dirty() {
		return 0, true
	}
	if w != nil {
		w.MarkSaved(hv.Path())
	}
	n, err := hv.Save()
	if err != nil {
		if h != nil {
			h.Notify(host.Error, "hex: cannot save "+filepath.Base(hv.Path())+": "+err.Error())
		}
		return 0, false
	}
	return n, true
}

// hexViews returns the hex viewer models of inst: the pane itself when it is
// one, else the hex content tabs it hosts (#1778).
func hexViews(inst *pane.Instance) []*hexview.Model {
	if inst == nil {
		return nil
	}
	if inst.Kind() == pane.KindHex {
		return []*hexview.Model{inst.Hex()}
	}
	var out []*hexview.Model
	for i := 0; i < inst.TabCount(); i++ {
		if c := inst.TabContent(i); c != nil && c.Kind() == pane.KindHex {
			out = append(out, c.Hex())
		}
	}
	return out
}

// dirtyHexName names a hex viewer with unsaved edits for the guard prompts;
// ok is false for anything else.
func dirtyHexName(c *pane.Instance) (string, bool) {
	if c == nil || c.Kind() != pane.KindHex || !c.Hex().Dirty() {
		return "", false
	}
	return filepath.Base(c.Hex().Path()), true
}

// binaryOpensInEditor reads files.binary_open (#2420): "editor" sends a
// sniffed binary to the text editor (insight off) instead of the hex viewer;
// anything else — the default "hex" included — keeps the hex viewer.
func (m Model) binaryOpensInEditor() bool {
	cfg := m.host.Config()
	if cfg == nil {
		return false
	}
	v, ok := cfg.Get("files.binary_open")
	return ok && v == "editor"
}
