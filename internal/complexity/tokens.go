package complexity

// tokens.go is the normalized token stream duplicate detection runs on
// (Roadmap 0560, #3044). The index measures every file once; the same
// parse yields, per file, the stream of its measured units' syntax nodes
// in pre-order — every named node of the highlight.SyntaxTree snapshot,
// which keeps the grammar's named nodes and the infix operator, so the
// stream encodes the tree's shape as well as its leaves. Each node becomes
// one 32-bit hash of its normalized spelling:
//
//   - an identifier leaf (identifier, type_identifier, field_identifier,
//     name, variable_name, …) becomes one placeholder, whatever its text, so
//     a copy with renamed variables, parameters, fields or types still
//     matches (a type-2 clone) — except the name of what is called or
//     accessed (the callee of a call, the member of a selector), which
//     keeps its text: two blocks calling different functions do the same
//     thing only by accident, and dropping those names drowned the real
//     clones of this repository in table rows that merely share a shape;
//   - a literal leaf (int_literal, string_content, number, …) becomes one
//     placeholder per node kind, so a changed constant or message still
//     matches but a string swapped for a number does not;
//   - a comment is dropped with its subtree;
//   - every other node hashes its kind and, for an inner node, its operator
//     (`a + b` and `a && b` differ) — a leaf of another kind (a keyword-like
//     break_statement, a primitive_type) hashes its text too.
//
// Only the measured units contribute: tokens outside every function are
// dropped, and the stream is cut into one segment per top-level unit, so a
// clone never spans two functions — the fix (#3045) extracts a helper from
// inside a function. A nested unit (a closure) is part of its enclosing
// unit's segment.

