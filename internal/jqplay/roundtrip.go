package jqplay

// roundtrip.go is the yq playground's round-trip output (#2798). The plain
// yq path re-serialises every output from the decoded value, so comments,
// anchors, quoting style and key order are gone from the result — for an
// edit-style program (`.spec.replicas = 3`) over a commented manifest that
// is exactly the part the user wanted to keep, and the reason to leave the
// playground for the real `yq`. With the toggle on, the program still runs
// over the decoded tree, but the output is produced by applying the *changes*
// back onto the original yaml.v3 node tree: an untouched subtree is the
// original node, comments and all; a changed scalar is a fresh node carrying
// the old one's comments; a deleted key vanishes with its comments; a new
// key is appended in gojq's order.
//
// The scope is deliberately narrow: one output per input document, with the
// same root shape as the document it came from. A program that reshapes the
// document — `map`, `to_entries`, `.items[]`, several outputs — falls back
// to the plain serializer, and the result reports why on the info row
// (Result.Note). The patched document is *verified* before it is shown:
// re-decoded, it must equal the program's output value, so a change the tree
// cannot express (deleting a key a merge supplies, a merge whose source
// changed) falls back rather than showing a document that means something
// else. Patching and verifying spend the same node budget the decoder
// applies to alias expansion (MaxYAMLNodes).
//
// Anchors and aliases need one more rule. The program ran over the
// *expanded* tree, so editing the node behind `&tpl` leaves every `*tpl`
// with its old value in the output. A patched anchor therefore turns the
// aliases that still hold the old value into copies of the old node — the
// alias is written out, the anchor stays where it was — and the result
// means exactly what the program produced. The input's own trees are never
// modified: every changed node is a copy, and an unchanged subtree is
// shared.

import (
	"context"
	"encoding/json"
	"math"
	"math/big"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// roundTripSkipped phrases the info-row note for an output the round-trip
// could not produce: the plain serializer wrote it instead.
func roundTripSkipped(why string) string { return "round-trip skipped: " + why }

// The reasons a round-trip falls back, as the info row shows them.
const (
	rtSkipCompact  = "compact output (-c)"
	rtSkipSlurp    = "slurped input (-s)"
	rtSkipMulti    = "the program produced several outputs"
	rtSkipReshaped = "the program reshaped the document"
	rtSkipNoDoc    = "no source document to patch"
	rtSkipTooBig   = "the document is too large to patch"
	rtSkipDiffers  = "the patched document reads back differently"
)

// roundTripYAML renders cur — the program's output for the document doc,
// which decoded to old — by patching doc's node tree. why names the reason
// when it cannot; text is then empty and the caller renders plainly.
func roundTripYAML(doc *yaml.Node, old, cur any) (text, why string) {
	if doc == nil {
		return "", rtSkipNoDoc
	}
	if valueShape(old) != valueShape(cur) {
		return "", rtSkipReshaped
	}
	root := doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			root = nil
		} else {
			root = doc.Content[0]
		}
	}
	var patched *yaml.Node
	if root == nil {
		patched = yamlNode(cur)
	} else {
		var ok bool
		patched, ok = patchTree(root, old, cur)
		if !ok {
			return "", rtSkipTooBig
		}
	}
	out := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{stripMergeTags(patched)}}
	if doc.Kind == yaml.DocumentNode {
		out.HeadComment, out.LineComment, out.FootComment = doc.HeadComment, doc.LineComment, doc.FootComment
	}
	text = encodeYAMLNode(out)
	// Verify: the text must read back as the value the program produced.
	// This is what makes the fallback honest — a tree that cannot express a
	// change never shows a document that means something else.
	var re yaml.Node
	if err := yaml.Unmarshal([]byte(text), &re); err != nil {
		return "", rtSkipDiffers
	}
	budget := MaxYAMLNodes
	got, err := yamlValue(&re, &budget)
	if err != nil {
		return "", rtSkipTooBig
	}
	budget = MaxYAMLNodes
	same, ok := sameValue(got, cur, &budget)
	if !ok {
		return "", rtSkipTooBig
	}
	if !same {
		return "", rtSkipDiffers
	}
	return text, ""
}

