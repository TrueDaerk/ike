package cronhint

import (
	"strconv"
	"strings"

	"ike/internal/lang"
	"ike/internal/linescan"
)

// spans.go is the context half of the cron hint (#1624): where in a buffer a
// run of characters is a cron expression rather than five arbitrary tokens.
// Three contexts cover what people actually read:
//
//   - Crontab lines — the schedule is positional, the first five fields of a
//     non-comment, non-assignment line, plus the @-shorthands.
//   - CI YAML — a `cron:` or `schedule:` key (GitHub Actions, GitLab CI)
//     names its value a schedule, so no shape guessing is needed.
//   - Quoted scalars in config formats (YAML, JSON, TOML) — accepted only
//     when the text both parses and carries a cron-specific character, so a
//     quoted list of numbers ("1 2 3 4 5") is never mistaken for a schedule.
//
// Every producer emits the same stand-in shape: the span covers the raw
// expression and Replace repeats it with the hint appended, so the hint reads
// as an annotation after the expression and the raw text is one caret motion
// away (#1594's positional reveal).

// hintSpan builds the stand-in span for the expression at rune columns
// [start, end) of line li. It reports false when the text is not a schedule
// this package can describe.
func hintSpan(li, start, end int, expr string) (lang.Span, bool) {
	hint, ok := Describe(expr)
	if !ok {
		return lang.Span{}, false
	}
	return lang.Span{
		Line: li, StartCol: start, EndCol: end,
		Capture: Capture, Replace: expr + Gap + hint,
	}, true
}

// CrontabSpans produces the hints for a crontab buffer: the leading schedule
// of every job line. Comments, blank lines and `NAME=value` environment
// assignments are skipped.
func CrontabSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		runes := []rune(line)
		if s, ok := crontabSpan(li, runes); ok {
			out = append(out, s)
		}
	}
	return out
}

// crontabSpan finds the schedule on one crontab line.
func crontabSpan(li int, runes []rune) (lang.Span, bool) {
	start := linescan.SkipSpace(runes, 0)
	if start >= len(runes) || runes[start] == '#' {
		return lang.Span{}, false
	}
	if isAssignment(runes, start) {
		return lang.Span{}, false
	}
	words := linescan.Words(runes, start)
	if len(words) == 0 {
		return lang.Span{}, false
	}
	if runes[words[0][0]] == '@' {
		w := words[0]
		return hintSpan(li, w[0], w[1], string(runes[w[0]:w[1]]))
	}
	// Five schedule fields plus at least one word of command: a line with
	// only five words is an unfinished job, not a schedule to annotate.
	if len(words) < 6 {
		return lang.Span{}, false
	}
	from, to := words[0][0], words[4][1]
	return hintSpan(li, from, to, string(runes[from:to]))
}

// YAMLSpans produces the hints for a YAML buffer: the value of a `cron:` or
// `schedule:` key (GitHub Actions, GitLab CI and friends), plus any quoted
// scalar that looks like a schedule on its own.
func YAMLSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		runes := []rune(line)
		if s, ok := yamlKeySpan(li, runes); ok {
			out = append(out, s)
			continue
		}
		out = append(out, quotedSpans(li, runes)...)
	}
	return out
}

// QuotedSpans produces the hints for quoted cron expressions in a config
// format that has no cron-specific key convention of its own (JSON, TOML). The
// shape guard in looksCron carries the whole burden here.
func QuotedSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		out = append(out, quotedSpans(li, []rune(line))...)
	}
	return out
}

// cronKeys are the mapping keys whose value is a schedule by convention.
var cronKeys = map[string]bool{"cron": true, "schedule": true, "crontab": true}

// yamlKeySpan finds a `cron:`/`schedule:` value on one YAML line. Sequence
// markers are skipped ("- cron: …"), and both quoted and plain scalars are
// accepted — the key already says the value is a schedule, so no shape guard
// applies.
func yamlKeySpan(li int, runes []rune) (lang.Span, bool) {
	i := linescan.SkipSpace(runes, 0)
	for i < len(runes) && runes[i] == '-' && i+1 < len(runes) && linescan.IsSpace(runes[i+1]) {
		i = linescan.SkipSpace(runes, i+1)
	}
	if i >= len(runes) || runes[i] == '#' {
		return lang.Span{}, false
	}
	colon := -1
	for j := i; j < len(runes); j++ {
		if runes[j] == ':' {
			colon = j
			break
		}
	}
	if colon < 0 {
		return lang.Span{}, false
	}
	key := strings.Trim(strings.TrimSpace(string(runes[i:colon])), `"'`)
	if !cronKeys[strings.ToLower(key)] {
		return lang.Span{}, false
	}
	start := linescan.SkipSpace(runes, colon+1)
	end := trimEnd(runes, start, linescan.CommentStart(runes, start))
	if start >= end {
		return lang.Span{}, false
	}
	if q := runes[start]; q == '"' || q == '\'' {
		if end-start < 2 || runes[end-1] != q {
			return lang.Span{}, false
		}
		start, end = start+1, end-1
	}
	if start >= end {
		return lang.Span{}, false
	}
	return hintSpan(li, start, end, string(runes[start:end]))
}

// quotedSpans finds the quoted scalars on a line that look like a schedule.
func quotedSpans(li int, runes []rune) []lang.Span {
	var out []lang.Span
	for i := 0; i < len(runes); i++ {
		q := runes[i]
		if q != '"' && q != '\'' {
			continue
		}
		j := i + 1
		for j < len(runes) && runes[j] != q {
			j++
		}
		if j >= len(runes) {
			break
		}
		if text := string(runes[i+1 : j]); looksCron(text) {
			if s, ok := hintSpan(li, i+1, j, text); ok {
				out = append(out, s)
			}
		}
		i = j
	}
	return out
}

// looksCron is the shape guard for a context that only knows the text is a
// string: it must carry a cron-specific character or a field name, so plain
// number lists and prose never turn into schedules.
func looksCron(text string) bool {
	fields := strings.Fields(text)
	if len(fields) == 1 && strings.HasPrefix(fields[0], "@") {
		return true // an @-shorthand is unambiguous on its own
	}
	if len(fields) != 5 && len(fields) != 6 {
		return false
	}
	for _, f := range fields {
		if strings.ContainsAny(f, "*/,-?") {
			return true
		}
		if _, err := strconv.Atoi(f); err != nil {
			return true // a name (MON, JAN) — only cron writes those here
		}
	}
	return false
}

// isAssignment reports whether the line starting at i is a crontab
// environment assignment (`SHELL=/bin/sh`, `MAILTO=""`).
func isAssignment(runes []rune, i int) bool {
	j := i
	for j < len(runes) && (isLetter(runes[j]) || runes[j] == '_' || (j > i && isDigit(runes[j]))) {
		j++
	}
	if j == i {
		return false
	}
	j = linescan.SkipSpace(runes, j)
	return j < len(runes) && runes[j] == '='
}

func trimEnd(runes []rune, start, end int) int {
	for end > start && linescan.IsSpace(runes[end-1]) {
		end--
	}
	return end
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func isLetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }
