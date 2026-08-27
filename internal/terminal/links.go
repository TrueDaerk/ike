package terminal

// links.go — clickable file:line references in terminal output (#1168).
// Compiler errors, test failures and grep output print `path/file.go:42[:col]`;
// a pragmatic regex over the rendered plain text finds such references,
// cmd+click resolves one against the session's cwd and — when the file really
// exists (a cheap os.Stat at CLICK time, never at render time) — the app opens
// it through the standard open funnel. The affordance is an always-on subtle
// underline, applied when a line is rendered: the live screen decorates inside
// the version-keyed render cache (#803), so the cached fast path never pays a
// second scan; scrollback rows decorate as they are windowed in.
// The keyboard route to the same targets — hint labels over the visible
// references — lives in hints.go (#2254).

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// linkNames is the closed list of extensionless files that read as a
// reference when they carry a `:line` suffix (#2254): build files, container
// recipes and the handful of shouty repository-root files. A closed,
// case-sensitive list is the false-positive guard the letter-led extension
// rule gives the general shape — anything not on it still needs an
// extension, so prose like `note:12` or a `host:port` pair stays inert.
var linkNames = []string{
	"GNUmakefile", "Makefile", "makefile",
	"Dockerfile", "Containerfile", "Vagrantfile", "Jenkinsfile",
	"Rakefile", "Gemfile", "Procfile", "Brewfile", "Justfile", "justfile",
	"Caddyfile", "Earthfile", "Tiltfile",
	"LICENSE", "LICENCE", "COPYING", "NOTICE", "README", "CHANGELOG",
	"AUTHORS", "CODEOWNERS", "MAINTAINERS", "INSTALL",
	"BUILD", "WORKSPACE",
}

// linkRe matches file references with a line (and optional column) suffix:
// `file.go:12`, `pkg/x.go:3:14`, `./rel/path.rs:7`, `/abs/dir/mod.c:42:1`,
// plus the extensionless well-known names (`Makefile:12`, `src/Dockerfile:4`,
// #2254). The final path component must carry a letter-led extension or be
// one of linkNames — that keeps timestamps (`12:30`) and bare `host:port`
// pairs out.
//
// Group 1 is the path, 2 the line, 3 the optional column. The match opens on
// a *consumed* left boundary — start of line or a rune that cannot belong to
// a path — because RE2 has no lookbehind and the fixed names would otherwise
// match inside a longer token (`foo-Makefile:3`). The boundary rune is part
// of the match but outside group 1, so the reported span still starts at the
// reference itself.
var linkRe = regexp.MustCompile(
	`(?:^|[^\w+.~\-/])(` +
		`/?[\w+.~\-]+(?:/[\w+.~\-]+)*\.[A-Za-z][A-Za-z0-9]*` +
		`|(?:/?[\w+.~\-]+(?:/[\w+.~\-]+)*/)?(?:` + strings.Join(linkNames, "|") + `)` +
		`):(\d+)(?::(\d+))?`)

// link is one detected reference: rune offsets [start, end) into the scanned
// plain-text line (the full match, numbers included), the path text, and the
// 1-based line/column (col 0 when the reference had none).
type link struct {
	start, end int
	path       string
	line, col  int
}

// scanLinks finds every file:line reference in the plain-text line. Offsets
// are rune indices — the coordinate space of terminal cells and of the ANSI
// splice helpers (wide runes count one; pragmatic, like the selection code).
func scanLinks(text string) []link {
	ms := linkRe.FindAllStringSubmatchIndex(text, -1)
	if len(ms) == 0 {
		return nil
	}
	out := make([]link, 0, len(ms))
	for _, m := range ms {
		l := link{
			start: utf8.RuneCountInString(text[:m[2]]), // group 1: the path
			end:   utf8.RuneCountInString(text[:m[1]]),
			path:  text[m[2]:m[3]],
		}
		l.line, _ = strconv.Atoi(text[m[4]:m[5]])
		if m[6] >= 0 {
			l.col, _ = strconv.Atoi(text[m[6]:m[7]])
		}
		out = append(out, l)
	}
	return out
}

// linkStat is os.Stat behind a variable: the seam the tests count through to
// keep the "never on a render path" posture honest.
var linkStat = os.Stat

