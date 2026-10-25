package complexitycli

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"ike/internal/complexity"
)

// markdown.go is the GitHub-flavoured Markdown rendering (#2963): `--md`
// on the CLI and the Complexity window's 'E' export. A report is a bold
// headline (the verdict or the summary), plain note lines and one table
// per section; the rows past MarkdownFold fold into a <details> block so a
// pull-request comment stays short. The Markdown type is exported so the
// tool window renders its current tab through the same writer.

// MarkdownFold is how many rows a section's table shows before the rest
// folds into a <details> block.
const MarkdownFold = 30

// Markdown is one report ready for WriteMarkdown. Every text is raw: the
// writer escapes it.
type Markdown struct {
	// Headline is the bold first line: the verdict or the summary; "" for
	// none.
	Headline string
	// Notes are the plain lines below the headline: totals, filters,
	// trailers.
	Notes []string
	// Sections are the tables, in order.
	Sections []MarkdownSection
}

// MarkdownSection is one table: an optional title, the header, which
// columns are numeric (right-aligned) and the rows in the view's order.
type MarkdownSection struct {
	// Title is rendered as a level-3 heading with the row count:
	// "### Changed (3)"; "" for an untitled table.
	Title   string
	Header  []string
	Numeric []bool
	Rows    [][]string
}

