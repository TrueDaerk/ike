package consthint

import "testing"

// TestEval covers the evaluator over the flavors' shared ground: arithmetic,
// bases, underscores, parens and unary sign.
func TestEval(t *testing.T) {
	cases := []struct {
		expr string
		f    Flavor
		want uint64
	}{
		{"10 * 1024 * 1024", FlavorPython, 10485760},
		{"10 << 20", FlavorGo, 10485760},
		{"60 * 60 * 24", FlavorPHP, 86400},
		{"10 / 2", FlavorGo, 5},
		{"10 // 3", FlavorPython, 3},
		{"10 % 3", FlavorPython, 1},
		{"(2 + 3) * 4", FlavorGo, 20},
		{"0xFF", FlavorPython, 255},
		{"0o755", FlavorGo, 493},
		{"0b1010", FlavorPHP, 10},
		{"0755", FlavorGo, 493},
		{"0755", FlavorPHP, 493},
		{"10_485_760", FlavorPython, 10485760},
		{"1_000 * 1_000", FlavorGo, 1000000},
		{"0xF0 & 0x1F", FlavorPython, 16},
		{"0x0F | 0xF0", FlavorPython, 255},
		{"0xFF ^ 0x0F", FlavorPython, 240},
		{"0xFF &^ 0x0F", FlavorGo, 240},
		{"1 << 8 >> 4", FlavorGo, 16},
		{"+5 * -(-2)", FlavorPython, 10},
		{"(1 << 70) / (1 << 10)", FlavorGo, 1 << 60},
	}
	for _, c := range cases {
		res, ok := Eval(c.expr, c.f)
		if !ok || res.Value != c.want {
			t.Errorf("Eval(%q, %v) = %+v, %v; want %d", c.expr, c.f, res, ok, c.want)
		}
	}
}

// TestEvalPrecedence: Go binds shifts and & at the multiplicative level;
// Python and PHP use the C ladder — the same source text must not evaluate
// under the wrong grammar.
func TestEvalPrecedence(t *testing.T) {
	cases := []struct {
		expr string
		f    Flavor
		want uint64
	}{
		{"1<<4 + 1", FlavorGo, 17},     // (1<<4) + 1
		{"1<<4 + 1", FlavorPython, 32}, // 1 << (4+1)
		{"1<<4 + 1", FlavorPHP, 32},
		{"2 + 3 & 1", FlavorGo, 3},     // 2 + (3&1)
		{"2 + 3 & 1", FlavorPython, 1}, // (2+3) & 1
		{"2 * 3 + 4", FlavorGo, 10},
		{"2 + 3 * 4", FlavorPHP, 14},
	}
	for _, c := range cases {
		res, ok := Eval(c.expr, c.f)
		if !ok || res.Value != c.want {
			t.Errorf("Eval(%q, %v) = %+v, %v; want %d", c.expr, c.f, res, ok, c.want)
		}
	}
}

// TestEvalRejects: everything that is not side-effect-free literal arithmetic
// — or that the flavors disagree on — fails rather than guessing: identifiers,
// calls, floats, strings, overflow, zero or inexact division, negative modulo
// and shift operands, oversized shifts, negative results.
func TestEvalRejects(t *testing.T) {
	cases := []struct {
		expr string
		f    Flavor
	}{
		{"MAX * 2", FlavorPython},
		{"get_limit()", FlavorPython},
		{"10 * KiB", FlavorGo},
		{"iota + 1", FlavorGo},
		{"1.5 * 2", FlavorPython},
		{"'x'", FlavorPHP},
		{"2 ** 8", FlavorPython},
		{"10 / 4", FlavorGo},      // inexact: Go truncates, Python floats
		{"10 / 0", FlavorPython},  // division by zero
		{"10 % 0", FlavorPython},  // modulo by zero
		{"-10 % 3", FlavorPython}, // truncated vs floored remainder
		{"-1 << 4", FlavorGo},     // negative shift operand
		{"1 << -2", FlavorPython}, // negative shift count
		{"1 << 500", FlavorGo},    // oversized shift
		{"1 << 200", FlavorPython},
		{"0xFFFFFFFFFFFFFFFF + 1", FlavorGo}, // past uint64
		{"3 - 5", FlavorPython},              // negative result
		{"-2 & 3", FlavorPython},             // negative bitwise operand
		{"0755", FlavorPython},               // leading-zero octal is a Python error
		{"0xFF_", FlavorGo},                  // trailing underscore
		{"08", FlavorGo},                     // 8 is no octal digit
		{"(1 + 2", FlavorGo},                 // unbalanced paren
		{"1 2", FlavorGo},                    // two literals, no operator
		{"1 &^ 2", FlavorPython},             // &^ is Go-only
		{"", FlavorGo},
	}
	for _, c := range cases {
		if res, ok := Eval(c.expr, c.f); ok {
			t.Errorf("Eval(%q, %v) = %+v, want rejection", c.expr, c.f, res)
		}
	}
}

// TestEvalResultFlags: the Single/Decimal flags drive the append-vs-replace
// rendering decision, so a bare literal and a parenthesised one must differ.
func TestEvalResultFlags(t *testing.T) {
	cases := []struct {
		expr            string
		f               Flavor
		single, decimal bool
	}{
		{"1024", FlavorPython, true, true},
		{"10_000", FlavorPython, true, true},
		{"0xFF", FlavorGo, true, false},
		{"0755", FlavorPHP, true, false},
		{"(1024)", FlavorPython, false, false},
		{"2 * 3", FlavorGo, false, false},
	}
	for _, c := range cases {
		res, ok := Eval(c.expr, c.f)
		if !ok || res.Single != c.single || res.Decimal != c.decimal {
			t.Errorf("Eval(%q) = %+v, %v; want Single=%v Decimal=%v",
				c.expr, res, ok, c.single, c.decimal)
		}
	}
}
