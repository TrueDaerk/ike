package jqplay

// structure.go layers the query line's visual structure (#2775) over the
// scanner's runs: every bracket gets its nesting depth for the rainbow, an
// unpaired one is flagged, and a `|` at depth zero — a pipeline stage break —
// is marked for emphasis. It reads only the KindOperator runs Tokens already
// produced, so a bracket or pipe inside a string or comment is never counted.

// Mark is the structural role of one rune of the program.
type Mark struct {
	// Depth is a bracket's nesting depth (0 for the outermost pair).
	Depth int
	// Bracket reports the rune is one of ( ) [ ] { }.
	Bracket bool
	// Unmatched reports a bracket with no partner (or the wrong one).
	Unmatched bool
	// Pipe reports a top-level `|` stage separator.
	Pipe bool
}

// Structure returns the marks of program keyed by rune index, given its
// tokens. pipes turns the top-level pipe marks on; a dialect whose programs
// are not pipelines (xmq's shell words) passes false and gets brackets only.
// It never fails: unbalanced input only yields Unmatched marks.
func Structure(program string, tokens []Token, pipes bool) map[int]Mark {
	r := []rune(program)
	out := map[int]Mark{}
	var stack []int // rune indices of open brackets
	for _, t := range tokens {
		if t.Kind != KindOperator || t.End-t.Start != 1 {
			continue
		}
		i := t.Start
		switch c := r[i]; c {
		case '(', '[', '{':
			out[i] = Mark{Depth: len(stack), Bracket: true}
			stack = append(stack, i)
		case ')', ']', '}':
			if n := len(stack); n > 0 && r[stack[n-1]] == openerOf(c) {
				stack = stack[:n-1]
				out[i] = Mark{Depth: n - 1, Bracket: true}
				continue
			}
			out[i] = Mark{Bracket: true, Unmatched: true}
		case '|':
			// `|=` is the update-assignment operator, not a stage break.
			if pipes && len(stack) == 0 && (i+1 >= len(r) || r[i+1] != '=') {
				out[i] = Mark{Pipe: true}
			}
		}
	}
	for _, i := range stack {
		out[i] = Mark{Bracket: true, Unmatched: true}
	}
	return out
}

// openerOf is the opening bracket a closing one pairs with.
func openerOf(c rune) rune {
	switch c {
	case ')':
		return '('
	case ']':
		return '['
	}
	return '{'
}
