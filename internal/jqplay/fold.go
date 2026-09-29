package jqplay

// fold.go computes the foldable regions of a *JSON* result: every multi-line
// object and array of the pretty-printed output, with the number of members
// it holds (#2029). The YAML half of the same job — indentation instead of
// delimiters — is yamlfold.go; both produce the Fold defined here, so the
// host installs and labels them through one code path (#2039). A jq result is
// regularly taller than the pane, and
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

	"github.com/charmbracelet/x/ansi"
)

// Fold is one foldable node of a result: the line it starts on, the line it
// ends on, and how many members it has. Unit names what Items counts and
// Closer the delimiter that finishes the placeholder — the two facts that
// differ between JSON's braced nodes and YAML's indented blocks, held as data
// so Label serves both (#2039).
type Fold struct {
	HeaderLine int
	EndLine    int
	Items      int
	// Unit is the plural noun Items is counted in — "keys" for a mapping,
	// "items" for a sequence, "lines" for a YAML block scalar. Label
	// singularizes it for a count of one.
	Unit string
	// Closer is the delimiter the placeholder ends with, so a folded JSON row
	// still reads as a complete value. YAML closes nothing and leaves it "".
	Closer string
	// Keys are the first keys of a mapping, in document order and capped at
	// maxPreviewKeys (#2782): LabelWithin previews as many as its width
	// budget allows, so a reader sees *what* is folded, not only how much.
	Keys []string
	// ElemType names the scalar type every item of a sequence shares
	// ("string", "number", …), "" when the items are mixed or containers.
	// Label then says `12 × string` instead of `12 items`.
	ElemType string
}

// maxPreviewKeys bounds how many keys a fold remembers for its preview: no
// pane is wide enough to show more, and a 10k-key object must not copy them.
const maxPreviewKeys = 16

// The units a fold counts its members in.
const (
	UnitKeys  = "keys"
	UnitItems = "items"
	UnitLines = "lines"
)

// Label is the placeholder a collapsed node renders as: the ellipsis, the
// member count in its own unit, and — in JSON — the closing delimiter, so a
// folded row still reads as a complete value (`"users": { ⋯ 3 keys }`).
func (f Fold) Label() string {
	label := "⋯ " + strconv.Itoa(f.Items) + " "
	if f.ElemType != "" {
		label = "⋯ " + strconv.Itoa(f.Items) + " × " + f.ElemType
	} else if f.Items == 1 {
		label += strings.TrimSuffix(f.Unit, "s")
	} else {
		label += f.Unit
	}
	if f.Closer != "" {
		label += " " + f.Closer
	}
	return label
}

// LabelWithin is Label with a preview of the mapping's first keys in front
// (`id, name, tags ⋯ 3 keys }`, #2782), as many as fit in budget cells; keys
// that did not fit are marked with a trailing `…`. The count always stays, so
// when not even one key fits the result is the plain Label.
func (f Fold) LabelWithin(budget int) string {
	base := f.Label()
	if len(f.Keys) == 0 {
		return base
	}
	baseW := ansi.StringWidth(base)
	best := base
	preview := ""
	for i, k := range f.Keys {
		if i > 0 {
			preview += ", "
		}
		preview += k
		shown := preview
		if i+1 < f.Items {
			shown += ", …"
		}
		if ansi.StringWidth(shown)+1+baseW > budget {
			break
		}
		best = shown + " " + base
	}
	return best
}

// Folds returns the foldable objects and arrays of a JSON result — the jq
// dialect's half of Dialect.Folds, kept under its original name for the tests
// and callers that only ever mean JSON.
func Folds(text string) []Fold { return jsonFolds(text) }

// jsonFolds returns the foldable objects and arrays of text in pre-order
// (outer before inner, the order the editor's innermost-fold lookup relies
// on). Single-line nodes are left out: they hide nothing, and a placeholder
// over `[]` would be longer than the value.
func jsonFolds(text string) []Fold {
	type frame struct {
		line   int
		object bool
		commas int
		filled bool
		// fresh is set right after the opener and after every comma: the
		// next token starts a member — an object's key, an array's item.
		fresh bool
		keys  []string
		// elem is the scalar type the array's items share so far; mixed
		// once two differ or one is a container (#2782).
		elem  string
		mixed bool
	}
	var stack []frame
	var out []Fold
	line, inString, escaped := 0, false, false
	// key collects the object key being read when the string started one.
	var key *strings.Builder
	// member is called on the first rune of every token: it marks the
	// enclosing node non-empty and, when the token starts a member, records
	// the key preview or the item's type.
	member := func(typ string) bool {
		n := len(stack)
		if n == 0 {
			return false
		}
		f := &stack[n-1]
		f.filled = true
		if !f.fresh {
			return false
		}
		f.fresh = false
		if f.object {
			return typ == "string" && len(f.keys) < maxPreviewKeys
		}
		switch {
		case typ == "" || (f.elem != "" && f.elem != typ):
			f.mixed = true
		default:
			f.elem = typ
		}
		return false
	}
	for _, r := range text {
		switch {
		case r == '\n':
			// A JSON string never carries a raw newline; resetting here keeps
			// an unterminated quote from swallowing the rest of the document.
			line, inString, escaped, key = line+1, false, false, nil
		case inString:
			switch {
			case escaped:
				escaped = false
			case r == '\\':
				escaped = true
			case r == '"':
				inString = false
				if key != nil {
					f := &stack[len(stack)-1]
					f.keys = append(f.keys, key.String())
					key = nil
				}
				continue
			}
			if key != nil {
				key.WriteRune(r)
			}
		case r == '"':
			inString = true
			if member("string") {
				key = &strings.Builder{}
			}
		case r == '{' || r == '[':
			member("")
			stack = append(stack, frame{line: line, object: r == '{', fresh: true})
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
				fold := Fold{HeaderLine: f.line, EndLine: line, Items: items, Unit: UnitItems, Closer: "]"}
				if f.object {
					fold.Unit, fold.Closer, fold.Keys = UnitKeys, "}", f.keys
				} else if !f.mixed {
					fold.ElemType = f.elem
				}
				out = append(out, fold)
			}
		case r == ',':
			if n := len(stack); n > 0 {
				stack[n-1].commas++
				stack[n-1].fresh = true
			}
		case r != ' ' && r != '\t' && r != '\r' && r != ':':
			member(jsonScalarType(r))
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

// jsonScalarType names the type of the scalar whose first rune is r, the way
// jq's `type` does. Only called for runes outside strings and delimiters.
func jsonScalarType(r rune) string {
	switch r {
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	}
	return "number"
}
