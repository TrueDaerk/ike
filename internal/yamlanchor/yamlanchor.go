// Package yamlanchor pairs YAML anchors (&name) with their aliases (*name)
// (#1629): a line-based scanner finds both token kinds outside comments,
// quoted scalars and block-scalar bodies, resolves each alias to the nearest
// preceding anchor of the same name in the same document, and derives from
// the pairing everything the editor needs — same-name coloring on a stable
// slot of the shared rainbow palette (#1589, the idcolor content-hash trick),
// an error capture for an alias no anchor defines, the goto/usages targets
// for the local navigation providers, and the resolved value text (merge-key
// `<<:` aware) for the alias hover preview.
//
// The scanner is deliberately structural, not a YAML parser: `&`/`*` start a
// token only at a node-start position (after `:`, `-`, `,`, `[`, `{`, `?` or
// the line start), which is exactly where YAML reserves the indicators, so a
// `*` inside a plain scalar like `a * b` never counts.
package yamlanchor

import (
	"hash/fnv"
	"strconv"
	"strings"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// Unresolved is the capture an alias without a matching anchor renders with:
// the editor styles it like an unmatched bracket (#1628) — the theme's error
// color plus an underline — unless a theme.captures.anchor.unresolved key
// overrides it.
const Unresolved = "anchor.unresolved"

// maxDepth bounds merge-key recursion; a chain deeper than this is a cycle or
// pathological input, and the preview stops expanding rather than looping.
const maxDepth = 10

// previewCap bounds the resolved-value preview: hover is a glance, not a
// pager, so a longer value truncates with an ellipsis line.
const previewCap = 16

// Mark is one anchor or alias token: the sigil plus the name, in rune
// coordinates. Doc numbers the YAML document (--- separated) the token lives
// in — pairing never crosses a document boundary. Anchor indexes the mark
// this alias resolves to in the same Scan result (-1 for anchors and for
// unresolved aliases).
type Mark struct {
	Line, Col, Len int
	Name           string
	Alias          bool
	Unresolved     bool
	Doc            int
	Anchor         int
}

// token is scanLine's raw find, before document tracking and resolution.
type token struct {
	col, length int
	name        string
	alias       bool
}

// Scan finds every anchor and alias in lines, in document order, with aliases
// resolved to the nearest preceding same-name anchor of their document — the
// YAML rule: an alias refers back, and a redefined anchor shadows the earlier
// one from that point on.
func Scan(lines []string) []Mark {
	var marks []Mark
	doc := 0
	anchors := map[string]int{} // name → mark index, current document
	inScalar, scalarIndent := false, 0
	for ln, line := range lines {
		if inScalar {
			if strings.TrimSpace(line) == "" || indentOf(line) > scalarIndent {
				continue // block-scalar body: anchors here are plain text
			}
			inScalar = false
		}
		if docMarker(line) {
			doc++
			anchors = map[string]int{}
		}
		toks, _, blockScalar := scanLine(line)
		for _, tk := range toks {
			mk := Mark{Line: ln, Col: tk.col, Len: tk.length, Name: tk.name, Alias: tk.alias, Doc: doc, Anchor: -1}
			if tk.alias {
				if ai, ok := anchors[tk.name]; ok {
					mk.Anchor = ai
				} else {
					mk.Unresolved = true
				}
			} else {
				anchors[tk.name] = len(marks)
			}
			marks = append(marks, mk)
		}
		if blockScalar {
			inScalar, scalarIndent = true, indentOf(line)
		}
	}
	return marks
}

// Spans is the lang.Language.Spans producer: every anchor and alias colored
// by the hash of its name — one shared rainbow slot per name, so an anchor
// and all its aliases read as a pair — and an unresolved alias carrying the
// error capture instead.
func Spans(lines []string) []lang.Span {
	marks := Scan(lines)
	if len(marks) == 0 {
		return nil
	}
	out := make([]lang.Span, 0, len(marks))
	for _, mk := range marks {
		capture := Capture(mk.Name)
		if mk.Unresolved {
			capture = Unresolved
		}
		out = append(out, lang.Span{Line: mk.Line, StartCol: mk.Col, EndCol: mk.Col + mk.Len, Capture: capture})
	}
	return out
}

// Slot hashes an anchor name onto a rainbow slot — content-keyed like
// idcolor.Slot, but case-sensitive because YAML anchor names are.
func Slot(name string) int {
	h := fnv.New32a()
	h.Write([]byte(name))
	return int(h.Sum32() % uint32(highlight.RainbowColors))
}

// Capture is the theme capture a name's marks render with: the shared rainbow
// palette (#1589), so theme.captures.rainbow.N overrides move anchor colors
// along with bracket and identifier colors.
func Capture(name string) string {
	return "rainbow." + strconv.Itoa(Slot(name))
}

// DefinitionAt resolves the alias covering (line, col) to its anchor mark.
// It reports false on anchors, unresolved aliases and every non-alias
// position, so a local-definition claim stays narrow.
func DefinitionAt(lines []string, line, col int) (Mark, bool) {
	marks := Scan(lines)
	mk, ok := at(marks, line, col)
	if !ok || !mk.Alias || mk.Anchor < 0 {
		return Mark{}, false
	}
	return marks[mk.Anchor], true
}

// UsagesAt lists every mark of the name under the cursor — the anchor(s) and
// all aliases of its document, in buffer order. It claims on anchors and
// aliases alike; false on every other position.
func UsagesAt(lines []string, line, col int) (string, []Mark, bool) {
	marks := Scan(lines)
	mk, ok := at(marks, line, col)
	if !ok {
		return "", nil, false
	}
	var refs []Mark
	for _, m := range marks {
		if m.Doc == mk.Doc && m.Name == mk.Name {
			refs = append(refs, m)
		}
	}
	return mk.Name, refs, true
}

// ResolveAt returns the value the alias under the cursor stands for: the
// anchored node's text, dedented, with `<<:` merge keys expanded recursively
// (cycle-guarded), truncated to previewCap lines. False when the cursor is
// not on a resolvable alias or the anchor carries no value.
func ResolveAt(lines []string, line, col int) (string, []string, bool) {
	marks := Scan(lines)
	mk, ok := at(marks, line, col)
	if !ok || !mk.Alias || mk.Anchor < 0 {
		return "", nil, false
	}
	value := resolveValue(lines, marks, mk.Anchor, map[int]bool{}, 0)
	if len(value) == 0 {
		return "", nil, false
	}
	if len(value) > previewCap {
		value = append(value[:previewCap:previewCap], "…")
	}
	return mk.Name, value, true
}

// at finds the mark covering (line, col); the end is inclusive so a cursor
// just past the name still counts as on it (the identAt convention).
func at(marks []Mark, line, col int) (Mark, bool) {
	for _, mk := range marks {
		if mk.Line == line && col >= mk.Col && col <= mk.Col+mk.Len {
			return mk, true
		}
	}
	return Mark{}, false
}

// resolveValue extracts the text of the node marks[ai] anchors: the rest of
// the anchor's line when the value is inline, else the more-indented block
// below, dedented. `<<: *other` lines inside the block are replaced by the
// other anchor's resolved value, re-indented to the merge line — recursively,
// with visited/depth guards so an anchor cycle terminates.
func resolveValue(lines []string, marks []Mark, ai int, visited map[int]bool, depth int) []string {
	if depth > maxDepth || visited[ai] {
		return nil
	}
	visited[ai] = true
	defer delete(visited, ai) // diamond merges may share a base anchor

	mk := marks[ai]
	if rest := afterToken(lines[mk.Line], mk); rest != "" {
		return []string{rest}
	}
	indent0 := indentOf(lines[mk.Line])
	var block []bline
	for i := mk.Line + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			block = append(block, bline{i, ""})
			continue
		}
		if docMarker(lines[i]) || indentOf(lines[i]) <= indent0 {
			break
		}
		block = append(block, bline{i, lines[i]})
	}
	for len(block) > 0 && block[len(block)-1].text == "" {
		block = block[:len(block)-1]
	}
	if len(block) == 0 {
		return nil
	}
	shift := minIndent(block)
	var out []string
	for _, bl := range block {
		if bl.text == "" {
			out = append(out, "")
			continue
		}
		text := dedent(bl.text, shift)
		if !mergeLine(text) {
			out = append(out, text)
			continue
		}
		// A merge key: splice each resolved alias value in place of the line,
		// re-indented to the merge key's own indent. Scan already resolved the
		// aliases on the buffer line, so `<<: [*a, *b]` expands both in order.
		pad := text[:indentOf(text)]
		spliced := false
		for _, am := range aliasesOn(marks, bl.no) {
			for _, vl := range resolveValue(lines, marks, am.Anchor, visited, depth+1) {
				out = append(out, pad+vl)
				spliced = true
			}
		}
		if !spliced {
			out = append(out, text) // nothing resolvable: keep the raw line
		}
	}
	return out
}

