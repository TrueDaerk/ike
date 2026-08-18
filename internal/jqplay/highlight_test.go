package jqplay

import "testing"

// highlight_test.go covers the query line's jq scanner (#1936): the classes
// it hands the renderer, and the guarantee that it never fails on the
// half-typed programs a live query line is full of.

// kinds renders the kind of every rune of program as a compact string, one
// letter per rune, so a whole classification is one assertion.
func kinds(program string) string {
	tokens := Tokens(program)
	letters := map[Kind]byte{
		KindPlain: '.', KindPath: 'p', KindString: 's', KindNumber: 'n',
		KindKeyword: 'k', KindFunc: 'f', KindVariable: 'v', KindFormat: '@',
		KindOperator: 'o', KindComment: '#',
	}
	out := make([]byte, len([]rune(program)))
	for i := range out {
		out[i] = letters[KindAt(tokens, i)]
	}
	return string(out)
}

func TestTokensClassifies(t *testing.T) {
	cases := []struct{ program, want string }{
		{".foo", "pppp"},
		{".a | .b", "pp.o.pp"},
		{".[]", "poo"},
		{"..", "pp"},
		{`.a == "x"`, "pp.oo.sss"},
		{"map(.n + 1)", "fffopp.o.no"},
		{"if .a then 1 else 2 end", "kk.pp.kkkk.n.kkkk.n.kkk"},
		{"$env.HOME", "vvvvppppp"},
		{"@base64", "@@@@@@@"},
		{".a # note", "pp.######"},
		{"1e-6", "nnnn"},
	}
	for _, c := range cases {
		if got := kinds(c.program); got != c.want {
			t.Errorf("kinds(%q) = %q, want %q", c.program, got, c.want)
		}
	}
}

// TestTokensNeverFails: a live query line spends most of its time holding an
// incomplete program, and the scanner must classify those without hanging or
// running past the end of the input.
func TestTokensNeverFails(t *testing.T) {
	for _, program := range []string{
		"", ".", "..", `"unterminated`, `"esc\`, ".foo[", "$", "@", "1e", "1e-",
		"map(", "if .a then", "\\", "🙂", `.["k`,
	} {
		tokens := Tokens(program)
		n := len([]rune(program))
		for _, tok := range tokens {
			if tok.Start < 0 || tok.End > n || tok.Start >= tok.End {
				t.Errorf("Tokens(%q) produced out-of-range token %+v (len %d)", program, tok, n)
			}
		}
	}
}

// TestTokensAreOrdered: KindAt stops scanning at the first token past the
// index it wants, which only holds if the tokens come out in order.
func TestTokensAreOrdered(t *testing.T) {
	tokens := Tokens(`.a | map(select(.b == "x")) | length`)
	prev := 0
	for _, tok := range tokens {
		if tok.Start < prev {
			t.Fatalf("token %+v starts before the previous one ended at %d", tok, prev)
		}
		prev = tok.End
	}
}

// TestKindAtOutsideAnyToken: whitespace and out-of-range indices fall back to
// the renderer's default class rather than to a neighbour's color.
func TestKindAtOutsideAnyToken(t *testing.T) {
	tokens := Tokens(".a .b")
	if got := KindAt(tokens, 2); got != KindPlain {
		t.Errorf("KindAt(space) = %v, want KindPlain", got)
	}
	if got := KindAt(tokens, 99); got != KindPlain {
		t.Errorf("KindAt(past the end) = %v, want KindPlain", got)
	}
}
