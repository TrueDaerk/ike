package highlight

// fold.go is the pure-Go side of code folding (#144): the fold-range model the
// CGo parser fills (parse_cgo.go collects nodes whose kind is listed in the
// language's FoldNodes) and the containment lookups the editor queries when a
// fold is toggled. Like the span and scope models, it compiles with or
// without cgo.

// Fold is one foldable region: a multi-line node whose body lines
// [HeaderLine+1, EndLine] can be collapsed behind the header line. Lines are
// 0-based buffer lines; the fold covers [HeaderLine, EndLine] inclusive.
type Fold struct {
	HeaderLine int
	EndLine    int
}

// HiddenLines is the number of buffer lines a collapsed fold hides — the
// count shown in the header placeholder.
func (f Fold) HiddenLines() int { return f.EndLine - f.HeaderLine }

// Contains reports whether line is inside the fold, header included.
func (f Fold) Contains(line int) bool { return f.HeaderLine <= line && line <= f.EndLine }

// InnermostFold returns the innermost fold containing line (header included).
// Folds must be in pre-order (outer before inner), which is how the parser
// emits them; the innermost containing fold is the one with the largest
// header line. ok is false when no fold contains line.
func InnermostFold(folds []Fold, line int) (Fold, bool) {
	var best Fold
	found := false
	for _, f := range folds {
		if f.Contains(line) && (!found || f.HeaderLine >= best.HeaderLine) {
			best, found = f, true
		}
	}
	return best, found
}