// patchTree patches root (decoded to old) into the tree for cur, in one
// pass — or two, when the first replaced an anchored node: the aliases that
// still hold the old value are then written out as copies of it, so the
// text means what the program produced.
func patchTree(root *yaml.Node, old, cur any) (*yaml.Node, bool) {
	budget := MaxYAMLNodes
	p := &patcher{budget: &budget, changed: map[string]bool{}}
	out, ok := p.node(root, old, cur)
	if !ok {
		return nil, false
	}
	if len(p.changed) == 0 {
		return out, true
	}
	p = &patcher{budget: &budget, changed: map[string]bool{}, inline: p.changed}
	return p.node(root, old, cur)
}

// encodeYAMLNode renders one node tree as encodeYAML renders a value: the
// playground's indentation, no trailing newline.
func encodeYAMLNode(n *yaml.Node) string {
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(yamlIndent)
	if err := enc.Encode(n); err != nil {
		return ""
	}
	if err := enc.Close(); err != nil {
		return ""
	}
	return strings.TrimRight(b.String(), "\n")
}

// stripMergeTags returns n with every merge key's implicit `!!merge` tag
// cleared, copying only the path down to each: yaml.v3 writes the tag out
// (`!!merge <<: *b`) although it resolved it itself on the way in, and the
// plain `<<` reads back as a merge all the same.
func stripMergeTags(n *yaml.Node) *yaml.Node {
	if n == nil || len(n.Content) == 0 {
		return n
	}
	var content []*yaml.Node
	for i, c := range n.Content {
		nc := c
		if n.Kind == yaml.MappingNode && i%2 == 0 && c.Tag == "!!merge" && c.Style&yaml.TaggedStyle == 0 {
			cp := *c
			cp.Tag = ""
			nc = &cp
		} else {
			nc = stripMergeTags(c)
		}
		if nc != c && content == nil {
			content = append(make([]*yaml.Node, 0, len(n.Content)), n.Content[:i]...)
		}
		if content != nil {
			content = append(content, nc)
		}
	}
	if content == nil {
		return n
	}
	cp := *n
	cp.Content = content
	return &cp
}

// valueShape classes a value by its root kind: the round-trip keeps a
// mapping a mapping and a sequence a sequence — anything else is a reshape.
func valueShape(v any) int {
	switch v.(type) {
	case map[string]any:
		return 2
	case []any:
		return 1
	}
	return 0
}

// patcher is one pass of patchTree over a document.
type patcher struct {
	budget *int
	// changed collects the anchors whose node this pass replaced.
	changed map[string]bool
	// inline, on the second pass, names the anchors the first pass changed:
	// an alias to one that still holds the old value is written out.
	inline map[string]bool
}

// node returns the node for cur, given that orig decoded to old: orig
// itself when nothing changed, a patched copy for a mapping or sequence
// whose members changed, and a fresh node — with orig's comments and
// anchor — where the value itself is new. ok is false once the budget is
// spent.
func (p *patcher) node(orig *yaml.Node, old, cur any) (*yaml.Node, bool) {
	if *p.budget <= 0 {
		return nil, false
	}
	*p.budget--
	same, ok := sameValue(old, cur, p.budget)
	if !ok {
		return nil, false
	}
	if same {
		if orig.Kind == yaml.AliasNode && p.inline[orig.Value] {
			return copyTree(orig.Alias), true
		}
		if !p.mentionsInlined(orig) {
			return orig, true
		}
	}
	var out *yaml.Node
	switch orig.Kind {
	case yaml.MappingNode:
		if om, ok1 := old.(map[string]any); ok1 {
			if cm, ok2 := cur.(map[string]any); ok2 {
				out, ok = p.mapping(orig, om, cm)
			}
		}
	case yaml.SequenceNode:
		if oa, ok1 := old.([]any); ok1 {
			if ca, ok2 := cur.([]any); ok2 {
				out, ok = p.sequence(orig, oa, ca)
			}
		}
	}
	if !ok {
		return nil, false
	}
	if out == nil {
		out = freshNode(orig, cur)
	}
	if orig.Anchor != "" && !same {
		p.changed[orig.Anchor] = true
	}
	return out, true
}

