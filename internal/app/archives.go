package app

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/archive"
	"ike/internal/archview"
	"ike/internal/gzfile"
	"ike/internal/host"
	"ike/internal/largefile"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/textenc"
)

// OpenArchiveMsg asks the root model to open an archive file in an archive
// viewer pane (#1762). Dispatched by the "archives" file handler when the
// explorer, `:e` or the CLI opens a supported archive, so a tar never lands in
// a raw text buffer.
type OpenArchiveMsg struct{ Path string }

// archiveProvider is the compile-in plugin claiming archive files by
// extension and content sniff, routing them to the archive viewer.
type archiveProvider struct{}

func (archiveProvider) ID() string { return "archives" }

func (archiveProvider) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{FileHandlers: []plugin.FileHandler{{
		ID: "archives.view",
		// Only the unambiguous single extensions are claimed outright: the
		// registry matches filepath.Ext, which reads ".tar.gz" as ".gz", and a
		// plain .gz is not ours. The compressed flavours arrive through Match
		// below, which looks *inside* the stream — so app.log.gz stays with
		// the gz handler while backup.tar.gz lands here.
		Extensions: []string{".tar", ".tgz", ".tbz", ".tbz2"},
		Match:      archive.IsArchive,
		Open: func(h host.API, path string) tea.Cmd {
			return h.Dispatch(OpenArchiveMsg{Path: path})
		},
	}}}
}

func init() { registry.Register(archiveProvider{}) }

// entrySep joins an archive path and a member path into the virtual path of a
// previewed entry: "/tmp/src.tar!cmd/main.go". The member's own file name is
// the tail, so the tab title, the language lookup and the highlighting all
// resolve from it — while nothing on disk answers to the whole string, which
// is exactly why the buffer is read-only.
const entrySep = "!"

// archiveEntryPath builds the virtual path of one archive member.
func archiveEntryPath(archivePath, entry string) string {
	return archivePath + entrySep + entry
}

// openArchivePane opens (or refocuses) the archive viewer for path as a tab in
// the pane the open asked for — the focused pane for the palette (#1825), the
// last-focused editor for the explorer's default open (#1851) — and otherwise
// split off the leaf viewerSplitTarget picks, the pane the user last worked in
// (#1779), which is what an explicit split open still does.
func (m *Model) openArchivePane(path string) {
	tabHost := m.takeViewerTabHost()
	if hostKey, tabIdx, _, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindArchive && c.Archive().Path() == path
	}); ok {
		m.focusContentAt(hostKey, tabIdx) // may live in a tab (#1778)
		return
	}
	if tabHost != "" {
		if _, ok := m.openContentTab(tabHost, pane.KindArchive, path); ok {
			return
		}
	}
	target := m.viewerSplitTarget()
	key := m.activeWS().Panes.AddArchiveView(path)
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

// openArchiveEntry extracts one entry into memory and shows it in a read-only
// editor tab (#1762). The large-file thresholds apply before the bytes are
// buffered, binary members are refused rather than rendered as garbage, and
// re-opening the same entry activates its existing tab.
func (m *Model) openArchiveEntry(archivePath, entry string) tea.Cmd {
	data, err := archive.ReadEntry(archivePath, entry, m.largeFileLimit())
	switch {
	case errors.Is(err, archive.ErrTooLarge):
		m.host.Notify(host.Warn, "archive entry too large to preview: "+entry)
		return nil
	case err != nil:
		m.host.Notify(host.Error, "cannot read "+entry+": "+err.Error())
		return nil
	}
	// A gzip member is compressed bytes, and compressed bytes are binary:
	// without this seam logs/app.log.gz would be refused as a blob nobody can
	// look at (#1948). Decompressing first makes the two viewers compose.
	if gzfile.IsGzip(data) {
		return m.openArchiveGzipEntry(archivePath, entry, data)
	}
	if isBinary(data) {
		m.host.Notify(host.Warn, "binary archive entry — no preview: "+entry)
		return nil
	}
	text, _, err := textenc.Decode(data, textenc.UTF8)
	if err != nil {
		m.host.Notify(host.Error, "cannot decode "+entry+": "+err.Error())
		return nil
	}
	vpath := archiveEntryPath(archivePath, entry)
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
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	return ed.Reparse()
}

