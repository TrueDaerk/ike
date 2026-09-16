package editor

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"ike/internal/editor/buffer"
	"ike/internal/editor/excmd"
	"ike/internal/editor/history"
)

// lastSubstitute remembers the previous :substitute so a bare ":s" (and, later,
// ":&") can repeat it.
type lastSubstitute struct {
	pattern string
	regex   bool
	repl    string
	flags   string
	valid   bool
}

// substitute runs a parsed ":[range]s/pat/repl/flags" command against the
// resolved line range. It reuses the editor's search-regex convention (literal
// by default, `\v` prefix for regex) for the pattern, supports the g/i/I/n
// flags and vim-style capture-group replacements (`&`, `\0`-`\9`), applies every
// change as one undo unit, and reports the outcome on the command-line message
// row.
func (m Model) substitute(cmd excmd.Command) Model {
	start, end, rerr := cmd.Range.Resolve(m.exResolver(), m.cursor.Line)
	if rerr != "" {
		m.cmdMsg = "E: " + rerr
		return m
	}

	pat, repl, flags, hasBody, perr := parseSub(cmd.Args)
	if perr != "" {
		m.cmdMsg = "E: " + perr
		return m
	}

	var regex bool
	if hasBody {
		if pat == "" {
			// Empty pattern reuses the last search, else the last substitute.
			switch {
			case !m.query.Empty():
				pat, regex = m.query.Pattern, m.query.Regex
			case m.lastSub.valid:
				pat, regex = m.lastSub.pattern, m.lastSub.regex
			default:
				m.cmdMsg = "E: no previous pattern"
				return m
			}
		} else {
			if strings.HasPrefix(pat, `\v`) {
				pat, regex = pat[2:], true
			}
			if !regex {
				// "\n" / "\r" written on the ":s" line is a line break (#2600).
				// This is also how the find/replace panel carries a break
				// through the single-line ex round trip (buildSubLine escapes
				// it, and we undo that here).
				pat = unescapeBreaks(pat)
			}
		}
	} else {
		// Bare ":s" repeats the last substitute (pattern, replacement, flags).
		if !m.lastSub.valid {
			m.cmdMsg = "E: no previous substitute"
			return m
		}
		pat, repl, flags, regex = m.lastSub.pattern, m.lastSub.repl, m.lastSub.flags, m.lastSub.regex
	}

	global, ci, countOnly, confirm, ferr := parseSubFlags(flags)
	if ferr != "" {
		m.cmdMsg = "E: " + ferr
		return m
	}

	// A substitution crosses line boundaries when the pattern can match a line
	// break or the replacement inserts one (#2600). Both break the per-line
	// assumption the fast path rests on, so they take their own route — and the
	// pattern is compiled for it.
	spanning := subSpansLines(pat, regex, repl)

	re, err := compileSub(pat, regex, ci, spanning)
	if err != nil {
		m.cmdMsg = "E: invalid pattern: " + err.Error()
		return m
	}
	if hasBody {
		m.lastSub = lastSubstitute{pattern: pat, regex: regex, repl: repl, flags: flags, valid: true}
	}

	// reach is how far past the range's last line a spanning match may run.
	reach := subReach(pat, regex)

	// The "c" flag drives an interactive match-by-match confirmation instead of
	// a one-shot batch replace (the "n" count-only flag takes precedence).
	if confirm && !countOnly {
		return m.beginSubstituteConfirm(re, repl, global, spanning, reach, start, end, pat)
	}

	if spanning {
		return m.substituteSpanning(re, repl, global, countOnly, reach, start, end, pat)
	}

	// Collect per-line replacements from the current text first; replacements
	// never span lines here, so line indices stay stable while we apply them.
	type change struct {
		line   int
		text   string
		oldLen int
	}
	var changes []change
	totalSubs, lastLine := 0, -1
	for i := start; i <= end; i++ {
		orig := m.buf.Line(i)
		newLine, n := substituteLine(re, orig, repl, global)
		if n == 0 {
			continue
		}
		totalSubs += n
		lastLine = i
		changes = append(changes, change{line: i, text: newLine, oldLen: utf8.RuneCountInString(orig)})
	}

	if totalSubs == 0 {
		m.cmdMsg = "E: pattern not found: " + pat
		return m
	}
	linesChanged := len(changes)
	if countOnly {
		m.cmdMsg = fmt.Sprintf("%d match%s on %d line%s", totalSubs, plural(totalSubs, "es"), linesChanged, plural(linesChanged, "s"))
		return m
	}

	m.mutate(func(rec *history.Recorder) buffer.Position {
		for _, ch := range changes {
			r := buffer.Range{
				Start: buffer.Position{Line: ch.line, Col: 0},
				End:   buffer.Position{Line: ch.line, Col: ch.oldLen},
			}
			rec.Apply(buffer.Edit{Range: r, Text: ch.text})
		}
		return buffer.Position{Line: lastLine, Col: 0}
	})
	m.cmdMsg = fmt.Sprintf("%d substitution%s on %d line%s", totalSubs, plural(totalSubs, "s"), linesChanged, plural(linesChanged, "s"))
	return m
}