// mentionsInlined reports whether an unchanged subtree holds an alias the
// second pass has to write out — the one reason not to share it as is.
func (p *patcher) mentionsInlined(n *yaml.Node) bool {
	if len(p.inline) == 0 {
		return false
	}
	if n.Kind == yaml.AliasNode {
		return p.inline[n.Value]
	}
	for _, c := range n.Content {
		if p.mentionsInlined(c) {
			return true
		}
	}
	return false
}

// mapping rewrites a mapping's pairs: an explicit key keeps its key node
// and patches its value, a key the output dropped goes with its comments, a
// merge pair (`<<: *base`) stays as written, and a key the output added —
// or one a merge supplied whose value changed — is appended as an explicit
// pair, which is what wins over the merge on the way back in.
func (p *patcher) mapping(orig *yaml.Node, om, cm map[string]any) (*yaml.Node, bool) {
	n := shallowNode(orig)
	n.Content = make([]*yaml.Node, 0, len(orig.Content))
	seen := make(map[string]bool, len(orig.Content)/2)
	for i := 0; i+1 < len(orig.Content); i += 2 {
		k, v := orig.Content[i], orig.Content[i+1]
		if k.Tag == "!!merge" {
			n.Content = append(n.Content, k, v)
			continue
		}
		key, err := yamlKey(k, p.budget)
		if err != nil {
			return nil, false
		}
		seen[key] = true
		nv, present := cm[key]
		if !present {
			continue // deleted: the key, its value and their comments go
		}
		pv, ok := p.node(v, om[key], nv)
		if !ok {
			return nil, false
		}
		n.Content = append(n.Content, k, pv)
	}
	for _, key := range sortedKeys(cm) {
		if seen[key] {
			continue
		}
		if ov, merged := om[key]; merged {
			// Supplied by a merge: still is, unless the output changed it.
			same, ok := sameValue(ov, cm[key], p.budget)
			if !ok {
				return nil, false
			}
			if same {
				continue
			}
		}
		n.Content = append(n.Content, yamlString(key), yamlNode(cm[key]))
	}
	return n, true
}

// sequence rewrites a sequence's items, aligning the output's items with
// the original's so a deleted or inserted item shifts its neighbours
// instead of rewriting them: an item found unchanged later in the original
// is that node (a deletion before it), an original item found unchanged
// later in the output is skipped over for now (an insertion before it), and
// otherwise the items are patched pairwise. The alignment searches are
// bounded by sequenceAlignLimit; larger sequences patch pairwise.
func (p *patcher) sequence(orig *yaml.Node, oa, ca []any) (*yaml.Node, bool) {
	n := shallowNode(orig)
	n.Content = make([]*yaml.Node, 0, len(ca))
	align := len(oa)*len(ca) <= sequenceAlignLimit
	j := 0
	for i, cv := range ca {
		if align {
			k, found, spent := findSame(oa, j, cv, p.budget)
			if !spent {
				return nil, false
			}
			if found {
				item, ok := p.node(orig.Content[k], oa[k], cv)
				if !ok {
					return nil, false
				}
				n.Content = append(n.Content, item)
				j = k + 1
				continue
			}
			if j < len(oa) {
				_, found, spent := findSame(ca, i+1, oa[j], p.budget)
				if !spent {
					return nil, false
				}
				if found {
					n.Content = append(n.Content, yamlNode(cv)) // inserted before oa[j]
					continue
				}
			}
		}
		if j < len(oa) {
			pv, ok := p.node(orig.Content[j], oa[j], cv)
			if !ok {
				return nil, false
			}
			n.Content = append(n.Content, pv)
			j++
			continue
		}
		n.Content = append(n.Content, yamlNode(cv))
	}
	return n, true
}

// sequenceAlignLimit bounds the pairwise searches of patcher.sequence by
// the product of the two lengths; beyond it items are patched positionally.
const sequenceAlignLimit = 4096

