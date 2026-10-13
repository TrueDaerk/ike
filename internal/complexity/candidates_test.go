package complexity

import (
	"reflect"
	"testing"
)

// candidates_test.go pins the extraction arithmetic (#2948) on hand-built
// trees: the acceptance fixture with the savings derived by hand, the
// filter, an else body, an else-if and a closure; golden_test.go's
// TestCandidateInvariants checks the identities over every fixture.

// at places node c on the 0-based lines first..last.
func at(c *node, first, last int) *node {
	c.StartLine, c.EndLine = first, last
	return c
}

// stmt builds a statement node whose text starts with the keyword kw, so
// its site kind reads as the language spells it ("if", "for").
func stmt(s *srcBuilder, kind, field, kw string, children ...*node) *node {
	c := s.leaf(kind, field, kw)
	c.Children = children
	return c
}

// allCandidates runs the measurement with the filter lifted, so every
// nesting node's candidate is visible.
func allCandidates(t *testing.T, root *node, lines []string) []Function {
	t.Helper()
	min, max := candidateMinSaving, candidateMax
	candidateMinSaving, candidateMax = 0, 1<<30
	defer func() { candidateMinSaving, candidateMax = min, max }()
	return measure(root, lines, goLike, comments{})
}

// acceptanceTree is the issue's fixture: an if at level 0 holding a for
// that holds two ifs.
//
//	func f() {        // line 0
//	  if a {          // 1   cog 1  (level 0)
//	    for {         // 2   cog 2  (nesting 1)
//	      if b {}     // 3   cog 3  (nesting 2)
//	      if c {}     // 4   cog 3  (nesting 2)
//	    }             // 5
//	  }               // 6
//	}                 // 7
//
// CCN 1 + 4 = 5, cognitive 1 + 2 + 3 + 3 = 9.
func acceptanceTree(s *srcBuilder) *node {
	return n("source_file", "", at(fn(s, "f",
		at(stmt(s, "if_statement", "", "if", s.leaf("identifier", "condition", "a"), n("block", "consequence",
			at(stmt(s, "for_statement", "", "for", n("block", "body",
				at(stmt(s, "if_statement", "", "if", s.leaf("identifier", "condition", "b"), n("block", "consequence")), 3, 3),
				at(stmt(s, "if_statement", "", "if", s.leaf("identifier", "condition", "c"), n("block", "consequence")), 4, 4),
			)), 2, 5),
		)), 1, 6),
	), 0, 7))
}

func TestCandidatesAcceptanceFixture(t *testing.T) {
	var s srcBuilder
	root := acceptanceTree(&s)
	f := one(t, allCandidates(t, root, []string{s.b.String()}), "f")
	if f.CCN != 5 || f.Cognitive != 9 {
		t.Fatalf("fixture CCN %d cog %d, want 5 and 9", f.CCN, f.Cognitive)
	}
	// One candidate per nesting node, in walk order. The derivation:
	//
	//   if (level 0): the if itself 1 → 1, the sites inside lose 0:
	//     saving 0; new fn = whole body: ccn 1 + 4 = 5, cog 9; rest ccn 1, cog 0.
	//   for (level 1): the for 2 → 1 saves 1, the two ifs lose 1 each:
	//     saving 3; new fn ccn 1 + 3 = 4, cog 1 + 2 + 2 = 5; rest ccn 2
	//     (the if), cog 1 (the if). 5 + 1 == 9 − 3.
	//   if b (level 2): 3 → 1 saves 2, nothing inside: new fn ccn 2 cog 1;
	//     rest ccn 4 cog 6 (1 + 2 + 3). Likewise if c.
	want := []Candidate{
		{Line: 1, EndLine: 6, Kind: "if", CogSaving: 0, NewCCN: 5, NewCog: 9, RestCCN: 1, RestCog: 0},
		{Line: 2, EndLine: 5, Kind: "for", CogSaving: 3, NewCCN: 4, NewCog: 5, RestCCN: 2, RestCog: 1},
		{Line: 3, EndLine: 3, Kind: "if", CogSaving: 2, NewCCN: 2, NewCog: 1, RestCCN: 4, RestCog: 6},
		{Line: 4, EndLine: 4, Kind: "if", CogSaving: 2, NewCCN: 2, NewCog: 1, RestCCN: 4, RestCog: 6},
	}
	// Unfiltered, the list is sorted by saving: the for first, then the
	// range order.
	wantSorted := []Candidate{want[1], want[2], want[3], want[0]}
	if !reflect.DeepEqual(f.Candidates, wantSorted) {
		t.Errorf("all candidates =\n%+v\nwant\n%+v", f.Candidates, wantSorted)
	}
	for _, c := range f.Candidates {
		if c.RestCog+c.NewCog != f.Cognitive-c.CogSaving || c.RestCCN+c.NewCCN != f.CCN+1 {
			t.Errorf("identity broken for %+v", c)
		}
	}
	// With the filter, only the for saves MinCandidateSaving or more.
	s = srcBuilder{}
	f = one(t, measure(acceptanceTree(&s), []string{s.b.String()}, goLike, comments{}), "f")
	if !reflect.DeepEqual(f.Candidates, []Candidate{want[1]}) {
		t.Errorf("filtered candidates = %+v, want the for only", f.Candidates)
	}
}

