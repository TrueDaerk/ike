package complexitypanel

// candidates.go is the extraction-candidate half of the breakdown (Roadmap
// 0551, #2948): under the expanded function's site rows follow its
// complexity.Candidates, one indented row each — `extract 120–160 (if):
// −18 cog · new fn ccn 9 cog 11 · rest ccn 12 cog 14` — best saving
// first. `enter` on one opens the file with the range selected (visual
// line selection, caret on the header) so the language server's Extract
// function is a chord away; `y` copies the line. A candidate row is a
// child row like a site row: Fn, Path, Rel, Lang and Change are the
// parent's, Parent its index.

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
)

// candidateRows builds the candidate child rows of the function row parent
// at index parentIdx, in the engine's order (saving descending, then
// range).
func candidateRows(parent Row, parentIdx int) []Row {
	out := make([]Row, len(parent.Fn.Candidates))
	for i := range parent.Fn.Candidates {
		out[i] = Row{Path: parent.Path, Rel: parent.Rel, Lang: parent.Lang, Fn: parent.Fn,
			Change: parent.Change, Test: parent.Test, Cand: &parent.Fn.Candidates[i], Parent: parentIdx}
	}
	return out
}

// candCell renders a candidate row's text for column c: the proposal in
// the function column, the header line in the line column; the other
// columns stay blank.
func (r Row) candCell(c Column) string {
	switch c {
	case ColFunc:
		return siteIndent + "extract " + candRange(r.Cand) + " " + candText(r.Cand)
	case ColLine:
		return strconv.Itoa(r.Cand.Line + 1)
	}
	return ""
}

// candRange is the candidate's 1-based line range: "120–160", "42" for a
// one-line node.
func candRange(c *complexity.Candidate) string {
	if c.EndLine == c.Line {
		return strconv.Itoa(c.Line + 1)
	}
	return strconv.Itoa(c.Line+1) + "–" + strconv.Itoa(c.EndLine+1)
}

// candText is the proposal without its range: "(if): −18 cog · new fn ccn
// 9 cog 11 · rest ccn 12 cog 14".
func candText(c *complexity.Candidate) string {
	return "(" + c.Kind + "): −" + strconv.Itoa(c.CogSaving) + " cog" +
		" · new fn ccn " + strconv.Itoa(c.NewCCN) + " cog " + strconv.Itoa(c.NewCog) +
		" · rest ccn " + strconv.Itoa(c.RestCCN) + " cog " + strconv.Itoa(c.RestCog)
}

// candKey is the cursor-keeping key of a candidate row under its parent.
func (r Row) candKey() string {
	return r.expandKey() + "\x00c\x00" + strconv.Itoa(r.Cand.Line) + "\x00" + strconv.Itoa(r.Cand.EndLine) + "\x00" + r.Cand.Kind
}

// candCopyText renders a candidate row as "path:120–160: extract (if): −18
// cog · new fn ccn 9 cog 11 · rest ccn 12 cog 14".
func candCopyText(r Row) string {
	return r.Rel + ":" + candRange(r.Cand) + ": extract " + candText(r.Cand)
}

// openCandidate asks the root model to open the file with the candidate's
// range selected.
func (m *Model) openCandidate(r Row) tea.Cmd {
	msg := OpenLocationMsg{Path: r.Path, Line: r.Cand.Line, EndLine: r.Cand.EndLine}
	return func() tea.Msg { return msg }
}
