package diff

import (
	"reflect"
	"strings"
	"testing"
)

// refine_test.go covers the token-level intra-line refinement (#2849): the
// tokenizer, the gap cleanup, the whole-line fallback, the size cap and the
// rune (not byte) columns of the spans.

func TestTokenize(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{"", nil},
		{"foo", []string{"foo"}},
		{"foo.bar_1(x)", []string{"foo", ".", "bar_1", "(", "x", ")"}},
		{"  a  :=\tb", []string{"  ", "a", "  ", ":", "=", "\t", "b"}},
		{"名前 = \"太郎\"", []string{"名前", " ", "=", " ", "\"", "太郎", "\""}},
		{"ok ✅✅", []string{"ok", " ", "✅", "✅"}},
	}
	for _, c := range cases {
		runes := []rune(c.line)
		var got []string
		for _, tok := range tokenize(runes, 0, len(runes)) {
			got = append(got, string(runes[tok.start:tok.end]))
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("tokenize(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

func TestRefineTokenLevel(t *testing.T) {
	cases := []struct {
		name        string
		left, right string
		opts        Options
		wantL       string // runes the left spans cover, concatenated
		wantR       string
		wantNil     bool // whole-line fallback: both sides nil
	}{
		{
			name: "renamed identifier inside a call",
			left: "fmt.Println(oldName, count)", right: "fmt.Println(newName, count)",
			wantL: "oldName", wantR: "newName",
		},
		{
			name: "changed string literal",
			left: `log("hello world")`, right: `log("hello there")`,
			wantL: "world", wantR: "there",
		},
		{
			name: "changed operator stays a one-rune edit",
			left: "if a <= b {", right: "if a >= b {",
			wantL: "<", wantR: ">",
		},
		{
			name: "short gap merges into the emphasis",
			left: "foo.bar = other.field", right: "qux.zap = other.field",
			wantL: "foo.bar", wantR: "qux.zap",
		},
		{
			name: "comma-space gap merges, longer gap stays",
			left: "call(a, b) + rest", right: "call(c, d) + rest",
			wantL: "a, b", wantR: "c, d",
		},
		{
			name: "three-rune gap stays unemphasized",
			left: "a + b", right: "x + y",
			wantL: "ab", wantR: "xy",
		},
		{
			name: "adjacent replace and insert form one span per side",
			left: "foo bar", right: "foo baz qux",
			wantL: "bar", wantR: "baz qux",
		},
		{
			name: "fully rewritten line falls back to whole-line emphasis",
			left: "return nil", right: "panic(err)",
			wantNil: true,
		},
		{
			name: "single changed token line falls back",
			left: "}", right: ")",
			wantNil: true,
		},
		{
			name: "empty side falls back",
			left: "", right: "abc",
			wantNil: true,
		},
		{
			name: "short line growing a long insertion keeps the insertion span",
			left: "x", right: "x + someLongExpression(with, args)",
			wantL: "", wantR: " + someLongExpression(with, args)",
		},
		{
			name: "re-indented and renamed, whitespace significant",
			left: "  value := 1", right: "\t\tresult := 1",
			wantL: "  value", wantR: "\t\tresult",
		},
		{
			name: "re-indented and renamed, ignoring whitespace",
			left: "  value := 1", right: "\t\tresult := 1",
			opts:  Options{IgnoreWhitespace: true},
			wantL: "value", wantR: "result",
		},
		{
			name: "re-indented and re-spaced, ignoring whitespace, keeps the value",
			left: "  b   =   2", right: "\tb = 9",
			opts:  Options{IgnoreWhitespace: true},
			wantL: "2", wantR: "9",
		},
		{
			name: "whitespace-only difference, ignoring whitespace",
			left: "  foo(x)", right: "\tfoo (x)",
			opts:    Options{IgnoreWhitespace: true},
			wantNil: true,
		},
		{
			name: "CJK runes map to rune columns",
			left: "名前 = \"太郎\"", right: "名前 = \"花子\"",
			wantL: "太郎", wantR: "花子",
		},
		{
			name: "emoji symbol is its own token",
			left: "status: ✅ done", right: "status: ❌ done",
			wantL: "✅", wantR: "❌",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ls, rs := refineWith(c.left, c.right, c.opts)
			if c.wantNil {
				if ls != nil || rs != nil {
					t.Fatalf("want whole-line fallback, got left %v right %v", ls, rs)
				}
				return
			}
			if got := spanText(c.left, ls); got != c.wantL {
				t.Errorf("left spans %v cover %q, want %q", ls, got, c.wantL)
			}
			if got := spanText(c.right, rs); got != c.wantR {
				t.Errorf("right spans %v cover %q, want %q", rs, got, c.wantR)
			}
		})
	}
}

func TestRefineCJKSpanColumns(t *testing.T) {
	// Columns are rune indices: 太郎 sits at runes [6,8), not at its byte
	// offset.
	ls, rs := refine("名前 = \"太郎\"", "名前 = \"花子\"")
	if want := []Span{{6, 8}}; !reflect.DeepEqual(ls, want) || !reflect.DeepEqual(rs, want) {
		t.Fatalf("spans left %v right %v, want %v on both sides", ls, rs, want)
	}
}

func TestRefineNearSizeCap(t *testing.T) {
	// 990 runes of filler plus a changed identifier: still refined, with the
	// span at the far end.
	prefix := strings.Repeat("ab ", 330)
	ls, rs := refine(prefix+"old", prefix+"new")
	want := []Span{{990, 993}}
	if !reflect.DeepEqual(ls, want) || !reflect.DeepEqual(rs, want) {
		t.Fatalf("spans left %v right %v, want %v on both sides", ls, rs, want)
	}
	// Over maxRefineRunes the pair skips refinement.
	over := strings.Repeat("ab ", 334)
	if ls, rs := refine(over+"old", over+"new"); ls != nil || rs != nil {
		t.Fatalf("a %d-rune line must skip refinement, got %v / %v", len([]rune(over))+3, ls, rs)
	}
}

func TestRefineDivergentLongPairFallsBack(t *testing.T) {
	// Two fully divergent lines at the cap: the Myers budget (#2505) and the
	// fallback both apply — no spans, and no quadratic blow-up.
	left := strings.Repeat("abcd ", 200)
	right := strings.Repeat("wxyz-", 200)
	if ls, rs := refine(left, right); ls != nil || rs != nil {
		t.Fatalf("divergent pair should fall back to whole-line emphasis, got %v / %v", ls, rs)
	}
}
