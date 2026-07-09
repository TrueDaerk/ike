package editor

import (
	"fmt"
	"regexp"
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
		} else if strings.HasPrefix(pat, `\v`) {
			pat, regex = pat[2:], true
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

	re, err := compileSub(pat, regex, ci)
	if err != nil {
		m.cmdMsg = "E: invalid pattern: " + err.Error()
		return m
	}
	if hasBody {
		m.lastSub = lastSubstitute{pattern: pat, regex: regex, repl: repl, flags: flags, valid: true}
	}

	// The "c" flag drives an interactive match-by-match confirmation instead of
	// a one-shot batch replace (the "n" count-only flag takes precedence).
	if confirm && !countOnly {
		return m.beginSubstituteConfirm(re, repl, global, start, end, pat)
	}

	// Collect per-line replacements from the current text first; replacements
	// never span lines, so line indices stay stable while we apply them.
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
// `\1`-`\9` the capture groups, `\&` a literal `&`, `\\` a literal backslash;
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
			default:
				b.WriteByte(n)
			}
		default:
			b.WriteByte(c)
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
	pat, s = scanDelim(s, d)
	repl, s = scanDelim(s, d)
	return pat, repl, strings.TrimSpace(s), true, ""
}

// scanDelim reads up to the first unescaped delim (or end of input), turning
// "\<delim>" into a literal delim and keeping every other backslash pair intact
// so the regex / replacement layers see them. It returns the text after delim.
func scanDelim(s string, delim byte) (string, string) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			if s[i+1] == delim {
				b.WriteByte(delim)
			} else {
				b.WriteByte(c)
				b.WriteByte(s[i+1])
			}
			i++
			continue
		}
		if c == delim {
			return b.String(), s[i+1:]
		}
		b.WriteByte(c)
	}
	return b.String(), ""
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
func compileSub(pattern string, regex, ci bool) (*regexp.Regexp, error) {
	expr := pattern
	if !regex {
		expr = regexp.QuoteMeta(pattern)
	}
	if ci {
		expr = "(?i)" + expr
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
