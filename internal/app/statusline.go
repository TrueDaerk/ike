package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/editor"
	"ike/internal/lang"
	"ike/internal/pane"
)

// statusline.go is the status line's segment model (#101): the editor status
// line is a pair of left/right slot lists rather than string concatenation in
// statusLine(), so new segments — toolchain, notifications, plugin-contributed
// ones later — plug in without touching the renderer.

// statusSegment is one status line slot: render returns the segment's text for
// this frame, or "" to hide the slot. ed is the active editor, nil when none.
type statusSegment struct {
	id     string
	render func(m Model, ed *editor.Model) string
}

// statusLeft and statusRight are the editor status line's slot lists, joined
// left-to-right with " │ ". Appending to them is the (in-process) extension
// point for future plugin segments.
var statusLeft = []statusSegment{
	{id: "mode", render: modeSegment},
	{id: "macro", render: macroSegment},
	{id: "file", render: fileSegment},
	{id: "eol", render: eolSegment},
	{id: "encoding", render: encodingSegment},
	{id: "indent", render: indentSegment},
	{id: "diagnostics", render: diagSegment},
	{id: "host", render: func(m Model, _ *editor.Model) string { return m.host.Status() }},
	{id: "lsp", render: func(m Model, ed *editor.Model) string { return m.focusedLangStatus(ed) }},
	{id: "toolchain", render: func(m Model, ed *editor.Model) string { return m.toolchainSegment(ed) }},
	{id: "notifications", render: func(m Model, _ *editor.Model) string { return m.notifSegment() }},
	{id: "todo", render: func(m Model, _ *editor.Model) string { return m.todoSegment() }},
}

// todoSegment is the project's comment-tag count from the TODO index (#61);
// hidden until the first full scan finishes and while the project is clean.
func (m Model) todoSegment() string {
	if !m.todo.Scanned() || m.todo.Count() == 0 {
		return ""
	}
	n := m.todo.Count()
	if n == 1 {
		return "1 TODO"
	}
	return strconv.Itoa(n) + " TODOs"
}

var statusRight = []statusSegment{
	{id: "branch", render: func(m Model, _ *editor.Model) string { return m.branchSegment() }},
	{id: "cursor", render: cursorSegment},
}

// branchSegment shows the git branch (Roadmap 0320, #463) with ahead/behind
// counters when the upstream diverges; hidden outside git repos. A detached
// HEAD shows the short commit hash the snapshot carries instead.
func (m Model) branchSegment() string {
	snap := m.vcs.snap
	if snap == nil || snap.Branch == "" {
		return ""
	}
	branch := snap.Branch
	// Long branch names would crowd out the left segments: clip mid-word,
	// JetBrains-style, keeping the discriminating prefix.
	if len(branch) > 24 {
		branch = branch[:23] + "…"
	}
	s := "⎇ " + branch
	if snap.Ahead > 0 {
		s += " ↑" + strconv.Itoa(snap.Ahead)
	}
	if snap.Behind > 0 {
		s += " ↓" + strconv.Itoa(snap.Behind)
	}
	return s
}

// renderSegments joins the non-empty slots with the segment divider.
func renderSegments(m Model, ed *editor.Model, segs []statusSegment) string {
	var parts []string
	for _, s := range segs {
		if s.render == nil {
			continue
		}
		if text := s.render(m, ed); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " │ ")
}

// modeSegment is the editor input mode; NORMAL when no editor exists.
func modeSegment(_ Model, ed *editor.Model) string {
	if ed == nil {
		return "NORMAL"
	}
	return ed.ModeName().String()
}

// macroSegment shows "recording @x" while a macro recording is active (#58),
// JetBrains/vim-style; hidden when idle.
func macroSegment(_ Model, ed *editor.Model) string {
	if ed == nil {
		return ""
	}
	if r := ed.Recording(); r != 0 {
		return "recording @" + string(r)
	}
	return ""
}

