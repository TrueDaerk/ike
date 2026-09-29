package jqplay

import "strings"

// values.go maps a result's output values onto the lines of its joined text
// (#2789), so the playground can mark where each value starts in a
// multi-output stream and say which value the result cursor is in.

// ValueStarts returns the 0-based line in Text() at which each output value
// starts, in output order. An xmq result has value boundaries only when its
// command wrote JSON (`to-json`); for other output languages it returns nil.
func (r Result) ValueStarts() []int {
	if len(r.Outputs) == 0 || (r.dialect == DialectXMQ && r.ext != "json") {
		return nil
	}
	sepLines := strings.Count(r.separator(), "\n")
	starts := make([]int, len(r.Outputs))
	line := 0
	for i, o := range r.Outputs {
		starts[i] = line
		line += strings.Count(o, "\n") + sepLines
	}
	return starts
}

// ValueIndex returns the 0-based index of the value whose lines hold line —
// a yq `---` separator belongs to the value above it — or -1 without values.
func ValueIndex(starts []int, line int) int {
	if len(starts) == 0 {
		return -1
	}
	idx := 0
	for i, s := range starts {
		if s > line {
			break
		}
		idx = i
	}
	return idx
}

// ValueGlyph is the gutter glyph naming one output value's type: `{` object,
// `[` array, `"` string, `#` number, `∅` null, `⊤`/`⊥` true/false. It reads
// the rendered text, so it serves JSON and YAML alike: a YAML block mapping
// or sequence counts as an object or array, any other plain scalar that is
// not a number, null or boolean as a string.
func ValueGlyph(out string) string {
	t := strings.TrimSpace(out)
	if t == "" {
		return `"`
	}
	switch t[0] {
	case '{':
		return "{"
	case '[':
		return "["
	case '"', '\'', '|', '>':
		return `"`
	}
	first, _, _ := strings.Cut(t, "\n")
	if first == "-" || strings.HasPrefix(first, "- ") {
		return "["
	}
	if strings.Contains(first, ": ") || strings.HasSuffix(first, ":") {
		return "{"
	}
	switch t {
	case "null", "~":
		return "∅"
	case "true":
		return "⊤"
	case "false":
		return "⊥"
	}
	if isNumber(t) {
		return "#"
	}
	return `"`
}

// isNumber reports whether s reads as a JSON/YAML number.
func isNumber(s string) bool {
	s = strings.TrimLeft(s, "+-")
	digits := false
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			digits = true
		case c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-':
		default:
			return false
		}
	}
	return digits
}
