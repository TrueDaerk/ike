package jqplay

// outline.go lists the depth-1 nodes of a result (#2793): the top-level keys
// of an object, the indices of an array, the values of a multi-output
// stream. The playground's structure strip shows them as jump targets beside
// the result, so a 10,000-line object is one click from any of its members.
//
// Like the fold scans it reads the text its own encoder wrote — JSON with a
// two-space indent (encodeJSON), YAML in block style (encodeYAML) — so a line
// scan is enough: a JSON string never holds a raw newline, and every direct
// member of the root starts on a line at the root's child indent.

import (
	"strconv"
	"strings"
)

// OutlineItem is one depth-1 node: the label the strip shows and the result
// line the node starts on (0-based, in Text()).
type OutlineItem struct {
	Label string
	Line  int
}

// Outline returns the depth-1 nodes of the result, in document order, or nil
// when the result has no structure to list: a scalar, a raw or compact
// result (one row per value — the rows are the overview), an xmq result in a
// language the scans do not read, an empty object or array.
//
// A single value lists its own members. A stream of several values lists the
// values themselves (`#1`, `#2`, …): the members of one value would name
// only part of what the buffer holds.
func (r Result) Outline() []OutlineItem {
	if len(r.Outputs) == 0 || r.opts.Raw || r.opts.Compact {
		return nil
	}
	if r.dialect == DialectXMQ && r.ext != "json" {
		return nil
	}
	if len(r.Outputs) > 1 {
		starts := r.ValueStarts()
		out := make([]OutlineItem, len(starts))
		for i, line := range starts {
			out[i] = OutlineItem{Label: "#" + strconv.Itoa(i+1), Line: line}
		}
		return out
	}
	if r.dialect == DialectYQ {
		return yamlOutline(r.Outputs[0])
	}
	return jsonOutline(r.Outputs[0])
}

// jsonOutline lists the members of a pretty-printed JSON object or array: the
// lines between the opener and the closer at the first member's indent that
// do not close a nested node.
func jsonOutline(text string) []OutlineItem {
	lines := strings.Split(text, "\n")
	if len(lines) < 3 {
		return nil // a scalar, or `{}` / `[]` — nothing to jump to
	}
	head := strings.TrimSpace(lines[0])
	if head != "{" && head != "[" {
		return nil
	}
	object := head == "{"
	child := yamlIndentOf(lines[1])
	var out []OutlineItem
	for i := 1; i < len(lines)-1; i++ {
		l := lines[i]
		if yamlIndentOf(l) != child {
			continue
		}
		t := l[child:]
		if t == "" || t[0] == '}' || t[0] == ']' {
			continue
		}
		label := "[" + strconv.Itoa(len(out)) + "]"
		if object {
			label = jsonKeyOf(t)
		}
		out = append(out, OutlineItem{Label: label, Line: i})
	}
	return out
}

// jsonKeyOf is the decoded key of a `"key": value` member row, the raw quoted
// token when it does not decode.
func jsonKeyOf(row string) string {
	if row == "" || row[0] != '"' {
		return row
	}
	escaped := false
	for i := 1; i < len(row); i++ {
		switch {
		case escaped:
			escaped = false
		case row[i] == '\\':
			escaped = true
		case row[i] == '"':
			if k, err := strconv.Unquote(row[:i+1]); err == nil {
				return k
			}
			return row[1:i]
		}
	}
	return row
}

// yamlOutline lists the root members of a block-style YAML document: every
// unindented `key:` row of a mapping, every unindented `- ` entry of a
// sequence. A scalar document yields nothing.
func yamlOutline(text string) []OutlineItem {
	lines := strings.Split(text, "\n")
	var out []OutlineItem
	seq := 0
	for i, l := range lines {
		if l == "" || l[0] == ' ' || l[0] == '#' {
			continue
		}
		if yamlDash(l) {
			out = append(out, OutlineItem{Label: "[" + strconv.Itoa(seq) + "]", Line: i})
			seq++
			continue
		}
		if k, ok := yamlRowKey(l); ok && yamlKeyRow(l, k) {
			out = append(out, OutlineItem{Label: yamlUnquoteKey(k), Line: i})
		}
	}
	return out
}

// yamlKeyRow reports whether k, the key yamlRowKey cut off row l, really is
// one: a quoted scalar document (`"a: b"`) contains `: ` too, but there the
// colon sits inside the quotes rather than right after them.
func yamlKeyRow(l, k string) bool {
	if l[0] != '"' && l[0] != '\'' {
		return true
	}
	return len(k) >= 2 && k[len(k)-1] == l[0]
}

// yamlUnquoteKey strips the quotes the encoder puts around a key that would
// not read as a plain scalar.
func yamlUnquoteKey(k string) string {
	if len(k) >= 2 && (k[0] == '"' && k[len(k)-1] == '"') {
		if u, err := strconv.Unquote(k); err == nil {
			return u
		}
	}
	if len(k) >= 2 && k[0] == '\'' && k[len(k)-1] == '\'' {
		return strings.ReplaceAll(k[1:len(k)-1], "''", "'")
	}
	return k
}
