//go:build cgo

package complexity

// duplicates_test.go covers the duplicate detector (#3044) on fixtures in
// Go, Python and PHP: an exact copy, a copy with renamed identifiers, one
// with changed literals, a block below the threshold, overlapping and
// nested matches, copies across files, and the kept call names.

import (
	"strings"
	"testing"

	_ "ike/plugins/languages/go"
	_ "ike/plugins/languages/php"
	_ "ike/plugins/languages/python"
)

// tokensOf tokenizes src as langID.
func tokensOf(t *testing.T, langID, src string) Tokens {
	t.Helper()
	_, toks, err := MeasureTokens(langID, strings.Split(src, "\n"))
	if err != nil {
		t.Fatalf("MeasureTokens(%s): %v", langID, err)
	}
	return toks
}

// goBody is a function body of ~40 tokens; the fixtures wrap it in units.
const goBody = `	total := 0
	for _, x := range xs {
		if x > 0 {
			total += x
		} else if x < -10 {
			total -= 10
		}
	}
	return total
`

// renamedBody is goBody with every local renamed and the literals changed.
const renamedBody = `	acc := 7
	for _, item := range items {
		if item > 0 {
			acc += item
		} else if item < -99 {
			acc -= 1
		}
	}
	return acc
`

func goFile(fns ...string) string { return "package a\n\n" + strings.Join(fns, "\n") }

func goFunc(name, params, body string) string {
	return "func " + name + "(" + params + ") int {\n" + body + "}\n"
}

func TestDuplicatesExactCopyAndRenamed(t *testing.T) {
	src := goFile(goFunc("sumPos", "xs []int", goBody), goFunc("sumNeg", "items []int", renamedBody))
	toks := tokensOf(t, "go", src)
	if len(toks.Seg) != 2 {
		t.Fatalf("segments = %v, want one per unit", toks.Seg)
	}
	unit := int(toks.Seg[1]) - int(toks.Seg[0])
	gs := Duplicates(map[string]Tokens{"/p/a.go": toks}, 10)
	if len(gs) != 1 {
		t.Fatalf("groups = %+v, want one", gs)
	}
	g := gs[0]
	if g.Lang != "go" || g.Tokens != unit || len(g.Occurrences) != 2 {
		t.Fatalf("group = %+v, want the whole unit (%d tokens) twice", g, unit)
	}
	// The occurrences are the two functions, header included: lines 2–11
	// and 14–23 (0-based), in source order.
	a, b := g.Occurrences[0], g.Occurrences[1]
	if a.Line != 2 || a.EndLine != 11 || b.Line != 14 || b.EndLine != 23 {
		t.Fatalf("occurrences = %+v / %+v", a, b)
	}
	if a.Start != 0 || a.End != unit || b.Start != unit || b.End != 2*unit {
		t.Fatalf("token ranges = %+v / %+v", a, b)
	}
	if g.Score() != 2*unit || g.Lines() != 10 || g.Files() != 1 {
		t.Fatalf("score %d lines %d files %d", g.Score(), g.Lines(), g.Files())
	}
}

func TestDuplicatesBelowThreshold(t *testing.T) {
	src := goFile(goFunc("sumPos", "xs []int", goBody), goFunc("sumNeg", "items []int", renamedBody))
	toks := tokensOf(t, "go", src)
	if gs := Duplicates(map[string]Tokens{"/p/a.go": toks}, toks.Len()); len(gs) != 0 {
		t.Fatalf("a threshold above the unit size must find nothing, got %+v", gs)
	}
	// A single function is never a clone of itself.
	one := tokensOf(t, "go", goFile(goFunc("sumPos", "xs []int", goBody)))
	if gs := Duplicates(map[string]Tokens{"/p/a.go": one}, 5); len(gs) != 0 {
		t.Fatalf("one unit must not match itself, got %+v", gs)
	}
}

func TestDuplicatesAcrossFilesSameLanguageOnly(t *testing.T) {
	a := tokensOf(t, "go", goFile(goFunc("sumPos", "xs []int", goBody)))
	b := tokensOf(t, "go", goFile(goFunc("sumNeg", "items []int", renamedBody)))
	// The Python twin of the body normalizes to a different kind stream and
	// is a different language anyway.
	py := tokensOf(t, "python", "def sum_pos(xs):\n    total = 0\n    for x in xs:\n        if x > 0:\n            total += x\n        elif x < -10:\n            total -= 10\n    return total\n\n"+
		"def sum_neg(ys):\n    acc = 7\n    for y in ys:\n        if y > 0:\n            acc += y\n        elif y < -99:\n            acc -= 1\n    return acc\n")
	gs := Duplicates(map[string]Tokens{"/p/a.go": a, "/p/b.go": b, "/p/c.py": py}, 10)
	if len(gs) != 2 {
		t.Fatalf("groups = %+v, want the Go pair and the Python pair", gs)
	}
	byLang := map[string]CloneGroup{}
	for _, g := range gs {
		byLang[g.Lang] = g
	}
	g := byLang["go"]
	if len(g.Occurrences) != 2 || g.Occurrences[0].Path != "/p/a.go" || g.Occurrences[1].Path != "/p/b.go" || g.Files() != 2 {
		t.Fatalf("go group = %+v, want one occurrence per file", g)
	}
	p := byLang["python"]
	if len(p.Occurrences) != 2 || p.Occurrences[0].Path != "/p/c.py" || p.Occurrences[0].Line != 0 || p.Occurrences[1].Line != 9 {
		t.Fatalf("python group = %+v, want both defs of c.py", p)
	}
}

