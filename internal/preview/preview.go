// Package preview renders a markdown buffer to styled terminal output as a
// live side-by-side pane (#62). The pane is bound to one source file path; the
// root model pushes buffer text into it on every editor change (debounced) and
// the current cursor line for scroll sync. Rendering goes through glamour with
// the style picked off the active palette's dark flag, so the preview follows
// the IDE theme. The rendered document is not inert (#2180): links are
// selectable and followable (links.go), and local images render as pixels
// through the Kitty graphics path (images.go). Nothing here touches the
// network — a remote link or image stays text until the user acts on it.
package preview

import (
	"fmt"
	"image/color"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	gansi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
	"ike/internal/ui"
)

// Debounce is how long after the last buffer change the preview re-renders.
// Rendering a document per keystroke would be wasted work; 200ms trails the
// typing burst closely enough to feel live.
const Debounce = 200 * time.Millisecond

// RenderTickMsg is the debounce timer firing for one preview pane. Key routes
// it to the owning instance; Seq drops stale ticks — only the tick armed by
// the newest SetSource call renders, so a typing burst renders once.
type RenderTickMsg struct {
	Key string
	Seq int
}

// CursorMsg carries the source editor's cursor line to every preview bound to
// Path, keeping the rendered view scrolled to what is being edited.
type CursorMsg struct {
	Path string
	Line int
}

// LinkMsg is the user acting on the selected link (#2180): enter follows it,
// the copy key puts the raw destination on the clipboard. The preview knows
// where the link points but nothing about opening files, browsers or toasts,
// so the root model owns the policy — resolve relative to Path, open through
// the ordinary open funnel, scroll an in-document anchor back through Key.
type LinkMsg struct {
	Key    string // preview pane key, to route an anchor scroll back
	Path   string // the previewed file, the resolution base for relative targets
	Target string // raw markdown destination, exactly as written
	Copy   bool   // copy the destination instead of following it
}

// heading is one scroll-sync anchor: a markdown heading's line in the source
// buffer and the line its rendering starts on in the output.
type heading struct {
	src      int // 0-based source line of the heading
	rendered int // 0-based first line of its rendering, -1 when not located
}

// Model is one live markdown preview bound to a source buffer path. It is a
// value type with pointer-receiver mutators, mirroring the other pane
// components, and is embedded in a pane.Instance.
type Model struct {
	key  string // owning pane key, for routing debounce ticks
	path string // source buffer path the preview is bound to
	pal  *theme.Palette

	w, h    int
	focused bool

	src     string // latest source text (pending or rendered)
	seq     int    // debounce sequence; a tick renders only when it matches
	lines   []string
	anchors []heading
	cursor  int // last known source cursor line (0-based), for follow scroll
	top     int // first rendered line shown

	// Link following (#2180): the rendered document's hyperlinks in reading
	// order and the selected one, -1 while nothing is selected.
	links []link
	sel   int

	// Inline images (#2180): decoded local images keyed by resolved path —
	// a map so value copies of the model share the pixels and the terminal's
	// placement state — plus the ones the latest render actually placed, and
	// the terminal's Kitty graphics capability as pushed by the app.
	images map[string]*inlineImage
	placed []*inlineImage
	gfx    bool

	// Diagram fences (#2421): the content-addressed cache of external
	// renderings, the mode override (empty follows preview.diagrams), the
	// async injector a finished render reports through, and whether this pane
	// already reported a missing renderer. The cache is a map so value copies
	// of the model share the renderings, like the image cache above.
	diag     map[string]*diagramState
	diagMode string
	send     func(tea.Msg)
	hinted   bool

	// In-pane search (#2409): the prompt on the last row and the matching
	// rendered lines. It lives behind a pointer so the value-receiver View
	// copies share it, like the explorer's speed search; nil means no
	// search is open.
	search *previewSearch
}

// New returns a preview bound to path. Content arrives via SetSourceImmediate
// (on open/restore) or SetSource (debounced live updates).
func New(key, path string, pal *theme.Palette) Model {
	return Model{key: key, path: path, pal: pal, sel: -1, images: map[string]*inlineImage{}}
}

