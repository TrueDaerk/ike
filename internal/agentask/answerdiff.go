package agentask

import (
	"strconv"
	"strings"

	"ike/internal/agenttrace"
)

// answerdiff.go reads the unified diff out of a simplify answer (#3009) so
// the app can apply it to the open buffer. Three shapes are understood: the
// first ```diff or ```patch fence, else a bare "--- a/… / +++ b/…" block in
// the text. Inside the diff, "@@" lines start hunks; ' ', '-' and '+' lines
// are its rows, a blank line counts as an empty context row (models drop
// the leading space), "\ No newline" notes and git's "diff" / "index" lines
// are skipped. The hunk header's counts are informational for the apply —
// it matches the before lines by content — but they, and "..." placeholder
// rows, expose a diff that leaves lines out (#3050): Elided names the first
// such hunk, and RunSimplify asks once more for the complete diff.

// AnswerDiff is the diff an answer carries, one entry per file section.
type AnswerDiff struct {
	Files []FileDiff
	// Elided says what the diff leaves out ("" when nothing): a row that
	// is a "..." placeholder standing in for lines, or a hunk clearly
	// shorter than its header counts (see hunkElision).
	Elided string
}

// FileDiff is one file's section: Path is the "+++" side with a leading
// "b/" stripped ("" when the diff names no file), Hunks its hunks in order.
type FileDiff struct {
	Path  string
	Hunks []agenttrace.DiffHunk
}

// Hunks counts the hunks of every file.
func (d AnswerDiff) Hunks() int {
	n := 0
	for _, f := range d.Files {
		n += len(f.Hunks)
	}
	return n
}

// ParseAnswerDiff extracts the diff of answer; false when there is none
// with at least one hunk.
func ParseAnswerDiff(answer string) (AnswerDiff, bool) {
	block, ok := diffBlock(answer)
	if !ok {
		return AnswerDiff{}, false
	}
	d := parseUnified(block)
	return d, d.Hunks() > 0
}

// HasDiff reports an answer ParseAnswerDiff finds a diff in.
func HasDiff(answer string) bool {
	_, ok := ParseAnswerDiff(answer)
	return ok
}

// diffBlock is the diff's lines: the first ```diff / ```patch fence's body,
// else the bare block from the first "--- " line followed by "+++ ".
func diffBlock(answer string) ([]string, bool) {
	lines := strings.Split(strings.ReplaceAll(answer, "\r\n", "\n"), "\n")
	for i, l := range lines {
		info := strings.TrimSpace(l)
		if !strings.HasPrefix(info, "```") {
			continue
		}
		lang := strings.ToLower(strings.TrimSpace(strings.TrimLeft(info, "`")))
		if f := strings.Fields(lang); len(f) > 0 {
			lang = f[0]
		}
		if lang != "diff" && lang != "patch" {
			continue
		}
		var body []string
		for _, b := range lines[i+1:] {
			if strings.HasPrefix(strings.TrimSpace(b), "```") {
				break
			}
			body = append(body, b)
		}
		return body, true
	}
	for i := 0; i+1 < len(lines); i++ {
		if strings.HasPrefix(lines[i], "--- ") && strings.HasPrefix(lines[i+1], "+++ ") {
			end := i
			for end < len(lines) && isUnifiedLine(lines[end]) {
				end++
			}
			return lines[i:end], true
		}
	}
	return nil, false
}

// isUnifiedLine reports a line that belongs to a bare unified diff.
func isUnifiedLine(l string) bool {
	if l == "" {
		return true
	}
	switch l[0] {
	case ' ', '+', '-', '@', '\\':
		return true
	}
	return strings.HasPrefix(l, "diff ") || strings.HasPrefix(l, "index ")
}

