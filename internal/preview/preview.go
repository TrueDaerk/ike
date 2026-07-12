// Package preview renders a markdown buffer to styled terminal output as a
// live side-by-side pane (#62). The pane is bound to one source file path; the
// root model pushes buffer text into it on every editor change (debounced) and
// the current cursor line for scroll sync. Rendering goes through glamour with
// the style picked off the active palette's dark flag, so the preview follows
// the IDE theme. Images degrade to their alt-text links — terminal image
// protocols are out of scope.
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
}

// New returns a preview bound to path. Content arrives via SetSourceImmediate
// (on open/restore) or SetSource (debounced live updates).
func New(key, path string, pal *theme.Palette) Model {
	return Model{key: key, path: path, pal: pal}
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

// Update handles the debounce tick and, when focused, scroll keys.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case RenderTickMsg:
		if msg.Key == m.key && msg.Seq == m.seq {
			m.render()
		}
	case tea.KeyPressMsg:
		m.handleKey(msg)
	}
	return nil
}

// handleKey scrolls the rendered document. The preview is read-only, so the
// vim motions map straight to view movement.
func (m *Model) handleKey(msg tea.KeyPressMsg) {
	switch msg.String() {
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
func (m Model) maxTop() int { return max(0, len(m.lines)-m.h) }

// View renders the visible window of the rendered document, hard-clamped to
// the pane interior.
func (m Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	var b strings.Builder
	for row := 0; row < m.h; row++ {
		if row > 0 {
			b.WriteByte('\n')
		}
		if i := m.top + row; i >= 0 && i < len(m.lines) {
			b.WriteString(ansi.Truncate(m.lines[i], m.w, "…"))
		}
	}
	return b.String()
}

// render runs glamour over the pending source at the current width and theme,
// rebuilds the scroll-sync anchors, and re-applies the follow scroll.
func (m *Model) render() {
	if m.w <= 0 {
		return
	}
	out, err := m.renderMarkdown()
	if err != nil {
		out = "preview error: " + err.Error()
	}
	m.lines = strings.Split(strings.TrimRight(out, "\n"), "\n")
	m.anchors = anchorHeadings(m.src, m.lines)
	m.follow()
}

// renderMarkdown renders the source through a fresh width- and theme-bound
// renderer. Glamour renderers are cheap to build relative to a render, and a
// fresh one per render keeps width/theme changes trivially correct.
func (m Model) renderMarkdown() (string, error) {
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(m.styleConfig()),
		glamour.WithWordWrap(max(10, m.w-2)),
	)
	if err != nil {
		return "", err
	}
	return r.Render(m.src)
}

// styleConfig picks the stock glamour style off the palette's dark flag and
// maps the heading and link colors onto the active palette, so the preview
// reads as part of the theme instead of a foreign block.
func (m Model) styleConfig() gansi.StyleConfig {
	pal := m.pal
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
