package app

import (
	"bytes"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"ike/internal/host"
	"ike/internal/imgview"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/workspace"
)

// OpenImageMsg asks the root model to open an image file in an image preview
// pane (#1479). Dispatched by the "images" file handler when the explorer or
// editor opens a supported image, so a PNG never lands in a raw text buffer.
type OpenImageMsg struct{ Path string }

// imageProvider is the compile-in plugin claiming image files by extension
// and magic-byte sniff, routing them to the image preview.
type imageProvider struct{}

func (imageProvider) ID() string { return "images" }

func (imageProvider) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{FileHandlers: []plugin.FileHandler{{
		ID:         "images.preview",
		Extensions: []string{".png", ".jpg", ".jpeg", ".gif", ".webp"},
		Match:      func(path string, head []byte) bool { return sniffImage(head) },
		Open: func(h host.API, path string) tea.Cmd {
			return h.Dispatch(OpenImageMsg{Path: path})
		},
	}}}
}

func init() { registry.Register(imageProvider{}) }

// sniffImage recognises the magic bytes of the supported formats, for image
// files with unusual extensions.
func sniffImage(head []byte) bool {
	switch {
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		return true
	case bytes.HasPrefix(head, []byte("\xff\xd8\xff")):
		return true
	case bytes.HasPrefix(head, []byte("GIF87a")) || bytes.HasPrefix(head, []byte("GIF89a")):
		return true
	case len(head) >= 12 && bytes.Equal(head[0:4], []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WEBP")):
		return true
	}
	return false
}

// openImagePreview opens (or refocuses) the image preview for path as a tab in
// the pane the open asked for — the focused pane for the palette (#1825), the
// last-focused editor for the explorer's default open (#1851) — and otherwise
// split off the leaf viewerSplitTarget picks, the pane the user last worked in
// (#1779), which is what an explicit split open still does.
func (m *Model) openImagePreview(path string) {
	tabHost := m.takeViewerTabHost()
	if hostKey, tabIdx, _, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindImage && c.Image().Path() == path
	}); ok {
		m.focusContentAt(hostKey, tabIdx) // may live in a tab (#1778)
		return
	}
	if tabHost != "" {
		if _, ok := m.openContentTab(tabHost, pane.KindImage, path); ok {
			return
		}
	}
	target := m.viewerSplitTarget()
	key := m.activeWS().Panes.AddImagePreview(path)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, layout.ZoneRight)
	if !ok {
		m.activeWS().Panes.Close(key)
		return
	}
	m.activeWS().Tree = tree
	m.layout()
	m.setFocus(key)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// imageSyncCmd reconciles the terminal's Kitty graphics state with the image
