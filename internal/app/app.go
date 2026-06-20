// Package app wires the root bubbletea model for IKE: a two-pane layout that
// hosts the file explorer and the editor, owns focus and layout, routes the
// explorer's open-file message to the editor, and renders the status line.
package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"ike/internal/editor"
	"ike/internal/explorer"
	"ike/internal/help"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/overlay"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/ui"
)

// Context ids the core panes advertise for context-scoped command/keymap
// resolution. Plugin panes advertise their own via plugin.ContextProvider.
const (
	ctxExplorer = "explorer"
	ctxEditor   = "editor"
)

// focus identifies which pane currently receives key input.
type focus int

const (
	focusExplorer focus = iota
	focusEditor
)

const (
	explorerWidth = 30 // outer width of the explorer pane (border included)
	statusHeight  = 1
	paneChrome    = 4 // border (2) + padding (2) per pane, horizontal and vertical-ish
)

// Model is the root model.
type Model struct {
	width    int
	height   int
	focus    focus
	explorer explorer.Model
	editor   editor.Model
	host     *host.Host
	reg      *registry.Registry
	help     *help.Help
	// shell is the single active floating overlay (Roadmap 0035). It hosts the
	// help cheat sheet today and any tea.Model-shaped content (modals, plugin
	// popups) tomorrow; v1 is single-level (one open shell at a time).
	shell *ui.Floating
	// tree is the pure split-tree layout (Roadmap 0036). It is loaded from the
	// per-project store or built as a default on first window size, and drives
	// both pane sizing and the View placement.
	tree layout.Node
	// lay caches the rectangles + dividers computed from tree for the current
	// viewport, so mouse hit-testing and rendering share one geometry.
	lay layout.Layout
	// drag is the active mouse gesture (resize or move), nil between drags.
	drag *dragState
}

// dragKind distinguishes the two mouse gestures.
type dragKind int

const (
	dragResize dragKind = iota // dragging a divider to change a split ratio
	dragMove                   // dragging a pane title bar to relocate it
)

// dragState holds the in-flight mouse gesture. For a resize it carries the
// divider being dragged; for a move it carries the source pane id. curX/curY
// track the latest mouse cell so the move can render live feedback (which pane
// and drop zone the release would target).
type dragState struct {
	kind    dragKind
	divider layout.Divider
	srcPane string
	curX    int
	curY    int
}

// New returns the initial root model rooted at the working directory, wired to
// the global plugin registry.
func New() Model {
	return NewWith(registry.Global(), host.MapConfig{})
}

// NewWith returns a root model backed by an explicit registry and config. It
// applies per-plugin enable/disable flags from config keys of the form
// "plugins.<id>.enabled" before the registry is queried.
func NewWith(reg *registry.Registry, cfg host.Config) Model {
	applyPluginConfig(reg, cfg)
	m := Model{
		focus:    focusExplorer,
		explorer: explorer.New("."),
		editor:   editor.New(),
		host:     host.New(cfg),
		reg:      reg,
		// help is a read-only consumer of the registry; the 0080 keymap resolver
		// is not wired yet, so commands render title-only (nil resolver).
		help:  help.New(reg, nil, helpMinCol(cfg)),
		shell: ui.New(shellConfig(cfg)),
	}
	// Restore a saved per-project layout if one matches the live pane set; an
	// unknown or stale layout is dropped and the default is built on first size.
	if t, ok := loadLayout(corePanes()); ok {
		m.tree = t
	}
	return m
}

// corePanes is the set of pane ids the root currently owns. Layout restore
// validates a saved tree against it so a stale file can never hide a pane.
func corePanes() map[string]bool {
	return map[string]bool{ctxExplorer: true, ctxEditor: true}
}

// shellConfig builds the floating shell configuration, reading optional tuning
// keys (margin, max width/height fraction) from cfg. The help dismiss set keeps
// the established esc/?/q so the cheat sheet behaves exactly as before.
func shellConfig(cfg host.Config) ui.Config {
	c := ui.Config{
		DismissKeys: []string{"esc", "?", "q"},
		Accent:      "69",
	}
	if cfg == nil {
		return c
	}
	if v, ok := cfg.Get("overlay.margin"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			c.Margin = n
		}
	}
	if v, ok := cfg.Get("overlay.max_width_fraction"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.MaxWidthFrac = f
		}
	}
	if v, ok := cfg.Get("overlay.max_height_fraction"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.MaxHeightFrac = f
		}
	}
	return c
}

