package langxml

import "strings"

// formatrange.go: reformat-selection for the built-in XML formatter (#1404)
// formats the element subtrees overlapping the selected lines and leaves the
// rest of the document byte-identical.

// formatRangeXML formats the shallowest subtrees overlapping [startLine,
// endLine] (0-based inclusive) and splices them back.
func formatRangeXML(text string, startLine, endLine int, opts xmlOptions) (string, error) {
	roots, err := parseXML(text)
	if err != nil {
		return "", err
	}
	if bad, checked := xmlParseHasErrors(text); checked && bad {
		return "", errXMLMalformed
	}
	group, depth := rangeTargets(roots, 0, startLine, endLine)
	if len(group) == 0 {
		return text, nil
	}
	// merge with siblings sharing boundary lines — two nodes on one line
	// cannot be spliced independently
	first, last := group[0].lineStart, group[len(group)-1].lineEnd
	lines := strings.Split(text, "\n")
	if last >= len(lines) {
		last = len(lines) - 1
	}
	p := &xmlPrinter{opts: opts, src: text}
	for _, n := range group {
		p.node(n, depth)
	}
	out := append(lines[:first:first], append(p.lines, lines[last+1:]...)...)
	return strings.Join(out, "\n"), nil
}

// rangeTargets picks the nodes to reformat: descend while a single element
// fully contains the selection and no sibling shares its boundary lines,
// then take the contiguous run of children overlapping the selection.
func rangeTargets(siblings []*xmlNode, depth, s, e int) ([]*xmlNode, int) {
	var hit []*xmlNode
	for _, n := range siblings {
		if n.lineEnd >= s && n.lineStart <= e {
			hit = append(hit, n)
		}
	}
	if len(hit) == 0 {
		return nil, depth
	}
	if len(hit) == 1 && hit[0].kind == xElem && !hit[0].mixed && !hit[0].preserve &&
		(hit[0].lineStart < s || hit[0].lineEnd > e) && len(hit[0].children) > 0 &&
		exclusiveLines(siblings, hit[0]) {
		// the selection sits inside this element: try to narrow to its kids
		inner, d := rangeTargets(hit[0].children, depth+1, s, e)
		if len(inner) > 0 && exclusiveGroup(hit[0], inner) {
			return inner, d
		}
	}
	// widen to every sibling sharing a boundary line with the hit run
	for changed := true; changed; {
		changed = false
		for _, n := range siblings {
			if containsNode(hit, n) {
				continue
			}
			if n.lineEnd >= hit[0].lineStart && n.lineStart <= hit[len(hit)-1].lineEnd {
				hit = insertNode(hit, n)
				changed = true
			}
		}
	}
	return hit, depth
}

// exclusiveLines reports that no sibling shares n's first or last line.
func exclusiveLines(siblings []*xmlNode, n *xmlNode) bool {
	for _, s := range siblings {
		if s == n {
			continue
		}
		if s.lineEnd == n.lineStart || s.lineStart == n.lineEnd {
			return false
		}
	}
	return true
}

// exclusiveGroup reports that the inner group's line span does not touch the
// container's own tag lines (open/close), so splicing keeps the tags intact.
func exclusiveGroup(container *xmlNode, group []*xmlNode) bool {
	first, last := group[0].lineStart, group[len(group)-1].lineEnd
	return first > container.lineStart && last < container.lineEnd
}

func containsNode(list []*xmlNode, n *xmlNode) bool {
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}

// insertNode keeps the list ordered by lineStart.
func insertNode(list []*xmlNode, n *xmlNode) []*xmlNode {
	out := make([]*xmlNode, 0, len(list)+1)
	added := false
	for _, x := range list {
		if !added && n.lineStart < x.lineStart {
			out = append(out, n)
			added = true
		}
		out = append(out, x)
	}
	if !added {
		out = append(out, n)
	}
	return out
}