// resolveLink turns a detected reference into an absolute file path: relative
// paths resolve against the session's cwd, and the target must exist as a
// regular file — the existence gate that keeps false regex positives (URLs,
// version strings) from opening ghost buffers. Runs at click time only — and
// at hint-mode activation (#2254), the same kind of one-off user gesture;
// never on a render path.
func resolveLink(l link, cwd string) (string, bool) {
	p := l.path
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	fi, err := linkStat(p)
	if err != nil || fi.IsDir() {
		return "", false
	}
	return p, true
}

// LinkAt reports the file reference under the pane-local cell (x, y), if any:
// the absolute file path and the 0-based line/column for openPathAt. The scan
// runs over the whole logical line (soft-wrap chain joined), so a reference
// broken across a wrap still resolves; ok is false on a mouse-reporting child
// (the click belongs to it), on no reference, or when the file does not exist.
func (m Model) LinkAt(x, y int) (path string, line, col int, ok bool) {
	if m.sess == nil || m.sess.WantsMouse() {
		return "", 0, 0, false
	}
	w := m.sess.Width()
	if w <= 0 {
		return "", 0, 0, false
	}
	v := m.virtualAt(x, y)
	first, last := m.logicalLineSpan(v.line)
	var b strings.Builder
	for l := first; l <= last; l++ {
		seg := m.sess.LineText(l)
		if l < last { // wrapped rows are full-width; pad defensively anyway
			if pad := w - utf8.RuneCountInString(seg); pad > 0 {
				seg += strings.Repeat(" ", pad)
			}
		}
		b.WriteString(seg)
	}
	idx := (v.line-first)*w + v.col
	for _, l := range scanLinks(b.String()) {
		if idx < l.start || idx >= l.end {
			continue
		}
		p, exists := resolveLink(l, m.sess.Cwd())
		if !exists {
			return "", 0, 0, false
		}
		c := 0
		if l.col > 0 {
			c = l.col - 1
		}
		return p, l.line - 1, c, true
	}
	return "", 0, 0, false
}

// Underline on/off, without touching any other attribute: a full SGR reset
// would wipe the child's own colors mid-line.
const (
	sgrUnderline   = "\x1b[4m"
	sgrNoUnderline = "\x1b[24m"
)

// decorateLinks underlines every file:line reference in a rendered multi-line
// view. Called once per grid version inside the Session render cache (#803):
// the cached-hit path returns the already-decorated string untouched.
func decorateLinks(view string) string {
	lines := strings.Split(view, "\n")
	changed := false
	for i, l := range lines {
		if d := decorateLinkLine(l); d != l {
			lines[i], changed = d, true
		}
	}
	if !changed {
		return view
	}
	return strings.Join(lines, "\n")
}

// decorateLinkLine underlines the reference spans of one ANSI-styled line.
// Spans are found on the stripped plain text and applied per visible rune —
// wrapping each rune keeps the underline alive across the renderer's own SGR
// resets inside the span.
func decorateLinkLine(line string) string {
	if !strings.Contains(line, ":") {
		return line // no reference without a colon; skip the strip+scan
	}
	spans := scanLinks(ansi.Strip(line))
	if len(spans) == 0 {
		return line
	}
	inSpan := func(v int) bool {
		for _, s := range spans {
			if v >= s.start && v < s.end {
				return true
			}
		}
		return false
	}
	var b strings.Builder
	visible := 0
	inEsc := false
	for i := 0; i < len(line); {
		if !inEsc && line[i] == 0x1b {
			inEsc = true
			b.WriteByte(line[i])
			i++
			continue
		}
		if inEsc {
			b.WriteByte(line[i])
			if line[i] >= 0x40 && line[i] <= 0x7e && line[i] != '[' {
				inEsc = false
			}
			i++
			continue
		}
		_, size := utf8.DecodeRuneInString(line[i:])
		if inSpan(visible) {
			b.WriteString(sgrUnderline)
			b.WriteString(line[i : i+size])
			b.WriteString(sgrNoUnderline)
		} else {
			b.WriteString(line[i : i+size])
		}
		visible++
		i += size
	}
	return b.String()
}