// findSame returns the index at or after from of the first item of items
// equal to v. spent is false once the budget ran out.
func findSame(items []any, from int, v any, budget *int) (idx int, found, spent bool) {
	for k := from; k < len(items); k++ {
		same, ok := sameValue(items[k], v, budget)
		if !ok {
			return 0, false, false
		}
		if same {
			return k, true, true
		}
	}
	return 0, false, true
}

// shallowNode copies a node without its content: kind, tag, style, anchor,
// comments and position come along, the members are the caller's to fill.
func shallowNode(orig *yaml.Node) *yaml.Node {
	n := *orig
	n.Content = nil
	return &n
}

// copyTree copies a subtree for writing an alias out in place: the same
// nodes, comments and styles, without the anchors — the originals keep
// theirs, and a document may define each once.
func copyTree(n *yaml.Node) *yaml.Node {
	if n == nil {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	}
	cp := *n
	cp.Anchor = ""
	if len(n.Content) > 0 {
		cp.Content = make([]*yaml.Node, len(n.Content))
		for i, c := range n.Content {
			cp.Content[i] = copyTree(c)
		}
	}
	return &cp
}

// freshNode builds the node for a new value in orig's place: yamlNode's
// rendering, carrying orig's comments so `replicas: 3 # scale me` keeps its
// remark, and orig's anchor so the aliases that name it still resolve. A
// string keeps a quoted style the original had, so `image: 'alpine:3'`
// edited stays single-quoted.
func freshNode(orig *yaml.Node, cur any) *yaml.Node {
	n := yamlNode(cur)
	n.HeadComment, n.LineComment, n.FootComment = orig.HeadComment, orig.LineComment, orig.FootComment
	n.Anchor = orig.Anchor
	if _, isStr := cur.(string); isStr && orig.Kind == yaml.ScalarNode && orig.Tag == "!!str" {
		switch orig.Style {
		case yaml.SingleQuotedStyle, yaml.DoubleQuotedStyle:
			if n.Style == 0 {
				n.Style = orig.Style
			}
		}
	}
	return n
}

// sameValue reports whether two decoded values are equal, numbers by their
// value rather than by their Go type — the decoder hands the program a
// json.Number where an assignment hands back an int. Each visited value
// spends one unit of budget; ok is false once it is gone.
func sameValue(a, b any, budget *int) (same, ok bool) {
	if *budget <= 0 {
		return false, false
	}
	*budget--
	switch av := a.(type) {
	case nil:
		return b == nil, true
	case bool:
		bv, isB := b.(bool)
		return isB && av == bv, true
	case string:
		bv, isS := b.(string)
		return isS && av == bv, true
	case []any:
		bv, isA := b.([]any)
		if !isA || len(av) != len(bv) {
			return false, true
		}
		for i := range av {
			same, ok := sameValue(av[i], bv[i], budget)
			if !ok || !same {
				return same, ok
			}
		}
		return true, true
	case map[string]any:
		bv, isM := b.(map[string]any)
		if !isM || len(av) != len(bv) {
			return false, true
		}
		for k, x := range av {
			y, present := bv[k]
			if !present {
				return false, true
			}
			same, ok := sameValue(x, y, budget)
			if !ok || !same {
				return same, ok
			}
		}
		return true, true
	}
	ar, aNum := numRat(a)
	br, bNum := numRat(b)
	if !aNum || !bNum {
		return false, true
	}
	if ar == nil || br == nil {
		// An infinity or NaN: equal only as the same float spelling.
		return ar == nil && br == nil && yamlFloat(floatOf(a)) == yamlFloat(floatOf(b)), true
	}
	return ar.Cmp(br) == 0, true
}