// afterToken returns the inline value following the mark on its line, with a
// trailing comment cut; empty when the anchor introduces a block.
func afterToken(line string, mk Mark) string {
	_, commentCol, _ := scanLine(line)
	runes := []rune(line)
	if commentCol >= 0 {
		runes = runes[:commentCol]
	}
	if mk.Col+mk.Len >= len(runes) {
		return ""
	}
	return strings.TrimSpace(string(runes[mk.Col+mk.Len:]))
}

// aliasesOn lists the resolved alias marks sitting on buffer line no.
func aliasesOn(marks []Mark, no int) []Mark {
	var out []Mark
	for _, m := range marks {
		if m.Line == no && m.Alias && m.Anchor >= 0 {
			out = append(out, m)
		}
	}
	return out
}

// mergeLine reports whether a line is a `<<:` merge key.
func mergeLine(s string) bool {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "<<") {
		return false
	}
	t = strings.TrimSpace(t[2:])
	return strings.HasPrefix(t, ":")
}

// scanLine tokenizes one line: the anchor/alias tokens outside quoted scalars
// and comments, the rune column a comment starts at (-1 without one), and
// whether the line's value introduces a block scalar (| or > plus modifiers),
// whose body the caller must skip.
func scanLine(s string) (toks []token, commentCol int, blockScalar bool) {
	runes := []rune(s)
	commentCol = -1
	last := rune(0) // last significant rune outside quotes; 0 = line start
	for i := 0; i < len(runes); {
		r := runes[i]
		if r == ' ' || r == '\t' {
			i++
			continue
		}
		if r == '#' && (i == 0 || runes[i-1] == ' ' || runes[i-1] == '\t') {
			commentCol = i
			break
		}
		switch {
		case r == '\'' && nodeStart(last):
			// Single-quoted scalar; '' escapes a quote inside it.
			for i++; i < len(runes); i++ {
				if runes[i] == '\'' {
					if i+1 < len(runes) && runes[i+1] == '\'' {
						i++
						continue
					}
					break
				}
			}
			i++
			last = '\''
		case r == '"' && nodeStart(last):
			for i++; i < len(runes) && runes[i] != '"'; i++ {
				if runes[i] == '\\' {
					i++
				}
			}
			i++
			last = '"'
		case (r == '&' || r == '*') && nodeStart(last):
			start := i
			i++
			ns := i
			for i < len(runes) && !strings.ContainsRune(" \t,[]{}", runes[i]) {
				i++
			}
			if i == ns {
				last = r // a lone sigil is not a token
				continue
			}
			toks = append(toks, token{col: start, length: i - start, name: string(runes[ns:i]), alias: r == '*'})
			last = 'a' // the token ends a node start
		case (r == '|' || r == '>') && nodeStart(last):
			// Block-scalar indicator: only modifiers ([+-] and an indent
			// digit) may follow before spaces/comment; anything else is a
			// plain scalar that merely starts with the rune.
			j := i + 1
			for j < len(runes) && strings.ContainsRune("+-0123456789", runes[j]) {
				j++
			}
			rest := strings.TrimSpace(string(runes[j:]))
			if rest == "" || strings.HasPrefix(rest, "#") {
				blockScalar = true
				return toks, commentCol, blockScalar
			}
			last = r
			i++
		default:
			last = r
			i++
		}
	}
	return toks, commentCol, blockScalar
}