// nestedArchiveNotice prefixes the refusal for an archive inside an archive.
// It replaces the blanket "binary archive entry" the member used to draw:
// there is no nested archive view, and saying so is the point (#1948).
const nestedArchiveNotice = "nested archive — extract not supported: "

// openArchiveGzipEntry shows a gzip member of an archive decompressed, the
// way the gz viewer shows a plain .gz (#1948): read-only buffer, language
// from the inner name, the decompressed-byte cap as the bomb guard, and a
// metadata notice for content that is still binary underneath.
//
// data holds the member's compressed bytes, already extracted under the
// large-file limit; the cap is applied a second time to the *decompressed*
// stream, because that is the size a bomb blows up to.
func (m *Model) openArchiveGzipEntry(archivePath, entry string, data []byte) tea.Cmd {
	// The compound name settles it before anything is decompressed: an
	// inner.tar.gz is an archive, and IKE opens no archive inside an archive.
	if gzfile.HasTarSuffix(entry) {
		m.host.Notify(host.Warn, nestedArchiveNotice+entry)
		return nil
	}
	c, err := gzfile.ReadBytes(entry, data, m.largeFileLimit())
	if err != nil {
		m.host.Notify(host.Error, "cannot read "+entry+": "+err.Error())
		return nil
	}
	// The name said nothing, so the payload has to: a tar header in the first
	// block is the same nested archive under a plainer name.
	if gzfile.IsNestedArchive(entry, c.Data) {
		m.host.Notify(host.Warn, nestedArchiveNotice+entry)
		return nil
	}
	text, notice := m.gzipBufferText(c, entry)
	return m.showGzipBuffer(archivePath, archiveGzipInner(entry, c.Name), text, notice)
}

// archiveGzipInner puts the decompressed name back where the member lived, so
// the virtual path still reads like a member of the archive:
// logs/app.log.gz → logs/app.log, and the tab, the language lookup and the
// refresh behave like they do for any other entry.
func archiveGzipInner(entry, inner string) string {
	if i := strings.LastIndex(entry, "/"); i >= 0 {
		return entry[:i+1] + inner
	}
	return inner
}

// largeFileLimits are the configured large-file thresholds (#149). Content
// that never touches disk — an extracted archive member (#1762), a
// decompressed gzip stream (#1763) — is held to the same rule as a file, and
// for those the byte cap doubles as the memory guard.
func (m *Model) largeFileLimits() largefile.Limits {
	var get largefile.Getter
	if cfg := m.host.Config(); cfg != nil {
		get = cfg.Get
	}
	return largefile.LimitsFrom(get)
}

// largeFileLimit is the byte ceiling alone, the form most callers want.
func (m *Model) largeFileLimit() int64 { return m.largeFileLimits().MaxBytes }

// isBinary reports whether data looks like a binary blob — a NUL byte in the
// leading window, the same cheap test `grep` uses. Text editors have nothing
// useful to show for one, and the point of the archive pane is that binary
// content never reaches a text buffer.
func isBinary(data []byte) bool {
	head := data
	if len(head) > 8192 {
		head = head[:8192]
	}
	return bytes.IndexByte(head, 0) >= 0
}

// handleArchviewMsg routes the archive pane's action messages; handled
// reports whether msg was one of them.
func (m Model) handleArchviewMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case archview.OpenEntryMsg:
		return m, m.openArchiveEntry(msg.Archive, msg.Entry), true
	case archview.ExtractMsg:
		// The pane names what to extract; the target directory, the safety
		// checks and the writing all live in archextract.go (#2249).
		m.startArchiveExtract(msg)
		return m, nil, true
	}
	return m, nil, false
}

// archiveEntryTitle renders the tab/status label of a read-only archive entry
// buffer: "main.go (src.tar)". A path with no separator is not one of ours and
// falls through to its own base name.
func archiveEntryTitle(vpath string) (string, bool) {
	i := strings.LastIndex(vpath, entrySep)
	if i < 0 {
		return "", false
	}
	arch, entry := vpath[:i], vpath[i+1:]
	if entry == "" {
		return "", false
	}
	return filepath.Base(entry) + " (" + filepath.Base(arch) + ")", true
}
