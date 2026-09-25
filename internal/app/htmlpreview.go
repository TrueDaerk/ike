package app

// htmlpreview.go wires the HTML preview pane (Epic 0530, #2740) into the root
// model: html.preview opens it beside the active HTML buffer, the editor
// change and caret seams keep it rendered and scrolled, and layout restore
// rebuilds it from disk. Everything follows the markdown preview (#62) — the
// same open semantics, the same debounce, the same restore — with the
// source-mapped cursor sync of internal/htmlpreview in place of the heading
// anchors, and with .html.gz pages read through the gz viewer's decompression.

import (
	"os"
	"strings"

	"ike/internal/gzfile"
	"ike/internal/host"
	"ike/internal/htmlpreview"
	"ike/internal/layout"
	"ike/internal/menu"
	"ike/internal/pane"
)

// htmlPreviewNeedsHTML is the toast of html.preview on anything but an HTML
// buffer.
const htmlPreviewNeedsHTML = "HTML preview needs an open .html file"

// openHTMLPreview opens a rendered preview pane for the active editor's HTML
// buffer, split to its right — openMarkdownPreview's rules exactly: the
// editor keeps focus, a preview already bound to the buffer is focused
// instead of duplicated, and a non-HTML buffer is a no-op with a toast. The
// gz viewer's buffer of a compressed page ("page.html.gz!page.html") is an
// HTML buffer like any other: its text is the decompressed page.
func (m *Model) openHTMLPreview() {
	target := m.activeEditorKey()
	if target == "" || m.activeWS().Tree == nil {
		m.host.Notify(host.Info, htmlPreviewNeedsHTML)
		return
	}
	ed := m.activeWS().Panes.Get(target).Editor()
	if ed == nil || !ed.HasFile() || !htmlpreview.IsHTMLPath(ed.Path()) {
		m.host.Notify(host.Info, htmlPreviewNeedsHTML)
		return
	}
	path := ed.Path()
	if hostKey, tabIdx, _, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindHTMLPreview && c.HTMLPreview().Path() == path
	}); ok {
		m.focusContentAt(hostKey, tabIdx) // may live in a tab (#1778)
		return
	}
	key := m.activeWS().Panes.AddHTMLPreview(path)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, layout.ZoneRight)
	if !ok {
		m.activeWS().Panes.Close(key)
		return
	}
	m.activeWS().Tree = tree
	m.layout()
	pv := m.activeWS().Panes.Get(key).HTMLPreview()
	pv.SetSourceImmediate(ed.Text())
	line, _ := ed.CursorPos()
	pv.SetCursorLine(line)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// htmlPreviewsForPath returns every HTML preview instance bound to path —
// dedicated panes and content tabs (#1778) alike.
func (m Model) htmlPreviewsForPath(path string) []*pane.Instance {
	var out []*pane.Instance
	m.contentInstances(func(_ string, _ int, c *pane.Instance) bool {
		if c.Kind() == pane.KindHTMLPreview && c.HTMLPreview().Path() == path {
			out = append(out, c)
		}
		return true
	})
	return out
}

// htmlPreviewByKey finds the HTML preview whose model answers to key, the
// debounce tick's route — matched by the model's own key, so a pane re-keyed
// by a tab move still receives its ticks.
func (m Model) htmlPreviewByKey(key string) *pane.Instance {
	_, _, inst, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindHTMLPreview && c.HTMLPreview().Key() == key
	})
	if !ok {
		return nil
	}
	return inst
}

// htmlPreviewSource reads the source a restored HTML preview renders. A gz
// viewer buffer path ("<file>.gz!<inner>") and a bare compressed page are
// decompressed through the gz viewer's own reader and cap; anything else is
// read from disk. ok=false (a vanished file) restores the pane empty rather
// than breaking the layout.
func (m *Model) htmlPreviewSource(path string) (string, bool) {
	gz := path
	if i := strings.LastIndex(path, entrySep); i > 0 {
		gz = path[:i]
	}
	if gzfile.IsPlain(gz, readHead(gz)) {
		c, err := gzfile.Read(gz, m.largeFileLimit())
		if err != nil {
			return "", false
		}
		text, _ := m.gzipBufferText(c, gz)
		return text, true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// restoreHTMLPreview fills a restored HTML preview from its source file.
func (m *Model) restoreHTMLPreview(inst *pane.Instance) {
	if text, ok := m.htmlPreviewSource(inst.HTMLPreview().Path()); ok {
		inst.HTMLPreview().SetSourceImmediate(text)
	}
}

// withHTMLPreviewItem appends the "HTML Preview" entry to the tab context
// menu when the clicked tab holds an HTML buffer — the menu doorway to
// html.preview next to the palette and the chord.
func withHTMLPreviewItem(items []menu.Item, path string) []menu.Item {
	if !htmlpreview.IsHTMLPath(path) {
		return items
	}
	return append(items, menu.Item{Title: "HTML Preview", Command: "html.preview"})
}
