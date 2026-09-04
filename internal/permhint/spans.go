package permhint

import (
	"strings"

	"ike/internal/lang"
	"ike/internal/linescan"
)

// spans.go is the context half of the permission hint (#1656): where in a
// buffer a run of three or four digits is a file mode rather than a port, a
// year or an exit code. The literal carries no syntax of its own — unlike a
// CIDR prefix (#1653) the shape alone is no evidence at all — so every producer
// keys on the thing that *carries* the mode:
//
//   - Shell — `chmod`'s first operand, and the `-m`/`--mode` value of `install`
//     and `mkdir`. Octal by definition here, so a bare `755` decodes.
//   - Dockerfile — the `--chmod=` flag of `COPY`/`ADD`, plus the shell scan over
//     `RUN` lines.
//   - Go and Python — the octal literals inside a call whose name is a mode
//     API: `os.Chmod`, `os.WriteFile`, `os.MkdirAll`, `os.FileMode(…)`,
//     `os.chmod`, `os.makedirs(…, mode=0o755)`, `Path(…).chmod(…)`.
//   - YAML — a `mode:`/`defaultMode:` key (Ansible, Kubernetes).
//
// The code and YAML producers additionally require the literal to be written as
// octal (HasOctalPrefix): a bare `644` is decimal in all three languages, and
// in YAML the number hints already read it as `= 01204` (#1627), which is the
// warning an Ansible `mode: 644` deserves. Every producer emits the same
// stand-in shape as the other hint families: the span covers the raw literal
// and Replace repeats it with the symbolic form appended, so the raw text is
// one caret motion away (#1594's positional reveal).

// hintSpan builds the stand-in span for the literal at rune columns
// [start, end) of line li. It reports false when the text is no permission
// literal this package can decode.
func hintSpan(li, start, end int, text string) (lang.Span, bool) {
	hint, ok := Describe(text)
	if !ok {
		return lang.Span{}, false
	}
	return lang.Span{
		Line: li, StartCol: start, EndCol: end,
		Capture: Capture, Replace: text + Gap + hint,
	}, true
}

// --- shell -----------------------------------------------------------------

// ShellSpans produces the hints for a shell buffer, ready to append to a
// language's lang.Language.Spans output.
func ShellSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		out = append(out, shellLineSpans(li, []rune(line))...)
	}
	return out
}

// shellCmd is what the command word most recently seen on a line says about the
// tokens after it.
type shellCmd int

const (
	shellNone  shellCmd = iota
	shellChmod          // chmod: the first operand is the mode
	shellFlag           // install/mkdir: only a -m/--mode value is a mode
)

// shellLineSpans scans one shell command line. The scan resets at every command
// separator, so `mkdir -p x && chmod 700 x` reads as the two commands it is.
func shellLineSpans(li int, runes []rune) []lang.Span {
	var out []lang.Span
	cmd, wantValue := shellNone, false
	for _, w := range linescan.Words(runes, 0) {
		tok := string(runes[w[0]:w[1]])
		if strings.HasPrefix(tok, "#") {
			break // the rest of the line is a comment
		}
		if isSeparator(tok) {
			cmd, wantValue = shellNone, false
			continue
		}
		if wantValue {
			if s, ok := hintSpan(li, w[0], w[1], tok); ok {
				out = append(out, s)
			}
			cmd, wantValue = shellNone, false
			continue
		}
		switch cmdName(tok) {
		case "chmod":
			cmd = shellChmod
			continue
		case "install", "mkdir":
			cmd = shellFlag
			continue
		}
		switch cmd {
		case shellChmod:
			if strings.HasPrefix(tok, "-") {
				continue // an option, not the mode operand
			}
			// The mode is chmod's first operand either way: a symbolic mode
			// (`u+x`) ends the scan just as a decoded octal one does.
			if s, ok := hintSpan(li, w[0], w[1], tok); ok {
				out = append(out, s)
			}
			cmd = shellNone
		case shellFlag:
			value, off, ok := modeFlag(tok)
			if !ok {
				continue
			}
			if off < 0 {
				wantValue = true
				continue
			}
			if s, ok := hintSpan(li, w[0]+off, w[1], value); ok {
				out = append(out, s)
			}
			cmd = shellNone
		}
	}
	return out
}

