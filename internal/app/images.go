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

// openImagePreview opens (or refocuses) the image preview for path, split off
// the leaf viewerSplitTarget picks — the pane the user last worked in, never
// the explorer they opened the file from (#1779).
func (m *Model) openImagePreview(path string) {
	if hostKey, tabIdx, _, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindImage && c.Image().Path() == path
	}); ok {
		m.focusContentAt(hostKey, tabIdx) // may live in a tab (#1778)
		return
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
	supported := m.kittyGfx != nil && *m.kittyGfx
	live := map[int]bool{}
	var raw []string
	hasImages := false
	m.contentInstances(func(_ string, _ int, inst *pane.Instance) bool {
		if inst.Kind() != pane.KindImage {
			return true
		}
		hasImages = true
		iv := inst.Image()
		iv.SetGraphics(supported)
		if !supported {
			return true
		}
		live[iv.ID()] = true
		if seqs := iv.SyncSeqs(); len(seqs) > 0 {
			raw = append(raw, seqs...)
			m.liveImages[iv.ID()] = true
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
		if inst.Kind() != pane.KindImage {
			return true
		}
		iv := inst.Image()
		if !iv.Transmitted() {
			return true
		}
		raw = append(raw, imgview.Delete(iv.ID()))
		iv.Reset()
		delete(m.liveImages, iv.ID())
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
