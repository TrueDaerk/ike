// Package htmlpreview is the HTML preview pane (Epic 0530, #2740): a live,
// read-only reading view of an .html/.htm/.xhtml buffer — or the decompressed
// buffer of an .html.gz — beside its editor. It is the markdown preview's
// sibling (internal/preview) and keeps its seams: the root model pushes the
// buffer text in on every editor change (debounced by the same interval) and
// the caret line for scroll sync; the pane re-renders on resize and theme
// switch; "/" searches the rendered lines through ui.LineSearch.
//
// Rendering is the UI-free core internal/htmlrender (#2739). What differs
// from the markdown preview is the scroll sync: the render core hands back a
// source map, so the caret's source line maps to its rendered line exactly
// instead of being interpolated between heading anchors.
//
// Local <img> sources show inline as Kitty graphics (images.go, #2743) —
// the markdown preview's reconcile and lifecycle, fed through the render
// core's Options.ImageBlock hook.
//
// The render runs synchronously on the update loop for now, behind the one
// function Render; the bounded, off-loop render of 0530/7 (#2745) replaces
// that function and nothing else (its image hook then runs off-loop too, so
// the image cache must be guarded or pre-decoded there).
package htmlpreview

import (
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/htmlrender"
	"ike/internal/imgview"
	"ike/internal/preview"
	"ike/internal/theme"
	"ike/internal/ui"
)

// Debounce is how long after the last buffer change the preview re-renders —
// the markdown preview's interval, so the two previews trail typing alike.
const Debounce = preview.Debounce

// RenderTickMsg is the debounce timer firing for one HTML preview pane. Key
// routes it to the owning instance; Seq drops stale ticks, so a typing burst
// renders once — the preview.RenderTickMsg contract, as its own type so the
// root model routes it to this pane kind.
type RenderTickMsg struct {
	Key string
	Seq int
}

// IsHTMLPath reports whether path names an HTML document the preview renders:
// .html, .htm and .xhtml, compressed (.html.gz) or not. The gz viewer's
// buffer path names the member inside the archive ("page.html.gz!page.html"),
// whose extension is the inner one, so both spellings of a compressed page
// qualify.
func IsHTMLPath(path string) bool {
	lower := strings.ToLower(path)
	for _, gz := range []string{".gz", ".gzip"} {
		lower = strings.TrimSuffix(lower, gz)
	}
	switch filepath.Ext(lower) {
	case ".html", ".htm", ".xhtml":
		return true
	}
	return false
}

// Render lays the document out with opts (the pane's interior width, its
// palette and its image hook). It is the single render seam of the pane:
// 0530/7 moves it off the update loop behind a generation counter without
// touching the callers.
func Render(src string, opts htmlrender.Options) htmlrender.Document {
	return htmlrender.Render([]byte(src), opts)
}

// Model is one live HTML preview bound to a source buffer path. It is a value
// type with pointer-receiver mutators, mirroring the other pane components,
// and is embedded in a pane.Instance.
type Model struct {
	key  string // owning pane key, for routing debounce ticks
	path string // source buffer path the preview is bound to
	pal  *theme.Palette

	w, h    int
	focused bool

	src    string // latest source text (pending or rendered)
	seq    int    // debounce sequence; a tick renders only when it matches
	doc    htmlrender.Document
	cursor int // last known source cursor line (0-based), for follow scroll
	top    int // first rendered line shown

	// Link following (#2741): the selected link of doc.Links plus one, 0
	// while nothing is selected (so the zero value selects nothing).
	sel int

	// In-pane search: the prompt on the last row and the matching rendered
	// lines. It lives behind a pointer so the value-receiver View copies
	// share it, like the markdown preview's; nil means no search is open.
	search *ui.LineSearch

	// Inline images (#2743): decoded local images by resolved path, the
	// ones the latest render found (in reading order, each once), the
	// terminal's Kitty graphics support and preview.html_images.
	images   map[string]*imgview.PlacedImage
	placed   []*imgview.PlacedImage
	gfx      bool
	imagesOn bool
}

// New returns a preview bound to path. Content arrives via SetSourceImmediate
// (on open/restore) or SetSource (debounced live updates). Inline images
// start enabled, the preview.html_images default.
func New(key, path string, pal *theme.Palette) Model {
	return Model{key: key, path: path, pal: pal, imagesOn: true}
}

// Key returns the owning pane key.
func (m Model) Key() string { return m.key }

// Path returns the source buffer path the preview is bound to.
func (m Model) Path() string { return m.path }

// Title returns the document's <title>, "" when it has none.
func (m Model) Title() string { return m.doc.Title }

// Lines returns the rendered lines (tests, status).
func (m Model) Lines() []string { return m.doc.Lines }

// Top returns the first rendered line shown.
func (m Model) Top() int { return m.top }

// SetFocused marks the preview focused; a focused preview consumes scroll keys.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-themes the preview and re-renders in the new colours.
func (m *Model) SetPalette(p *theme.Palette) {
	m.pal = p
	m.render()
}

// SetSize records the interior size and re-renders: the output is wrapped at
// the pane width, so a width change invalidates every rendered line.
func (m *Model) SetSize(w, h int) {
	if w == m.w && h == m.h {
		return
	}
	widthChanged := w != m.w
	m.w, m.h = w, h
	// An image block is fitted to the pane height too, so a height change
	// re-renders a document that draws one.
	if widthChanged || len(m.ImageIDs()) > 0 {
		m.render()
		return
	}
	m.follow()
}