// modeFlag reads a mode-carrying option. It returns the value and its rune
// offset inside the token — `-m755`, `-pm755`, `--mode=755` — or offset -1 when
// the option takes the next token as its value (`-m 755`, `--mode 755`).
func modeFlag(tok string) (value string, offset int, ok bool) {
	if v, found := strings.CutPrefix(tok, "--mode="); found {
		return v, len("--mode="), true
	}
	if tok == "--mode" {
		return "", -1, true
	}
	if len(tok) < 2 || tok[0] != '-' || tok[1] == '-' {
		return "", 0, false
	}
	// A short-option cluster: `-m`, `-pm`, `-m755`, `-pm755`. The mode follows
	// the `m` inside the cluster, or is the next token when `m` ends it.
	i := strings.IndexByte(tok, 'm')
	if i < 1 {
		return "", 0, false
	}
	if i == len(tok)-1 {
		return "", -1, true
	}
	return tok[i+1:], i + 1, true
}

// cmdName reduces a token to the command it names: the shell punctuation that
// can open a command word is stripped (a `$(` or a backtick) and a path is
// reduced to its base name (`/bin/chmod`).
func cmdName(tok string) string {
	tok = strings.TrimLeft(tok, "$(`{!")
	if i := strings.LastIndexByte(tok, '/'); i >= 0 {
		tok = tok[i+1:]
	}
	return tok
}

// isSeparator reports whether a token ends the current command.
func isSeparator(tok string) bool {
	switch tok {
	case ";", "&", "&&", "|", "||", "{", "}", "(", ")", "then", "do", "else":
		return true
	}
	return strings.HasSuffix(tok, ";")
}

// --- Dockerfile ------------------------------------------------------------

// DockerfileSpans produces the hints for a Dockerfile: the `--chmod=` flag of
// COPY/ADD, and the shell scan over RUN lines.
func DockerfileSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		runes := []rune(line)
		ws := linescan.Words(runes, 0)
		if len(ws) == 0 {
			continue
		}
		switch strings.ToUpper(string(runes[ws[0][0]:ws[0][1]])) {
		case "COPY", "ADD":
			const flag = "--chmod="
			for _, w := range ws[1:] {
				tok := string(runes[w[0]:w[1]])
				if len(tok) <= len(flag) || !strings.EqualFold(tok[:len(flag)], flag) {
					continue
				}
				// The flag is ASCII, so its length is a rune count too.
				if s, ok := hintSpan(li, w[0]+len(flag), w[1], tok[len(flag):]); ok {
					out = append(out, s)
				}
			}
		case "RUN":
			out = append(out, shellLineSpans(li, runes)...)
		}
	}
	return out
}

// --- code (Go, Python) -----------------------------------------------------

// goCalls are the Go functions whose octal literal argument is a file mode.
// Matching is on the last dotted segment, so `fs.FileMode`, `f.Chmod` and a
// vendored `unix.Chmod` all land here.
var goCalls = map[string]bool{
	"chmod": true, "fchmod": true, "lchmod": true,
	"mkdir": true, "mkdirall": true, "mkdirtemp": true,
	"writefile": true, "openfile": true, "filemode": true,
}

// pythonCalls are the Python equivalents. `open` is deliberately absent: its
// numeric arguments are buffer sizes and file descriptors, not modes.
var pythonCalls = map[string]bool{
	"chmod": true, "fchmod": true, "lchmod": true,
	"mkdir": true, "makedirs": true, "mknod": true, "mkfifo": true,
}

// GoSpans produces the hints for a Go buffer: the octal literals inside a call
// to a mode API.
func GoSpans(lines []string) []lang.Span { return codeSpans(lines, goCalls) }

// PythonSpans produces the hints for a Python buffer, the same way.
func PythonSpans(lines []string) []lang.Span { return codeSpans(lines, pythonCalls) }

