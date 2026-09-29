package jqplay

// xmqcomplete.go completes XPath arguments on the xmq query line (#2790):
// after a path-taking command (`select /html/body/`) the popup offers the
// element names, `@attributes` and root steps that exist at that path in the
// parsed document — the jq key completion's analogue for markup. The
// document is read once when the input is parsed (parseXMQ, off the UI
// goroutine for large inputs) into a small element tree: XML through a
// lenient encoding/xml scan like htmldom.XMLXPathAt, HTML through the DOM
// inspector's parser (htmldom.Parse), the tree the at-path seed's XPath
// addresses. The path walk is capped by completionNodeBudget per stage like
// the jq key walk, so a huge document answers with what it saw instead of
// stalling the keystroke.

import (
	"encoding/xml"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"

	"ike/internal/htmldom"
)

// xmqNode is one element of the completion tree; the document node is the
// nameless root.
type xmqNode struct {
	name   string
	attrs  []string
	kids   []*xmqNode
	parent *xmqNode
}

// xmqPathCommands are the commands whose first argument is an XPath.
var xmqPathCommands = map[string]bool{
	"select": true, "delete": true, "sort": true,
	"replace": true, "substitute": true, "for-each": true,
}

// buildXMQTree parses the document for completion. HTML is recognized the
// way the xmq CLI itself decides (a leading doctype or <html>), so the tree
// is the one the binary's XPath runs over.
func buildXMQTree(text string) *xmqNode {
	head := strings.ToLower(strings.TrimLeftFunc(text, unicode.IsSpace))
	if strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html") {
		return htmlXMQTree(htmldom.Parse(text).Root)
	}
	return xmlXMQTree(text)
}

// htmlXMQTree converts the htmldom tree's elements.
func htmlXMQTree(root *html.Node) *xmqNode {
	var conv func(n *html.Node, into *xmqNode)
	conv = func(n *html.Node, into *xmqNode) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			x := &xmqNode{name: c.Data, parent: into}
			for _, a := range c.Attr {
				x.attrs = appendUnique(x.attrs, a.Key)
			}
			into.kids = append(into.kids, x)
			conv(c, x)
		}
	}
	doc := &xmqNode{}
	conv(root, doc)
	return doc
}

// xmlXMQTree scans XML leniently — a half-edited document still yields the
// part that parses. Names are spelled as written (`prefix:local`), which is
// how an XPath over the document names them; namespace declarations are not
// attributes.
func xmlXMQTree(text string) *xmqNode {
	dec := xml.NewDecoder(strings.NewReader(text))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	doc := &xmqNode{}
	cur := doc
	for {
		tok, err := dec.RawToken()
		if err != nil {
			return doc
		}
		switch t := tok.(type) {
		case xml.StartElement:
			x := &xmqNode{name: xmlName(t.Name), parent: cur}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
					continue
				}
				x.attrs = appendUnique(x.attrs, xmlName(a.Name))
			}
			cur.kids = append(cur.kids, x)
			cur = x
		case xml.EndElement:
			// Close up to the matching open element; a stray end tag is
			// dropped rather than unwinding the whole stack.
			name := xmlName(t.Name)
			for n := cur; n != doc; n = n.parent {
				if n.name == name {
					cur = n.parent
					break
				}
			}
		}
	}
}

// xmlName spells a raw (unresolved) name as written.
func xmlName(n xml.Name) string {
	if n.Space != "" {
		return n.Space + ":" + n.Local
	}
	return n.Local
}

func appendUnique(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}

