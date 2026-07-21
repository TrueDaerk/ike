// Package snippet expands LSP snippet syntax (#846): tabstops ($1, ${2},
// ${3:default}, choices ${4|a,b|}, the final $0), variables ($NAME,
// ${NAME:default}) and escapes (\$, \\, \}). Expand returns the plain text to
// insert plus the tabstop rune offsets in visit order, so the editor can run a
// tab/shift-tab placeholder session. Variables resolve to their default (or
// empty) — there is no editor context here. A malformed source returns an
// error; callers fall back to inserting the raw text.
package snippet

import (
	"errors"
	"sort"
	"unicode"
)

// Expand parses src and returns the expanded plain text and the tabstop rune
// offsets into that text, in visit order: ascending tabstop number with $0
// last; a duplicate (mirrored) number keeps its first occurrence. A placeholder
// stop sits at the end of its default text, so tabbing on accepts the default
// and typing appends to it. When src contains tabstops but no $0, an implicit
// final stop at the end of the text is appended. No tabstops yields nil stops.
func Expand(src string) (string, []int, error) {
	p := &parser{src: []rune(src)}
	if err := p.parse(false); err != nil {
		return "", nil, err
	}
	text := string(p.out)
	if len(p.stops) == 0 {
		return text, nil, nil
	}
	// Visit order: ascending number, $0 last, source order within a number.
	sort.SliceStable(p.stops, func(i, j int) bool {
		return visitKey(p.stops[i].n) < visitKey(p.stops[j].n)
	})
	seen := map[int]bool{}
	var offs []int
	final := false
	for _, s := range p.stops {
		if seen[s.n] {
			continue
		}
		seen[s.n] = true
		offs = append(offs, s.off)
		if s.n == 0 {
			final = true
		}
	}
	if !final {
		offs = append(offs, len(p.out))
	}
	return text, offs, nil
}

// visitKey orders tabstop numbers: $0 is the exit point and visits last.
func visitKey(n int) int {
	if n == 0 {
		return int(^uint(0) >> 1)
	}
	return n
}

type stop struct{ n, off int }

type parser struct {
	src   []rune
	i     int
	out   []rune
	stops []stop
}

// parse consumes src into out. Inside a placeholder default it returns at the
// closing '}' without consuming it; at top level a stray '}' is literal text.
func (p *parser) parse(inPlaceholder bool) error {
	for p.i < len(p.src) {
		r := p.src[p.i]
		switch {
		case r == '\\' && p.i+1 < len(p.src):
			p.out = append(p.out, p.src[p.i+1])
			p.i += 2
		case r == '}' && inPlaceholder:
			return nil
		case r == '$':
			if err := p.dollar(); err != nil {
				return err
			}
		default:
			p.out = append(p.out, r)
			p.i++
		}
	}
	if inPlaceholder {
		return errors.New("unterminated placeholder")
	}
	return nil
}

// dollar handles a '$' at p.i: $N, ${...}, $NAME, or a literal dollar.
func (p *parser) dollar() error {
	p.i++
	if p.i >= len(p.src) {
		p.out = append(p.out, '$')
		return nil
	}
	switch r := p.src[p.i]; {
	case unicode.IsDigit(r):
		p.stops = append(p.stops, stop{n: p.number(), off: len(p.out)})
	case r == '{':
		p.i++
		return p.braced()
	case isVarRune(r):
		p.varName() // bare variable: no context to resolve, expands empty
	default:
		p.out = append(p.out, '$')
	}
	return nil
}

// braced handles the content after "${": a numbered tabstop (bare, with a
// ":default", or with "|choices|") or a variable (bare or with ":default").
func (p *parser) braced() error {
	if p.i >= len(p.src) {
		return errors.New("unterminated ${")
	}
	if unicode.IsDigit(p.src[p.i]) {
		n := p.number()
		switch {
		case p.at('}'):
			p.stops = append(p.stops, stop{n: n, off: len(p.out)})
			p.i++
			return nil
		case p.at(':'):
			p.i++
			if err := p.parse(true); err != nil {
				return err
			}
			p.stops = append(p.stops, stop{n: n, off: len(p.out)})
			return p.expect('}')
		case p.at('|'):
			p.i++
			if err := p.firstChoice(); err != nil {
				return err
			}
			p.stops = append(p.stops, stop{n: n, off: len(p.out)})
			return p.expect('}')
		}
		return errors.New("malformed tabstop")
	}
	if isVarRune(p.src[p.i]) {
		p.varName()
		switch {
		case p.at('}'):
			p.i++
			return nil
		case p.at(':'):
			p.i++
			if err := p.parse(true); err != nil {
				return err
			}
			return p.expect('}')
		}
		return errors.New("malformed variable")
	}
	return errors.New("malformed placeholder")
}

// firstChoice emits the first option of a "|a,b,c|" choice list and consumes
// through the closing '|'.
func (p *parser) firstChoice() error {
	first := true
	for p.i < len(p.src) {
		r := p.src[p.i]
		switch {
		case r == '\\' && p.i+1 < len(p.src):
			if first {
				p.out = append(p.out, p.src[p.i+1])
			}
			p.i += 2
		case r == ',':
			first = false
			p.i++
		case r == '|':
			p.i++
			return nil
		default:
			if first {
				p.out = append(p.out, r)
			}
			p.i++
		}
	}
	return errors.New("unterminated choice")
}

// number consumes a digit run.
func (p *parser) number() int {
	n := 0
	for p.i < len(p.src) && unicode.IsDigit(p.src[p.i]) {
		n = n*10 + int(p.src[p.i]-'0')
		p.i++
	}
	return n
}

// varName consumes a variable-name run.
func (p *parser) varName() {
	for p.i < len(p.src) && isVarRune(p.src[p.i]) {
		p.i++
	}
}

func (p *parser) at(r rune) bool { return p.i < len(p.src) && p.src[p.i] == r }

func (p *parser) expect(r rune) error {
	if !p.at(r) {
		return errors.New("expected '" + string(r) + "'")
	}
	p.i++
	return nil
}

func isVarRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