// helpMinCol reads the optional help.min_column_width config value; 0 (the
// default) lets the overlay pick its built-in minimum.
func helpMinCol(cfg host.Config) int {
	if cfg == nil {
		return 0
	}
	if v, ok := cfg.Get("help.min_column_width"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

// applyPluginConfig reads "plugins.<id>.enabled" toggles. Only an explicit
// "false" disables; everything else leaves the plugin enabled (the default).
func applyPluginConfig(reg *registry.Registry, cfg host.Config) {
	if cfg == nil {
		return
	}
	for _, id := range reg.PluginIDs() {
		if v, ok := cfg.Get("plugins." + id + ".enabled"); ok && v == "false" {
			reg.SetEnabled(id, false)
		}
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update owns global keys (quit, focus switch), routes open/close messages, and
// forwards everything else to the focused pane.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.shell.SetSize(m.width, m.height)
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case explorer.OpenFileMsg:
		return m.openPath(msg.Path)

	case host.OpenFileRequest:
		return m.openPath(msg.Path)

	case host.OpenModalRequest:
		// A plugin asked to present content as a floating modal; host it in the
		// single active shell. v1 is single-level, so this replaces any open shell.
		m.shell.SetContent(ui.ModelContent{Heading: msg.Title, Body: msg.View})
		m.shell.SetSize(m.width, m.height)
		m.shell.Open()
		return m, nil

	case editor.CloseMsg:
		m.editor = editor.New()
		m.layout()
		m.focus = focusExplorer
		m.syncFocus()
		return m, nil

	case tea.KeyMsg:
		// The floating shell, when open, consumes every key (scroll + dismiss) and
		// shadows all other routing.
		if m.shell.IsOpen() {
			m.shell.Update(msg)
			return m, nil
		}
		// Global keys take priority, but only when the editor is not actively
		// capturing text (insert/command mode), so typing "q" into a file works.
		if m.editorCapturing() {
			return m.routeKey(msg)
		}
		keys := msg.String()
		// "?" opens the help overlay (binding/command ownership moves to 0070/0080
		// once they land; help only consumes it).
		if keys == "?" {
			m.help.Snapshot(m.focusContext())
			m.shell.SetContent(m.help)
			m.shell.SetSize(m.width, m.height)
			m.shell.Open()
			return m, nil
		}
		// Plugin key bindings resolve before core only when they explicitly
		// out-prioritise core; otherwise core keys keep precedence.
		if k, ok := m.reg.ResolveKey(keys, m.focusContext()); ok {
			if k.Priority > plugin.CorePriority || !isCoreKey(keys, m.focus) {
				return m, k.Action(m.host)
			}
		}
		switch keys {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.focus == focusExplorer {
				return m, tea.Quit
			}
		case "tab":
			m.toggleFocus()
			return m, nil
		}
		return m.routeKey(msg)
	}
	return m, nil
}

// openPath opens path: a registered FileHandler claims it if one matches,
// otherwise it loads into the editor. EventFileOpened hooks fire either way.
func (m Model) openPath(path string) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if h, ok := m.reg.ResolveHandler(path, readHead(path)); ok {
		cmds = append(cmds, h.Open(m.host, path))
	} else if err := m.editor.Load(path); err == nil {
		m.focus = focusEditor
		m.syncFocus()
	}
	cmds = append(cmds, m.fireHooks(plugin.EventFileOpened, path)...)
	return m, tea.Batch(cmds...)
}

// fireHooks invokes every enabled hook subscribed to event, collecting their
// commands.
func (m Model) fireHooks(event plugin.Event, payload any) []tea.Cmd {
	var cmds []tea.Cmd
	for _, h := range m.reg.Hooks(event) {
		if c := h.Notify(m.host, payload); c != nil {
			cmds = append(cmds, c)
		}
	}
	return cmds
}

// RunCommand looks up and runs a registered command by id, returning its
// tea.Cmd. It is the seam the command palette (Roadmap 0070) drives.
func (m Model) RunCommand(id string) tea.Cmd {
	if c, ok := m.reg.Command(id); ok {
		return c.Run(m.host)
	}
	return nil
}

// focusContext reports the context id advertised by the focused pane, used for
// context-scoped command/keymap resolution.
func (m Model) focusContext() string {
	if m.focus == focusExplorer {
		return ctxExplorer
	}
	return ctxEditor
}

// isCoreKey reports whether keys is handled by a core binding in the current
// focus, so a plugin must out-prioritise it to take over.
func isCoreKey(keys string, f focus) bool {
	switch keys {
	case "ctrl+c", "tab":
		return true
	case "q":
		return f == focusExplorer
	}
	return false
}

// readHead returns the leading bytes of path for content sniffing, or nil.
func readHead(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return buf[:n]
}

// editorCapturing reports whether the editor is focused and in a text-capturing
// mode, in which case global single-letter keys must not be intercepted.
func (m Model) editorCapturing() bool {
	if m.focus != focusEditor {
		return false
	}
	mode := m.editor.ModeName()
	return mode == editor.Insert || mode == editor.Command
}

// routeKey forwards a key to the focused pane.
func (m Model) routeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.focus == focusExplorer {
		m.explorer, cmd = m.explorer.Update(msg)
	} else {
		m.editor, cmd = m.editor.Update(msg)
	}
	return m, cmd
}