// substituteSpanning is the batch replace for a substitution that crosses line
// boundaries (#2600): the matches are collected as buffer ranges over the
// joined range text, then applied bottom-up inside one recorder — so an edit
// that adds or removes lines can never invalidate the position of an edit that
// has not run yet, and the whole run stays one undo unit like any other :s.
func (m Model) substituteSpanning(re *regexp.Regexp, repl string, global, countOnly bool, reach, start, end int, pat string) Model {
	hits := collectSubMatches(m.buf, re, global, true, reach, start, end)
	if len(hits) == 0 {
		m.cmdMsg = "E: pattern not found: " + pat
		return m
	}
	lines := map[int]bool{}
	for _, h := range hits {
		for l := h.start.Line; l <= h.end.Line; l++ {
			lines[l] = true
		}
	}
	if countOnly {
		m.cmdMsg = fmt.Sprintf("%d match%s on %d line%s", len(hits), plural(len(hits), "es"), len(lines), plural(len(lines), "s"))
		return m
	}

	edits := make([]buffer.Edit, len(hits))
	for i, h := range hits {
		edits[i] = buffer.Edit{
			Range: buffer.Range{Start: h.start, End: h.end},
			Text:  expandRepl(repl, h.groups),
		}
	}
	// Where the cursor lands: the last replacement's final line, once the line
	// shift every earlier replacement introduces is added in.
	last := edits[len(edits)-1]
	shift := 0
	for _, e := range edits[:len(edits)-1] {
		shift += strings.Count(e.Text, "\n") - (e.Range.End.Line - e.Range.Start.Line)
	}
	cursor := buffer.Position{Line: last.Range.Start.Line + shift + strings.Count(last.Text, "\n"), Col: 0}

	m.mutate(func(rec *history.Recorder) buffer.Position {
		for i := len(edits) - 1; i >= 0; i-- {
			rec.Apply(edits[i])
		}
		return m.buf.ClampCursor(cursor)
	})
	m.cmdMsg = fmt.Sprintf("%d substitution%s on %d line%s", len(hits), plural(len(hits), "s"), len(lines), plural(len(lines), "s"))
	return m
}

// subMatch is one match of a substitution: the buffer range it covers (which
// may span lines, #2600) and the capture-group strings for the replacement.
type subMatch struct {
	start, end buffer.Position
	groups     []string
}

// collectSubMatches finds every match of re in lines [start, end] in reading
// order. spanning selects the joined-text scan, which is the only one that can
// see a match crossing a line boundary; without it each line is scanned on its
// own, exactly as the substitute engine always has. Without the "g" flag only
// the first match per line is taken, matching vim. Zero-width matches are
// skipped.
//
// reach is how many lines past end the scan may read so a match that *starts*
// inside the range can finish outside it — ":s/foo\nbar/x/" on the foo line is
// about the pair, not about where the range happens to stop. Matches starting
// past end are dropped.
func collectSubMatches(b *buffer.Buffer, re *regexp.Regexp, global, spanning bool, reach, start, end int) []subMatch {
	var out []subMatch
	if !spanning {
		for i := start; i <= end; i++ {
			line := b.Line(i)
			for _, loc := range re.FindAllStringSubmatchIndex(line, -1) {
				if loc[0] == loc[1] {
					continue
				}
				out = append(out, subMatch{
					start:  buffer.Position{Line: i, Col: byteToRune(line, loc[0])},
					end:    buffer.Position{Line: i, Col: byteToRune(line, loc[1])},
					groups: submatchStrings(line, loc),
				})
				if !global {
					break
				}
			}
		}
		return out
	}
	scanEnd := end + reach
	if last := b.LineCount() - 1; scanEnd > last {
		scanEnd = last
	}
	text, starts := joinRange(b, start, scanEnd)
	seen := map[int]bool{}
	for _, loc := range re.FindAllStringSubmatchIndex(text, -1) {
		if loc[0] == loc[1] {
			continue
		}
		s := rangePos(text, starts, start, loc[0])
		if s.Line > end {
			continue // the match starts outside the range; only its tail may
		}
		if !global {
			if seen[s.Line] {
				continue
			}
			seen[s.Line] = true
		}
		out = append(out, subMatch{
			start:  s,
			end:    rangePos(text, starts, start, loc[1]),
			groups: submatchStrings(text, loc),
		})
	}
	return out
}

