package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/gzfile"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/textenc"
)

// OpenGzipMsg asks the root model to open a plain gzip file transparently
// decompressed (#1763). Dispatched by the "gzip" file handler when the
// explorer, `:e` or the CLI opens one, so `app.log.gz` never lands in a buffer
// full of compressed bytes.
type OpenGzipMsg struct{ Path string }

// gzipProvider is the compile-in plugin claiming plain gzip files, routing
// them to a read-only buffer holding their decompressed content.
type gzipProvider struct{}

func (gzipProvider) ID() string { return "gzip" }

func (gzipProvider) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{FileHandlers: []plugin.FileHandler{{
		ID: "gzip.view",
		// No Extensions on purpose. The registry matches filepath.Ext, which
		// reads ".tar.gz" as ".gz" — claiming that extension outright would
		// steal every compressed tar from the archive viewer (#1762). Match
		// looks at the content instead and declines anything holding a tar,
		// so the two handlers partition the gzip files between them.
		Match: gzfile.IsPlain,
		Open: func(h host.API, path string) tea.Cmd {
			return h.Dispatch(OpenGzipMsg{Path: path})
		},
	}}}
}

func init() { registry.Register(gzipProvider{}) }

// openGzipFile decompresses path into memory and shows it in a read-only
// editor tab (#1763). The decompressed byte cap is the large-file threshold
// (#149) — applied to the *output*, so a bomb costs one capped buffer — and
// binary content degrades to a metadata notice rather than a garbage buffer.
// Re-opening the same file activates its existing tab.
func (m *Model) openGzipFile(path string) tea.Cmd {
	c, err := gzfile.Read(path, m.largeFileLimit())
	if err != nil {
		m.host.Notify(host.Error, "cannot read "+baseName(path)+": "+err.Error())
		return nil
	}
	text, notice := m.gzipBufferText(c, path)
	return m.showGzipBuffer(path, c.Name, text, notice)
}

// gzipBufferText renders what the buffer shows for c: the decompressed text,
// or — when the content is binary — a metadata notice describing it. The
// second result is the toast to raise alongside, empty for a clean read.
func (m *Model) gzipBufferText(c gzfile.Content, path string) (text, notice string) {
	if isBinary(c.Data) {
		return gzipMetadata(c, path), "binary content — showing gzip metadata: " + baseName(path)
	}
	decoded, _, err := textenc.Decode(c.Data, textenc.UTF8)
	if err != nil {
		return gzipMetadata(c, path), "cannot decode " + c.Name + ": " + err.Error()
	}
	if c.Truncated {
		notice = fmt.Sprintf("gzip content truncated at the large-file limit (%s shown)", humanBytes(int64(len(c.Data))))
	}
	// A line-count blowout is the other large-file threshold: a 900 MB log
	// compresses small enough to clear the byte cap and still be unusable.
	if lines := m.largeFileLimits().MaxLines; lines > 0 {
		if cut, dropped := truncateLines(decoded, lines); dropped {
			decoded = cut
			notice = fmt.Sprintf("gzip content truncated at the large-file limit (%d lines shown)", lines)
		}
	}
	return decoded, notice
}

// showGzipBuffer installs text as a read-only buffer for the gz file, reusing
// an already open tab for the same archive.
func (m *Model) showGzipBuffer(path, inner, text, notice string) tea.Cmd {
	vpath := archiveEntryPath(path, inner)
	key := m.fileEditorKey()
	if key == "" {
		key = m.spawnEditor()
	}
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		return nil
	}
	if idx := inst.TabForPath(vpath); idx >= 0 {
		m.activateTab(inst, idx)
		m.setFocus(key)
		return nil
	}
	// Same tab policy as a file open (#156): an empty scratch tab is filled in
	// place, anything else gets a fresh tab.
	if ed := inst.Editor(); ed == nil || !ed.IsEmpty() {
		inst.AddTab()
		m.installEmitter(key)
	}
	ed := inst.Editor()
	if ed == nil {
		return nil
	}
	ed.ShowReadOnly(vpath, text)
	if notice != "" {
		m.host.Notify(host.Warn, notice)
	}
	if m.watcher != nil {
		m.watcher.Track(path) // the *outer* file is what changes on disk
	}
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	return ed.Reparse()
}

// refreshGzipBuffers re-decompresses every open gz preview of path after the
// watcher reported it changed (#1763). The buffer's own path names content
// inside the archive, so the editor's reload never fires for it — the root
// model re-installs the content instead. Nothing is dirty to lose: the buffer
// is read-only.
//
// The returned command re-runs the parse for every refreshed buffer.
// ShowReadOnly drops the cached spans and advances the document version, and a
// read-only buffer can never schedule a parse of its own the way an edit does
// — so without this the preview silently loses its highlighting the first time
// the file changes on disk (#1853).
func (m *Model) refreshGzipBuffers(path string) tea.Cmd {
	if !gzfile.IsPlain(path, readHead(path)) {
		return nil
	}
	prefix := path + entrySep
	var stale []*editor.Model
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			ed := inst.TabEditor(i)
			if ed != nil && ed.ReadOnly() && strings.HasPrefix(ed.Path(), prefix) {
				stale = append(stale, ed)
			}
		}
	}
	if len(stale) == 0 {
		return nil
	}
	c, err := gzfile.Read(path, m.largeFileLimit())
	if err != nil {
		return nil // a half-written file: keep showing the previous content
	}
	text, _ := m.gzipBufferText(c, path)
	var cmds []tea.Cmd
	for _, ed := range stale {
		ed.ShowReadOnly(archiveEntryPath(path, c.Name), text)
		if cmd := ed.Reparse(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// gzipMetadata renders the notice shown instead of content that cannot be
// displayed as text: what the archive holds and how it compresses.
func gzipMetadata(c gzfile.Content, path string) string {
	var b strings.Builder
	b.WriteString("Binary content — no preview.\n\n")
	fmt.Fprintf(&b, "Archive:       %s\n", baseName(path))
	fmt.Fprintf(&b, "Content:       %s\n", c.Name)
	fmt.Fprintf(&b, "Compressed:    %s\n", humanBytes(c.Compressed))
	switch {
	case c.OriginalOK:
		fmt.Fprintf(&b, "Decompressed:  %s\n", humanBytes(c.Original))
	case c.Truncated:
		fmt.Fprintf(&b, "Decompressed:  more than %s\n", humanBytes(int64(len(c.Data))))
	default:
		fmt.Fprintf(&b, "Decompressed:  %s\n", humanBytes(int64(len(c.Data))))
	}
	if r := c.Ratio(); r > 0 {
		fmt.Fprintf(&b, "Ratio:         %.1f×\n", r)
	}
	return b.String()
}

// truncateLines caps text at max lines, reporting whether it had more.
func truncateLines(text string, max int) (string, bool) {
	cut := text
	for i, n := 0, 0; i < len(cut); i++ {
		if cut[i] != '\n' {
			continue
		}
		if n++; n == max {
			if i+1 >= len(cut) {
				return text, false // exactly max lines, trailing newline and all
			}
			return cut[:i], true
		}
	}
	return text, false
}

// humanBytes formats a byte count for the metadata notice.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
