package editor

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/register"
	"ike/internal/highlight"
	"ike/internal/structval"
)

// structvalue.go is the editor half of the structural-value copies (#2499):
// `gy` (editor.yankValue) puts the *decoded inner* value under the caret on
// the clipboard, `gY` (editor.yankValueOuter) the raw construct around it.
//
// The gesture answers the everyday question a manifest or a fixture poses —
// "give me this value as text": the JSON string without its quotes and with
// its escapes resolved (an embedded document, a `\n`-joined script), the YAML
// block scalar folded per its header, the inner markup of an HTML element, the
// TOML value of a key. Before it, that was a hand-made visual selection plus a
// round of escape surgery.
//
// The syntax walk is internal/highlight (SyntaxChainAt), the meaning is
// internal/structval; this file only decides *when* to run them and how the
// result reaches the clipboard. Like the #1660 path breadcrumb it is a
// whole-buffer analysis, so it is off in large-file mode — and unlike the
// breadcrumb it parses on demand, once per keypress, never per frame.

// yankValueToastCells bounds the copied text shown in the toast. The first
// line is the recognisable part; the length hint carries the rest.
const yankValueToastCells = 40

// yankStructuralValue runs gy (outer=false) and gY (outer=true): it copies the
// structural value at the caret into the system-clipboard register `+`, which
// also records it in the clipboard history like every other yank. The returned
// command is the feedback toast, or the notice explaining why nothing was
// copied.
func (m *Model) yankStructuralValue(outer bool) tea.Cmd {
	if m.insert.active {
		m.commitInsert()
	}
	if m.InsightOff() {
		return notice("structural values are off in large-file mode")
	}
	lines := m.buf.Lines()
	chain := highlight.SyntaxChainAt(m.langPath(), lines, m.cursor.Line, m.cursor.Col)
	val, ok := structval.Extract(m.langID(), strings.Join(lines, "\n"), chain)
	text := val.Inner
	if outer {
		text = val.Outer
	}
	if !ok || text == "" {
		return notice(structval.NoValue)
	}
	// A trailing newline marks the entry linewise, the same rule the app-wide
	// clipboard history applies, so pasting a copied block opens a line.
	m.regs.Yank('+', register.Entry{Text: text, Linewise: strings.HasSuffix(text, "\n")})
	if cmd := m.takeClipboardSignal(); cmd != nil {
		return cmd
	}
	return notice(copiedValueLabel(text))
}

// copiedValueLabel is the copy toast: the first line of what was copied — the
// part that makes it recognisable — plus a size hint, so a multi-line subtree
// and a one-word scalar are told apart at a glance.
func copiedValueLabel(text string) string {
	body := strings.TrimSuffix(text, "\n")
	first, rows := body, strings.Count(body, "\n")+1
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		first = body[:i]
	}
	hint := countedUnit(len([]rune(text)), "char")
	if rows > 1 {
		hint = countedUnit(rows, "line") + ", " + hint
	}
	return `copied "` + truncRightCells(first, yankValueToastCells) + `" (` + hint + ")"
}

// countedUnit renders "1 line" / "3 lines".
func countedUnit(n int, unit string) string {
	return strconv.Itoa(n) + " " + unit + plural(n, "s")
}

// truncRightCells clips s to max rune cells, marking the cut with a trailing
// ellipsis — the mirror of docpath.go's truncLeftCells, which keeps the tail
// because a path's tail is the interesting end. A copied value's *head* is.
func truncRightCells(s string, max int) string {
	r := []rune(s)
	if len(r) <= max || max < 2 {
		return s
	}
	return string(r[:max-1]) + "…"
}