func TestCandidatesFlatFunctionHasNone(t *testing.T) {
	var s srcBuilder
	// if a {} if b {} — two ifs at level 0 save nothing; a function
	// without structures has nothing to extract.
	root := n("source_file", "",
		fn(&s, "flat",
			stmt(&s, "if_statement", "", "if", s.leaf("identifier", "condition", "a"), n("block", "consequence")),
			stmt(&s, "if_statement", "", "if", s.leaf("identifier", "condition", "b"), n("block", "consequence"))),
		fn(&s, "empty"))
	fns := measure(root, []string{s.b.String()}, goLike, comments{})
	for _, name := range []string{"flat", "empty"} {
		if f := one(t, fns, name); len(f.Candidates) != 0 {
			t.Errorf("%s candidates = %+v, want none", name, f.Candidates)
		}
	}
}

func TestCandidatesTopThreeBySavingThenRange(t *testing.T) {
	var s srcBuilder
	// for { for { if; if; if; if } } — the inner for saves 1 + 4 = 5, each
	// inner if 2 (dropped), the outer for 0; raise the ifs by one more
	// level to make four candidates at 3 and check the cut and the order.
	ifs := func(first int) []*node {
		var out []*node
		for i := 0; i < 4; i++ {
			out = append(out, at(stmt(&s, "if_statement", "", "if", s.leaf("identifier", "condition", "c"), n("block", "consequence")), first+i, first+i))
		}
		return out
	}
	root := n("source_file", "", at(fn(&s, "f",
		at(stmt(&s, "for_statement", "", "for", n("block", "body",
			at(stmt(&s, "for_statement", "", "for", n("block", "body",
				at(stmt(&s, "for_statement", "", "for", n("block", "body", ifs(3)...)), 2, 7),
			)), 1, 8),
		)), 0, 9),
	), 0, 10))
	f := one(t, measure(root, []string{s.b.String()}, goLike, comments{}), "f")
	// Levels: for 0 (1), for 1 (2), for 2 (3), ifs at 3 (4 each): cog 22.
	// Savings: innermost for 2 + 4×2 = 10; middle for 1 + 5×1 = 6; each if
	// 3; outer for 0. Kept: 10, 6 and the first if by range.
	want := []Candidate{
		{Line: 2, EndLine: 7, Kind: "for", CogSaving: 10, NewCCN: 6, NewCog: 9, RestCCN: 3, RestCog: 3},
		{Line: 1, EndLine: 8, Kind: "for", CogSaving: 6, NewCCN: 7, NewCog: 15, RestCCN: 2, RestCog: 1},
		{Line: 3, EndLine: 3, Kind: "if", CogSaving: 3, NewCCN: 2, NewCog: 1, RestCCN: 7, RestCog: 18},
	}
	if f.Cognitive != 22 || !reflect.DeepEqual(f.Candidates, want) {
		t.Errorf("cog %d candidates =\n%+v\nwant\n%+v", f.Cognitive, f.Candidates, want)
	}
}

