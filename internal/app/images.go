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
// the focused leaf like the diff viewer.
func (m *Model) openImagePreview(path string) {
	for _, key := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindImage && inst.Image().Path() == path {
			m.setFocus(key)
			return
		}
	}
	key := m.activeWS().Panes.AddImagePreview(path)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, m.activeWS().Panes.Focused(), key, layout.ZoneRight)
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
// resize, delete when a pane closed or the workspace switched away — so no
// ghost graphics outlive their pane. On a terminal whose support is still
// unknown it emits the capability probe once (first image pane); without
// support it does nothing and the panes render their metadata fallback.
func (m *Model) imageSyncCmd() tea.Cmd {
	supported := m.kittyGfx != nil && *m.kittyGfx
	live := map[int]bool{}
	var raw []string
	hasImages := false
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindImage {
			continue
		}
		hasImages = true
		iv := inst.Image()
		iv.SetGraphics(supported)
		if !supported {
			continue
		}
		live[iv.ID()] = true
		if seqs := iv.SyncSeqs(); len(seqs) > 0 {
			raw = append(raw, seqs...)
			m.liveImages[iv.ID()] = true
		}
	}
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

// handleKittyGraphics consumes a Kitty graphics APC response: the capability
// probe's acknowledgement flips support on and the next reconcile pass
// transmits every open image pane.
func (m *Model) handleKittyGraphics(ev uv.KittyGraphicsEvent) {
	if m.kittyGfx == nil && imgview.QueryResponseOK(ev.Options.ID, ev.Payload) {
		ok := true
		m.kittyGfx = &ok
	}
}
