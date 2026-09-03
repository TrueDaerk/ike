package app

import (
	"os"

	tea "charm.land/bubbletea/v2"

	"ike/internal/archive"
	"ike/internal/datasrc"
	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/palette"
	"ike/internal/pane"
)

// openas.go is the "Open file as…" chooser (#2420): a locked palette mode
// listing every registered viewer plus the text editor and the hex viewer, so
// any file can be forced into any of them regardless of extension or sniff —
// a .db as text, a .png as bytes. A pick a file cannot satisfy (not a valid
// database, not a recognised image) fails with a notification and leaves the
// current tab untouched.

// openAsPrefix selects the chooser inside the palette. The root model only
// ever opens it locked, so the rune merely has to be unique among the modes.
const openAsPrefix = '-'

// ShowOpenAsMsg opens the "Open file as…" chooser (file.openAs) for the
// current subject: the explorer's selection when the tree is focused, the
// focused editor's file otherwise.
type ShowOpenAsMsg struct{}

// OpenAsMsg opens the chooser's remembered path with one target handler.
type OpenAsMsg struct{ Mode string }

// openAs target modes, the chooser's rows.
const (
	openAsEditor   = "editor"
	openAsHex      = "hex"
	openAsImage    = "image"
	openAsArchive  = "archive"
	openAsData     = "data"
	openAsMarkdown = "markdown"
	openAsGzip     = "gzip"
)

// openAsMode is the palette Mode listing the open targets. The rows are the
// registered file handlers plus the two paths every file supports — text
// editor and hex viewer — so the list is the complete answer to "what can
// this IDE show a file as".
type openAsMode struct{}

// Prefix implements palette.Mode.
func (openAsMode) Prefix() rune { return openAsPrefix }

// Placeholder implements palette.Mode.
func (openAsMode) Placeholder() string { return "Open file as: viewer…" }

// Results implements palette.Mode: the fixed target rows, fuzzy-matched on
// their titles. Activation dispatches OpenAsMsg; the root model remembered
// the subject path when it opened the chooser.
func (openAsMode) Results(query string, _ palette.Context) []palette.Item {
	rows := []struct{ title, detail, mode string }{
		{"Text editor", "any file, insight off for binaries", openAsEditor},
		{"Hex", "offset | hex | ASCII bytes", openAsHex},
		{"Image", "PNG, JPEG, GIF, WebP", openAsImage},
		{"Archive", "tar, tgz, zip-like listings", openAsArchive},
		{"Data", "SQLite, DuckDB, Parquet", openAsData},
		{"Markdown preview", "rendered markdown", openAsMarkdown},
		{"Gzip", "plain .gz, decompressed text", openAsGzip},
	}
	var items []palette.Item
	for _, r := range rows {
		match, ok := fuzzy.Match(query, r.title)
		if !ok {
			continue
		}
		items = append(items, palette.Item{
			Title:  r.title,
			Detail: r.detail,
			Spans:  match.Positions,
			Score:  match.Score,
			Msg:    OpenAsMsg{Mode: r.mode},
		})
	}
	return items
}

// openFileAsPicker resolves the subject and opens the chooser locked to its
// mode. Without a subject — no file selected, a file-less buffer focused —
// the chooser refuses with the reason instead of opening uselessly.
func (m *Model) openFileAsPicker() {
	path, ok := m.refactorTarget()
	if !ok || path == "" {
		m.host.Notify(host.Info, "open file as: select a file in the explorer or focus a file's tab first")
		return
	}
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		m.host.Notify(host.Info, "open file as: "+baseName(path)+" is not an openable file")
		return
	}
	m.openAsPath = canonicalPath(path)
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, openAsPrefix)
}

// openFileAs opens the remembered subject with the picked target. Targets
// with a content contract (image, archive, data, gzip) are validated against
// the file's head first: a failed pick is a notification, never a broken
// pane, and the current tab stays untouched.
func (m Model) openFileAs(msg OpenAsMsg) (tea.Model, tea.Cmd) {
	path := m.openAsPath
	if path == "" {
		return m, nil
	}
	head := readHead(path)
	switch msg.Mode {
	case openAsEditor:
		return m.openPathMode(path, false, true)
	case openAsHex:
		return m, m.host.Dispatch(OpenHexMsg{Path: path, Forced: true})
	case openAsImage:
		if !sniffImage(head) {
			m.host.Notify(host.Warn, baseName(path)+" is not a recognised image (PNG, JPEG, GIF, WebP)")
			return m, nil
		}
		m.openImagePreview(path)
		return m, nil
	case openAsArchive:
		if !archive.IsArchive(path, head) {
			m.host.Notify(host.Warn, baseName(path)+" is not a recognised archive")
			return m, nil
		}
		m.openArchivePane(path)
		return m, nil
	case openAsData:
		if !datasrc.IsDatabase(path, head) {
			m.host.Notify(host.Warn, baseName(path)+" is not a SQLite, DuckDB or Parquet file")
			return m, nil
		}
		return m, m.openDataPane(path)
	case openAsMarkdown:
		m.openMarkdownPaneFor(path)
		return m, nil
	case openAsGzip:
		if len(head) < 2 || head[0] != 0x1f || head[1] != 0x8b {
			m.host.Notify(host.Warn, baseName(path)+" is not gzip data")
			return m, nil
		}
		return m, m.openGzipFile(path)
	}
	return m, nil
}

// openMarkdownPaneFor opens a rendered markdown preview of the file at path —
// the chooser's variant of openMarkdownPreview, which is bound to the active
// editor's buffer. The file is read once; an unreadable file refuses with a
// notification.
func (m *Model) openMarkdownPaneFor(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		m.host.Notify(host.Warn, "cannot read "+baseName(path)+": "+err.Error())
		return
	}
	if hostKey, tabIdx, _, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindMarkdown && c.Preview().Path() == path
	}); ok {
		m.focusContentAt(hostKey, tabIdx)
		return
	}
	key := m.activeWS().Panes.AddMarkdownPreview(path)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, m.viewerSplitTarget(), key, layout.ZoneRight)
	if !ok {
		m.activeWS().Panes.Close(key)
		return
	}
	m.activeWS().Tree = tree
	m.layout()
	m.activeWS().Panes.Get(key).Preview().SetSourceImmediate(string(data))
	m.setFocus(key)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}