// nodeStart reports whether a token may begin after the given significant
// rune: at line start or after the YAML structure indicators — exactly the
// positions where & and * are reserved.
func nodeStart(last rune) bool {
	return last == 0 || strings.ContainsRune(":-,[{?", last)
}

// docMarker reports a document boundary: --- (new document) or ... (end) at
// column 0, alone or followed by content.
func docMarker(line string) bool {
	for _, m := range []string{"---", "..."} {
		if strings.HasPrefix(line, m) {
			rest := line[len(m):]
			if rest == "" || rest[0] == ' ' || rest[0] == '\t' {
				return true
			}
		}
	}
	return false
}

// indentOf counts leading space runes (YAML forbids tabs in indentation).
func indentOf(s string) int {
	n := 0
	for _, r := range s {
		if r != ' ' {
			break
		}
		n++
	}
	return n
}

// bline is one line of an anchored block: its buffer line number (for alias
// lookups during merge expansion) and its raw text ("" for a blank line).
type bline struct {
	no   int
	text string
}

// minIndent is the smallest indent across the non-blank block lines.
func minIndent(block []bline) int {
	min := -1
	for _, bl := range block {
		if bl.text == "" {
			continue
		}
		if n := indentOf(bl.text); min < 0 || n < min {
			min = n
		}
	}
	if min < 0 {
		return 0
	}
	return min
}

// dedent removes up to shift leading spaces.
func dedent(s string, shift int) string {
	n := indentOf(s)
	if n > shift {
		n = shift
	}
	return s[n:]
}
