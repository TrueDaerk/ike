// Package consthint extends the number conceals into source code (#1701): a
// constant assignment recognised in a Python, Go or PHP buffer gets the same
// field-context reading a config value gets (#1627, #1685) — the constant's
// name carries the unit (`MAX_BYTES`, `TIMEOUT_MS`, `CACHE_TTL`), including
// the user's editor.number_hint_units mapping — and a pure literal-arithmetic
// right-hand side is evaluated first, so `10 * 1024 * 1024` conceals as
// `10 MiB` and `60 * 60 * 24` as `86_400`.
//
// The evaluator in this file is deliberately narrow: number literals and the
// side-effect-free integer operators only (`+ - * / % << >> & | ^`, parens,
// unary sign). Anything else — an identifier, a call, a float, a string —
// fails the parse and leaves the source untouched, which is the safety rule
// the issue demands. Semantics the three languages disagree on are refused
// rather than guessed: division must be exact (Go truncates where Python
// divides into a float), modulo and the bitwise family require non-negative
// operands (truncated vs floored remainders), and a shift count is capped.
// Operator precedence is per-flavor: Go binds `<<`/`&` at the multiplicative
// level where Python and PHP use the C ladder.
//
// The spans it produces reuse the numhint/epochtime captures, so they ride the
// same conceal channels: gated by the same editor.*_hints toggles and revealed
// under and next to the caret like every other value conceal (#1594, #1686).
// It is a leaf package — pure Go over internal/lang's span type plus the
// numhint formatting and the epochtime decoders — so the language plugins can
// call it without a dependency cycle. Which line *is* a constant assignment is
// the per-language half and lives in spans.go.
package consthint

import "math/big"

// Flavor selects the source language whose literal syntax and operator
// precedence the evaluator applies. The differences are small but real: a
// leading-zero octal (`0755`) is a literal in Go and PHP and a syntax error in
// Python 3, and Go binds `<<`, `>>` and `&` at the multiplicative level where
// Python and PHP use the C precedence ladder — `1<<4 + 1` is 17 in Go and 32
// in Python.
type Flavor int

const (
	FlavorPython Flavor = iota
	FlavorGo
	FlavorPHP
)

// Result is a successful evaluation. Single marks a right-hand side that is
// one bare literal (no operators, no parens); Decimal marks that literal as
// written in plain decimal digits — the two flags decide whether the producer
// renders a replacement (a computed expression) or an appended reading (a
// prefixed literal, whose decimal value is the one thing worth showing).
type Result struct {
	Value   uint64
	Single  bool
	Decimal bool
}

// maxBits bounds intermediate magnitudes. Anything past an uint64 cannot
// render anyway; the headroom above 64 lets an expression overshoot and come
// back (`(1<<70)/(1<<10)`) without letting a shift chain allocate real memory.
const maxBits = 128

// maxShift bounds a shift count: a larger count is a typo or an id, and the
// result could not fit maxBits regardless.
const maxShift = 256

// Eval evaluates a pure literal-arithmetic expression. It reports false for
// anything that is not one — identifiers, calls, floats, strings — and for
// every arithmetic outcome the three languages do not agree on or that cannot
// render: inexact or zero division, negative modulo/bitwise operands,
// oversized shifts, intermediates past maxBits, and results that are negative
// or beyond an uint64.
func Eval(expr string, f Flavor) (Result, bool) {
	toks, ok := lex(expr, f)
	if !ok {
		return Result{}, false
	}
	p := parser{toks: toks, f: f}
	v, ok := p.expr(1)
	if !ok || p.i != len(p.toks) {
		return Result{}, false
	}
	if v.Sign() < 0 || !v.IsUint64() {
		return Result{}, false
	}
	return Result{
		Value:   v.Uint64(),
		Single:  len(toks) == 1,
		Decimal: len(toks) == 1 && toks[0].dec,
	}, true
}

// --- lexer -------------------------------------------------------------------

type tokKind int

const (
	tokNum tokKind = iota
	tokOp
	tokLParen
	tokRParen
)

type token struct {
	kind tokKind
	op   string
	val  *big.Int
	dec  bool // literal written in plain decimal digits
}