func TestDuplicatesDifferentCalleeIsNoClone(t *testing.T) {
	body := func(call string) string {
		return "\tfor _, x := range xs {\n\t\tif x > 0 {\n\t\t\t" + call + "(x, total)\n\t\t}\n\t}\n\treturn total\n"
	}
	src := goFile(goFunc("a", "xs []int, total int", body("report")), goFunc("b", "ys []int, acc int", body("discard")))
	toks := tokensOf(t, "go", src)
	// The kept callee names split the units into two matching halves at
	// most, never the whole unit.
	unit := int(toks.Seg[1])
	for _, g := range Duplicates(map[string]Tokens{"/p/a.go": toks}, 5) {
		if g.Tokens == unit {
			t.Fatalf("units calling different functions must not match whole: %+v", g)
		}
	}
	same := goFile(goFunc("a", "xs []int, total int", body("report")), goFunc("b", "ys []int, acc int", body("report")))
	gs := Duplicates(map[string]Tokens{"/p/a.go": tokensOf(t, "go", same)}, 5)
	if len(gs) != 1 || gs[0].Tokens != unit {
		t.Fatalf("the same callee must match whole: %+v", gs)
	}
}

func TestDuplicatesOverlappingAndNested(t *testing.T) {
	// One function repeating a block three times: the block matches
	// itself at every shift, but the occurrences come back disjoint.
	block := "\tif a > b {\n\t\tc = a + b\n\t} else {\n\t\tc = a - b\n\t}\n"
	src := goFile("func f(a, b, c int) int {\n" + block + block + block + "\treturn c\n}\n")
	toks := tokensOf(t, "go", src)
	gs := Duplicates(map[string]Tokens{"/p/a.go": toks}, 5)
	if len(gs) == 0 {
		t.Fatal("a thrice-repeated block must be found")
	}
	for _, g := range gs {
		for i := 1; i < len(g.Occurrences); i++ {
			p, q := g.Occurrences[i-1], g.Occurrences[i]
			if p.Path == q.Path && q.Start < p.End {
				t.Fatalf("occurrences overlap: %+v", g)
			}
		}
	}
	// Two copies of a function that each hold two copies of a block: the
	// block's four occurrences all lie inside the function's two and
	// collapse into that group.
	fn := func(name string) string {
		return "func " + name + "(a, b, c int) int {\n" + block + "\tc *= 2\n" + block + "\treturn c\n}\n"
	}
	two := tokensOf(t, "go", goFile(fn("f"), fn("g")))
	gs = Duplicates(map[string]Tokens{"/p/a.go": two}, 5)
	if len(gs) != 1 || len(gs[0].Occurrences) != 2 || gs[0].Tokens != int(two.Seg[1]) {
		t.Fatalf("groups = %+v, want the two whole functions only", gs)
	}
}

func TestDuplicatesPHP(t *testing.T) {
	body := func(v, n string) string {
		return "function " + n + "($" + v + "s) {\n\t$sum = 0;\n\tforeach ($" + v + "s as $" + v + ") {\n\t\tif ($" + v + " > 0) {\n\t\t\t$sum += $" + v + ";\n\t\t} elseif ($" + v + " < -10) {\n\t\t\t$sum -= 10;\n\t\t}\n\t}\n\treturn $sum;\n}\n"
	}
	toks := tokensOf(t, "php", "<?php\n"+body("x", "sumPos")+"\n"+body("item", "sumNeg"))
	gs := Duplicates(map[string]Tokens{"/p/a.php": toks}, 10)
	if len(gs) != 1 || len(gs[0].Occurrences) != 2 || gs[0].Lang != "php" || gs[0].Tokens != int(toks.Seg[1]) {
		t.Fatalf("groups = %+v, want both PHP functions as one clone", gs)
	}
}

func TestDuplicatesOrderAndKey(t *testing.T) {
	big := goFile(goFunc("a", "xs []int", goBody), goFunc("b", "items []int", renamedBody))
	small := "func s1(a, b int) int {\n\tif a > b {\n\t\treturn a\n\t}\n\treturn b\n}\n"
	small2 := strings.ReplaceAll(strings.ReplaceAll(small, "s1", "s2"), "a, b", "p, q")
	small3 := strings.ReplaceAll(small, "s1", "s3")
	toks := tokensOf(t, "go", big+"\n"+small+"\n"+small2+"\n"+small3)
	gs := Duplicates(map[string]Tokens{"/p/a.go": toks}, 5)
	if len(gs) != 2 {
		t.Fatalf("groups = %+v, want the big pair and the small triple", gs)
	}
	if gs[0].Score() < gs[1].Score() {
		t.Fatalf("groups must come score-first: %d then %d", gs[0].Score(), gs[1].Score())
	}
	if len(gs[1].Occurrences) != 3 && len(gs[0].Occurrences) != 3 {
		t.Fatalf("the small function must be found thrice: %+v", gs)
	}
	for _, g := range gs {
		if !strings.HasPrefix(g.Key(), "/p/a.go:") || strings.Count(g.Key(), "\x00") != len(g.Occurrences) {
			t.Fatalf("key %q", g.Key())
		}
	}
	if Duplicates(nil, 0) != nil {
		t.Fatal("no streams, no groups")
	}
}

func TestViewDuplicates(t *testing.T) {
	toks := tokensOf(t, "go", goFile(goFunc("a", "xs []int", goBody), goFunc("b", "items []int", renamedBody)))
	v := View{Tokens: map[string]Tokens{"/p/a.go": toks}}
	if gs := v.Duplicates(10); len(gs) != 1 {
		t.Fatalf("View.Duplicates = %+v", gs)
	}
}