func (m *Model) toggleFocus() {
	if m.focus == focusExplorer {
		m.focus = focusEditor
	} else {
		m.focus = focusExplorer
	}
	m.syncFocus()
}

func (m *Model) syncFocus() {
	m.explorer.SetFocused(m.focus == focusExplorer)
	m.editor.SetFocused(m.focus == focusEditor)
}

// bodyRect is the viewport the layout tree tiles: the full width and every row
// above the status line.
func (m *Model) bodyRect() layout.Rect {
	return layout.Rect{X: 0, Y: 0, W: m.width, H: m.height - statusHeight}
}

// layout recomputes the layout geometry from the current terminal size and
// pushes the resulting interior size into each pane. The tree is built lazily on
// the first real window size so a default ratio can key off the actual width.
func (m *Model) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	if m.tree == nil {
		m.tree = layout.Default(m.width, explorerWidth)
	}
	m.lay = layout.Compute(m.tree, m.bodyRect())
	if r, ok := m.lay.Panes[ctxExplorer]; ok {
		m.explorer.SetSize(paneInterior(r.W), paneInterior(r.H))
	}
	if r, ok := m.lay.Panes[ctxEditor]; ok {
		m.editor.SetSize(paneInterior(r.W), paneInterior(r.H))
	}
	m.syncFocus()
}

// paneInterior maps an outer pane dimension to the content area inside the
// border + padding chrome, never dropping below one cell.
func paneInterior(outer int) int {
	if v := outer - paneChrome; v >= 1 {
		return v
	}
	return 1
}

// handleMouse runs the drag state machine: press hit-tests the layout to start a
// resize (divider) or move (title bar), motion updates the in-flight gesture,
// and release commits and persists. Wheel events and any mouse activity while a
// shell overlay is open are ignored (overlays are not draggable).
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.shell.IsOpen() || msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		return m, nil
	}
	switch msg.Action {
	case tea.MouseActionPress:
		hit := m.lay.Hit(msg.X, msg.Y)
		switch hit.Kind {
		case layout.HitDivider:
			m.drag = &dragState{kind: dragResize, divider: *hit.Divider, curX: msg.X, curY: msg.Y}
		case layout.HitTitle:
			m.drag = &dragState{kind: dragMove, srcPane: hit.Pane, curX: msg.X, curY: msg.Y}
		}
	case tea.MouseActionMotion:
		if m.drag == nil {
			return m, nil
		}
		m.drag.curX, m.drag.curY = msg.X, msg.Y
		if m.drag.kind == dragResize {
			m.drag.divider.ResizeTo(msg.X, msg.Y)
			m.layout()
		}
	case tea.MouseActionRelease:
		if m.drag == nil {
			return m, nil
		}
		if m.drag.kind == dragMove {
			if target, ok := m.lay.PaneAt(msg.X, msg.Y); ok && target != m.drag.srcPane {
				zone := layout.DropZone(m.lay.Panes[target], msg.X, msg.Y)
				m.tree = layout.Move(m.tree, m.drag.srcPane, target, zone)
				m.layout()
			}
		}
		m.drag = nil
		saveLayout(m.tree)
	}
	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	if m.width == 0 {
		return "starting ike…"
	}
	body := m.renderNode(m.tree, m.bodyRect())
	base := lipgloss.JoinVertical(lipgloss.Left, body, m.statusLine())
	if m.shell.IsOpen() {
		return overlay.Center(base, m.shell.View(), m.width, m.height)
	}
	return base
}

// renderNode walks the layout tree, rendering each leaf into its rectangle and
// joining splits with a one-cell divider, mirroring Compute's geometry exactly.
func (m Model) renderNode(n layout.Node, r layout.Rect) string {
	switch t := n.(type) {
	case *layout.Leaf:
		return m.renderPane(t.Pane, r)
	case *layout.Split:
		a, _, b := t.Children(r)
		if t.Orient == layout.Horizontal {
			return lipgloss.JoinHorizontal(lipgloss.Top,
				m.renderNode(t.A, a), dividerV(r.H), m.renderNode(t.B, b))
		}
		return lipgloss.JoinVertical(lipgloss.Left,
			m.renderNode(t.A, a), dividerH(r.W), m.renderNode(t.B, b))
	}
	return ""
}