// Key returns the owning pane key.
func (m Model) Key() string { return m.key }

// Path returns the source buffer path the preview is bound to.
func (m Model) Path() string { return m.path }

// SetFocused marks the preview focused; a focused preview consumes scroll keys.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-themes the preview and re-renders in the new style.
func (m *Model) SetPalette(p *theme.Palette) {
	m.pal = p
	m.render()
}

// SetSize records the interior size and re-renders: glamour output is
// width-wrapped, so a resize invalidates every rendered line.
func (m *Model) SetSize(w, h int) {
	if w == m.w && h == m.h {
		return
	}
	m.w, m.h = w, h
	m.render()
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
// to its mapped position.
func (m *Model) SetCursorLine(line int) {
	m.cursor = line
	m.follow()
}

// Update handles the debounce tick and, when focused, scroll and link keys.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case RenderTickMsg:
		if msg.Key == m.key && msg.Seq == m.seq {
			m.render()
		}
	case DiagramMsg:
		// A fenced diagram finished rendering (#2421): cache it and re-render
		// so the code block is replaced by the diagram. The missing-tool
		// report carries no result — the root model owns that notification.
		if msg.Key == m.key && !msg.Missing {
			m.applyDiagram(msg)
		}
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return nil
}

// handleKey scrolls the rendered document and drives link selection. The
// preview is read-only, so the vim motions map straight to view movement;
// tab/shift+tab walk the links, enter follows the selected one and y copies
// its destination (#2180), both as a LinkMsg the root model acts on.
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	// The open search prompt owns the keyboard (#2409): every key is query
	// text until enter applies it or esc abandons the search.
	if m.search != nil && m.search.input {
		return m.searchKey(msg)
	}
	switch msg.String() {
	case "/", "ctrl+f", "cmd+f", "super+f":
		// The shared search key and the find chord open the same prompt.
		// ctrl+f is deliberately unbound in the keymap table (#2409) so
		// vim's page-forward survives in the editor; the panes that have a
		// search answer the chord themselves.
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
		m.closeSearch()
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
		m.scrollTo(len(m.lines))
	case "tab":
		m.selectLink(1)
	case "shift+tab":
		m.selectLink(-1)
	case "enter":
		return m.linkCmd(false)
	case "y":
		return m.linkCmd(true)
	}
	return nil
}

// selectLink moves the selection delta links along, wrapping, and scrolls the
// chosen link into view. With no link in the document it is a no-op.
func (m *Model) selectLink(delta int) {
	if len(m.links) == 0 {
		m.sel = -1
		return
	}
	switch {
	case m.sel < 0 && delta > 0:
		m.sel = 0
	case m.sel < 0:
		m.sel = len(m.links) - 1
	default:
		m.sel = ((m.sel+delta)%len(m.links) + len(m.links)) % len(m.links)
	}
	m.reveal(m.links[m.sel].row)
}

// HasLinks reports whether the rendered document holds a followable link —
// the condition under which the pane claims the tab key (#2180).
func (m Model) HasLinks() bool { return len(m.links) > 0 }

// SelectedTarget returns the destination of the selected link, if any. It is
// the seam the root model's tests and the status line read.
func (m Model) SelectedTarget() (string, bool) {
	if m.sel < 0 || m.sel >= len(m.links) {
		return "", false
	}
	return m.links[m.sel].target, true
}

// linkCmd turns the selected link into the LinkMsg the root model follows or
// copies. Nothing selected yields no command — the key is inert rather than
// guessing at a link.
func (m Model) linkCmd(copy bool) tea.Cmd {
	target, ok := m.SelectedTarget()
	if !ok {
		return nil
	}
	msg := LinkMsg{Key: m.key, Path: m.path, Target: target, Copy: copy}
	return func() tea.Msg { return msg }
}

// ScrollToAnchor scrolls to the heading whose slug is slug and reports
// whether one exists — how an in-document "#anchor" link lands (#2180).
func (m *Model) ScrollToAnchor(slug string) bool {
	line, ok := HeadingLine(m.src, slug)
	if !ok {
		return false
	}
	for _, a := range m.anchors {
		if a.src == line && a.rendered >= 0 {
			m.scrollTo(a.rendered)
			return true
		}
	}
	// The heading exists but its rendering could not be located; fall back on
	// the proportional mapping rather than refusing the jump.
	m.scrollTo(m.mapLine(line))
	return true
}