// SetSource stores text as the newest pending source and arms the debounce
// timer, returning the tick command. Earlier pending ticks are orphaned by the
// bumped sequence and render nothing.
func (m *Model) SetSource(text string) tea.Cmd {
	m.src = text
	m.seq++
	key, seq := m.key, m.seq
	return tea.Tick(Debounce, func(time.Time) tea.Msg {
		return RenderTickMsg{Key: key, Seq: seq}
	})
}

// SetSourceImmediate stores text and renders synchronously, bypassing the
// debounce — used when the pane opens or restores, where the first paint
// should not wait.
func (m *Model) SetSourceImmediate(text string) {
	m.src = text
	m.seq++
	m.render()
}

// SetCursorLine records the source cursor line and scrolls the rendered view
// to the line the source map puts it on.
func (m *Model) SetCursorLine(line int) {
	m.cursor = line
	m.follow()
}

// Update handles the debounce tick and, when focused, scroll, search and link
// keys.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case RenderTickMsg:
		if msg.Key == m.key && msg.Seq == m.seq {
			m.render()
		}
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return nil
}

// handleKey scrolls the rendered document and drives the links. The preview
// is read-only, so the vim motions map straight to view movement, as in the
// markdown preview; tab/shift+tab walk the links, enter follows the selected
// one (or, none selected, syncs the editor caret back) and y copies it.
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	// The open search prompt owns the keyboard: every key is query text
	// until enter applies it or esc abandons the search.
	if m.search != nil && m.search.Open {
		return m.searchKey(msg)
	}
	switch msg.String() {
	case "/", "ctrl+f", "cmd+f", "super+f":
		// The shared search key and the find chord open the same prompt,
		// exactly as in the markdown preview (#2409).
		m.openSearch()
	case "n":
		if m.search != nil {
			m.stepMatch(1)
		}
	case "N":
		if m.search != nil {
			m.stepMatch(-1)
		}
	case "esc":
		if m.search == nil {
			m.clearSelection()
		}
		m.closeSearch()
	case "tab":
		m.selectLink(1)
	case "shift+tab":
		m.selectLink(-1)
	case "enter":
		return m.enter()
	case "y":
		return m.copySelected()
	case "up", "k":
		m.scrollTo(m.top - 1)
	case "down", "j":
		m.scrollTo(m.top + 1)
	case "pgup", "ctrl+u":
		m.scrollTo(m.top - m.pageStep())
	case "pgdown", "ctrl+d":
		m.scrollTo(m.top + m.pageStep())
	case "home", "g":
		m.scrollTo(0)
	case "end", "G":
		m.scrollTo(len(m.doc.Lines))
	}
	return nil
}

// pageStep is one page-scroll increment: just under a viewport of lines.
func (m Model) pageStep() int { return max(1, m.h-1) }

// ScrollBy scrolls the rendered view by delta lines (mouse wheel).
func (m *Model) ScrollBy(delta int) { m.scrollTo(m.top + delta) }

// scrollTo clamps and applies a new top line.
func (m *Model) scrollTo(top int) {
	m.top = min(max(top, 0), m.maxTop())
}

// maxTop is the largest top offset that still fills the viewport when the
// document is long enough, and 0 otherwise.
func (m Model) maxTop() int { return max(0, len(m.doc.Lines)-m.viewHeight()) }

// View renders the visible window of the rendered document, hard-clamped to
// the pane interior.
func (m Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	var b strings.Builder
	body := m.viewHeight()
	for row := 0; row < body; row++ {
		if row > 0 {
			b.WriteByte('\n')
		}
		if i := m.top + row; i >= 0 && i < len(m.doc.Lines) {
			b.WriteString(ansi.Truncate(m.highlightLink(i), m.w, "…"))
		}
	}
	if body < m.h {
		b.WriteByte('\n')
		b.WriteString(ansi.Truncate(m.searchLine(), m.w, "…"))
	}
	return b.String()
}

// viewHeight is the room the rendered document gets: the whole pane, minus
// the search prompt row while a search is open.
func (m Model) viewHeight() int {
	if m.search == nil || m.h <= 1 {
		return m.h
	}
	return m.h - 1
}

// render lays the pending source out at the current width and theme and
// re-applies the follow scroll. A pane that has not been sized yet renders on
// its first SetSize instead.
func (m *Model) render() {
	if m.w <= 0 {
		return
	}
	m.beginImages()
	m.doc = Render(m.src, htmlrender.Options{Width: m.w, Palette: m.palette(), ImageBlock: m.imageBlock})
	m.forgetUnplaced()
	if m.sel > len(m.doc.Links) {
		m.sel = len(m.doc.Links) // an edit dropped links: keep the last
	}
	if m.search != nil {
		m.recomputeMatches()
	}
	m.follow()
}

// follow scrolls the rendered view to the caret's source line through the
// render core's source map — line-accurate, unlike the markdown preview's
// heading interpolation — aiming it a third down the viewport.
func (m *Model) follow() {
	line, ok := m.doc.LineForSourceLine(m.cursor)
	if !ok {
		m.scrollTo(0)
		return
	}
	m.scrollTo(line - m.h/3)
}

// palette returns the palette the preview styles against, falling back to the
// default when the pane was built without one (zero-value models, tests).
func (m Model) palette() *theme.Palette {
	if m.pal == nil {
		return theme.DefaultPalette()
	}
	return m.pal
}