// MarkdownEscape makes s safe inside a table cell or a line: `|`,
// backticks, `*` and `<` (which GitHub would read as a table separator, a
// code span, emphasis or an HTML tag) are escaped and newlines folded to
// spaces.
func MarkdownEscape(s string) string {
	if !strings.ContainsAny(s, "|`*<\n\r") {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '|':
			b.WriteString(`\|`)
		case '`':
			b.WriteString("\\`")
		case '*':
			b.WriteString(`\*`)
		case '<':
			b.WriteString("&lt;")
		case '\n', '\r':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// WriteMarkdown renders md.
func WriteMarkdown(w io.Writer, md Markdown) {
	var paras []string
	if md.Headline != "" {
		paras = append(paras, "**"+MarkdownEscape(md.Headline)+"**")
	}
	for _, n := range md.Notes {
		paras = append(paras, MarkdownEscape(n))
	}
	if len(paras) > 0 {
		fmt.Fprintln(w, strings.Join(paras, "\n\n"))
	}
	for i, s := range md.Sections {
		if len(paras) > 0 || i > 0 {
			fmt.Fprintln(w)
		}
		writeMarkdownSection(w, s)
	}
}

// RenderMarkdown is WriteMarkdown into a string.
func RenderMarkdown(md Markdown) string {
	var b bytes.Buffer
	WriteMarkdown(&b, md)
	return b.String()
}

// writeMarkdownSection prints one section: the heading, the first
// MarkdownFold rows and the rest inside <details>.
func writeMarkdownSection(w io.Writer, s MarkdownSection) {
	if s.Title != "" {
		fmt.Fprintf(w, "### %s (%d)\n\n", MarkdownEscape(s.Title), len(s.Rows))
	}
	head := s.Rows
	var rest [][]string
	if len(head) > MarkdownFold {
		head, rest = s.Rows[:MarkdownFold], s.Rows[MarkdownFold:]
	}
	writeMarkdownTable(w, s, head)
	if len(rest) == 0 {
		return
	}
	fmt.Fprintf(w, "\n<details>\n<summary>%d more %s</summary>\n\n", len(rest), plural(len(rest), "row", "rows"))
	writeMarkdownTable(w, s, rest)
	fmt.Fprintln(w, "\n</details>")
}

// writeMarkdownTable prints the header, the alignment row and rows.
func writeMarkdownTable(w io.Writer, s MarkdownSection, rows [][]string) {
	line := func(cells []string) {
		var b strings.Builder
		b.WriteByte('|')
		for i := range s.Header {
			c := ""
			if i < len(cells) {
				c = cells[i]
			}
			b.WriteString(" " + MarkdownEscape(c) + " |")
		}
		fmt.Fprintln(w, b.String())
	}
	line(s.Header)
	align := make([]string, len(s.Header))
	for i := range align {
		align[i] = "---"
		if i < len(s.Numeric) && s.Numeric[i] {
			align[i] = "---:"
		}
	}
	fmt.Fprintln(w, "|", strings.Join(align, " | "), "|")
	for _, r := range rows {
		line(r)
	}
}

// --- the CLI reports ---

// itoa is the cell spelling of an int.
func itoa(n int) string { return strconv.Itoa(n) }

// fromTo is a "before→after (Δ)" cell.
func fromTo(before, after int) string {
	return itoa(before) + "→" + itoa(after) + " (" + signed(after-before) + ")"
}

// trailerLines captures what write prints as non-empty lines.
func trailerLines(write func(io.Writer)) []string {
	var b bytes.Buffer
	write(&b)
	var out []string
	for _, l := range strings.Split(b.String(), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// numericFrom marks every column from index first on as numeric.
func numericFrom(n, first int) []bool {
	out := make([]bool, n)
	for i := first; i < n; i++ {
		out[i] = true
	}
	return out
}

// headlineAndNotes splits the text summary into the bold summary line
// (its "summary: " prefix dropped) and the notes below it; extra lines
// follow.
func headlineAndNotes(lines []string, extra ...string) (string, []string) {
	if len(lines) == 0 {
		return "", extra
	}
	return strings.TrimPrefix(lines[0], "summary: "), append(lines[1:], extra...)
}

// projectMarkdown is the function table (#2963): the summary line, the
// notes the text report prints and the table in the report's order.
func projectMarkdown(r projectReport) Markdown {
	lines := trailerLines(func(w io.Writer) {
		r.writeSummaryLine(w)
		if r.s.args.Pins {
			r.writePinsLine(w)
		}
		if r.s.filtered() {
			fmt.Fprintf(w, "shown: %d of %d functions (%s%s)\n", len(r.rows), r.summary.Functions, r.s.filterText(), r.s.testsNote())
		}
		fmt.Fprintln(w, r.skipped.line())
	})
	md := Markdown{}
	md.Headline, md.Notes = headlineAndNotes(lines)
	header := []string{"File", "Function", "Line", "CCN", "Cog", "NLOC", "Params", "Cmt%"}
	if r.s.churn != nil {
		header = append(header, "Commits")
	}
	if r.s.args.Pins {
		header = append(header, "Pinned CCN", "Pinned cog", "ΔCCN", "Δcog")
	}
	sec := MarkdownSection{Header: header, Numeric: numericFrom(len(header), 2)}
	for _, rw := range r.rows {
		f := rw.fn
		name := f.Name
		if f.Baseline == complexity.BaselineAccepted {
			name += acceptedMark
		}
		pin := r.s.pinOf(rw.file, f, rw.ordinal)
		if pin != nil {
			name += pinMark
		}
		if rw.hidden {
			name += hiddenMark // #3031
		}
		cells := []string{rw.file, name, itoa(f.Line + 1), itoa(f.CCN), itoa(f.Cognitive), itoa(f.NLOC), itoa(f.Params),
			itoa(complexity.DensityPercent(f.CommentDensity))}
		if r.s.churn != nil {
			cells = append(cells, itoa(r.s.churn[rw.file]))
		}
		if r.s.args.Pins && pin != nil {
			dc, dg := pin.SincePin(f)
			cells = append(cells, itoa(pin.CCN), itoa(pin.Cognitive), complexity.SignedDelta(dc), complexity.SignedDelta(dg))
		}
		sec.Rows = append(sec.Rows, cells)
	}
	md.Sections = []MarkdownSection{sec}
	return md
}

// groupsMarkdown is the --by class|file table (#2963): the summary line
// and one row per group, the class column only per class.
func groupsMarkdown(r groupReport) Markdown {
	byClass := r.by == complexity.ByClass
	lines := trailerLines(func(w io.Writer) {
		r.writeSummaryLine(w)
		if r.s.filtered() {
			noun := "files"
			if byClass {
				noun = "classes"
			}
			fmt.Fprintf(w, "shown: %d of %d %s (%s%s)\n", len(r.groups), r.total, noun, r.s.groupFilterText(), r.s.testsNote())
		}
		fmt.Fprintln(w, r.skipped.line())
	})
	md := Markdown{}
	md.Headline, md.Notes = headlineAndNotes(lines)
	header := []string{"File"}
	if byClass {
		header = append(header, "Class")
	}
	lead := len(header)
	header = append(header, "Funcs", "NLOC", "ΣCCN", "CCN avg", "CCN max", "Σcog", "Cog avg", "Cog max", "Cmt%", "Over", "Excess")
	if r.s.churn != nil {
		header = append(header, "Commits", "Hot")
	}
	sec := MarkdownSection{Header: header, Numeric: numericFrom(len(header), lead)}
	for _, g := range r.groups {
		cells := []string{g.File}
		if byClass {
			cells = append(cells, g.Class)
		}
		cells = append(cells, itoa(g.Functions), itoa(g.NLOC),
			itoa(g.CCNSum), fmt.Sprintf("%.1f", g.CCNAvg), itoa(g.CCNMax),
			itoa(g.CogSum), fmt.Sprintf("%.1f", g.CogAvg), itoa(g.CogMax),
			itoa(complexity.DensityPercent(g.CommentDensity)), itoa(g.OverThreshold), itoa(g.Excess))
		if r.s.churn != nil {
			commits, hot := r.s.hot(g)
			cells = append(cells, itoa(commits), itoa(hot))
		}
		sec.Rows = append(sec.Rows, cells)
	}
	md.Sections = []MarkdownSection{sec}
	return md
}

// briefMarkdown is --brief --md (#2963): the brief's columns as a table,
// with no headline or notes, like the text brief.
func briefMarkdown(r groupReport) Markdown {
	byClass := r.by == complexity.ByClass
	header := []string{"File"}
	if byClass {
		header = append(header, "Class")
	}
	lead := len(header)
	header = append(header, "Funcs", "NLOC", "ΣCCN", "Σcog", "Max CCN/cog", "Cmt%")
	if r.s.churn != nil {
		header = append(header, "Commits", "Hot")
	}
	sec := MarkdownSection{Header: header, Numeric: numericFrom(len(header), lead)}
	for _, g := range r.groups {
		cells := []string{g.File}
		if byClass {
			cells = append(cells, g.Class)
		}
		cells = append(cells, itoa(g.Functions), itoa(g.NLOC), itoa(g.CCNSum), itoa(g.CogSum),
			itoa(g.CCNMax)+"/"+itoa(g.CogMax), itoa(complexity.DensityPercent(g.CommentDensity)))
		if r.s.churn != nil {
			commits, hot := r.s.hot(g)
			cells = append(cells, itoa(commits), itoa(hot))
		}
		sec.Rows = append(sec.Rows, cells)
	}
	return Markdown{Sections: []MarkdownSection{sec}}
}

// diffMarkdown is --diff --md (#2963), the pull-request comment: the
// verdict, the compared revisions and totals, then the changed / added /
// removed tables — or the per-file table under --by file.
func diffMarkdown(r diffReport) Markdown {
	d := r.diff
	md := Markdown{Headline: fmt.Sprintf("Complexity: %s (excess %s, Σcog %s, ΣCCN %s)", verdictText(d.Verdict),
		signed(d.DeltaExcess()), signed(d.DeltaCognitive()), signed(d.DeltaCCN()))}
	if r.headSha != "" {
		md.Notes = append(md.Notes, fmt.Sprintf("%s (%s) → %s (%s)", r.rev, shortSha(r.sha), r.headRev, shortSha(r.headSha)))
	} else {
		md.Notes = append(md.Notes, fmt.Sprintf("%s (%s) → working tree", r.rev, shortSha(r.sha)))
	}
	b, h := d.Base, d.Head
	md.Notes = append(md.Notes, fmt.Sprintf("functions %d→%d · ΣCCN %s · Σcog %s · excess %s",
		b.Functions, h.Functions, fromTo(b.CCNSum, h.CCNSum), fromTo(b.CogSum, h.CogSum), fromTo(b.Excess, h.Excess)))
	changed, added, removed := r.sections()
	if len(changed)+len(added)+len(removed) == 0 {
		md.Notes = append(md.Notes, "no function changed its complexity")
	}
	if r.s.args.NoTests {
		md.Notes = append(md.Notes, "tests hidden (--no-tests)")
	}
	if l := r.skipped.line(); l != "" {
		md.Notes = append(md.Notes, l)
	}
	if r.by == complexity.ByFile {
		if files := r.files(); len(files) > 0 {
			header := []string{"File", "Changed", "Added", "Removed", "ΣCCN", "Σcog", "NLOC", "Excess"}
			sec := MarkdownSection{Title: "Files", Header: header, Numeric: numericFrom(len(header), 1)}
			for _, fc := range files {
				fb, fa := fc.Before, fc.After
				sec.Rows = append(sec.Rows, []string{fc.file, itoa(fc.Changed), itoa(fc.Added), itoa(fc.Removed),
					fromTo(fb.CCNSum, fa.CCNSum), fromTo(fb.CogSum, fa.CogSum),
					itoa(fb.NLOC) + "→" + itoa(fa.NLOC), fromTo(fb.Excess, fa.Excess)})
			}
			md.Sections = append(md.Sections, sec)
		}
		return md
	}
	header := []string{"File", "Function", "Line", "CCN", "Cog", "NLOC", "Cmt%"}
	if len(changed) > 0 {
		sec := MarkdownSection{Title: "Changed", Header: header, Numeric: numericFrom(len(header), 2)}
		for _, c := range changed {
			cb, ca := c.ch.Before, c.ch.After
			sec.Rows = append(sec.Rows, []string{c.file, ca.Name, itoa(ca.Line + 1),
				fromTo(cb.CCN, ca.CCN), fromTo(cb.Cognitive, ca.Cognitive), fromTo(cb.NLOC, ca.NLOC),
				fromTo(complexity.DensityPercent(cb.CommentDensity), complexity.DensityPercent(ca.CommentDensity))})
		}
		md.Sections = append(md.Sections, sec)
	}
	section := func(title string, rows []row) {
		if len(rows) == 0 {
			return
		}
		sec := MarkdownSection{Title: title, Header: header, Numeric: numericFrom(len(header), 2)}
		for _, rw := range rows {
			f := rw.fn
			sec.Rows = append(sec.Rows, []string{rw.file, f.Name, itoa(f.Line + 1), itoa(f.CCN), itoa(f.Cognitive), itoa(f.NLOC),
				itoa(complexity.DensityPercent(f.CommentDensity))})
		}
		md.Sections = append(md.Sections, sec)
	}
	section("Added", added)
	section("Removed", removed)
	return md
}

// trendMarkdown is --trend --md: the window, one row per commit oldest
// first with the sums and their Δ, and the totals line.
func trendMarkdown(r trendReport) Markdown {
	pts := r.trend.Points
	md := Markdown{Headline: fmt.Sprintf("Complexity trend: last %d %s (first parent)", len(pts), plural(len(pts), "commit", "commits"))}
	if len(pts) == 0 {
		md.Notes = []string{"no commit to measure"}
		return md
	}
	lines := trailerLines(func(w io.Writer) { writeTrendText(w, r) })
	for _, l := range lines {
		if strings.HasPrefix(l, "totals: ") || l == "tests hidden (--no-tests)" {
			md.Notes = append(md.Notes, l)
		}
	}
	header := []string{"Commit", "Date", "Subject", "Funcs", "ΣCCN", "Σcog", "Excess", "Over"}
	numeric := numericFrom(len(header), 3)
	sec := MarkdownSection{Header: header, Numeric: numeric}
	for i, p := range pts {
		s := p.Summary
		dl := trendDelta(pts, i)
		cell := func(v, delta int) string {
			if i == 0 {
				return itoa(v)
			}
			return itoa(v) + " (" + signed(delta) + ")"
		}
		sec.Rows = append(sec.Rows, []string{p.Short, p.Time.Format("2006-01-02"), p.Subject, itoa(s.Functions),
			cell(s.CCNSum, dl.CCN), cell(s.CogSum, dl.Cognitive), cell(s.Excess, dl.Excess), cell(s.OverThreshold, dl.OverThreshold)})
	}
	md.Sections = []MarkdownSection{sec}
	return md
}