// lex splits expr into tokens, reporting false on the first rune that cannot
// be part of a literal-arithmetic expression.
func lex(expr string, f Flavor) ([]token, bool) {
	runes := []rune(expr)
	var out []token
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case r == ' ' || r == '\t':
			i++
		case r >= '0' && r <= '9':
			v, dec, n, ok := lexNumber(runes[i:], f)
			if !ok {
				return nil, false
			}
			out = append(out, token{kind: tokNum, val: v, dec: dec})
			i += n
		case r == '(':
			out = append(out, token{kind: tokLParen})
			i++
		case r == ')':
			out = append(out, token{kind: tokRParen})
			i++
		default:
			op, n := lexOp(runes[i:], f)
			if n == 0 {
				return nil, false
			}
			out = append(out, token{kind: tokOp, op: op})
			i += n
		}
	}
	return out, len(out) > 0
}

// lexOp reads one operator. The two-rune forms come first so `<<` never lexes
// as two broken `<`s; `//` is Python's floor division (in Go and PHP a `//`
// opens the comment the producers already cut off), and `&^` is Go's and-not.
func lexOp(runes []rune, f Flavor) (string, int) {
	if len(runes) >= 2 {
		switch two := string(runes[:2]); two {
		case "<<", ">>":
			return two, 2
		case "//":
			if f == FlavorPython {
				return two, 2
			}
			return "", 0
		case "&^":
			if f == FlavorGo {
				return two, 2
			}
			return "", 0
		}
	}
	switch runes[0] {
	case '+', '-', '*', '/', '%', '&', '|', '^':
		return string(runes[0]), 1
	}
	return "", 0
}

// lexNumber reads one integer literal: decimal, `0x` hex, `0o` octal, `0b`
// binary — with digit-separating underscores — plus the leading-zero octal of
// Go and PHP, which Python 3 rejects. It reports false when the literal runs
// into something that makes it not an integer (a `.`, a letter): the whole
// expression is then not literal arithmetic.
func lexNumber(runes []rune, f Flavor) (v *big.Int, dec bool, n int, ok bool) {
	base, digits, i := 10, "", 0
	dec = true
	if runes[0] == '0' && len(runes) > 1 {
		switch runes[1] {
		case 'x', 'X':
			base, i, dec = 16, 2, false
		case 'o', 'O':
			base, i, dec = 8, 2, false
		case 'b', 'B':
			base, i, dec = 2, 2, false
		default:
			if isDigit(runes[1]) || runes[1] == '_' {
				// Leading-zero octal: a literal in Go and PHP, a syntax
				// error in Python 3 — refusing it there beats misreading it.
				if f == FlavorPython {
					return nil, false, 0, false
				}
				base, i, dec = 8, 1, false
			}
		}
	}
	start := i
	for i < len(runes) && (digitInBase(runes[i], base) || runes[i] == '_') {
		if runes[i] != '_' {
			digits += string(runes[i])
		}
		i++
	}
	if i == start || digits == "" || runes[i-1] == '_' {
		return nil, false, 0, false
	}
	if i < len(runes) && (isLetter(runes[i]) || runes[i] == '_' || runes[i] == '.') {
		return nil, false, 0, false
	}
	v, ok = new(big.Int).SetString(digits, base)
	if !ok {
		return nil, false, 0, false
	}
	return v, dec, i, true
}

// --- parser ------------------------------------------------------------------

// precedence returns the binding level of op under flavor f, 0 for an operator
// the flavor has no place for. Go's grammar has two binary levels for these
// operators; Python and PHP share the C ladder.
func precedence(op string, f Flavor) int {
	if f == FlavorGo {
		switch op {
		case "*", "/", "%", "<<", ">>", "&", "&^":
			return 2
		case "+", "-", "|", "^":
			return 1
		}
		return 0
	}
	switch op {
	case "*", "/", "//", "%":
		return 6
	case "+", "-":
		return 5
	case "<<", ">>":
		return 4
	case "&":
		return 3
	case "^":
		return 2
	case "|":
		return 1
	}
	return 0
}

type parser struct {
	toks []token
	i    int
	f    Flavor
}

