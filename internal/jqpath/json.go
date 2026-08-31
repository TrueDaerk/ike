package jqpath

// json.go is the positioned JSON parser: encoding/json can decode the values
// but throws the offsets away, and the offsets are the point here. The parser
// is strict JSON over a stream of top-level values (an ordinary document, or
// one value per line for `.jsonl`), matching what the jq playground accepts.
// Values decode to the playground's shapes: json.Number for numbers, so a
// 64-bit id survives, and map[string]any / []any for the containers.

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// jparse scans text byte-wise while tracking the 0-based line and rune column
// of the read head, which is all a Span needs.
type jparse struct {
	text string
	i    int // byte index
	line int
	col  int // rune column
}

// parseJSONNodes parses a stream of top-level JSON values.
func parseJSONNodes(text string) ([]*node, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("input is empty")
	}
	p := &jparse{text: text}
	var docs []*node
	for {
		p.skipWS()
		if p.i >= len(p.text) {
			break
		}
		n, err := p.value()
		if err != nil {
			return nil, err
		}
		docs = append(docs, n)
	}
	return docs, nil
}

// next consumes one rune, keeping line and column current.
func (p *jparse) next() rune {
	r, size := utf8.DecodeRuneInString(p.text[p.i:])
	p.i += size
	if r == '\n' {
		p.line++
		p.col = 0
	} else {
		p.col++
	}
	return r
}

// peek returns the byte at the read head, or 0 at the end of input.
func (p *jparse) peek() byte {
	if p.i >= len(p.text) {
		return 0
	}
	return p.text[p.i]
}

func (p *jparse) skipWS() {
	for p.i < len(p.text) {
		switch p.text[p.i] {
		case ' ', '\t', '\r', '\n':
			p.next()
		default:
			return
		}
	}
}

func (p *jparse) errf(format string, args ...any) error {
	return fmt.Errorf("input is not valid JSON: %s (line %d)", fmt.Sprintf(format, args...), p.line+1)
}

// value parses one JSON value into a positioned node.
func (p *jparse) value() (*node, error) {
	p.skipWS()
	line, col := p.line, p.col
	var n *node
	var err error
	switch c := p.peek(); {
	case c == '{':
		n, err = p.object()
	case c == '[':
		n, err = p.array()
	case c == '"':
		var s string
		s, err = p.string()
		n = &node{val: s}
	case c == 't' || c == 'f' || c == 'n':
		n, err = p.literal()
	case c == '-' || c >= '0' && c <= '9':
		n, err = p.number()
	case c == 0:
		return nil, p.errf("unexpected end of input")
	default:
		return nil, p.errf("unexpected %q", string(rune(c)))
	}
	if err != nil {
		return nil, err
	}
	n.span = Span{Line: line, Start: col, End: p.col}
	if p.line != line {
		n.span.End = -1 // multi-line value: highlight through its first line
	}
	return n, nil
}

func (p *jparse) object() (*node, error) {
	p.next() // '{'
	n := &node{obj: map[string]*node{}}
	val := map[string]any{}
	p.skipWS()
	if p.peek() == '}' {
		p.next()
		n.val = val
		return n, nil
	}
	for {
		p.skipWS()
		if p.peek() != '"' {
			return nil, p.errf("expected object key")
		}
		key, err := p.string()
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.peek() != ':' {
			return nil, p.errf("expected ':' after object key")
		}
		p.next()
		child, err := p.value()
		if err != nil {
			return nil, err
		}
		n.obj[key] = child // duplicate keys: last wins, like encoding/json
		val[key] = child.val
		p.skipWS()
		switch p.peek() {
		case ',':
			p.next()
		case '}':
			p.next()
			n.val = val
			return n, nil
		default:
			return nil, p.errf("expected ',' or '}' in object")
		}
	}
}

func (p *jparse) array() (*node, error) {
	p.next() // '['
	n := &node{}
	val := []any{}
	p.skipWS()
	if p.peek() == ']' {
		p.next()
		n.val = val
		return n, nil
	}
	for {
		child, err := p.value()
		if err != nil {
			return nil, err
		}
		n.arr = append(n.arr, child)
		val = append(val, child.val)
		p.skipWS()
		switch p.peek() {
		case ',':
			p.next()
		case ']':
			p.next()
			n.val = val
			return n, nil
		default:
			return nil, p.errf("expected ',' or ']' in array")
		}
	}
}

// string consumes a quoted string token and decodes its escapes through
// encoding/json, so `\uXXXX` pairs and every escape read exactly as JSON does.
func (p *jparse) string() (string, error) {
	start := p.i
	p.next() // '"'
	for {
		if p.i >= len(p.text) {
			return "", p.errf("unterminated string")
		}
		r := p.next()
		if r == '\\' {
			if p.i >= len(p.text) {
				return "", p.errf("unterminated string")
			}
			p.next()
			continue
		}
		if r == '"' {
			break
		}
		if r == '\n' {
			return "", p.errf("newline in string")
		}
	}
	var s string
	if err := json.Unmarshal([]byte(p.text[start:p.i]), &s); err != nil {
		return "", p.errf("invalid string")
	}
	return s, nil
}

// number consumes a number token, validated through encoding/json and kept as
// json.Number.
func (p *jparse) number() (*node, error) {
	start := p.i
	for p.i < len(p.text) {
		c := p.peek()
		if c >= '0' && c <= '9' || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' {
			p.next()
			continue
		}
		break
	}
	tok := p.text[start:p.i]
	var num json.Number
	if err := json.Unmarshal([]byte(tok), &num); err != nil {
		return nil, p.errf("invalid number %q", tok)
	}
	return &node{val: num}, nil
}

func (p *jparse) literal() (*node, error) {
	rest := p.text[p.i:]
	for lit, v := range map[string]any{"true": true, "false": false, "null": nil} {
		if strings.HasPrefix(rest, lit) {
			for range lit {
				p.next()
			}
			return &node{val: v}, nil
		}
	}
	return nil, p.errf("unexpected literal")
}