// panes after every Update pass (#1479): transmit on first show and after a
// resize, delete when a pane closed — so no ghost graphics outlive their
// pane. A workspace switch or teardown releases its placements separately
// via releaseWorkspaceImages (#1547). On a terminal whose support is still
// unknown it emits the capability probe once (first image pane); without
// support it does nothing and the panes render their metadata fallback.
func (m *Model) imageSyncCmd() tea.Cmd {
	// This runs on *every* message (#2187), and the walk below allocates a
	// key slice per pane host plus a map. A workspace that never opened an
	// image pane cannot have anything to reconcile — the mint counter says so
	// without touching an instance — and neither can one whose placements are
	// already released.
	if !m.activeWS().Panes.ImagesMinted() && !m.activeWS().Panes.PreviewsMinted() &&
		!m.activeWS().Panes.NotebooksMinted() && len(m.liveImages) == 0 {
		return nil
	}
	supported := m.kittyGfx != nil && *m.kittyGfx
	var live map[int]bool
	var raw []string
	hasImages := false
	mark := func(ids ...int) {
		if live == nil {
			live = map[int]bool{}
		}
		for _, id := range ids {
			live[id] = true
		}
	}
	m.contentInstances(func(_ string, _ int, inst *pane.Instance) bool {
		switch inst.Kind() {
		case pane.KindImage:
			iv := inst.Image()
			hasImages = true
			iv.SetGraphics(supported)
			if !supported {
				return true
			}
			mark(iv.ID())
			if seqs := iv.SyncSeqs(); len(seqs) > 0 {
				raw = append(raw, seqs...)
				m.liveImages[iv.ID()] = true
			}
		case pane.KindMarkdown:
			// A markdown preview holds one placement per local image its
			// buffer references (#2180) — same reconcile, several ids.
			pv := inst.Preview()
			pv.SetGraphics(supported)
			if !pv.HasImages() {
				return true
			}
			hasImages = true
			if !supported {
				return true
			}
			mark(pv.ImageIDs()...)
			if seqs := pv.SyncSeqs(); len(seqs) > 0 {
				raw = append(raw, seqs...)
				for _, id := range pv.TransmittedIDs() {
					m.liveImages[id] = true
				}
			}
		case pane.KindNotebook:
			// A notebook holds one placement per image output (#2425) —
			// the markdown preview's reconcile, over cells instead of
			// inline references.
			nv := inst.Notebook()
			nv.SetGraphics(supported)
			if !nv.HasImages() {
				return true
			}
			hasImages = true
			if !supported {
				return true
			}
			mark(nv.ImageIDs()...)
			if seqs := nv.SyncSeqs(); len(seqs) > 0 {
				raw = append(raw, seqs...)
				for _, id := range nv.TransmittedIDs() {
					m.liveImages[id] = true
				}
			}
		}
		return true
	})
	for id := range m.liveImages {
		if !live[id] {
			raw = append(raw, imgview.Delete(id))
			delete(m.liveImages, id)
		}
	}
	if hasImages && m.kittyGfx == nil && !m.gfxQueried {
		m.gfxQueried = true
		raw = append(raw, imgview.Query())
	}
	if len(raw) == 0 {
		return nil
	}
	return tea.Raw(strings.Join(raw, ""))
}

// releaseWorkspaceImages deletes the Kitty placements of every image pane a
// workspace holds and resets their transmission state (#1547), returning the
// cmd emitting the delete sequences (nil when nothing is resident). Called
// when a workspace parks (project switch) or is torn down: liveImages is
// re-initialized empty in buildModel, so without the explicit deletes the
// placements — a full PNG each in the host terminal's graphics memory —
// survived for the process lifetime. The reset makes the resume's reconcile
// pass transmit again.
func (m *Model) releaseWorkspaceImages(w *workspace.Workspace) tea.Cmd {
	if w == nil || w.Panes == nil {
		return nil
	}
	var raw []string
	forEachContent(w.Panes, func(_ string, _ int, inst *pane.Instance) bool {
		switch inst.Kind() {
		case pane.KindImage:
			iv := inst.Image()
			if !iv.Transmitted() {
				return true
			}
			raw = append(raw, imgview.Delete(iv.ID()))
			iv.Reset()
			delete(m.liveImages, iv.ID())
		case pane.KindMarkdown:
			// The preview's inline images (#2180) are placements too, and
			// leak the same way if a parked workspace keeps them resident.
			pv := inst.Preview()
			for _, id := range pv.TransmittedIDs() {
				raw = append(raw, imgview.Delete(id))
				delete(m.liveImages, id)
			}
			pv.ResetImages()
		case pane.KindNotebook:
			// A notebook's image outputs (#2425) are placements for the
			// same reason and leak the same way.
			nv := inst.Notebook()
			for _, id := range nv.TransmittedIDs() {
				raw = append(raw, imgview.Delete(id))
				delete(m.liveImages, id)
			}
			nv.ResetImages()
		}
		return true
	})
	if len(raw) == 0 {
		return nil
	}
	return tea.Raw(strings.Join(raw, ""))
}

// handleKittyGraphics consumes a Kitty graphics APC response: the capability
// probe's acknowledgement flips support on and the next reconcile pass
// transmits every open image pane.
func (m *Model) handleKittyGraphics(ev uv.KittyGraphicsEvent) {
	if m.kittyGfx == nil && imgview.QueryResponseOK(ev.Options.ID, ev.Payload) {
		ok := true
		m.kittyGfx = &ok
	}
}
