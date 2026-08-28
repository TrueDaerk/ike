package preview

// links.go is the preview's link layer (#2180): the rendered document's
// hyperlinks are discovered, selected with tab/shift+tab and followed with
// enter. Discovery reads the OSC 8 hyperlink sequences glamour already emits
// around every link label and URL, so a link's row and byte span come from
// the rendering itself rather than from a second markdown parse that could
// drift out of sync with it. In-document "#anchor" links are the exception —
// glamour renders those as bare text — and are recovered from the source and
// located by label.

import (
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// link is one followable hyperlink in the rendered output: where its label
// sits (row plus the byte span inside that raw ANSI line, for the selection
// highlight) and the raw markdown destination it points at.
type link struct {
	row        int
	start, end int // byte offsets of the label inside lines[row]
	target     string
	label      string
}

// osc8Open is the opening half of a hyperlink sequence: ESC ] 8 ; params ; uri
// followed by a string terminator.
const osc8Open = "\x1b]8;"

// hyperlink is one raw OSC 8 span found in a line.
type hyperlink struct {
	uri        string
	label      string
	start, end int
}

// scanHyperlinks returns the OSC 8 spans of one rendered line in order. A
// sequence with an empty uri closes the previous span; an unterminated or
// unclosed span is dropped rather than guessed at.
func scanHyperlinks(line string) []hyperlink {
	var out []hyperlink
	open := -1     // byte offset just past the opening sequence
	var uri string // destination of the currently open span
	for i := 0; i < len(line); {
		j := strings.Index(line[i:], osc8Open)
		if j < 0 {
			break
		}
		seqStart := i + j
		body, next, ok := osc8Body(line, seqStart)
		if !ok {
			break
		}
		// body is "params;uri"; an empty uri closes the open span.
		_, dest, _ := strings.Cut(body, ";")
		if dest == "" {
			if open >= 0 {
				out = append(out, hyperlink{uri: uri, label: ansi.Strip(line[open:seqStart]), start: open, end: seqStart})
				open = -1
			}
		} else {
			open, uri = next, dest
		}
		i = next
	}
	return out
}

// osc8Body returns the "params;uri" payload of the OSC 8 sequence starting at
// pos and the offset just past its terminator.
func osc8Body(line string, pos int) (body string, next int, ok bool) {
	body, next, ok = oscEnd(line, pos)
	if !ok {
		return "", 0, false
	}
	return strings.TrimPrefix(body, "8;"), next, true
}

// indexLinks collects the followable links of the rendered document in
// reading order. Rows in blocks (image blocks that render as pixels, #2180)
// carry no selectable link. The scroll-sync anchors must already be built:
// the in-document links are placed relative to them.
func (m *Model) indexLinks(blocks map[int]bool) []link {
	out := scanLinks(m.lines, blocks)
	out = append(out, m.anchorLinks(blocks)...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].row != out[j].row {
			return out[i].row < out[j].row
		}
		return out[i].start < out[j].start
	})
	return out
}

// scanLinks reads the links out of the rendered output's OSC 8 sequences.
// Glamour emits every such link twice — once around the label, once around
// the URL it prints after it — under the same destination, so consecutive
// spans sharing a target collapse into the one entry the user selects.
func scanLinks(lines []string, skip map[int]bool) []link {
	var out []link
	for row, line := range lines {
		if skip[row] {
			continue
		}
		for _, h := range scanHyperlinks(line) {
			if h.uri == "" {
				continue
			}
			if n := len(out); n > 0 && out[n-1].target == h.uri {
				continue // the URL half of the link just recorded
			}
			out = append(out, link{row: row, start: h.start, end: h.end, target: h.uri, label: h.label})
		}
	}
	return out
}

// anchorLinkRe matches a markdown link into the document itself. Glamour
// renders these as bare styled text — no hyperlink sequence and no printed
// URL — so they are the one link kind the rendered output cannot name, and
// they are recovered from the source instead.
var anchorLinkRe = regexp.MustCompile(`(^|[^!])\[([^\]]+)\]\((#[^)\s]+)\)`)

// emphasisRe matches the inline markers glamour renders away, so a label
// written "[**Deep** section](#x)" is searched for as "Deep section".
var emphasisRe = regexp.MustCompile("[*_`]")