// completeXMQPath answers the XPath argument under the cursor; ok is false
// when the cursor is not in a path position, and the caller falls back to
// the command list. Inside one it offers, by what precedes the partial:
//
//   - nothing yet (`select `): the root element as `/name`, and `//`;
//   - `/` or `//`: the child (descendant) element names at that path;
//   - `@`: the attributes of the path's elements (`/a/b/@`, `//b[@`);
//   - `[`: the child names of the step the predicate filters.
//
// Anything the walk cannot resolve (functions, axes, a closed predicate
// before the cursor) stays silent rather than guessing.
func completeXMQPath(r []rune, pos int, root *xmqNode) (items []Candidate, start int, ok bool) {
	cmd, argStart, path, inPath := xmqPathArg(r, pos)
	if !inPath || !xmqPathCommands[cmd] {
		return nil, 0, false
	}
	if root == nil {
		return nil, 0, true
	}
	if path == "" {
		for _, k := range root.kids {
			items = append(items, Candidate{Label: "/" + k.name, Insert: "/" + k.name, Detail: "root element"})
		}
		items = append(items, Candidate{Label: "//", Insert: "//", Detail: "any depth"})
		return capItems(items), argStart, true
	}
	pr := []rune(path)
	n := len(pr)
	for n > 0 && xmqNameRune(pr[n-1]) {
		n--
	}
	partial := string(pr[n:])
	start = pos - (len(pr) - n)
	head := string(pr[:n])
	attr := strings.HasSuffix(head, "@")
	if attr {
		head = strings.TrimSuffix(head, "@")
		partial = "@" + partial
		start--
	}
	var (
		set  []*xmqNode
		desc bool
		rok  bool
	)
	switch {
	case head == "":
		// A relative path: evaluated against the document node.
		set, rok = []*xmqNode{root}, true
	case strings.HasSuffix(head, "//"):
		set, rok = resolveXPath(root, strings.TrimSuffix(head, "//"))
		desc = true
	case strings.HasSuffix(head, "/"):
		// `/a/b/` offers b's children, `/a/b/@` b's own attributes.
		set, rok = resolveXPath(root, strings.TrimSuffix(head, "/"))
	case strings.HasSuffix(head, "["):
		set, rok = resolveXPath(root, strings.TrimSuffix(head, "["))
	}
	if !rok {
		return nil, 0, true
	}
	if desc {
		budget := completionNodeBudget
		set = descendantsOrSelf(set, &budget)
	}
	budget := completionNodeBudget
	if attr {
		items = attrCandidates(set, &budget)
	} else {
		items = childCandidates(set, &budget)
	}
	return filterPrefix(items, partial), start, true
}

// xmqPathArg scans the command line up to the cursor with ShellWords' rules
// and reports the word before the one under the cursor (the command whose
// argument it is), the raw rune index that argument starts at, and its
// unquoted text so far. inPath is false only when a backslash escape leaves
// the scan mid-rune — every other cursor position sits in some word.
func xmqPathArg(r []rune, pos int) (cmd string, argStart int, path string, inPath bool) {
	var (
		words   []string
		cur     strings.Builder
		started bool
		quote   rune
	)
	argStart = pos
	for i := 0; i < pos; i++ {
		c := r[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else if quote == '"' && c == '\\' && i+1 < pos {
				i++
				cur.WriteRune(r[i])
			} else {
				cur.WriteRune(c)
			}
		case c == ' ' || c == '\t':
			if started {
				words = append(words, cur.String())
				cur.Reset()
				started = false
			}
		case c == '\'' || c == '"':
			if !started {
				argStart = i
			}
			started, quote = true, c
		case c == '\\':
			if i+1 >= pos {
				return "", 0, "", false
			}
			if !started {
				argStart = i
			}
			started = true
			i++
			cur.WriteRune(r[i])
		default:
			if !started {
				argStart = i
			}
			started = true
			cur.WriteRune(c)
		}
	}
	if !started {
		argStart = pos
	}
	if len(words) == 0 {
		return "", 0, "", false
	}
	return words[len(words)-1], argStart, cur.String(), true
}

// xmqNameRune reports whether c can be part of an element or attribute name
// being typed. `.` is left out: it spells the self and parent steps.
func xmqNameRune(c rune) bool {
	return c == '_' || c == '-' || c == ':' || unicode.IsLetter(c) || unicode.IsDigit(c)
}

// resolveXPath walks a location path from the document node: absolute and
// relative paths both start there, as the CLI evaluates them. ok is false
// for a step the walk does not model. Every stage of the walk (a step, a
// `//` expansion) and the final listing visit at most completionNodeBudget
// nodes each: a stage that hits the cap hands on what it reached, so a huge
// document still answers, and the total stays bounded by the path's length.
func resolveXPath(root *xmqNode, expr string) ([]*xmqNode, bool) {
	set := []*xmqNode{root}
	segs, ok := splitXPath(expr)
	if !ok {
		return nil, false
	}
	desc := false
	for i, seg := range segs {
		if seg == "" {
			// The leading slash of an absolute path, or the middle of `//`.
			desc = i > 0
			continue
		}
		if desc {
			budget := completionNodeBudget
			set = descendantsOrSelf(set, &budget)
			desc = false
		}
		budget := completionNodeBudget
		if set, ok = applyXPathStep(set, seg, &budget); !ok {
			return nil, false
		}
	}
	return set, true
}