// parseUnified reads the file sections and hunks of a diff block.
func parseUnified(lines []string) AnswerDiff {
	// Blank lines after the last row are the fence's or the text's, not
	// empty context rows.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	var d AnswerDiff
	var file *FileDiff
	var hunk *agenttrace.DiffHunk
	flush := func() {
		if hunk != nil && file != nil && len(hunk.Lines) > 0 {
			file.Hunks = append(file.Hunks, *hunk)
			if d.Elided == "" {
				d.Elided = hunkElision(*hunk, d.Hunks())
			}
		}
		hunk = nil
	}
	newFile := func(path string) {
		flush()
		d.Files = append(d.Files, FileDiff{Path: path})
		file = &d.Files[len(d.Files)-1]
	}
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		switch {
		case strings.HasPrefix(l, "--- ") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "+++ "):
			newFile(diffPath(lines[i+1][4:]))
			i++
		case strings.HasPrefix(l, "@@"):
			flush()
			if file == nil {
				newFile("")
			}
			h := parseHunkHeader(l)
			hunk = &h
		case hunk == nil:
			// "diff --git", "index …" and prose before the first hunk.
		case l == "":
			hunk.Lines = append(hunk.Lines, " ")
		case l[0] == ' ' || l[0] == '-' || l[0] == '+':
			hunk.Lines = append(hunk.Lines, l)
		case l[0] == '\\':
			// "\ No newline at end of file"
		case strings.HasPrefix(l, "diff ") || strings.HasPrefix(l, "index "):
			flush()
		default:
			flush() // prose: the diff is over
		}
	}
	flush()
	// A section without hunks says nothing.
	out := d.Files[:0]
	for _, f := range d.Files {
		if len(f.Hunks) > 0 {
			out = append(out, f)
		}
	}
	d.Files = out
	return d
}

// hunkElision says what hunk number n leaves out, "" when nothing: a row
// whose text is a placeholder for lines (hasElisionMarker), or — models
// miscount small hunks freely, so only clearly — before or after rows fewer
// than the header counts by at least ten lines and more than a tenth.
func hunkElision(h agenttrace.DiffHunk, n int) string {
	hunk := "hunk " + strconv.Itoa(n)
	for _, l := range h.Lines {
		if hasElisionMarker(l[1:]) {
			return hunk + " stands a placeholder in for lines (" + strconv.Quote(strings.TrimSpace(l[1:])) + ")"
		}
	}
	before, after := 0, 0
	for _, l := range h.Lines {
		switch l[0] {
		case ' ':
			before, after = before+1, after+1
		case '-':
			before++
		case '+':
			after++
		}
	}
	if short(before, h.OldLines) {
		return hunk + " holds " + strconv.Itoa(before) + " of the " + strconv.Itoa(h.OldLines) + " before lines its header counts"
	}
	if short(after, h.NewLines) {
		return hunk + " holds " + strconv.Itoa(after) + " of the " + strconv.Itoa(h.NewLines) + " after lines its header counts"
	}
	return ""
}

// short reports got rows clearly fewer than the want a header counts.
func short(got, want int) bool {
	missing := want - got
	return missing >= 10 && missing*10 > want
}

// hasElisionMarker reports a row's text that stands in for left-out lines:
// after an optional comment opener, "..." / "…" / "[...]" alone, or "..."
// followed by a space or a parenthesis ("... (unchanged)", "... rest of
// the switch"). A spread like "...rest" is code, not a marker.
func hasElisionMarker(text string) bool {
	t := strings.TrimSpace(text)
	for _, c := range []string{"//", "#", "/*", "--", "*"} {
		if strings.HasPrefix(t, c) {
			t = strings.TrimSpace(t[len(c):])
			break
		}
	}
	t = strings.ReplaceAll(t, "…", "...")
	switch t {
	case "...", "[...]", "(...)":
		return true
	}
	return strings.HasPrefix(t, "... ") || strings.HasPrefix(t, "...(") || strings.HasPrefix(t, "[...] ")
}

// diffPath is a "+++" header's path: the timestamp after a tab and git's
// "b/" prefix dropped; /dev/null is "".
func diffPath(s string) string {
	if i := strings.IndexByte(s, '\t'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if s == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(s, "b/")
}

// parseHunkHeader reads "@@ -a,b +c,d @@"; a header it cannot read yields
// a hunk at an unknown position, which the apply does not need.
func parseHunkHeader(l string) agenttrace.DiffHunk {
	var h agenttrace.DiffHunk
	f := strings.Fields(l)
	for _, p := range f[1:] {
		switch {
		case strings.HasPrefix(p, "-"):
			h.OldStart, h.OldLines = hunkRange(p[1:])
		case strings.HasPrefix(p, "+"):
			h.NewStart, h.NewLines = hunkRange(p[1:])
		case p == "@@":
			return h
		}
	}
	return h
}

// hunkRange reads "a,b" (or "a", one line).
func hunkRange(s string) (int, int) {
	start, count, ok := strings.Cut(s, ",")
	a, _ := strconv.Atoi(start)
	if !ok {
		return a, 1
	}
	b, _ := strconv.Atoi(count)
	return a, b
}
