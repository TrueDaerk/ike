package app

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/archive"
	"ike/internal/archview"
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

// openArchivePane opens (or refocuses) the archive viewer for path, split off
// the focused leaf like the image preview.
func (m *Model) openArchivePane(path string) {
	for _, key := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindArchive && inst.Archive().Path() == path {
			m.setFocus(key)
			return
		}
	}
	key := m.activeWS().Panes.AddArchiveView(path)
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
	if open, ok := msg.(archview.OpenEntryMsg); ok {
		return m, m.openArchiveEntry(open.Archive, open.Entry), true
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