// codeSpans annotates the octal literals in the argument list of every call
// whose name is in names. The whole argument region is scanned rather than a
// fixed position: the mode is the last argument of `os.WriteFile`, the second
// of `os.Chmod` and the only one of `os.FileMode`, and no other argument of
// those calls is written as an octal literal. A matched call consumes its
// region, so a nested `os.Chmod(p, os.FileMode(0o755))` annotates once.
func codeSpans(lines []string, names map[string]bool) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		runes := []rune(line)
		for i := 0; i < len(runes); i++ {
			if runes[i] != '(' || !names[callName(runes, i)] {
				continue
			}
			end := matchParen(runes, i)
			out = append(out, argLiterals(li, runes, i+1, end)...)
			i = end
		}
	}
	return out
}

// callName returns the lowercased last dotted segment of the function name
// ending right before the `(` at index i, or "" when no name precedes it.
func callName(runes []rune, i int) string {
	j := i
	for j > 0 && (isIdentRune(runes[j-1]) || runes[j-1] == '.') {
		j--
	}
	name := string(runes[j:i])
	if k := strings.LastIndexByte(name, '.'); k >= 0 {
		name = name[k+1:]
	}
	if name == "" || isDigit(rune(name[0])) {
		return ""
	}
	return strings.ToLower(name)
}

// matchParen returns the index of the `)` closing the `(` at index i, or the
// line end when the call spans lines — the mode literal is on the opening line
// in either case.
func matchParen(runes []rune, i int) int {
	depth := 0
	var quote rune
	for j := i; j < len(runes); j++ {
		switch r := runes[j]; {
		case quote != 0:
			if r == '\\' {
				j++
			} else if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'' || r == '`':
			quote = r
		case r == '(':
			depth++
		case r == ')':
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return len(runes)
}

// argLiterals emits a hint per octal literal in the rune range [from, to).
// Quoted text is skipped — a `"0644"` string is a path or a format, not a mode
// — and a run glued to an identifier (`x0644`) is not a literal at all.
func argLiterals(li int, runes []rune, from, to int) []lang.Span {
	var out []lang.Span
	if to > len(runes) {
		to = len(runes)
	}
	for i := from; i < to; i++ {
		switch r := runes[i]; {
		case r == '"' || r == '\'' || r == '`':
			for i++; i < to && runes[i] != r; i++ {
				if runes[i] == '\\' {
					i++
				}
			}
		case isIdentRune(r):
			j := i
			for j < to && isIdentRune(runes[j]) {
				j++
			}
			glued := i > 0 && (isIdentRune(runes[i-1]) || runes[i-1] == '.')
			if tok := string(runes[i:j]); !glued && HasOctalPrefix(tok) {
				if s, ok := hintSpan(li, i, j, tok); ok {
					out = append(out, s)
				}
			}
			i = j - 1
		}
	}
	return out
}

// --- YAML ------------------------------------------------------------------

// modeKeys are the mapping keys whose value is a file mode by convention:
// Ansible's `mode`/`directory_mode`, Kubernetes' `defaultMode`.
var modeKeys = map[string]bool{
	"mode": true, "defaultmode": true, "default_mode": true,
	"directory_mode": true, "file_mode": true, "dir_mode": true,
}

// YAMLSpans produces the hints for a YAML buffer: the value of a `mode:`-family
// key, quoted or plain. The value must be written as octal (`0644`, `'0755'`,
// `0o644`) — see the HasOctalPrefix note at the top of this file.
func YAMLSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		if s, ok := yamlKeySpan(li, []rune(line)); ok {
			out = append(out, s)
		}
	}
	return out
}

// yamlKeySpan finds a mode value on one YAML line. Sequence markers are skipped
// ("- mode: 0644") and a surrounding quote is trimmed off the value, so the
// span covers the literal itself and not the scalar's chrome.
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
	if !modeKeys[strings.ToLower(key)] {
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
	if text := string(runes[start:end]); HasOctalPrefix(text) {
		return hintSpan(li, start, end, text)
	}
	return lang.Span{}, false
}

// --- shared scanning helpers -----------------------------------------------

func trimEnd(runes []rune, start, end int) int {
	for end > start && linescan.IsSpace(runes[end-1]) {
		end--
	}
	return end
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func isIdentRune(r rune) bool {
	return r == '_' || isDigit(r) || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}
