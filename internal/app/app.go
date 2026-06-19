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
	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor"
	"ike/internal/explorer"
	"ike/internal/help"
	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
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
	return Model{
		focus:    focusExplorer,
		explorer: explorer.New("."),
		editor:   editor.New(),
		host:     host.New(cfg),
		reg:      reg,
		// help is a read-only consumer of the registry; the 0080 keymap resolver
		// is not wired yet, so commands render title-only (nil resolver).
		help: help.New(reg, nil, helpMinCol(cfg)),
	}
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
		m.help.SetSize(m.width, m.height)
		return m, nil

	case explorer.OpenFileMsg:
		return m.openPath(msg.Path)

	case host.OpenFileRequest:
		return m.openPath(msg.Path)

	case editor.CloseMsg:
		m.editor = editor.New()
		m.layout()
		m.focus = focusExplorer
		m.syncFocus()
		return m, nil

	case tea.KeyMsg:
		// The help overlay, when open, consumes every key (scroll + dismiss) and
		// shadows all other routing.
		if m.help.IsOpen() {
			m.help.Update(msg)
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
			m.help.SetSize(m.width, m.height)
			m.help.Open(m.focusContext())
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

// layout recomputes child pane sizes from the current terminal size.
func (m *Model) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	bodyHeight := m.height - statusHeight
	contentHeight := bodyHeight - paneChrome
	if contentHeight < 1 {
		contentHeight = 1
	}

	expContent := explorerWidth - paneChrome
	if expContent < 1 {
		expContent = 1
	}
	editorWidth := m.width - explorerWidth
	edContent := editorWidth - paneChrome
	if edContent < 1 {
		edContent = 1
	}

	m.explorer.SetSize(expContent, contentHeight)
	m.editor.SetSize(edContent, contentHeight)
	m.syncFocus()
}

// View implements tea.Model.
func (m Model) View() string {
	if m.width == 0 {
		return "starting ike…"
	}
	bodyHeight := m.height - statusHeight

	explorerPane := pane("EXPLORER", m.explorer.View(), explorerWidth, bodyHeight, m.focus == focusExplorer)
	editorWidth := m.width - explorerWidth
	editorPane := pane(m.editorTitle(), m.editor.View(), editorWidth, bodyHeight, m.focus == focusEditor)

	body := lipgloss.JoinHorizontal(lipgloss.Top, explorerPane, editorPane)
	base := lipgloss.JoinVertical(lipgloss.Left, body, m.statusLine())
	if m.help.IsOpen() {
		return overlayCenter(base, m.help.View(), m.width, m.height)
	}
	return base
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

// pane renders a titled bordered box around content; the focused pane gets a
// brighter border.
func pane(title, content string, width, height int, focused bool) string {
	border := lipgloss.RoundedBorder()
	style := lipgloss.NewStyle().
		Border(border).
		Width(width-2).
		Height(height-2).
		Padding(0, 1)
	if focused {
		style = style.BorderForeground(lipgloss.Color("69"))
	} else {
		style = style.BorderForeground(lipgloss.Color("240"))
	}
	titleStyle := lipgloss.NewStyle().Bold(true)
	return style.Render(lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render(title), content))
}

func baseName(path string) string { return filepath.Base(path) }

// overlayCenter composites the floating pane `top` centered over `base`, a
// canvas w columns by h rows. Each overlaid row is spliced into the base row by
// visual column, preserving the ANSI styling on both sides of the pane. base is
// returned unchanged if the pane does not fit.
func overlayCenter(base, top string, w, h int) string {
	if top == "" {
		return base
	}
	topLines := strings.Split(top, "\n")
	topH := len(topLines)
	topW := 0
	for _, l := range topLines {
		if lw := ansi.StringWidth(l); lw > topW {
			topW = lw
		}
	}
	if topW > w || topH > h {
		return base // does not fit; leave the base untouched
	}

	baseLines := strings.Split(base, "\n")
	// Pad the canvas to h rows so centering math is stable.
	for len(baseLines) < h {
		baseLines = append(baseLines, "")
	}
	x := (w - topW) / 2
	y := (h - topH) / 2

	for i, tl := range topLines {
		row := y + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		baseLines[row] = spliceLine(baseLines[row], tl, x, topW, w)
	}
	return strings.Join(baseLines, "\n")
}

// spliceLine overwrites `line` with `top` (visual width topW) starting at visual
// column x, keeping the styled remainder of `line` to the right. canvasW is the
// full row width used to right-pad short base lines.
func spliceLine(line, top string, x, topW, canvasW int) string {
	left := ansi.Truncate(line, x, "")
	if pad := x - ansi.StringWidth(left); pad > 0 {
		left += strings.Repeat(" ", pad)
	}
	right := ansi.TruncateLeft(line, x+topW, "")
	return left + "\x1b[0m" + top + "\x1b[0m" + right
}