// fileSegment is the focused file's display path plus its state markers.
func fileSegment(_ Model, ed *editor.Model) string {
	if ed == nil {
		return "no file"
	}
	file := "no file"
	if ed.HasFile() {
		file = displayPath(ed.Path())
	}
	if ed.Dirty() {
		file += " [+]"
	}
	if ed.Stale() {
		file += " [disk changed]"
	}
	if ed.InsightOff() {
		// Large-file mode (#149): say why highlighting/LSP are absent.
		file += " [large file]"
	}
	return file
}

// eolSegment is the focused file's on-disk line-ending flavor (#66) — "LF" or
// "CRLF", flagged when the load saw mixed endings. Clicking converts later
// (#30); the file.setLineEndings commands are the interaction path.
func eolSegment(_ Model, ed *editor.Model) string {
	if ed == nil || !ed.HasFile() {
		return ""
	}
	s := ed.LineEnding()
	if ed.MixedEOL() {
		s += " (mixed)"
	}
	return s
}

// encodingSegment is the focused file's on-disk character encoding (#66);
// converted via the file.setEncoding commands.
func encodingSegment(_ Model, ed *editor.Model) string {
	if ed == nil || !ed.HasFile() {
		return ""
	}
	return ed.EncodingName()
}

// indentSegment is the focused buffer's effective indent style and width —
// "Spaces: 2" or "Tab: 4" — including any .editorconfig override (#63).
func indentSegment(_ Model, ed *editor.Model) string {
	if ed == nil || !ed.HasFile() {
		return ""
	}
	spaces, width := ed.IndentInfo()
	if spaces {
		return "Spaces: " + strconv.Itoa(width)
	}
	return "Tab: " + strconv.Itoa(width)
}

// diagSegment is the focused buffer's diagnostic counts; hidden when clean.
func diagSegment(_ Model, ed *editor.Model) string {
	if ed == nil {
		return ""
	}
	errs, warns := ed.DiagnosticCounts()
	if errs == 0 && warns == 0 {
		return ""
	}
	return strconv.Itoa(errs) + "E " + strconv.Itoa(warns) + "W"
}

// cursorSegment is the caret position, the right slot list's sole default.
func cursorSegment(_ Model, ed *editor.Model) string {
	line, col := 1, 1
	if ed != nil {
		line, col = ed.Cursor()
	}
	return "Ln " + strconv.Itoa(line) + ", Col " + strconv.Itoa(col)
}

// toolchainSegment names the focused buffer's effective interpreter (#101):
// the same lang.Interpreter resolution the terminal shims (#98) and the
// toolchain settings page (0160) read. A venv interpreter shows the venv
// directory's name, anything else the binary's base name. Resolution stats
// the filesystem and scans PATH, so the label is cached per language until
// the next config reload.
func (m Model) toolchainSegment(ed *editor.Model) string {
	if ed == nil || !ed.HasFile() {
		return ""
	}
	l, ok := lang.ByPath(ed.Path())
	if !ok || l.Toolchain == nil {
		return ""
	}
	if label, cached := m.toolchainSeg[l.ID]; cached {
		return label
	}
	explicit := ""
	if c := config.Get(); c != nil {
		explicit = c.Lang[l.ID]["interpreter"]
	}
	label := ""
	if path, _ := lang.Interpreter(l.ID, ".", explicit); path != "" {
		label = l.ID + ":" + interpreterName(path)
	}
	m.toolchainSeg[l.ID] = label
	return label
}

// interpreterName is an interpreter path's short display name: the virtualenv
// directory's name when the binary lives in one (bin's parent carries
// pyvenv.cfg), else the binary's base name — often version-bearing, e.g.
// "python3.12".
func interpreterName(path string) string {
	root := filepath.Dir(filepath.Dir(path))
	if _, err := os.Stat(filepath.Join(root, "pyvenv.cfg")); err == nil {
		return filepath.Base(root)
	}
	return filepath.Base(path)
}

// notifSegment counts the notifications that arrived since the history view
// was last opened (#101): "● N", cleared by notifications.history. Opening on
// click waits on mouse support (#30); the command is the interaction path.
func (m Model) notifSegment() string {
	if m.notifUnseen == 0 {
		return ""
	}
	return "● " + strconv.Itoa(m.notifUnseen)
}