// anchorLinks locates the source's "#anchor" links in the rendered output by
// their labels. The search starts just above where the scroll-sync mapping
// puts the link's source line, so a label that also occurs earlier in the
// document (a heading with the same words, the usual case) does not capture
// it; a label that cannot be located at all is dropped rather than pointed at
// the wrong row. Placement is approximate the same way scroll sync is (#62).
func (m *Model) anchorLinks(skip map[int]bool) []link {
	var out []link
	inFence := false
	for srcLine, text := range strings.Split(m.src, "\n") {
		if fenceRe.MatchString(text) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, mt := range anchorLinkRe.FindAllStringSubmatch(text, -1) {
			label := emphasisRe.ReplaceAllString(mt[2], "")
			row, ok := m.findLabelRow(label, clamp(m.mapLine(srcLine)-2, 0, len(m.lines)), skip)
			if !ok {
				continue
			}
			start, end, _ := findPlainSpan(m.lines[row], label)
			out = append(out, link{row: row, start: start, end: end, target: mt[3], label: label})
		}
	}
	return out
}

// findLabelRow returns the first rendered row at or after from whose printable
// text contains label, falling back to a scan from the top when the mapped
// start overshot it.
func (m *Model) findLabelRow(label string, from int, skip map[int]bool) (int, bool) {
	for _, start := range [2]int{from, 0} {
		for row := start; row < len(m.lines); row++ {
			if skip[row] {
				continue
			}
			if _, _, ok := findPlainSpan(m.lines[row], label); ok {
				return row, true
			}
		}
	}
	return 0, false
}

// findPlainSpan locates text in the printable content of a rendered line and
// returns its byte span in the raw line. The span is contiguous in the raw
// string, so it may enclose escape sequences glamour split the text with —
// which is what the selection highlight wants to wrap anyway.
func findPlainSpan(line, text string) (start, end int, ok bool) {
	if text == "" {
		return 0, 0, false
	}
	plain, offs := plainMap(line)
	i := strings.Index(plain, text)
	if i < 0 {
		return 0, 0, false
	}
	return offs[i], offs[i+len(text)-1] + 1, true
}

// plainMap returns the printable content of a rendered line together with the
// raw byte offset each of its bytes came from.
func plainMap(line string) (string, []int) {
	var b strings.Builder
	offs := make([]int, 0, len(line))
	for i := 0; i < len(line); {
		if n := escapeLen(line, i); n > 0 {
			i += n
			continue
		}
		b.WriteByte(line[i])
		offs = append(offs, i)
		i++
	}
	return b.String(), offs
}

// escapeLen returns the length of the escape sequence starting at pos, or 0
// when a printable byte starts there. CSI and OSC cover everything glamour
// emits; any other escape is treated as the two-byte form.
func escapeLen(line string, pos int) int {
	if line[pos] != 0x1b || pos+1 >= len(line) {
		return 0
	}
	switch line[pos+1] {
	case '[':
		for i := pos + 2; i < len(line); i++ {
			if c := line[i]; c >= 0x40 && c <= 0x7e {
				return i - pos + 1
			}
		}
		return len(line) - pos
	case ']':
		if _, next, ok := oscEnd(line, pos); ok {
			return next - pos
		}
		return len(line) - pos
	}
	return 2
}

// oscEnd returns the offset just past the terminator (BEL or ST) of the OSC
// sequence starting at pos.
func oscEnd(line string, pos int) (body string, next int, ok bool) {
	rest := line[pos+2:]
	if k := strings.IndexByte(rest, '\a'); k >= 0 {
		return rest[:k], pos + 2 + k + 1, true
	}
	if k := strings.Index(rest, "\x1b\\"); k >= 0 {
		return rest[:k], pos + 2 + k + 2, true
	}
	return "", 0, false
}

// Slug renders heading text as the GitHub-flavoured anchor slug markdown
// links use: lower-cased, inline punctuation dropped, spaces hyphenated.
func Slug(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '\t':
			b.WriteByte('-')
		case r > 0x7f:
			b.WriteRune(r) // keep non-ASCII letters, as GitHub does
		}
	}
	return b.String()
}

// HeadingLine returns the 0-based source line of the heading whose slug is
// slug, for following a "#anchor" link into a markdown file. Fenced code is
// skipped, matching the scroll-sync anchors.
func HeadingLine(src, slug string) (int, bool) {
	inFence := false
	for i, line := range strings.Split(src, "\n") {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if match := headingRe.FindStringSubmatch(line); match != nil && Slug(match[1]) == slug {
			return i, true
		}
	}
	return 0, false
}