// reveal scrolls the minimum amount that brings rendered line row into the
// viewport, keeping a line of context on the side it entered from.
func (m *Model) reveal(row int) {
	switch {
	case row < m.top:
		m.scrollTo(row - 1)
	case row >= m.top+m.h:
		m.scrollTo(row - m.h + 2)
	}
}

// pageStep is one page-scroll increment: just under a viewport of lines.
func (m Model) pageStep() int { return max(1, m.h-1) }

// ScrollBy scrolls the rendered view by delta lines (mouse wheel).
func (m *Model) ScrollBy(delta int) { m.scrollTo(m.top + delta) }

// scrollTo clamps and applies a new top line.
func (m *Model) scrollTo(top int) {
	m.top = clamp(top, 0, m.maxTop())
}

// maxTop is the largest top offset that still fills the viewport when the
// document is long enough, and 0 otherwise.
func (m Model) maxTop() int { return max(0, len(m.lines)-m.viewHeight()) }

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
		if i := m.top + row; i >= 0 && i < len(m.lines) {
			b.WriteString(ansi.Truncate(m.highlightLinks(i), m.w, "…"))
		}
	}
	if body < m.h {
		b.WriteByte('\n')
		b.WriteString(ansi.Truncate(m.searchLine(), m.w, "…"))
	}
	return b.String()
}

// viewHeight is the room the rendered document gets: the whole pane, minus
// the search prompt row while a search is open (#2409).
func (m Model) viewHeight() int {
	if m.search == nil || m.h <= 1 {
		return m.h
	}
	return m.h - 1
}

// highlightLinks returns rendered line i with the selected link's label in
// reverse video (#2180). The label's byte span is known exactly from the OSC 8
// scan, so the marker is spliced into the raw line and every surrounding
// colour, hyperlink and reset survives untouched.
func (m Model) highlightLinks(i int) string {
	if m.sel < 0 || m.sel >= len(m.links) {
		return m.lines[i]
	}
	l := m.links[m.sel]
	if l.row != i || l.end > len(m.lines[i]) {
		return m.lines[i]
	}
	line := m.lines[i]
	return line[:l.start] + "\x1b[7m" + line[l.start:l.end] + "\x1b[27m" + line[l.end:]
}

// render runs glamour over the pending source at the current width and theme,
// substitutes the inline image blocks, re-indexes the links, rebuilds the
// scroll-sync anchors, and re-applies the follow scroll. Image blocks go in
// *before* the anchors are built, so a heading's rendered line is the line it
// really occupies and scroll sync stays correct around images (#2180).
func (m *Model) render() {
	if m.w <= 0 {
		return
	}
	// Diagram fences are resolved against the source before glamour sees it
	// (#2421): a fence with a finished rendering becomes a sentinel the
	// substitution below replaces, an unrendered one keeps its code block.
	src, subs := m.prepareDiagrams(m.src)
	out, err := m.renderMarkdown(src)
	if err != nil {
		out = "preview error: " + err.Error()
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	lines, blocks := m.placeImages(lines, subs)
	m.lines = lines
	m.anchors = anchorHeadings(m.src, m.lines)
	m.links = m.indexLinks(blocks)
	if m.sel >= len(m.links) {
		m.sel = len(m.links) - 1
	}
	m.follow()
}

// renderMarkdown renders the source through a fresh width- and theme-bound
// renderer. Glamour renderers are cheap to build relative to a render, and a
// fresh one per render keeps width/theme changes trivially correct.
func (m Model) renderMarkdown(src string) (string, error) {
	return Render(src, m.w, m.palette())
}

// Render renders markdown source for a pane of the given interior width in
// the palette's style — the pane-free half of renderMarkdown, so anything
// else showing markdown reads exactly like the preview does. The notebook
// viewer renders its markdown cells through it (#2425). Diagram fences
// (#2421) are the preview's own step and stay above this line.
func Render(src string, width int, pal *theme.Palette) (string, error) {
	wrap := max(10, width-2)
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(styleConfigFor(pal)),
		glamour.WithWordWrap(wrap),
	)
	if err != nil {
		return "", err
	}
	out, err := r.Render(src)
	if err != nil {
		return "", err
	}
	// Glamour cannot hang-indent wrapped list items itself (#2105).
	return ui.HangingIndent(out, wrap), nil
}

