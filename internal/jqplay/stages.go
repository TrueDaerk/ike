package jqplay

import "github.com/itchyny/gojq"

// stages.go is the engine half of the playground's pipeline stepping (#2785):
// debugging a long pipeline used to mean deleting its tail to look at what an
// earlier stage produced. Stepping runs the program cut *after* a chosen stage
// instead, so the cut is a pure function of the program text — which stages
// it has, which one holds the caret, and what the program is up to one of
// them.
//
// The boundaries come from the same splitter the multi-line view breaks its
// rows at (pipeSegments), restricted to the program's own level: a `|` in a
// string, a comment, `||`, `|=`, or inside brackets / `if … end` / a
// `def …;` body is not where the outer pipeline hands a value on.

// Stages returns the stages of program's top-level pipeline, in order, as rune
// ranges with the pipe and the blanks around it excluded — the span the query
// line highlights. A program without a top-level pipe is one stage; the empty
// program is one empty stage, so the result is never empty.
func Stages(program string) []Line {
	r := []rune(program)
	segs := pipeSegments(program, r, true)
	out := make([]Line, 0, len(segs))
	for i, seg := range segs {
		start, end := seg.Start, seg.End
		if i < len(segs)-1 {
			end-- // the pipe that ends every stage but the last
		}
		for start < end && isBlank(r[start]) {
			start++
		}
		for end > start && isBlank(r[end-1]) {
			end--
		}
		out = append(out, Line{Start: start, End: end})
	}
	return out
}

// StageAt reports the 1-based stage holding rune position pos: the stage whose
// text it is in or right behind, a position on a pipe or the blanks after one
// belonging to the stage that follows.
func StageAt(stages []Line, pos int) int {
	for i, st := range stages {
		if pos <= st.End {
			return i + 1
		}
	}
	return len(stages)
}

// StageProgram returns program cut after its k-th (1-based) top-level stage —
// the program whose output is that stage's. k at or past the last stage is the
// whole program, unchanged.
//
// A cut can end on a binding whose body it removed: `.[] as $x | …` cut after
// its first stage is `.[] as $x`, which does not parse. A binding passes its
// input on to its body, so the stage's output is exactly what `| .` makes of
// it; the identity is appended only when the bare cut does not parse and the
// completed one does, so an ordinary syntax error still reports against the
// text the user wrote.
func StageProgram(program string, k int) string {
	stages := Stages(program)
	if k < 1 || k >= len(stages) {
		return program
	}
	cut := string([]rune(program)[:stages[k-1].End])
	if _, err := gojq.Parse(cut); err != nil {
		if _, err := gojq.Parse(cut + " | ."); err == nil {
			return cut + " | ."
		}
	}
	return cut
}

// nestToken tracks the constructs a top-level pipe must not be inside, and
// reports whether t was one of their delimiters (and so no pipe). Brackets
// nest as themselves; `if` opens a block `end` closes, and `def` opens a
// function body its `;` closes — a `;` inside the body's own brackets is an
// argument separator and never reaches the def. A closing bracket also closes
// any keyword block still open inside it, so an `if` that was only ever an
// object key (`{if: 1}`) cannot swallow the rest of the program.
func nestToken(r []rune, t Token, nest *[]rune) bool {
	switch t.Kind {
	case KindKeyword:
		switch string(r[t.Start:t.End]) {
		case "if":
			*nest = append(*nest, 'i')
			return true
		case "def":
			*nest = append(*nest, 'd')
			return true
		case "end":
			popNest(nest, 'i', false)
			return true
		}
	case KindOperator:
		if t.End != t.Start+1 {
			return false
		}
		switch c := r[t.Start]; c {
		case '(', '[', '{':
			*nest = append(*nest, c)
			return true
		case ')', ']', '}':
			popNest(nest, openerOf(c), true)
			return true
		case ';':
			popNest(nest, 'd', false)
			return true
		}
	}
	return false
}

// popNest closes the innermost open construct if it is want. through lets a
// closing bracket also close the keyword blocks opened inside it; an
// unmatched closer leaves the stack as it is.
func popNest(nest *[]rune, want rune, through bool) {
	s := *nest
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == want {
			*nest = s[:i]
			return
		}
		if !through || (s[i] != 'i' && s[i] != 'd') {
			return
		}
	}
}

// isBlank reports a rune the stage spans trim.
func isBlank(c rune) bool { return c == ' ' || c == '\t' || c == '\n' }
