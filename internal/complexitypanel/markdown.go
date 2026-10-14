package complexitypanel

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitycli"
)

// markdown.go is the 'E' export (#2963): the current tab as GitHub-
// flavoured Markdown, rendered through the CLI's writer
// (complexitycli.WriteMarkdown) so a pasted tab and `ike complexity --md`
// read alike. The rows are the CSV export's: the visible ones, filter and
// sort applied, breakdown rows left out. The Project / File tabs (grouped
// or not) and the tree render the totals line and the table, the Changes
// tab the verdict, the totals and the changed / added / removed sections.

// ExportMarkdownMsg asks the root model to write the current tab to a
// Markdown scratch file ('E').
type ExportMarkdownMsg struct {
	// Label names the exported tab for the notification ("Changes").
	Label string
	// Markdown is the complete file content.
	Markdown string
}

// exportMarkdown builds the current tab's Markdown and hands it to the
// root model.
func (m *Model) exportMarkdown() tea.Cmd {
	md := m.Markdown()
	label := tabNames[m.tab]
	return func() tea.Msg { return ExportMarkdownMsg{Label: label, Markdown: md} }
}

// Markdown renders the current tab (see markdown.go).
func (m *Model) Markdown() string {
	return complexitycli.RenderMarkdown(m.markdownReport())
}

// markdownReport assembles the current tab's report.
func (m *Model) markdownReport() complexitycli.Markdown {
	layout := m.layout()
	md := complexitycli.Markdown{Headline: m.totalsText()}
	if m.tab == TabChanges {
		md.Headline = "Complexity: " + strings.ToUpper(m.changesDiff().Verdict.String())
		md.Notes = []string{m.changesTotals()}
	}
	if m.tab == TabChanges && m.Grouping() == complexity.ByNone {
		md.Sections = m.changesMarkdownSections(layout)
		return md
	}
	sec := m.markdownSection("", layout)
	if m.treeActive() {
		for _, hr := range m.tree.Visible() {
			if hr.Item.Row.child() {
				continue
			}
			cells := m.treeCells(hr, layout)
			if i := m.layoutIndex(ColFunc); i >= 0 {
				// The fold marker means nothing on paper; no-break spaces
				// keep the depth visible in a rendered table.
				cells[i] = strings.Repeat("  ", hr.Depth()) + hr.Item.Name
			}
			sec.Rows = append(sec.Rows, cells)
		}
	} else {
		for _, r := range m.rows {
			if r.child() || r.Gone {
				continue
			}
			sec.Rows = append(sec.Rows, r.cells(layout))
		}
	}
	md.Sections = []complexitycli.MarkdownSection{sec}
	return md
}

// changesMarkdownSections splits the Changes rows into the changed, added
// and removed tables, the section column left out.
func (m *Model) changesMarkdownSections(layout []Column) []complexitycli.MarkdownSection {
	var cols []Column
	for _, c := range layout {
		if c != ColChange {
			cols = append(cols, c)
		}
	}
	kinds := []complexity.ChangeKind{complexity.Changed, complexity.Added, complexity.Removed}
	var out []complexitycli.MarkdownSection
	for _, k := range kinds {
		title := k.String()
		sec := m.markdownSection(strings.ToUpper(title[:1])+title[1:], cols)
		for _, r := range m.rows {
			if r.Change == k && !r.child() {
				sec.Rows = append(sec.Rows, r.cells(cols))
			}
		}
		if len(sec.Rows) > 0 {
			out = append(out, sec)
		}
	}
	return out
}

// markdownSection is an empty table over layout: the on-screen labels
// without the sort marker, the numeric columns right-aligned.
func (m *Model) markdownSection(title string, layout []Column) complexitycli.MarkdownSection {
	sec := complexitycli.MarkdownSection{Title: title, Header: m.columnTitles(layout), Numeric: make([]bool, len(layout))}
	for i, c := range layout {
		sec.Numeric[i] = c.numeric() && c != ColCommit && c != ColDate
	}
	return sec
}
