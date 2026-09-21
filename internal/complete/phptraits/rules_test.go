package phptraits

// rules_test.go is the table test over the two pure rules of the source: how
// the text before the cursor classifies (accessAt) and which member an access
// syntax reaches (offers) — the rule CLAUDE.md asks to keep in one function.

import (
	"testing"

	"ike/internal/phpindex"
)

func TestAccessAt(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		col    int // -1 = end of line
		want   access
		prefix string
	}{
		{"arrow", "        $this->", -1, accessArrow, ""},
		{"arrow partial", "        $this->ab", -1, accessArrow, "ab"},
		{"arrow spaced", "        $this ->  ab", -1, accessArrow, "ab"},
		{"arrow mid line", "        $this->abc();", 15, accessArrow, ""},
		{"arrow inside partial", "        $this->abc();", 17, accessArrow, "ab"},
		{"self", "        self::", -1, accessStatic, ""},
		{"self partial", "        self::MA", -1, accessStatic, "MA"},
		{"self property", "        self::$re", -1, accessStatic, "$re"},
		{"self dollar only", "        self::$", -1, accessStatic, "$"},
		{"static", "        static::make();", 16, accessStatic, ""},
		{"upper-case keyword", "        SELF::", -1, accessStatic, ""},
		{"start of line", "self::", -1, accessStatic, ""},
		{"other variable", "        $that->", -1, accessNone, ""},
		{"identifier ending in self", "        myself::", -1, accessNone, ""},
		{"other object", "        $other->foo();", 16, accessNone, ""},
		{"plain code", "    public function run(): void", -1, accessNone, ""},
		{"empty", "", -1, accessNone, ""},
		{"column before the operator", "        $this->abc();", 8, accessNone, ""},
		{"column past the call", "        $this->abc();", 21, accessNone, ""},
		{"column beyond the line", "        $this->", 99, accessArrow, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			col := c.col
			if col < 0 {
				col = len([]rune(c.line))
			}
			got, prefix := accessAt(c.line, col)
			if got != c.want || prefix != c.prefix {
				t.Errorf("accessAt(%q, %d) = (%v, %q), want (%v, %q)", c.line, col, got, prefix, c.want, c.prefix)
			}
		})
	}
}

func TestOffers(t *testing.T) {
	member := func(k phpindex.MemberKind, static bool) phpindex.Member {
		return phpindex.Member{Kind: k, Static: static}
	}
	cases := []struct {
		name        string
		m           phpindex.Member
		arrow, stat bool
	}{
		{"method", member(phpindex.MemberMethod, false), true, false},
		// PHP allows `$this->staticMethod()`, so a static method is offered
		// after both operators.
		{"static method", member(phpindex.MemberMethod, true), true, true},
		{"property", member(phpindex.MemberProperty, false), true, false},
		{"static property", member(phpindex.MemberProperty, true), false, true},
		// A constant has no instance form: `::` only, static flag or not.
		{"constant", member(phpindex.MemberConst, false), false, true},
		{"enum case", member(phpindex.MemberCase, false), false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := offers(accessArrow, c.m); got != c.arrow {
				t.Errorf("offers($this->) = %v, want %v", got, c.arrow)
			}
			if got := offers(accessStatic, c.m); got != c.stat {
				t.Errorf("offers(self::) = %v, want %v", got, c.stat)
			}
			if offers(accessNone, c.m) {
				t.Error("offers(accessNone) = true, want false")
			}
		})
	}
}

func TestItemTextAndDetail(t *testing.T) {
	method := phpindex.Member{
		Kind: phpindex.MemberMethod, Name: "abc", Visibility: "public",
		Params: "(int $times = 1)", ReturnType: "string", Declaring: `App\Models\B`,
	}
	if got := itemText(accessArrow, method); got != "abc(" {
		t.Errorf("itemText(method) = %q, want %q", got, "abc(")
	}
	if got := detail(method, "class"); got != "abc(int $times = 1): string"+detailSep+"class B" {
		t.Errorf("detail(method) = %q", got)
	}
	prop := phpindex.Member{Kind: phpindex.MemberProperty, Name: "$x", Type: "int", Declaring: `App\Traits\C`}
	if got := itemText(accessArrow, prop); got != "x" {
		t.Errorf("itemText(property, ->) = %q, want %q", got, "x")
	}
	if got := itemText(accessStatic, prop); got != "$x" {
		t.Errorf("itemText(property, ::) = %q, want %q", got, "$x")
	}
	if got := detail(prop, "trait"); got != "int $x"+detailSep+"trait C" {
		t.Errorf("detail(property) = %q", got)
	}
	konst := phpindex.Member{Kind: phpindex.MemberConst, Name: "K", Declaring: `App\Traits\C`}
	if got := itemText(accessStatic, konst); got != "K" {
		t.Errorf("itemText(const) = %q", got)
	}
	if got := detail(konst, ""); got != "K"+detailSep+"C" {
		t.Errorf("detail(const, unknown kind) = %q", got)
	}
	if got := detail(phpindex.Member{Kind: phpindex.MemberMethod, Name: "run"}, ""); got != "run()" {
		t.Errorf("detail(undeclared method) = %q", got)
	}
}

func TestMatchesPrefix(t *testing.T) {
	cases := []struct {
		name, prefix string
		want         bool
	}{
		{"abc", "", true},
		{"abc", "ab", true},
		{"fromC", "fromc", true},
		{"fromC", "FROM", true},
		{"abc", "b", false},
		{"$registry", "$", true},
		{"$registry", "$re", true},
		{"$registry", "$x", false},
	}
	for _, c := range cases {
		if got := matchesPrefix(c.name, c.prefix); got != c.want {
			t.Errorf("matchesPrefix(%q, %q) = %v, want %v", c.name, c.prefix, got, c.want)
		}
	}
}