// numRat reads a numeric value exactly; nil with isNum for a float that has
// no rational value (an infinity, NaN).
func numRat(v any) (r *big.Rat, isNum bool) {
	switch t := v.(type) {
	case json.Number:
		r, ok := new(big.Rat).SetString(t.String())
		if !ok {
			return nil, false
		}
		return r, true
	case int:
		return new(big.Rat).SetInt64(int64(t)), true
	case int64:
		return new(big.Rat).SetInt64(t), true
	case *big.Int:
		return new(big.Rat).SetInt(t), true
	case float64:
		if math.IsInf(t, 0) || math.IsNaN(t) {
			return nil, true
		}
		r, ok := new(big.Rat).SetString(strconv.FormatFloat(t, 'g', -1, 64))
		if !ok {
			return nil, false
		}
		return r, true
	}
	return nil, false
}

// floatOf is the float64 behind a non-rational numeric value (NaN otherwise).
func floatOf(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return math.NaN()
}

// roundTripper is the stream's round-trip state (#2798): the one-output
// lookahead that decides whether an output is its input document's only
// one, and the note the pages report.
type roundTripper struct {
	// skip, when set, is a reason that applies to every output — the
	// toggles or the input rule it out before any program runs.
	skip string
	// la is an output pulled ahead of its turn to learn whether the one
	// before it was its document's only output; end is the stream's end
	// pulled ahead the same way.
	la  *rtLookahead
	end *rtEnd
	// multi is the input index known to produce several outputs, -1 for
	// none yet: its outputs render plainly without another lookahead.
	multi int
	// note is the first reason an output fell back, "" while none did.
	note string
}

type rtLookahead struct {
	out any
	idx int
}

type rtEnd struct{ errMsg string }

// newRoundTripper builds the stream's round-trip state for opts over in,
// nil when the toggle is off or the dialect has no node trees to patch.
func newRoundTripper(in *Input, opts Options) *roundTripper {
	if !opts.RoundTrip || in.Dialect() != DialectYQ {
		return nil
	}
	rt := &roundTripper{multi: -1}
	switch {
	case opts.Compact:
		rt.skip = rtSkipCompact
	case opts.Slurp:
		rt.skip = rtSkipSlurp
	case len(in.docs) == 0:
		rt.skip = rtSkipNoDoc
	}
	if rt.skip != "" {
		rt.note = roundTripSkipped(rt.skip)
	}
	return rt
}

// next is stream.next under the round-trip: the output and its rendered
// text — patched when it is its document's only output and the patch
// verifies, plain otherwise, with the note recording the first fallback.
func (rt *roundTripper) next(ctx context.Context, s *stream) (out any, text string, errMsg string, ok bool) {
	var idx int
	switch {
	case rt.la != nil:
		out, idx = rt.la.out, rt.la.idx
		rt.la = nil
	case rt.end != nil:
		e := rt.end
		rt.end = nil
		return nil, "", e.errMsg, false
	default:
		o, errMsg, ok := s.pull(ctx)
		if !ok {
			return nil, "", errMsg, false
		}
		out, idx = o, s.lastIdx
	}
	if rt.skip != "" {
		return out, s.in.dialect.encodeWith(out, s.opts), "", true
	}
	if idx != rt.multi {
		// Look one output ahead: a second one from the same document means
		// neither is *the* document.
		o2, errMsg2, ok2 := s.pull(ctx)
		if ok2 {
			rt.la = &rtLookahead{out: o2, idx: s.lastIdx}
			if s.lastIdx == idx {
				rt.multi = idx
			}
		} else {
			rt.end = &rtEnd{errMsg: errMsg2}
		}
	}
	if idx == rt.multi {
		rt.fallback(rtSkipMulti)
		return out, s.in.dialect.encodeWith(out, s.opts), "", true
	}
	if str, isStr := out.(string); isStr && s.opts.Raw {
		return out, str, "", true // -r: a bare string is what the user asked for
	}
	var doc *yaml.Node
	if idx < len(s.in.docs) {
		doc = s.in.docs[idx]
	}
	text, why := roundTripYAML(doc, s.in.values[idx], out)
	if why != "" {
		rt.fallback(why)
		return out, s.in.dialect.encodeWith(out, s.opts), "", true
	}
	return out, text, "", true
}

// fallback records the first reason an output was rendered plainly.
func (rt *roundTripper) fallback(why string) {
	if rt.note == "" {
		rt.note = roundTripSkipped(why)
	}
}