func TestCandidatesElseBodyAndElseIf(t *testing.T) {
	var s srcBuilder
	// for { if a {} else { for { if {} } } } — the else at level 1 holds a
	// for (level 2, cog 3) and an if (level 3, cog 4): CCN 5, cog
	// 1 + 2 + 1 + 3 + 4 = 11.
	root := n("source_file", "", at(fn(&s, "f",
		at(stmt(&s, "for_statement", "", "for", n("block", "body",
			at(stmt(&s, "if_statement", "", "if", s.leaf("identifier", "condition", "a"), n("block", "consequence"),
				at(n("block", "alternative",
					at(stmt(&s, "for_statement", "", "for", n("block", "body",
						at(stmt(&s, "if_statement", "", "if", s.leaf("identifier", "condition", "b"), n("block", "consequence")), 4, 4),
					)), 3, 5),
				), 2, 6),
			), 1, 6),
		)), 0, 7),
	), 0, 8))
	f := one(t, allCandidates(t, root, []string{s.b.String()}), "f")
	if f.CCN != 5 || f.Cognitive != 11 {
		t.Fatalf("CCN %d cog %d, want 5 and 11", f.CCN, f.Cognitive)
	}
	// The else body: its keyword stays (+1 in the rest), the body's sites
	// sat at level 2 and drop to 0: saving 2 + 2 = 4; new fn ccn 1 + 2 = 3,
	// cog 7 − 4 = 3 (for 1, if 2); rest ccn 3, cog 11 − 4 − 3 = 4 (for 1,
	// if 2, else 1).
	var els *Candidate
	for i := range f.Candidates {
		if f.Candidates[i].Kind == "else" {
			els = &f.Candidates[i]
		}
	}
	want := Candidate{Line: 2, EndLine: 6, Kind: "else", CogSaving: 4, NewCCN: 3, NewCog: 3, RestCCN: 3, RestCog: 4}
	if els == nil || *els != want {
		t.Errorf("else candidate = %+v, want %+v", els, want)
	}
	// The if (level 1) extracted whole takes its else along: 2 → 1 saves
	// 1, the for and the inner if lose 1 each: 3; the else stays flat, so
	// the new fn reads if 1 + else 1 + for 2 + if 3 = 7.
	byLine := map[int]Candidate{}
	for _, c := range f.Candidates {
		byLine[c.Line] = c
	}
	if c := byLine[1]; c.Kind != "if" || c.CogSaving != 3 || c.NewCCN != 4 || c.NewCog != 7 || c.RestCCN != 2 || c.RestCog != 1 {
		t.Errorf("if candidate = %+v", c)
	}
	// Sorted: the else and the inner for (level 2: 2 + 2 = 4) tie at 4 and
	// go by range, then the two at 3 by range (if at line 1, if at 4), the
	// outer for (0) last.
	if len(f.Candidates) != 5 || f.Candidates[0].Kind != "else" || f.Candidates[1].Line != 3 || f.Candidates[2].Line != 1 || f.Candidates[3].Line != 4 || f.Candidates[4].CogSaving != 0 {
		t.Errorf("order = %+v", f.Candidates)
	}

	// An else-if is part of its chain, not a candidate of its own.
	s = srcBuilder{}
	root = n("source_file", "", fn(&s, "g",
		stmt(&s, "for_statement", "", "for", n("block", "body",
			stmt(&s, "if_statement", "", "if", s.leaf("identifier", "condition", "a"), n("block", "consequence"),
				stmt(&s, "if_statement", "alternative", "if", s.leaf("identifier", "condition", "b"), n("block", "consequence",
					stmt(&s, "for_statement", "", "for", n("block", "body",
						stmt(&s, "if_statement", "", "if", s.leaf("identifier", "condition", "c"), n("block", "consequence")))))))))))
	g := one(t, allCandidates(t, root, []string{s.b.String()}), "g")
	kinds := map[string]int{}
	for _, c := range g.Candidates {
		kinds[c.Kind]++
	}
	if kinds["else if"] != 0 || kinds["if"] != 2 || kinds["for"] != 2 || len(g.Candidates) != 4 {
		t.Errorf("else-if chain candidates = %+v", g.Candidates)
	}
}

func TestCandidatesClosureBodyBelongsToClosure(t *testing.T) {
	var s srcBuilder
	// outer: for { closure { for { if; if } } } — the closure's structures
	// are the closure's candidates; the outer unit sees only its for, whose
	// saving counts the closure's sites at the outer nesting.
	root := n("source_file", "", at(fn(&s, "outer",
		at(stmt(&s, "for_statement", "", "for", n("block", "body",
			at(n("func_literal", "", n("parameter_list", "parameters"), n("block", "body",
				at(stmt(&s, "for_statement", "", "for", n("block", "body",
					at(stmt(&s, "if_statement", "", "if", s.leaf("identifier", "condition", "a"), n("block", "consequence")), 4, 4),
					at(stmt(&s, "if_statement", "", "if", s.leaf("identifier", "condition", "b"), n("block", "consequence")), 5, 5),
				)), 3, 6),
			)), 2, 7),
		)), 1, 8),
	), 0, 9))
	fns := allCandidates(t, root, []string{s.b.String()})
	outer, inner := one(t, fns, "outer"), one(t, fns, "(anonymous)#1")
	// Outer: for (level 0, 1), closure raises, for at level 2 (3), ifs at
	// level 3 (4 each): cog 12, CCN 1. Its only candidate is the for at
	// level 0: saving 0, the new fn would hold everything.
	if outer.Cognitive != 12 || len(outer.Candidates) != 1 || outer.Candidates[0].Kind != "for" || outer.Candidates[0].Line != 1 {
		t.Errorf("outer cog %d candidates = %+v", outer.Cognitive, outer.Candidates)
	}
	if c := outer.Candidates[0]; c.CogSaving != 0 || c.NewCCN != 2 || c.NewCog != 12 || c.RestCCN != 1 || c.RestCog != 0 {
		t.Errorf("outer for candidate = %+v", c)
	}
	// The closure: for (1), ifs at level 1 (2 each): cog 5, CCN 4; its
	// candidates are its own for (saving 0) and ifs (1 each).
	if inner.Cognitive != 5 || inner.CCN != 4 || len(inner.Candidates) != 3 {
		t.Errorf("closure cog %d ccn %d candidates = %+v", inner.Cognitive, inner.CCN, inner.Candidates)
	}
	for _, c := range inner.Candidates {
		if c.Line < 3 || c.EndLine > 6 {
			t.Errorf("closure candidate outside its range: %+v", c)
		}
	}
}