// styleConfig picks the stock glamour style off the palette's dark flag and
// maps the heading and link colors onto the active palette, so the preview
// reads as part of the theme instead of a foreign block.
func (m Model) styleConfig() gansi.StyleConfig { return styleConfigFor(m.palette()) }

// styleConfigFor is styleConfig for an explicit palette, so a caller without a
// preview model (Render) themes its output the same way. A nil palette falls
// back to the default, exactly like the model's own accessor.
func styleConfigFor(pal *theme.Palette) gansi.StyleConfig {
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	cfg := styles.LightStyleConfig
	if pal.Dark {
		cfg = styles.DarkStyleConfig
	}
	accent := hexColor(pal.Accent)
	link := hexColor(pal.Info)
	cfg.Heading.Color = &accent
	cfg.Link.Color = &link
	cfg.LinkText.Color = &accent
	return cfg
}

// palette returns the palette the preview styles against, falling back to the
// default when the pane was built without one (zero-value models, tests).
func (m Model) palette() *theme.Palette {
	if m.pal == nil {
		return theme.DefaultPalette()
	}
	return m.pal
}

// headingRe matches an ATX heading line; fenced code blocks are excluded by
// anchorHeadings' fence tracking, not here.
var headingRe = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*#*\s*$`)

// fenceRe matches a code-fence delimiter line.
var fenceRe = regexp.MustCompile("^\\s*(```|~~~)")

// anchorHeadings maps each source heading line to the rendered line its text
// reappears on. The scan walks both sides in order, so repeated heading texts
// pair up positionally. A heading whose text is not found (wrapped mid-word by
// the renderer, say) keeps rendered = -1 and is skipped by the follow scroll —
// approximate mapping is the contract (#62).
func anchorHeadings(src string, rendered []string) []heading {
	var out []heading
	plain := make([]string, len(rendered))
	for i, l := range rendered {
		plain[i] = ansi.Strip(l)
	}
	next := 0
	inFence := false
	for i, line := range strings.Split(src, "\n") {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		match := headingRe.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		h := heading{src: i, rendered: -1}
		for j := next; j < len(plain); j++ {
			if strings.Contains(plain[j], match[1]) {
				h.rendered = j
				next = j + 1
				break
			}
		}
		out = append(out, h)
	}
	return out
}

// follow scrolls the rendered view to the cursor's mapped position: the
// nearest preceding heading anchor, advanced proportionally within its section
// so long sections still track. With no usable anchor the whole document maps
// proportionally.
func (m *Model) follow() {
	if len(m.lines) == 0 {
		m.top = 0
		return
	}
	target := m.mapLine(m.cursor)
	// Aim the target a third down the viewport: context above, room below.
	m.scrollTo(target - m.h/3)
}

// mapLine translates a source line into a rendered line via the heading
// anchors.
func (m Model) mapLine(srcLine int) int {
	srcTotal := max(1, strings.Count(m.src, "\n")+1)
	// Section bounds around the cursor, in both coordinate spaces.
	loSrc, loRen := 0, 0
	hiSrc, hiRen := srcTotal, len(m.lines)
	for _, a := range m.anchors {
		if a.rendered < 0 {
			continue
		}
		if a.src <= srcLine {
			loSrc, loRen = a.src, a.rendered
		} else {
			hiSrc, hiRen = a.src, a.rendered
			break
		}
	}
	if hiSrc <= loSrc {
		return loRen
	}
	frac := float64(srcLine-loSrc) / float64(hiSrc-loSrc)
	return loRen + int(frac*float64(hiRen-loRen))
}

// hexColor formats a palette color as the #rrggbb string glamour styles take.
func hexColor(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
