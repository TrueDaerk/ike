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
// Rendering runs off the update loop (0530/7, #2745): every change — source,
// width, theme, image support — only marks the pane as owing a render, and
// RenderCmd dispatches it as a tea.Cmd tagged with a generation. The result
// comes back as a RenderedMsg; one whose generation is no longer the newest
// is dropped, a newer dispatch cancels the older render through its context,
// and so do closing the pane and closing the source buffer. The render is
// bounded by preview.html_render_budget_kb (htmlrender.Options.Budget).
// While one is in flight the pane keeps drawing the previous document under
// a "rendering…" notice. The image hook runs on the render goroutine against
// a private copy of the decoded-image cache (images.go), adopted with the
// document on the loop.
//
// Browser mode (0530/8, #2746, browser.go) swaps the text rendering for a
// headless browser's screenshot of the page, shown through imgview's zoom
// and pan; b toggles it, r re-renders it.
package htmlpreview

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/config"
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

// RenderedMsg is a finished off-loop render (#2745) on its way back to the
// pane that dispatched it. Key routes it to the owning instance and Gen drops
// it when a newer render was dispatched meanwhile; Took is the render's wall
// time, which the performance HUD books against the pane.
type RenderedMsg struct {
	Key  string
	Gen  int
	Took time.Duration

	doc  htmlrender.Document
	imgs *imageRun
}

// renderGen mints render generations process-wide rather than per pane: a
// project switch rebuilds the panes under the same keys ("htmlpreview"), and
// a result of the old workspace still queued must not match a new pane's
// generation.
var renderGen atomic.Int64

// renderingNotice marks a pane whose render is in flight: the previous
// document stays on screen under it until the new one lands.
const renderingNotice = " rendering… "

// shotNotice marks a pane in browser mode whose first screenshot is being
// taken (#2746): the text rendering shows under it until the shot lands.
const shotNotice = " screenshot… "

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

	// The off-loop render (#2745): the render budget in bytes, the
	// generation of the newest dispatched render, whether a render is owed
	// (dirty) or running (inflight), and the running one's cancellation.
	budget   int
	gen      int
	dirty    bool
	inflight bool
	cancel   context.CancelFunc
	anchor   string // LandOnAnchor's target, applied when the render lands

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

	// Browser screenshot mode (#2746), see browser.go.
	br browserState
}

// New returns a preview bound to path. Content arrives via SetSourceImmediate
// (on open/restore) or SetSource (debounced live updates). Inline images
// start enabled and the render budget at its default, the shipped
// preview.html_images and preview.html_render_budget_kb.
func New(key, path string, pal *theme.Palette) Model {
	m := Model{key: key, path: path, pal: pal, imagesOn: true, budget: config.DefaultHTMLRenderBudgetKB * 1024}
	m.SetBrowser("", 0)
	return m
}

// SetRenderBudget applies preview.html_render_budget_kb: a render stops
// after kb KiB of source. A change re-renders; kb <= 0 is ignored.
func (m *Model) SetRenderBudget(kb int) {
	if kb <= 0 || kb*1024 == m.budget {
		return
	}
	m.budget = kb * 1024
	m.invalidate()
}

// RenderBudgetKB returns the render budget in KiB.
func (m Model) RenderBudgetKB() int { return m.budget / 1024 }

// Truncated reports whether the shown document hit the render budget.
func (m Model) Truncated() bool { return m.doc.Truncated }

// Pending reports whether a render is owed or in flight — the pane is
// showing an older document than its inputs describe.
func (m Model) Pending() bool { return m.dirty || m.inflight }

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
func (m *Model) SetFocused(f bool) {
	m.focused = f
	if m.br.shot != nil {
		m.br.shot.SetFocused(f)
	}
}

// SetPalette re-themes the preview and re-renders in the new colours.
func (m *Model) SetPalette(p *theme.Palette) {
	m.pal = p
	if m.br.shot != nil {
		m.br.shot.SetPalette(p)
	}
	m.invalidate()
}

