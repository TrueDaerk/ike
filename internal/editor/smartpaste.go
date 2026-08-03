package editor

// smartpaste.go implements smart paste (#1476): a multi-line paste strips the
// block's common leading indentation and re-applies the indentation of the
// paste target line, preserving relative indentation inside the block —
// JetBrains' default paste behavior. The transform runs on the register text
// before the insert, so the paste stays one history change (one undo unit)
// and works with any register, dot-repeat and multi-caret paste (the target
// indent is resolved per caret). editor.smart_paste = false disables it;
// terminal bracketed paste (PasteText) always splices verbatim.

import (
	"strings"

	"ike/internal/editor/register"
)

// smartPasteEntry returns the register text to paste at line: linewise
// multi-line entries are re-indented to the target line's indentation,
// everything else (single-line, charwise, smart paste off) passes through
// unchanged.
func (m *Model) smartPasteEntry(e register.Entry, line int) string {
	if !m.smartPaste || !e.Linewise {
		return e.Text
	}
	return m.smartPasteText(e.Text, line)
}

// smartPasteText re-indents a multi-line block for pasting at line. A
// single-line block is returned unchanged.
func (m *Model) smartPasteText(text string, line int) string {
	trailing := strings.HasSuffix(text, "\n")
	body := strings.TrimSuffix(text, "\n")
	if !strings.Contains(body, "\n") {
		return text
	}
	out := reindentBlock(body, m.targetIndent(line), m.tabWidth, m.useSpaces)
	if trailing {
		out += "\n"
	}
	return out
}

// smartPasteInsertText re-indents a multi-line clipboard paste landing in an
// open insert session: the first line splices verbatim at the cursor, the
// continuation lines re-indent to the current line's indentation with their
// relative structure preserved.
func (m *Model) smartPasteInsertText(text string) string {
	if !m.smartPaste {
		return text
	}
	i := strings.Index(text, "\n")
	if i < 0 {
		return text
	}
	return text[:i+1] + reindentBlock(text[i+1:], m.targetIndent(m.cursor.Line), m.tabWidth, m.useSpaces)
}

// targetIndent is the indentation of the paste context: the target line's own
// leading whitespace, or the nearest non-blank line above when it is blank.
func (m *Model) targetIndent(line int) string {
	if line >= m.buf.LineCount() {
		line = m.buf.LineCount() - 1
	}
	for i := line; i >= 0; i-- {
		if s := m.buf.Line(i); strings.TrimSpace(s) != "" {
			return leadingWhitespace(s)
		}
	}
	return ""
}

// reindentBlock strips the common leading indentation (measured in display
// columns, so tabs and spaces mix) of body's non-blank lines and prefixes
// each with target plus the line's relative indent, re-emitted in the
// buffer's indent style. Blank lines stay empty.
func reindentBlock(body, target string, tabWidth int, useSpaces bool) string {
	lines := strings.Split(body, "\n")
	common := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if w := indentWidth(leadingWhitespace(l), tabWidth); common < 0 || w < common {
			common = w
		}
	}
	if common < 0 {
		return body // all lines blank
	}
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			lines[i] = ""
			continue
		}
		lead := leadingWhitespace(l)
		rel := indentWidth(lead, tabWidth) - common
		lines[i] = target + indentString(rel, tabWidth, useSpaces) + l[len(lead):]
	}
	return strings.Join(lines, "\n")
}

// indentWidth is the display width of a leading whitespace run, expanding
// tabs to the next tab stop.
func indentWidth(ws string, tabWidth int) int {
	w := 0
	for i := 0; i < len(ws); i++ {
		if ws[i] == '\t' {
			w += tabWidth - w%tabWidth
		} else {
			w++
		}
	}
	return w
}

// indentString renders a relative indent of w display columns in the
// buffer's indent style: spaces under expandtab, tabs (plus space remainder)
// otherwise.
func indentString(w, tabWidth int, useSpaces bool) string {
	if w <= 0 {
		return ""
	}
	if useSpaces {
		return strings.Repeat(" ", w)
	}
	return strings.Repeat("\t", w/tabWidth) + strings.Repeat(" ", w%tabWidth)
}