// joinRange joins lines [start, end] with "\n" and returns the byte offset each
// line begins at inside the result.
func joinRange(b *buffer.Buffer, start, end int) (string, []int) {
	lines := make([]string, 0, end-start+1)
	starts := make([]int, 0, end-start+1)
	n := 0
	for i := start; i <= end; i++ {
		line := b.Line(i)
		lines = append(lines, line)
		starts = append(starts, n)
		n += len(line) + 1 // + the joining "\n"
	}
	return strings.Join(lines, "\n"), starts
}

// rangePos maps a byte offset inside a joined range back to a buffer position.
// An offset landing on a joining newline belongs to the line before it, at its
// end — where a match running to the end of a line should report.
func rangePos(text string, starts []int, first, byteOff int) buffer.Position {
	i := sort.Search(len(starts), func(k int) bool { return starts[k] > byteOff }) - 1
	if i < 0 {
		i = 0
	}
	return buffer.Position{Line: first + i, Col: byteToRune(text[starts[i]:], byteOff-starts[i])}
}

// substituteLine replaces matches of re in line with repl (all matches when
// global, otherwise the first). It returns the rewritten line and the number of
// replacements. Zero-width matches are skipped, mirroring the search layer.
func substituteLine(re *regexp.Regexp, line, repl string, global bool) (string, int) {
	locs := re.FindAllStringSubmatchIndex(line, -1)
	if len(locs) == 0 {
		return line, 0
	}
	var b strings.Builder
	last, count := 0, 0
	for _, m := range locs {
		if m[0] == m[1] {
			continue // skip empty matches
		}
		if !global && count >= 1 {
			break
		}
		b.WriteString(line[last:m[0]])
		b.WriteString(expandRepl(repl, submatchStrings(line, m)))
		last = m[1]
		count++
	}
	if count == 0 {
		return line, 0
	}
	b.WriteString(line[last:])
	return b.String(), count
}

// submatchStrings turns a FindAllStringSubmatchIndex match into group strings:
// index 0 is the whole match, 1.. are the capture groups ("" when unmatched).
func submatchStrings(line string, m []int) []string {
	groups := make([]string, len(m)/2)
	for i := range groups {
		if m[2*i] >= 0 {
			groups[i] = line[m[2*i]:m[2*i+1]]
		}
	}
	return groups
}

