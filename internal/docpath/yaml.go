package docpath

import "strings"

// yaml.go is the indentation-driven scanner. YAML's block structure is written
// in the columns, so the path needs no parse tree: every line contributes its
// `- ` dashes and its `key:` at the columns they sit at, and each column pops
// the frames it is no longer nested inside. A flow value hands off to the JSON
// scanner (json.go) — inside `{ }` / `[ ]` YAML *is* that grammar.

// yframe is one enclosing block node: a mapping key, or a sequence (Seq) whose
// index counts the `-` items seen at that column. indent is the column the
// node starts at, which is what later lines compare against.
type yframe struct {
	indent int
	seq    bool
	index  int
	key    string
}

// scanYAML returns the path to the caret at (line, col) in a YAML buffer.
func scanYAML(src Source, line, col int) []Step {
	n := src.LineCount()
	if n == 0 {
		return nil
	}
	if line >= n {
		line, col = n-1, len([]rune(src.Line(n-1)))
	}
	var stack []yframe
	var flow *jscan
	inScalar, scalarIndent := false, 0

	for ln := 0; ln <= line; ln++ {
		raw := src.Line(ln)
		last := ln == line
		stop := -1
		if last {
			stop = col
		}

		// An open flow collection owns every line until it closes; the JSON
		// scanner carries its own state across them.
		if flow != nil {
			flow.feed([]rune(raw), 0, stop)
			if last {
				return append(ysteps(stack), flow.steps()...)
			}
			if len(flow.stack) == 0 {
				flow = nil
			}
			continue
		}
		// A block scalar's body is text, not structure: `a: |` followed by
		// indented lines keeps the path of `a` for all of them.
		if inScalar {
			if strings.TrimSpace(raw) == "" || indentOf(raw) > scalarIndent {
				continue
			}
			inScalar = false
		}
		if strings.TrimSpace(raw) == "" {
			// A blank line carries no structure — not even where its caret
			// sits, since an empty line has no columns to indent. It keeps the
			// enclosing node, which is the answer that stays stable while a
			// block is being written.
			continue
		}
		if docMarker(raw) {
			stack = stack[:0]
			continue
		}
		r := []rune(stripComment(raw))
		at := indentOf(string(r))
		if at >= len(r) {
			continue // comment-only line
		}

		// Sequence dashes, outermost first: `- - a: 1` opens two levels.
		for at < len(r) && r[at] == '-' && (at+1 == len(r) || r[at+1] == ' ' || r[at+1] == '\t') {
			if stop >= 0 && at > stop {
				return ysteps(stack) // the caret sits left of this dash
			}
			// Pop only what is strictly deeper: a sequence may share its
			// parent key's column (`a:` then `- x` at column 0), so an equal
			// column must not close the key that owns it.
			for len(stack) > 0 && stack[len(stack)-1].indent > at {
				stack = stack[:len(stack)-1]
			}
			if top := len(stack) - 1; top >= 0 && stack[top].seq && stack[top].indent == at {
				stack[top].index++ // the next item of the same sequence
			} else {
				stack = append(stack, yframe{indent: at, seq: true})
			}
			at++
			for at < len(r) && (r[at] == ' ' || r[at] == '\t') {
				at++
			}
		}

		// A mapping key at this column closes every node at or deeper than it.
		if key, val, ok := parseKey(r, at); ok {
			if stop >= 0 && at > stop {
				return ysteps(stack)
			}
			for len(stack) > 0 && stack[len(stack)-1].indent >= at {
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, yframe{indent: at, key: key})
			at = val
		}

		for at < len(r) && (r[at] == ' ' || r[at] == '\t') {
			at++
		}
		if at >= len(r) {
			continue
		}
		switch {
		case r[at] == '{' || r[at] == '[':
			flow = &jscan{}
			flow.feed(r, at, stop)
			if last && stop >= at {
				return append(ysteps(stack), flow.steps()...)
			}
			if len(flow.stack) == 0 {
				flow = nil
			}
		case blockScalar(r, at):
			inScalar, scalarIndent = true, indentOf(string(r))
		}
	}
	return ysteps(stack)
}

// ysteps renders the enclosing block nodes as the path.
func ysteps(stack []yframe) []Step {
	out := make([]Step, 0, len(stack))
	for _, f := range stack {
		if f.seq {
			out = append(out, Step{Seq: true, Index: f.index})
			continue
		}
		out = append(out, Step{Key: f.key})
	}
	return out
}

// parseKey reads the mapping key starting at column i, returning it and the
// column its value starts at. A key ends at a `:` that is followed by a space
// or the line end — YAML's own rule, which keeps `url: http://x` and
// `time: 12:30` one key each. A leading anchor or tag (`&a`, `!!str`) is
// skipped: it decorates the node, it is not part of its name.
func parseKey(r []rune, i int) (string, int, bool) {
	for i < len(r) && (r[i] == '&' || r[i] == '!') {
		for i < len(r) && r[i] != ' ' && r[i] != '\t' {
			i++
		}
		for i < len(r) && (r[i] == ' ' || r[i] == '\t') {
			i++
		}
	}
	if i >= len(r) {
		return "", 0, false
	}
	if r[i] == '"' || r[i] == '\'' {
		text, next := readQuoted(r, i)
		for next < len(r) && (r[next] == ' ' || r[next] == '\t') {
			next++
		}
		if next < len(r) && r[next] == ':' {
			return text, next + 1, true
		}
		return "", 0, false
	}
	for j := i; j < len(r); j++ {
		if r[j] == '{' || r[j] == '[' {
			return "", 0, false // a flow value, not a key
		}
		if r[j] != ':' || !(j+1 == len(r) || r[j+1] == ' ' || r[j+1] == '\t') {
			continue
		}
		key := strings.TrimSpace(string(r[i:j]))
		if key == "" {
			return "", 0, false
		}
		return key, j + 1, true
	}
	return "", 0, false
}

// blockScalar reports whether the value at column i introduces a block scalar
// (`|`, `>` with the optional chomping and indentation indicators).
func blockScalar(r []rune, i int) bool {
	if r[i] != '|' && r[i] != '>' {
		return false
	}
	for j := i + 1; j < len(r); j++ {
		if r[j] == '-' || r[j] == '+' || (r[j] >= '0' && r[j] <= '9') {
			continue
		}
		return r[j] == ' ' || r[j] == '\t'
	}
	return true
}

// stripComment cuts a `#` comment off a line. A `#` counts only at the line
// start or after whitespace and outside quotes, which is YAML's rule — a `#`
// inside `pass#word` is data.
func stripComment(line string) string {
	r := []rune(line)
	quote := rune(0)
	for i, c := range r {
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#' && (i == 0 || r[i-1] == ' ' || r[i-1] == '\t'):
			return string(r[:i])
		}
	}
	return line
}

// docMarker reports whether a line separates or ends a YAML document; the path
// never crosses one.
func docMarker(line string) bool {
	t := strings.TrimRight(line, " \t")
	return t == "---" || t == "..." || strings.HasPrefix(t, "--- ")
}

// indentOf returns the column of a line's first non-blank rune (its length
// when the line is blank).
func indentOf(line string) int {
	for i, c := range []rune(line) {
		if c != ' ' && c != '\t' {
			return i
		}
	}
	return len([]rune(line))
}