// statusLine renders the bottom status bar. With an editor focused it shows
// the segment slots (mode, file, diagnostics, …, cursor); with a terminal or
// the explorer focused it names that pane kind instead, so the line always
// says where input goes (#381).
func (m Model) statusLine() string {
	style := lipgloss.NewStyle().
		Width(m.width).
		Background(m.pal().Panel).
		Foreground(m.pal().Foreground)

	if d := m.drag; d != nil && (d.kind == dragMove || d.kind == dragTab) {
		hint := "MOVE " + m.paneLabel(d.srcPane)
		if d.kind == dragTab {
			if ed := m.panes.Get(d.srcPane).TabEditor(d.srcTab); ed != nil && ed.HasFile() {
				hint = "MOVE " + filepath.Base(ed.Path())
			}
		}
		if tgt, ok := m.lay.PaneAt(d.curX, d.curY); ok && tgt != d.srcPane {
			if zone, can := m.dropZoneFor(d, tgt, m.lay.Panes[tgt]); can {
				hint += " → " + zoneArrow(zone) + " of " + m.paneLabel(tgt)
			} else {
				hint += "  (drop on a pane or this pane's edge)"
			}
		} else if zone, near := m.selfDropZone(d); near {
			hint += " → split " + zoneArrow(zone)
		} else {
			hint += "  (drop on a pane or this pane's edge)"
		}
		return style.Foreground(m.pal().DropTarget).Render(" " + hint)
	}

	// A non-editor focus names itself instead of implying editor input (#381):
	// mirroring the active editor while a terminal owns the keystrokes made it
	// hard to tell where input goes.
	if inst := m.panes.FocusedInstance(); inst != nil && inst.Kind() != pane.KindEditor {
		left := " "
		switch inst.Kind() {
		case pane.KindTerminal:
			left += "TERMINAL"
			t := inst.Terminal()
			seg := ""
			if s := t.ShellPath(); s != "" {
				seg = filepath.Base(s)
			}
			if d := t.Dir(); d != "" {
				if seg != "" {
					seg += " · "
				}
				seg += displayDir(d)
			}
			if seg != "" {
				left += " │ " + seg
			}
			if !t.Running() {
				left += " [exited]"
			}
		case pane.KindMarkdown:
			left += "PREVIEW │ " + filepath.Base(inst.Preview().Path())
		case pane.KindDiff:
			l, r := inst.Diff().Titles()
			left += "DIFF │ " + l + " ⇄ " + r
			if n := inst.Diff().HunkCount(); n > 0 {
				hunk := "–"
				if c := inst.Diff().CurrentHunk(); c >= 0 {
					hunk = strconv.Itoa(c + 1)
				}
				left += " │ hunk " + hunk + "/" + strconv.Itoa(n)
			}
		default:
			left += "EXPLORER"
		}
		if s := m.host.Status(); s != "" {
			left += " │ " + s
		}
		if s := m.notifSegment(); s != "" {
			left += " │ " + s
		}
		return style.Render(left)
	}

	// The ":" / "/" command line renders inside the editor pane (vim-style),
	// not here — the status line keeps its segments while typing a command.
	ed := m.activeEditor()
	left := " " + renderSegments(m, ed, statusLeft)
	right := renderSegments(m, ed, statusRight) + " "
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return style.Render(left + strings.Repeat(" ", gap) + right)
}

// focusedLangStatus returns the tracked server state for the focused editor's
// language (#380): the status line's server segment follows the buffer instead
// of echoing the last event globally. Empty when no file is open, the language
// is unknown, or no server state was ever reported for it.
func (m Model) focusedLangStatus(ed *editor.Model) string {
	if ed == nil || !ed.HasFile() {
		return ""
	}
	l, ok := lang.ByPath(ed.Path())
	if !ok {
		return ""
	}
	return m.lspStatus[l.ID]
}