import (
	"encoding/binary"
	"errors"
	"hash/fnv"
	"sort"
	"strings"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// Tokens is one file's normalized token stream: parallel slices of the
// token hashes and their 0-based lines, and the segment starts. Segment i
// spans the tokens [Seg[i], Seg[i+1]) (the last one runs to len(Hash)); a
// clone never crosses a segment. Lang is the language id the stream was
// built from: clones are only looked for among files of one language.
type Tokens struct {
	Lang string
	Hash []uint32
	Line []int32
	Seg  []int32
}

// Len counts the tokens.
func (t Tokens) Len() int { return len(t.Hash) }

// segEnd is the end (exclusive) of segment i.
func (t Tokens) segEnd(i int) int {
	if i+1 < len(t.Seg) {
		return int(t.Seg[i+1])
	}
	return len(t.Hash)
}

// MeasureTokens is Measure plus the file's token stream from the same
// parse (#3044): the index's extractor, so a file is parsed once for both.
func MeasureTokens(langID string, lines []string) ([]Function, Tokens, error) {
	l, ok := lang.ByID(langID)
	if !ok || l.Complexity == nil {
		return nil, Tokens{}, ErrUnsupported
	}
	root := syntaxTree(langID, lines)
	if root == nil {
		return nil, Tokens{}, ErrUnsupported
	}
	fns := measure(root, lines, l.Complexity, comments{line: l.LineComment, block: l.BlockComment})
	return fns, tokenize(root, strings.Join(lines, "\n"), langID, fns), nil
}

// The placeholder spellings of the normalized leaves.
const (
	tokIdent = "\x00id"
	tokLit   = "\x00lit:"
)

// lineRange is one top-level unit's line span.
type lineRange struct{ lo, hi int }

// unitRanges merges fns' line spans into the disjoint top-level ranges:
// a nested unit lies inside its parent's span and vanishes into it.
func unitRanges(fns []Function) []lineRange {
	rs := make([]lineRange, 0, len(fns))
	for _, f := range fns {
		rs = append(rs, lineRange{f.Line, f.EndLine})
	}
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].lo != rs[j].lo {
			return rs[i].lo < rs[j].lo
		}
		return rs[i].hi > rs[j].hi
	})
	out := rs[:0]
	for _, r := range rs {
		if n := len(out); n > 0 && r.lo <= out[n-1].hi {
			if r.hi > out[n-1].hi {
				out[n-1].hi = r.hi
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// tokenize builds the stream of root's nodes inside the units fns.
func tokenize(root *highlight.SyntaxNode, src, langID string, fns []Function) Tokens {
	ranges := unitRanges(fns)
	t := Tokens{Lang: langID}
	if len(ranges) == 0 {
		return t
	}
	// rangeAt is the index of the range holding line, -1 for none; the
	// walk is in source order, so it only ever moves forward.
	ri := 0
	rangeAt := func(line int) int {
		for ri < len(ranges) && line > ranges[ri].hi {
			ri++
		}
		if ri < len(ranges) && line >= ranges[ri].lo {
			return ri
		}
		return -1
	}
	seg := -1
	h := fnv.New32a()
	emit := func(spell string, line int, r int) {
		if r != seg {
			t.Seg = append(t.Seg, int32(len(t.Hash)))
			seg = r
		}
		h.Reset()
		h.Write([]byte(spell))
		t.Hash = append(t.Hash, h.Sum32())
		t.Line = append(t.Line, int32(line))
	}
	type item struct {
		n      *highlight.SyntaxNode
		parent string
	}
	stack := []item{{n: root}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		n := it.n
		stack = stack[:len(stack)-1]
		if n != root && isComment(n.Kind) {
			continue
		}
		if n.EndLine < ranges[0].lo || n.StartLine > ranges[len(ranges)-1].hi {
			continue
		}
		if n != root {
			if r := rangeAt(n.StartLine); r >= 0 {
				emit(spelling(n, it.parent, src), n.StartLine, r)
			}
		}
		for i := len(n.Children) - 1; i >= 0; i-- {
			stack = append(stack, item{n.Children[i], n.Kind})
		}
	}
	return t
}

// spelling is n's normalized text (see the file comment).
func spelling(n *highlight.SyntaxNode, parent, src string) string {
	if len(n.Children) > 0 {
		if n.Operator != "" {
			return n.Kind + "\x00" + n.Operator
		}
		return n.Kind
	}
	switch {
	case isIdentifier(n.Kind):
		if isAccess(parent) {
			return n.Kind + "\x00" + n.Text(src)
		}
		return tokIdent
	case isLiteral(n.Kind):
		return tokLit + n.Kind
	}
	return n.Kind + "\x00" + n.Text(src)
}

// isComment reports a comment node kind (comment, line_comment,
// block_comment, doc_comment, …).
func isComment(kind string) bool { return strings.Contains(kind, "comment") }

// isIdentifier reports the leaf kinds that name something: identifier and
// its grammar variants (type_identifier, field_identifier,
// property_identifier, package_identifier), name and variable_name.
func isIdentifier(kind string) bool {
	return kind == "identifier" || strings.HasSuffix(kind, "_identifier") ||
		kind == "name" || strings.HasSuffix(kind, "_name")
}

// literalMarks are the substrings of a literal leaf's kind: Go's
// int_literal / interpreted_string_literal_content / rune_literal, Python's
// integer / float / string_content / string_start, JavaScript's number /
// string_fragment, PHP's integer / string_content / encapsed_string, shell's
// number / raw_string / heredoc_content.
var literalMarks = []string{"literal", "string", "number", "integer", "float", "escape", "fragment", "content", "rune", "char", "heredoc"}

// isLiteral reports a literal leaf kind.
func isLiteral(kind string) bool {
	for _, mark := range literalMarks {
		if strings.Contains(kind, mark) {
			return true
		}
	}
	return false
}

// GobEncode packs the stream for the index cache (#2967): the hashes as
// raw little-endian words, the lines and segment starts as varint deltas
// — a fifth of gob's default struct encoding over four million tokens.
func (t Tokens) GobEncode() ([]byte, error) {
	buf := make([]byte, 0, 8+len(t.Lang)+4*len(t.Hash)+2*len(t.Line)+2*len(t.Seg))
	buf = binary.AppendUvarint(buf, uint64(len(t.Lang)))
	buf = append(buf, t.Lang...)
	buf = binary.AppendUvarint(buf, uint64(len(t.Hash)))
	for _, h := range t.Hash {
		buf = binary.LittleEndian.AppendUint32(buf, h)
	}
	buf = binary.AppendUvarint(buf, uint64(len(t.Line)))
	var prev int32
	for _, l := range t.Line {
		buf = binary.AppendVarint(buf, int64(l-prev))
		prev = l
	}
	buf = binary.AppendUvarint(buf, uint64(len(t.Seg)))
	prev = 0
	for _, s := range t.Seg {
		buf = binary.AppendVarint(buf, int64(s-prev))
		prev = s
	}
	return buf, nil
}

// errTokens is the decode error of a damaged stream.
var errTokens = errors.New("complexity: damaged token stream")

// GobDecode unpacks GobEncode's bytes.
func (t *Tokens) GobDecode(buf []byte) error {
	uvarint := func() (int, bool) {
		v, n := binary.Uvarint(buf)
		if n <= 0 || v > uint64(len(buf)) {
			return 0, false
		}
		buf = buf[n:]
		return int(v), true
	}
	varint := func() (int32, bool) {
		v, n := binary.Varint(buf)
		if n <= 0 {
			return 0, false
		}
		buf = buf[n:]
		return int32(v), true
	}
	n, ok := uvarint()
	if !ok {
		return errTokens
	}
	*t = Tokens{Lang: string(buf[:n])}
	buf = buf[n:]
	if n, ok = uvarint(); !ok || 4*n > len(buf) {
		return errTokens
	}
	t.Hash = make([]uint32, n)
	for i := range t.Hash {
		t.Hash[i] = binary.LittleEndian.Uint32(buf[4*i:])
	}
	buf = buf[4*n:]
	if n, ok = uvarint(); !ok {
		return errTokens
	}
	t.Line = make([]int32, n)
	var prev int32
	for i := range t.Line {
		d, ok := varint()
		if !ok {
			return errTokens
		}
		prev += d
		t.Line[i] = prev
	}
	if n, ok = uvarint(); !ok {
		return errTokens
	}
	t.Seg = make([]int32, n)
	prev = 0
	for i := range t.Seg {
		d, ok := varint()
		if !ok {
			return errTokens
		}
		prev += d
		t.Seg[i] = prev
	}
	if len(t.Line) != len(t.Hash) {
		return errTokens
	}
	return nil
}

// accessMarks are substrings of the node kinds whose identifier children
// name what is called or accessed rather than a local: Go's call_expression
// and selector_expression, JavaScript's member_expression, Python's
// attribute, PHP's member_call_expression and scoped_call_expression,
// shell's command_name.
var accessMarks = []string{"call", "selector", "member", "attribute", "scoped", "command"}

// isAccess reports a call / member-access parent kind.
func isAccess(kind string) bool {
	for _, mark := range accessMarks {
		if strings.Contains(kind, mark) {
			return true
		}
	}
	return false
}