// SetSize records the interior size and re-renders: the output is wrapped at
// the pane width, so a width change invalidates every rendered line.
func (m *Model) SetSize(w, h int) {
	if w == m.w && h == m.h {
		return
	}
	widthChanged := w != m.w
	m.w, m.h = w, h
	if m.br.shot != nil {
		m.br.shot.SetSize(w, h)
	}
	// An image block is fitted to the pane height too, so a height change
	// re-renders a document that draws one.
	if widthChanged || len(m.ImageIDs()) > 0 {
		m.invalidate()
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

// SetSourceImmediate stores text and owes a render right away, bypassing the
// debounce — used when the pane opens or restores, where the first paint
// should not wait. The render itself still runs off the loop: the next
// RenderCmd dispatches it.
func (m *Model) SetSourceImmediate(text string) {
	m.src = text
	m.seq++
	m.invalidate()
}

// SetCursorLine records the source cursor line and scrolls the rendered view
// to the line the source map puts it on.
func (m *Model) SetCursorLine(line int) {
	m.cursor = line
	m.follow()
}

// Update handles the debounce tick, the finished render and, when focused,
// scroll, search and link keys.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case RenderTickMsg:
		if msg.Key == m.key && msg.Seq == m.seq {
			m.invalidate()
			return m.RenderCmd()
		}
	case RenderedMsg:
		// Only the newest dispatched render lands; an older one finished
		// before its cancellation was seen and describes stale inputs.
		if msg.Key == m.key && msg.Gen == m.gen && m.inflight {
			m.apply(msg)
		}
	case ShotMsg:
		// Only the newest dispatched screenshot lands (#2746).
		if msg.Key == m.key && msg.Gen == m.br.gen && m.br.running {
			return m.applyShot(msg)
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
	// Browser mode (#2746): b toggles it, r re-renders the screenshot, and
	// while the screenshot shows every other key zooms and pans it.
	switch msg.String() {
	case "b":
		return m.ToggleBrowser()
	case "r":
		return m.Rerender()
	}
	if m.browserShown() {
		return m.br.shot.Update(msg)
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

// ScrollBy scrolls the rendered view by delta lines (mouse wheel) — in
// browser mode, pans the screenshot (#2746).
func (m *Model) ScrollBy(delta int) {
	if m.browserShown() {
		m.br.shot.Wheel(delta)
		return
	}
	m.scrollTo(m.top + delta)
}

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
	if m.browserShown() {
		return m.br.shot.View()
	}
	var b strings.Builder
	body := m.viewHeight()
	for row := 0; row < body; row++ {
		if row > 0 {
			b.WriteByte('\n')
		}
		line := ""
		if i := m.top + row; i >= 0 && i < len(m.doc.Lines) {
			line = m.highlightLink(i)
		}
		if row == 0 && (m.Pending() || m.br.running) {
			b.WriteString(m.withNotice(line))
			continue
		}
		b.WriteString(ansi.Truncate(line, m.w, "…"))
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

// withNotice right-aligns the rendering notice over line, the first visible
// row, cutting the line short to make room.
func (m Model) withNotice(line string) string {
	notice := renderingNotice
	if m.br.running {
		notice = shotNotice
	}
	nw := ansi.StringWidth(notice)
	if m.w <= nw {
		return ansi.Truncate(line, m.w, "")
	}
	line = ansi.Truncate(line, m.w-nw, "")
	pad := m.w - nw - ansi.StringWidth(line)
	st := lipgloss.NewStyle().Foreground(m.palette().Background).Background(m.palette().Hint)
	return line + "\x1b[0m" + strings.Repeat(" ", pad) + st.Render(notice)
}

// invalidate marks the pane as owing a render: its inputs — source, width,
// theme, image support, budget — changed. RenderCmd dispatches it.
func (m *Model) invalidate() { m.dirty = true }

// RenderCmd dispatches the owed render (#2745), or returns nil when none is
// owed or the pane has no width yet (it renders on its first SetSize). The
// render runs on a Cmd goroutine over a snapshot of the pane's inputs; a
// render still in flight is cancelled, and its result — should it finish
// anyway — carries an older generation and is dropped by Update.
func (m *Model) RenderCmd() tea.Cmd {
	// A browser screenshot owed since the mode came back from a layout
	// restore (#2746) rides along.
	if shot := m.shotCmd(false); shot != nil {
		return tea.Batch(m.textRenderCmd(), shot)
	}
	return m.textRenderCmd()
}

// textRenderCmd is RenderCmd's text rendering half.
func (m *Model) textRenderCmd() tea.Cmd {
	if !m.dirty || m.w <= 0 {
		return nil
	}
	m.dirty = false
	m.CancelRender()
	m.gen = int(renderGen.Add(1))
	cx, cancel := context.WithCancel(context.Background())
	m.cancel, m.inflight = cancel, true
	key, gen, src := m.key, m.gen, m.src
	run := m.newImageRun()
	opts := htmlrender.Options{Width: m.w, Palette: m.palette(), ImageBlock: run.block, Budget: m.budget}
	return func() tea.Msg {
		start := time.Now()
		doc, err := htmlrender.RenderContext(cx, []byte(src), opts)
		if err != nil {
			return nil // cancelled: nobody is waiting, so no wake either
		}
		return RenderedMsg{Key: key, Gen: gen, Took: time.Since(start), doc: doc, imgs: run}
	}
}

// CancelRender abandons the render in flight, if any, and keeps the shown
// document — the source buffer closed (#2745), or a newer render replaces it.
func (m *Model) CancelRender() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.inflight = false
}

// Interrupt cancels the render in flight but keeps owing it, so the next
// RenderCmd dispatches it again: the pane's workspace parks, and a result
// arriving while nothing routes to it would be lost.
func (m *Model) Interrupt() {
	if m.inflight {
		m.CancelRender()
		m.dirty = true
	}
	// So does a browser screenshot in flight (#2746).
	if m.br.running {
		m.cancelShot()
		m.br.owed = true
	}
}

// Close releases the pane's background work: a closed pane cancels its
// render instead of letting it finish for nobody.
func (m *Model) Close() {
	m.CancelRender()
	m.dirty = false
	m.closeBrowser()
}

// Flush finishes an owed or in-flight render synchronously on the caller's
// goroutine, superseding a dispatched one (whose result then arrives stale).
// For tests and headless callers that need the document now.
func (m *Model) Flush() {
	if !m.Pending() {
		return
	}
	m.dirty = true
	if cmd := m.RenderCmd(); cmd != nil {
		if msg := cmd(); msg != nil {
			m.Update(msg)
		}
	}
}

// apply adopts a finished render: the document, the images it placed, and
// everything derived from the document — the link selection, the search
// matches and the follow scroll.
func (m *Model) apply(msg RenderedMsg) {
	m.cancel, m.inflight = nil, false
	m.doc = msg.doc
	m.adoptImages(msg.imgs)
	if m.sel > len(m.doc.Links) {
		m.sel = len(m.doc.Links) // an edit dropped links: keep the last
	}
	if m.search != nil {
		m.recomputeMatches()
	}
	m.follow()
	if m.anchor != "" {
		// A followed link's fragment wins over the caret; it waits out
		// renders that are already superseded by another owed one.
		m.ScrollToAnchor(m.anchor)
		if !m.dirty {
			m.anchor = ""
		}
	}
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
