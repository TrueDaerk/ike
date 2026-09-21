package phpindex

// access_test.go covers the syntax-tree symbol extraction the navigation
// fallback resolves the token under the cursor with (#2670): the access
// shapes `$this->abc(`, `$this->abc`, `self::K`, `self::$x` and
// `static::make()`, the receivers that must *not* claim, and the positions
// inside an access that are not the member.

import (
	"strings"
	"testing"

	_ "ike/plugins/languages/php"
)

const accessSrc = `<?php

namespace App\Traits;

trait A
{
    public function run(Other $o): void
    {
        $this->abc();
        $this->prop;
        self::K;
        self::$sp;
        static::make();
        $o->foreign();
        parent::gone();
        $this->outer($this->inner());
    }
}
`

// posOf returns the 0-based line/column of the first occurrence of needle in
// src, offset by within runes.
func posOf(t *testing.T, src, needle string, within int) Pos {
	t.Helper()
	for i, line := range strings.Split(src, "\n") {
		if c := strings.Index(line, needle); c >= 0 {
			return Pos{Line: i, Col: len([]rune(line[:c])) + within}
		}
	}
	t.Fatalf("%q not found in source", needle)
	return Pos{}
}

func TestMemberAccessAtShapes(t *testing.T) {
	if !(&Index{}).Available() {
		t.Skip("no PHP grammar in this build (cgo off)")
	}
	cases := []struct {
		name   string
		needle string
		within int
		want   Access
	}{
		{"method call on the name", "$this->abc(", 8, Access{Name: "abc", Kinds: []MemberKind{MemberMethod, MemberProperty}}},
		{"method call on the paren", "$this->abc(", 10, Access{Name: "abc", Kinds: []MemberKind{MemberMethod, MemberProperty}}},
		{"property access", "$this->prop", 8, Access{Name: "prop", Kinds: []MemberKind{MemberProperty, MemberMethod}}},
		{"class constant", "self::K", 6, Access{Name: "K", Kinds: []MemberKind{MemberConst, MemberCase}, Static: true}},
		{"static property", "self::$sp", 7, Access{Name: "$sp", Kinds: []MemberKind{MemberProperty}, Static: true}},
		{"static call", "static::make(", 8, Access{Name: "make", Kinds: []MemberKind{MemberMethod}, Static: true}},
		// The nearest enclosing access wins, not the outer call.
		{"nested call", "$this->outer($this->inner())", 21, Access{Name: "inner", Kinds: []MemberKind{MemberMethod, MemberProperty}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := MemberAccessAt(accessSrc, posOf(t, accessSrc, tc.needle, tc.within))
			if !ok {
				t.Fatalf("no access resolved at %q+%d", tc.needle, tc.within)
			}
			if got.Name != tc.want.Name || got.Static != tc.want.Static {
				t.Fatalf("access = %+v, want %+v", got, tc.want)
			}
			if len(got.Kinds) != len(tc.want.Kinds) {
				t.Fatalf("kinds = %v, want %v", got.Kinds, tc.want.Kinds)
			}
			for i := range got.Kinds {
				if got.Kinds[i] != tc.want.Kinds[i] {
					t.Fatalf("kinds = %v, want %v", got.Kinds, tc.want.Kinds)
				}
			}
		})
	}
}

// TestMemberAccessAtPasses pins the positions the extraction must *not*
// claim: a foreign receiver and `parent::` are the server's business, and
// standing on `$this` itself names no member.
func TestMemberAccessAtPasses(t *testing.T) {
	if !(&Index{}).Available() {
		t.Skip("no PHP grammar in this build (cgo off)")
	}
	cases := []struct {
		name   string
		needle string
		within int
	}{
		{"foreign receiver", "$o->foreign(", 5},
		{"parent scope", "parent::gone(", 9},
		{"on $this itself", "$this->abc(", 2},
		{"plain statement", "public function run", 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := MemberAccessAt(accessSrc, posOf(t, accessSrc, tc.needle, tc.within)); ok {
				t.Fatalf("expected no access, got %+v", got)
			}
		})
	}
}

// TestLookupAccessTriesEveryKind guards the `::` ambiguity: a constant
// access resolves a constant, and an enum case when there is no constant.
func TestLookupAccessTriesEveryKind(t *testing.T) {
	x, _ := fixture(t, defaultOpts())
	konst := Access{Name: "K", Kinds: []MemberKind{MemberConst, MemberCase}, Static: true}
	if got := x.LookupAccess(traitC, konst); len(got) != 1 || got[0].Kind != MemberConst {
		t.Fatalf("self::K in C = %+v, want the constant", got)
	}
	// X is used by the enum Status, so `self::Active` inside X falls through
	// the constant kind to the enum case.
	active := Access{Name: "Active", Kinds: []MemberKind{MemberConst, MemberCase}, Static: true}
	got := x.LookupAccess(traitX, active)
	if len(got) != 1 || got[0].Kind != MemberCase || got[0].Declaring != `App\Enums\Status` {
		t.Fatalf("self::Active in X = %+v, want the enum case on Status", got)
	}
	if got := x.LookupAccess(traitA, Access{Name: "nope", Kinds: []MemberKind{MemberMethod}}); len(got) != 0 {
		t.Fatalf("unknown member resolved to %+v", got)
	}
}
