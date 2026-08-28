package app

// previewlinks.go follows the links of a markdown preview (#2180). The
// preview pane owns selection and emits a preview.LinkMsg; the policy —
// what a destination means and where it opens — lives here, next to the open
// funnel, the platform opener and the toasts.

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/preview"
)

// followPreviewLink acts on the link the user activated in a preview:
//
//   - Copy puts the raw destination on the clipboard, whatever it points at.
//   - "#anchor" scrolls the preview itself to that heading.
//   - An absolute URL goes to the platform opener — never automatically, only
//     on this explicit key.
//   - Anything else is a file path resolved against the previewed document and
//     opened through the ordinary open funnel, at the anchor's heading line
//     when the target is markdown. A target that does not exist is a toast,
//     not a silent no-op.
func (m Model) followPreviewLink(msg preview.LinkMsg) (tea.Model, tea.Cmd) {
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
		if pv := m.previewByKey(msg.Key); pv != nil && pv.ScrollToAnchor(preview.Slug(target[1:])) {
			return m, nil
		}
		m.host.Notify(host.Info, "no heading for "+target)
		return m, nil
	}
	if preview.Remote(target) {
		if err := browserOpen(target); err != nil {
			m.host.Notify(host.Error, "open link failed: "+err.Error())
			return m, nil
		}
		m.host.Notify(host.Info, "opened "+target)
		return m, nil
	}
	path, frag := splitFragment(target)
	if path == "" { // "#" alone, or a bare fragment separator
		return m, nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(msg.Path), path)
	}
	path = filepath.Clean(path)
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		m.host.Notify(host.Info, "link target not found: "+target)
		return m, nil
	}
	// A "file.md#section" link lands on the heading, so following it inside
	// the docs behaves like following it in a browser; the editor's cursor
	// move re-syncs any preview of that file in turn.
	if frag != "" && isMarkdownPath(path) {
		if line, ok := preview.HeadingLine(readFileOrEmpty(path), preview.Slug(frag)); ok {
			return m.openPathAt(path, line, 0)
		}
	}
	return m.openPath(path, false)
}

// splitFragment splits a markdown destination into its path and its "#anchor"
// fragment.
func splitFragment(target string) (path, frag string) {
	if i := strings.IndexByte(target, '#'); i >= 0 {
		return target[:i], target[i+1:]
	}
	return target, ""
}

// previewByKey returns the preview model of the pane that emitted a LinkMsg —
// a dedicated pane or a content tab (#1778) — or nil when it has since closed.
func (m Model) previewByKey(key string) *preview.Model {
	if _, _, inst, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindMarkdown && c.Preview().Key() == key
	}); ok {
		return inst.Preview()
	}
	return nil
}