// primary parses a literal, a parenthesised expression or a sign-prefixed
// primary. Unary sign binds tighter than every binary operator, which all
// three languages agree on for the operators covered here.
func (p *parser) primary() (*big.Int, bool) {
	if p.i >= len(p.toks) {
		return nil, false
	}
	t := p.toks[p.i]
	switch t.kind {
	case tokNum:
		p.i++
		return new(big.Int).Set(t.val), true
	case tokLParen:
		p.i++
		v, ok := p.expr(1)
		if !ok || p.i >= len(p.toks) || p.toks[p.i].kind != tokRParen {
			return nil, false
		}
		p.i++
		return v, true
	case tokOp:
		if t.op == "+" || t.op == "-" {
			p.i++
			v, ok := p.primary()
			if !ok {
				return nil, false
			}
			if t.op == "-" {
				v.Neg(v)
			}
			return v, true
		}
	}
	return nil, false
}

// expr is precedence climbing over the flavor's table. Every operator here is
// left-associative, so the recursion asks for strictly higher precedence.
func (p *parser) expr(min int) (*big.Int, bool) {
	lhs, ok := p.primary()
	if !ok {
		return nil, false
	}
	for p.i < len(p.toks) && p.toks[p.i].kind == tokOp {
		op := p.toks[p.i].op
		prec := precedence(op, p.f)
		if prec == 0 {
			return nil, false
		}
		if prec < min {
			break
		}
		p.i++
		rhs, ok := p.expr(prec + 1)
		if !ok {
			return nil, false
		}
		lhs, ok = apply(op, lhs, rhs)
		if !ok {
			return nil, false
		}
	}
	return lhs, true
}

// apply computes one binary operation, refusing every case the flavors
// disagree on or that cannot end well: inexact or zero division (Go truncates
// where Python floats), negative modulo/bitwise/shift operands (truncated vs
// floored remainders, sign-extension conventions), oversized shift counts and
// intermediates past maxBits.
func apply(op string, a, b *big.Int) (*big.Int, bool) {
	bounded := func(v *big.Int) (*big.Int, bool) { return v, v.BitLen() <= maxBits }
	switch op {
	case "+":
		return bounded(new(big.Int).Add(a, b))
	case "-":
		return bounded(new(big.Int).Sub(a, b))
	case "*":
		return bounded(new(big.Int).Mul(a, b))
	case "/":
		if b.Sign() == 0 {
			return nil, false
		}
		q, r := new(big.Int).QuoRem(a, b, new(big.Int))
		if r.Sign() != 0 {
			return nil, false
		}
		return q, true
	case "//":
		if a.Sign() < 0 || b.Sign() <= 0 {
			return nil, false
		}
		return new(big.Int).Div(a, b), true
	case "%":
		if a.Sign() < 0 || b.Sign() <= 0 {
			return nil, false
		}
		return new(big.Int).Mod(a, b), true
	case "<<":
		if a.Sign() < 0 || b.Sign() < 0 || !b.IsUint64() || b.Uint64() > maxShift {
			return nil, false
		}
		return bounded(new(big.Int).Lsh(a, uint(b.Uint64())))
	case ">>":
		if a.Sign() < 0 || b.Sign() < 0 || !b.IsUint64() || b.Uint64() > maxShift {
			return nil, false
		}
		return new(big.Int).Rsh(a, uint(b.Uint64())), true
	case "&":
		if a.Sign() < 0 || b.Sign() < 0 {
			return nil, false
		}
		return new(big.Int).And(a, b), true
	case "|":
		if a.Sign() < 0 || b.Sign() < 0 {
			return nil, false
		}
		return new(big.Int).Or(a, b), true
	case "^":
		if a.Sign() < 0 || b.Sign() < 0 {
			return nil, false
		}
		return new(big.Int).Xor(a, b), true
	case "&^":
		if a.Sign() < 0 || b.Sign() < 0 {
			return nil, false
		}
		return new(big.Int).AndNot(a, b), true
	}
	return nil, false
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func isLetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }

func digitInBase(r rune, base int) bool {
	switch base {
	case 16:
		return isDigit(r) || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
	case 10:
		return isDigit(r)
	case 8:
		return r >= '0' && r <= '7'
	case 2:
		return r == '0' || r == '1'
	}
	return false
}
