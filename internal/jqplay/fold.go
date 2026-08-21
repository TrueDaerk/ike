package jqplay

// fold.go computes the foldable regions of a *result*: every multi-line
// object and array of the pretty-printed output, with the number of members
// it holds (#2029). A jq result is regularly taller than the pane, and
// scanning its shape before opening the interesting branch is what folding is
// for — the same thing #144 gives an ordinary buffer.
//
// The ranges are computed here rather than taken from the Tree-sitter parse
// of the substitute buffer for two reasons: the result window must fold in a
// cgo-free build too (no grammar there), and the placeholder is supposed to
// say *how big* the collapsed node is — "12 items", not "12 lines", which the
// generic fold tag cannot know. The host installs them on the result editor,
// so the collapsing itself, the vim z-commands and every fold-aware motion
// remain the editor's (#1741) — the playground grows no second fold engine.
//
// The scan is a plain rune walk over the encoded text, not a re-decode: the
// text was produced by encode() (gojq.Marshal + json.Indent) and is valid
// JSON by construction, so structure is a matter of counting delimiters
// outside strings. Malformed text cannot arrive here, but an unbalanced tail
// simply yields no fold for the unclosed node rather than an error.

import (
	"sort"
	"strconv"
	"strings"
)

// Fold is one foldable node of a result: the line its opening delimiter sits
// on, the line its closing delimiter sits on, and how many members it has.
// Object reports which spelling the placeholder uses — keys or items.
type Fold struct {
	HeaderLine int
	EndLine    int
	Items      int
	Object     bool
}

// Label is the placeholder a collapsed node renders as: the ellipsis, the
// member count in its own unit, and the closing delimiter — so a folded row
// still reads as a complete value (`"users": { ⋯ 3 keys }`).
func (f Fold) Label() string {
	unit := "items"
	if f.Object {
		unit = "keys"
	}
	if f.Items == 1 {
		unit = strings.TrimSuffix(unit, "s")
	}
	closer := "]"
	if f.Object {
		closer = "}"
	}
	return "⋯ " + strconv.Itoa(f.Items) + " " + unit + " " + closer
}

// Folds returns the foldable objects and arrays of text in pre-order (outer
// before inner, the order the editor's innermost-fold lookup relies on).
// Single-line nodes are left out: they hide nothing, and a placeholder over
// `[]` would be longer than the value.
func Folds(text string) []Fold {
	type frame struct {
		line   int
		object bool
		commas int
		filled bool
	}
	var stack []frame
	var out []Fold
	line, inString, escaped := 0, false, false
	content := func() {
		if n := len(stack); n > 0 {
			stack[n-1].filled = true
		}
	}
	for _, r := range text {
		switch {
		case r == '\n':
			// A JSON string never carries a raw newline; resetting here keeps
			// an unterminated quote from swallowing the rest of the document.
			line, inString, escaped = line+1, false, false
		case inString:
			switch {
			case escaped:
				escaped = false
			case r == '\\':
				escaped = true
			case r == '"':
				inString = false
			}
		case r == '"':
			inString = true
			content()
		case r == '{' || r == '[':
			content()
			stack = append(stack, frame{line: line, object: r == '{'})
		case r == '}' || r == ']':
			if len(stack) == 0 {
				continue // unbalanced tail: nothing to close
			}
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if line > f.line {
				items := 0
				if f.filled {
					items = f.commas + 1
				}
				out = append(out, Fold{HeaderLine: f.line, EndLine: line, Items: items, Object: f.object})
			}
		case r == ',':
			if n := len(stack); n > 0 {
				stack[n-1].commas++
			}
		case r != ' ' && r != '\t' && r != '\r':
			content()
		}
	}
	// The walk closes inner nodes first; the consumers want pre-order.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].HeaderLine != out[j].HeaderLine {
			return out[i].HeaderLine < out[j].HeaderLine
		}
		return out[i].EndLine > out[j].EndLine
	})
	return out
}