// Pane border colors. The drag colors give live feedback during a move: the
// pane being carried and the pane it would drop onto are tinted distinctly.
const (
	colorPaneFocus  = "69"  // focused pane border
	colorPaneBlur   = "240" // unfocused pane border
	colorMoveSource = "203" // the pane currently being moved
	colorDropTarget = "220" // the pane a release would drop onto
)

// renderPane renders a single leaf at its outer rectangle, mapping the pane id
// to its title, content, and focus state. During a move drag the source pane and
// the hovered drop target are recolored and the target's title shows the drop
// zone, so the gesture is visible. Unknown ids (future plugin panes) render an
// empty titled box rather than crashing.
func (m Model) renderPane(id string, r layout.Rect) string {
	var title, content string
	var focused bool
	switch id {
	case ctxExplorer:
		title, content, focused = "EXPLORER", m.explorer.View(), m.focus == focusExplorer
	case ctxEditor:
		title, content, focused = m.editorTitle(), m.editor.View(), m.focus == focusEditor
	default:
		title = strings.ToUpper(id)
	}

	border := colorPaneBlur
	if focused {
		border = colorPaneFocus
	}
	if d := m.drag; d != nil && d.kind == dragMove {
		if id == d.srcPane {
			border = colorMoveSource
			title = "⤴ " + title
		} else if tgt, ok := m.lay.PaneAt(d.curX, d.curY); ok && tgt == id && tgt != d.srcPane {
			border = colorDropTarget
			title = title + "  " + zoneArrow(layout.DropZone(r, d.curX, d.curY))
		}
	}
	return paneBox(title, content, r.W, r.H, border)
}

// zoneArrow is the short drop-zone marker shown in a target pane's title.
func zoneArrow(z layout.Zone) string {
	switch z {
	case layout.ZoneLeft:
		return "◧ left"
	case layout.ZoneRight:
		return "right ◨"
	case layout.ZoneTop:
		return "⬒ top"
	default:
		return "⬓ bottom"
	}
}

// dividerV renders the vertical gutter between two horizontally-arranged panes.
func dividerV(h int) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	return style.Render(strings.TrimRight(strings.Repeat("│\n", h), "\n"))
}

// dividerH renders the horizontal gutter between two vertically-stacked panes.
func dividerH(w int) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	return style.Render(strings.Repeat("─", w))
}

// editorTitle returns the editor pane title: file basename with a dirty marker.
func (m Model) editorTitle() string {
	if !m.editor.HasFile() {
		return "EDITOR"
	}
	name := baseName(m.editor.Path())
	if m.editor.Dirty() {
		name += " *"
	}
	return name
}

// statusLine renders the bottom status bar: mode, file, dirty flag, cursor, and
// any active command line.
func (m Model) statusLine() string {
	style := lipgloss.NewStyle().
		Width(m.width).
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252"))

	// During a move drag the status line narrates the gesture and its target.
	if d := m.drag; d != nil && d.kind == dragMove {
		hint := "MOVE " + m.paneLabel(d.srcPane)
		if tgt, ok := m.lay.PaneAt(d.curX, d.curY); ok && tgt != d.srcPane {
			hint += " → " + zoneArrow(layout.DropZone(m.lay.Panes[tgt], d.curX, d.curY)) + " of " + m.paneLabel(tgt)
		} else {
			hint += "  (drop on another pane)"
		}
		return style.Foreground(lipgloss.Color(colorDropTarget)).Render(" " + hint)
	}

	if cl := m.editor.CommandLine(); cl != "" {
		return style.Render(cl)
	}
	if s := m.host.Status(); s != "" {
		return style.Render(" " + s)
	}

	mode := m.editor.ModeName().String()
	file := "no file"
	if m.editor.HasFile() {
		file = baseName(m.editor.Path())
	}
	dirty := ""
	if m.editor.Dirty() {
		dirty = " [+]"
	}
	line, col := m.editor.Cursor()
	left := " " + mode + " │ " + file + dirty
	right := "Ln " + strconv.Itoa(line) + ", Col " + strconv.Itoa(col) + " "
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return style.Render(left + strings.Repeat(" ", gap) + right)
}

// paneBox renders a titled bordered box around content with the given border
// color (focus state and drag feedback are decided by the caller).
func paneBox(title, content string, width, height int, borderColor string) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Width(width-2).
		Height(height-2).
		Padding(0, 1).
		BorderForeground(lipgloss.Color(borderColor))
	titleStyle := lipgloss.NewStyle().Bold(true)
	return style.Render(lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render(title), content))
}

func baseName(path string) string { return filepath.Base(path) }

// paneLabel is the human label for a pane id used in the drag status hint.
func (m Model) paneLabel(id string) string {
	if id == ctxEditor {
		return m.editorTitle()
	}
	return strings.ToUpper(id)
}