// expandRepl expands a vim-style replacement: `&` and `\0` are the whole match,
// `\1`-`\9` the capture groups, `\&` a literal `&`, `\\` a literal backslash,
// `\n` and `\r` a line break (#2600 — vim spells it `\r`, and `\n` joins it
// here because that is what the find/replace panel's ex hand-off writes);
// any other `\x` contributes x. `$` is literal (no Go `$name` expansion).
func expandRepl(repl string, groups []string) string {
	var b strings.Builder
	for i := 0; i < len(repl); i++ {
		c := repl[i]
		switch {
		case c == '&':
			b.WriteString(groups[0])
		case c == '\\' && i+1 < len(repl):
			n := repl[i+1]
			i++
			switch {
			case n >= '0' && n <= '9':
				if g := int(n - '0'); g < len(groups) {
					b.WriteString(groups[g])
				}
			case n == '&':
				b.WriteByte('&')
			case n == '\\':
				b.WriteByte('\\')
			case n == 'n', n == 'r':
				b.WriteByte('\n')
			default:
				b.WriteByte(n)
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// subSpansLines reports whether a substitution can cross a line boundary: the
// pattern can match a newline, or the replacement inserts one (#2600). It is
// the switch between the per-line engine and the joined-range one.
func subSpansLines(pat string, regex bool, repl string) bool {
	if strings.Contains(pat, "\n") || strings.Contains(repl, "\n") {
		return true
	}
	if escapesBreak(repl) {
		return true
	}
	// A literal pattern's "\n" was already turned into a real break above, so
	// only a regex still carries one as an escape. "(?s)" makes "." match a
	// newline, which is the other way a regex reaches across a line.
	return regex && (escapesBreak(pat) || strings.Contains(pat, "(?s"))
}

// escapesBreak reports whether s contains a `\n` or `\r` escape, skipping over
// `\\` so an escaped backslash never reads as the start of one.
func escapesBreak(s string) bool { return countBreakEscapes(s) > 0 }

// countBreakEscapes counts the `\n` / `\r` escapes in s.
func countBreakEscapes(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			continue
		}
		if s[i+1] == 'n' || s[i+1] == 'r' {
			n++
		}
		i++ // the escaped byte is consumed, `\\n` is a backslash then an n
	}
	return n
}

// subReach is how many lines past a range's end a match of pat may still run:
// the line breaks the pattern itself holds. A literal pattern carries them as
// real breaks by now, a regex may still spell them as escapes. A pattern whose
// match can stretch further than it is written (a regex repeating a break)
// simply stops at the range's reach, which is the bound that keeps the scan
// proportional to the range.
func subReach(pat string, regex bool) int {
	n := strings.Count(pat, "\n")
	if regex {
		n += countBreakEscapes(pat)
	}
	return n
}

// unescapeBreaks turns the `\n` / `\r` escapes of a *literal* pattern into real
// line breaks (#2600). Every other escape is left exactly as written — `\d`
// stays a backslash and a d, which is how a literal search has always spelled
// it — and `\\` is stepped over whole, so an escaped backslash can never pair
// with a following n into a break.
func unescapeBreaks(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		switch s[i+1] {
		case 'n', 'r':
			b.WriteByte('\n')
			i++
		case '\\':
			b.WriteString(`\\`)
			i++
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// parseSub splits an ":s" argument "<d>pat<d>repl<d>flags" (d = any non-alnum,
// non-backslash delimiter) into its parts, unescaping "\<d>" in pat and repl.
// hasBody is false for a bare ":s" (repeat the last substitute).
func parseSub(args string) (pat, repl, flags string, hasBody bool, errMsg string) {
	if args == "" {
		return "", "", "", false, ""
	}
	d := args[0]
	if isAlnum(d) || d == '\\' {
		return "", "", "", false, "invalid substitute delimiter: " + string(d)
	}
	s := args[1:]
	pat, s = excmd.ScanDelim(s, d)
	repl, s = excmd.ScanDelim(s, d)
	return pat, repl, strings.TrimSpace(s), true, ""
}

// parseSubFlags reads the g/i/I/n/c flag letters; an unknown letter is an error.
func parseSubFlags(flags string) (global, ci, countOnly, confirm bool, errMsg string) {
	for _, r := range flags {
		switch r {
		case 'g':
			global = true
		case 'i':
			ci = true
		case 'I':
			ci = false
		case 'n':
			countOnly = true
		case 'c':
			confirm = true
		case ' ', '\t':
		default:
			return false, false, false, false, "unknown flag: " + string(r)
		}
	}
	return global, ci, countOnly, confirm, ""
}

// compileSub builds the substitution regexp from the search-layer convention:
// a literal pattern is quoted, `i` prepends the case-insensitive flag.
//
// spanning marks the joined-range engine (#2600), which runs the expression
// over several lines at once: there `^` and `$` must keep meaning line start
// and line end, which per-line matching gave them for free, so the multi-line
// flag goes on.
func compileSub(pattern string, regex, ci, spanning bool) (*regexp.Regexp, error) {
	expr := pattern
	if !regex {
		expr = regexp.QuoteMeta(pattern)
	}
	switch {
	case ci && spanning:
		expr = "(?mi)" + expr
	case ci:
		expr = "(?i)" + expr
	case spanning:
		expr = "(?m)" + expr
	}
	return regexp.Compile(expr)
}

func isAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// plural returns suffix when n != 1, else "".
func plural(n int, suffix string) string {
	if n == 1 {
		return ""
	}
	return suffix
}