// splitXPath cuts expr at its top-level slashes — the ones outside a
// predicate or a quoted literal. An unbalanced bracket or quote is not a
// path the walk can resolve.
func splitXPath(expr string) ([]string, bool) {
	var (
		segs  []string
		depth int
		quote rune
		from  int
	)
	for i, c := range expr {
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '[':
			depth++
		case c == ']':
			depth--
			if depth < 0 {
				return nil, false
			}
		case c == '/' && depth == 0:
			segs = append(segs, expr[from:i])
			from = i + 1
		}
	}
	if depth != 0 || quote != 0 {
		return nil, false
	}
	return append(segs, expr[from:]), true
}

// applyXPathStep moves the set along one step: `name`, `*`, `.` or `..`,
// optionally followed by predicates. A positional predicate (`div[2]`)
// picks among each parent's matching children as XPath does; any other
// predicate is kept as a superset — completion only needs the names the
// step could reach. Axes and functions are not modeled.
func applyXPathStep(set []*xmqNode, seg string, budget *int) ([]*xmqNode, bool) {
	name, preds := seg, ""
	if i := strings.IndexByte(seg, '['); i >= 0 {
		name, preds = seg[:i], seg[i:]
	}
	if name == "" || strings.ContainsAny(name, "()@") || strings.Contains(name, "::") {
		return nil, false
	}
	pick := 0
	if strings.HasPrefix(preds, "[") && strings.HasSuffix(preds, "]") {
		if k, err := strconv.Atoi(preds[1 : len(preds)-1]); err == nil && k > 0 {
			pick = k
		}
	}
	var out []*xmqNode
	seen := map[*xmqNode]bool{}
	add := func(n *xmqNode) {
		if n != nil && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, n := range set {
		switch name {
		case ".":
			add(n)
			continue
		case "..":
			add(n.parent)
			continue
		}
		k := 0
		for _, c := range n.kids {
			if *budget <= 0 {
				return out, true
			}
			*budget--
			if name != "*" && c.name != name {
				continue
			}
			k++
			if pick == 0 || k == pick {
				add(c)
			}
		}
	}
	return out, true
}

// descendantsOrSelf expands the set to every element below it (the `//`
// step), within the budget.
func descendantsOrSelf(set []*xmqNode, budget *int) []*xmqNode {
	var out []*xmqNode
	seen := map[*xmqNode]bool{}
	stack := append([]*xmqNode(nil), set...)
	for len(stack) > 0 && *budget > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[n] {
			continue
		}
		seen[n] = true
		*budget--
		out = append(out, n)
		for i := len(n.kids) - 1; i >= 0; i-- {
			stack = append(stack, n.kids[i])
		}
	}
	return out
}

// childCandidates lists the distinct child element names of the set, in
// document order of first appearance.
func childCandidates(set []*xmqNode, budget *int) []Candidate {
	var items []Candidate
	seen := map[string]bool{}
	for _, n := range set {
		for _, c := range n.kids {
			if *budget <= 0 {
				return items
			}
			*budget--
			if seen[c.name] {
				continue
			}
			seen[c.name] = true
			items = append(items, Candidate{Label: c.name, Insert: c.name, Detail: "element"})
		}
	}
	return items
}

// attrCandidates lists the distinct attribute names on the set's elements.
func attrCandidates(set []*xmqNode, budget *int) []Candidate {
	var items []Candidate
	seen := map[string]bool{}
	for _, n := range set {
		if *budget <= 0 {
			return items
		}
		*budget--
		for _, a := range n.attrs {
			if seen[a] {
				continue
			}
			seen[a] = true
			items = append(items, Candidate{Label: "@" + a, Insert: "@" + a, Detail: "attribute"})
		}
	}
	return items
}

// capItems applies MaxCompletionItems to an unfiltered list.
func capItems(items []Candidate) []Candidate {
	if len(items) > MaxCompletionItems {
		return items[:MaxCompletionItems]
	}
	return items
}
