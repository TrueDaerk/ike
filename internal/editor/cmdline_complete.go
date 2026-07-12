package editor

import (
	"path/filepath"
	"strconv"
	"strings"

	"ike/internal/pathcomplete"
)

// cmdline_complete.go is shell-style path completion for the ex command line
// (#543): tab on a ":e <partial>" / ":w <partial>" line extends the path
// argument to the longest unambiguous prefix via the shared
// internal/pathcomplete engine, and while several entries match, their names
// render as a dim hint after the cursor. Relative arguments complete against
// the process working directory — the same base :e/:w resolve against.

// pathVerbs are the ex commands whose argument is a filesystem path.
var pathVerbs = map[string]bool{
	"e": true, "edit": true,
	"w": true, "write": true,
	"wq": true, "x": true, "xit": true,
}

// maxCmdSuggest caps the hint row's entries; the rest collapses into "+N".
const maxCmdSuggest = 5

// splitPathArg splits an ex line into the untouched prefix (verb + the
// whitespace after it) and the path argument being completed. ok is false
// when the verb takes no path or no argument has been started yet.
func splitPathArg(line string) (prefix, arg string, ok bool) {
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return "", "", false
	}
	verb := strings.TrimSuffix(line[:i], "!")
	if !pathVerbs[verb] {
		return "", "", false
	}
	j := i
	for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
		j++
	}
	return line[:j], line[j:], true
}

// completeCmdlinePath applies a tab press on the ":" line: the path argument
// extends to the longest unambiguous prefix and the candidates land in the
// hint row. A line without a completable path argument is left untouched.
func (m *Model) completeCmdlinePath() {
	prefix, arg, ok := splitPathArg(m.cmdline)
	if !ok {
		return
	}
	r := pathcomplete.Complete(arg)
	m.cmdline = prefix + r.Completed
	m.cmdSuggest = r.Candidates
}

// refreshCmdlineSuggest narrows an open hint row as the user keeps typing;
// inert while no hint row is showing (suggestions appear on tab, not on every
// keystroke).
func (m *Model) refreshCmdlineSuggest() {
	if m.cmdSuggest == nil {
		return
	}
	_, arg, ok := splitPathArg(m.cmdline)
	if !ok {
		m.cmdSuggest = nil
		return
	}
	m.cmdSuggest = pathcomplete.Complete(arg).Candidates
}

// suggestHint renders the hint row content appended after the cmdline cursor:
// the final path component of up to maxCmdSuggest candidates, then "+N".
// Empty when at most one candidate is known (nothing ambiguous to show).
func (m Model) suggestHint() string {
	if len(m.cmdSuggest) < 2 {
		return ""
	}
	shown := len(m.cmdSuggest)
	if shown > maxCmdSuggest {
		shown = maxCmdSuggest
	}
	parts := make([]string, 0, shown+1)
	for _, c := range m.cmdSuggest[:shown] {
		parts = append(parts, lastPathComponent(c))
	}
	if n := len(m.cmdSuggest) - shown; n > 0 {
		parts = append(parts, "+"+strconv.Itoa(n))
	}
	return strings.Join(parts, "  ")
}

// lastPathComponent is the candidate's final path element, keeping a
// directory's trailing separator.
func lastPathComponent(c string) string {
	sep := string(filepath.Separator)
	if i := strings.LastIndexByte(strings.TrimSuffix(c, sep), filepath.Separator); i >= 0 {
		return c[i+1:]
	}
	return c
}
