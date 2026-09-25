package app

// htmlpreviewlinks.go follows the links of an HTML preview (0530/3, #2741)
// and runs its reverse cursor sync. The pane owns selection, the click
// hit-test and the anchor index and emits an htmlpreview.LinkMsg or
// SourceLineMsg; the policy lives here, beside the markdown preview's
// (previewlinks.go), whose rules it keeps.

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/htmlpreview"
	"ike/internal/preview"
)

// followHTMLPreviewLink acts on the link the user activated in an HTML
// preview:
//
//   - Copy puts the raw destination on the clipboard.
//   - "#anchor" scrolls the preview to the element with that id or name.
//   - An absolute URL (http, https, mailto, any scheme but file:) goes to the
//     platform opener of open-in-browser — only on this explicit action.
//   - Anything else is a local path resolved against the previewed document
//     and opened in IKE: an HTML page opens with its own preview beside it
//     (landing on the fragment's anchor), a markdown file at the fragment's
//     heading, any other kind through the ordinary open funnel. A target that
//     does not exist is a toast.
func (m Model) followHTMLPreviewLink(msg htmlpreview.LinkMsg) (tea.Model, tea.Cmd) {
	target := strings.TrimSpace(msg.Target)
	if target == "" {
		return m, nil
	}
	if msg.Copy {
		m.copyToClipboard(target)
		m.host.Notify(host.Info, "copied "+target)
		return m, nil
	}
	if strings.HasPrefix(target, "#") {
		if inst := m.htmlPreviewByKey(msg.Key); inst != nil && inst.HTMLPreview().ScrollToAnchor(target[1:]) {
			return m, nil
		}
		m.host.Notify(host.Info, "no anchor for "+target)
		return m, nil
	}
	path, frag, ok := htmlLinkPath(target)
	if !ok {
		if err := browserOpen(target); err != nil {
			m.host.Notify(host.Error, "open link failed: "+err.Error())
			return m, nil
		}
		m.host.Notify(host.Info, "opened "+target)
		return m, nil
	}
	if path == "" { // "?query" or a bare separator: nothing to open
		return m, nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(htmlPreviewBase(msg.Path)), path)
	}
	path = filepath.Clean(path)
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		m.host.Notify(host.Info, "link target not found: "+target)
		return m, nil
	}
	switch {
	case htmlpreview.IsHTMLPath(path):
		return m.openHTMLWithPreview(path, frag)
	case frag != "" && isMarkdownPath(path):
		if line, ok := preview.HeadingLine(readFileOrEmpty(path), preview.Slug(frag)); ok {
			return m.openPathAt(path, line, 0)
		}
	}
	return m.openPath(path, false)
}

// htmlLinkPath splits a local link destination into its path — percent-
// decoded, query dropped, a file: scheme stripped — and its fragment. ok is
// false for a remote destination (any other scheme).
func htmlLinkPath(target string) (path, frag string, ok bool) {
	if preview.Remote(target) {
		u, err := url.Parse(target)
		if err != nil || !strings.EqualFold(u.Scheme, "file") {
			return "", "", false
		}
		return u.Path, u.Fragment, true
	}
	path, frag = splitFragment(target)
	path, _, _ = strings.Cut(path, "?")
	if dec, err := url.PathUnescape(path); err == nil {
		path = dec
	}
	return path, frag, true
}

// htmlPreviewBase is the file a preview's relative links resolve against: the
// archive itself for a gz viewer buffer ("page.html.gz!page.html"), whose
// directory is where the page's neighbours live.
func htmlPreviewBase(path string) string {
	if i := strings.LastIndex(path, entrySep); i > 0 {
		return path[:i]
	}
	return path
}

// openHTMLWithPreview opens an HTML page followed from a preview in an editor
// and gives it a preview of its own — the source pane had one, so the page
// reads the way the one it was reached from does — then lands that preview on
// the fragment's anchor.
func (m Model) openHTMLWithPreview(path, frag string) (tea.Model, tea.Cmd) {
	model, cmd := m.openPath(path, false)
	mm, ok := model.(Model)
	if !ok || mm.editorForPath(canonicalPath(path)) == nil {
		return model, cmd
	}
	mm.openHTMLPreview()
	if frag != "" {
		for _, inst := range mm.htmlPreviewsForPath(canonicalPath(path)) {
			inst.HTMLPreview().LandOnAnchor(frag)
		}
	}
	return mm, cmd
}

// syncEditorToHTMLPreview is the reverse cursor sync: the editor showing the
// previewed buffer takes the focus with its caret on the source line of the
// rendered line the preview asked about. With the source closed it says so.
func (m Model) syncEditorToHTMLPreview(msg htmlpreview.SourceLineMsg) (tea.Model, tea.Cmd) {
	key := m.editorWithFile(msg.Path)
	if key == "" {
		m.host.Notify(host.Info, "source not open: "+filepath.Base(msg.Path))
		return m, nil
	}
	m.setFocus(key)
	if ed := m.activeWS().Panes.Get(key).Editor(); ed != nil {
		ed.JumpTo(msg.Line, 0)
	}
	return m, nil
}
