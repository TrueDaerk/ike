// Package langcrontab registers the crontab language (#1624): the files whose
// whole content is cron expressions — a user crontab, /etc/crontab, the drop-in
// files under /etc/cron.d, and the `*.cron` / `*.crontab` files projects keep in
// their repository. Like ini (#1595), csv (#1589) and dotenv (#1619) there is no
// Tree-sitter grammar; the structure is Go-computed through the
// lang.Language.Spans seam (#1585): `#` lines are comments, `NAME=value` lines
// style as assignments, a job line styles its five schedule fields apart from
// its command, and the schedule additionally carries the human-readable hint
// produced by internal/cronhint.
//
// Self-registers via init(); blank-imported in cmd/ike/main.go.
package langcrontab

import (
	"ike/internal/cronhint"
	"ike/internal/lang"
	"ike/internal/secret"
	"ike/plugins/languages/register"
)

func init() {
	register.Language(lang.Language{
		ID:         "crontab",
		Extensions: []string{"cron", "crontab"},
		// The conventional base names: a user crontab has no extension at
		// all, and the system files are named for their location.
		Filenames:   []string{"crontab", ".crontab"},
		LineComment: "#",
		Spans:       crontabSpans,
	})
}

// crontabSpans emits the highlight spans for a crontab buffer: comments,
// environment assignments, the schedule fields and the command, plus the
// schedule hints (#1624). The secret masks on suspect environment
// assignments (`DB_PASSWORD=…`, #2345) come first of all, then the hint
// spans — overlapping spans resolve first-covering-wins, so a mask or hint
// must precede the field styling it sits on.
func crontabSpans(lines []string) []lang.Span {
	out := append(secret.PairSpans(lines, "="), cronhint.CrontabSpans(lines)...)
	for li, line := range lines {
		out = append(out, lineSpans(li, []rune(line))...)
	}
	return out
}

// lineSpans styles one crontab line.
func lineSpans(li int, runes []rune) []lang.Span {
	start := skipSpace(runes, 0)
	if start >= len(runes) {
		return nil
	}
	if runes[start] == '#' {
		return []lang.Span{{Line: li, StartCol: start, EndCol: len(runes), Capture: "comment"}}
	}
	fields := words(runes, start)
	if len(fields) == 0 {
		return nil
	}
	if eq := assignmentEnd(runes, start); eq > start {
		return []lang.Span{
			{Line: li, StartCol: start, EndCol: eq, Capture: "property"},
			{Line: li, StartCol: eq, EndCol: eq + 1, Capture: "punctuation"},
			{Line: li, StartCol: eq + 1, EndCol: len(runes), Capture: "string"},
		}
	}
	// The schedule: an @-shorthand is one field, otherwise the first five.
	schedule := 5
	if runes[fields[0][0]] == '@' {
		schedule = 1
	}
	if len(fields) <= schedule {
		return nil
	}
	var out []lang.Span
	for _, f := range fields[:schedule] {
		out = append(out, lang.Span{Line: li, StartCol: f[0], EndCol: f[1], Capture: "number"})
	}
	cmd := fields[schedule]
	out = append(out, lang.Span{Line: li, StartCol: cmd[0], EndCol: len(runes), Capture: "string"})
	return out
}

// assignmentEnd returns the column of the `=` of an environment assignment
// starting at start, or start when the line is not one.
func assignmentEnd(runes []rune, start int) int {
	j := start
	for j < len(runes) && (isLetter(runes[j]) || runes[j] == '_' || (j > start && isDigit(runes[j]))) {
		j++
	}
	if j == start {
		return start
	}
	k := skipSpace(runes, j)
	if k < len(runes) && runes[k] == '=' {
		return k
	}
	return start
}

// words returns the [start, end) rune-column ranges of the whitespace-
// separated words from index i on.
func words(runes []rune, i int) [][2]int {
	var out [][2]int
	for i < len(runes) {
		i = skipSpace(runes, i)
		if i >= len(runes) {
			break
		}
		j := i
		for j < len(runes) && !isSpace(runes[j]) {
			j++
		}
		out = append(out, [2]int{i, j})
		i = j
	}
	return out
}

func skipSpace(runes []rune, i int) int {
	for i < len(runes) && isSpace(runes[i]) {
		i++
	}
	return i
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' }

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func isLetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }
