// Package complexity is the code-complexity engine (Roadmap 0550, #2913,
// #2915): it measures every function of a buffer — cyclomatic complexity
// (McCabe's CCN, the numbers lizard and gocyclo print), cognitive complexity
// (Sonar's definition, as gocognit and complexipy implement it), non-comment
// lines, parameter count and nesting depth — on the Tree-sitter parse IKE
// already has, read through the language's lang.ComplexitySpec.
//
// The package is pure: no UI, no git, no settings. Both algorithms live here
// once; a language plugin only declares which of its node kinds are
// functions, branches, selectors and arms, else arms, nesting scopes, boolean
// operators, jumps and calls (see internal/lang/complexity.go). The project
// index, the diff against HEAD and the tool window are the epic's later steps
// and build on Measure and Summarize.
//
// Rules (see wiki/architecture/complexity.md for the worked examples):
//
//   - CCN = 1 + Branch nodes + Arms + boolean operators inside the unit. A
//     nested function unit (closure, lambda, inner def) is measured on its
//     own and excluded from the enclosing unit's CCN.
//   - Cognitive: +1 and the current nesting level for each Branch and each
//     Selector; +1 flat for an else and for an else-if (a Branch in the else
//     position); +1 per sequence of like boolean operators, parentheses
//     flattened (`a && b && c` = 1, `a && b || c` = 2); +1 per Jump with a
//     target; +1 per direct recursive call. Branch bodies, Selector arms, else
//     bodies, Nesting kinds and nested units raise the nesting level. A nested
//     unit's structures count toward every enclosing unit at the enclosing
//     unit's nesting (Sonar: lambdas are part of the method they sit in) and
//     toward the nested unit itself from level zero.
//   - Depth is the deepest nesting level a unit's walk reaches, closures
//     included; NLOC the lines of the unit's range that are neither blank nor
//     comment-only, by the language's comment markers.
//   - CommentLines are the comment-only lines of the unit's range (#2934),
//     CommentDensity = CommentLines / (NLOC + CommentLines). Only the body
//     counts: a doc comment above the header (Go doc comment, PHPDoc, JSDoc)
//     lies outside the range, and a Python docstring is a string statement,
//     not a comment, so neither raises the density.
//   - Sites (#2946) record every increment above with its position, kind,
//     CCN, cognitive cost, nesting penalty and level, so a unit's totals
//     are the sum of its sites and the tool window can say which `if`,
//     `switch` or `&&` chain costs what.
package complexity

import (
	"errors"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// ErrUnsupported is returned by Measure for a language that is not
// registered, declares no lang.ComplexitySpec, or yields no syntax tree (a
// build without cgo). Nothing panics; the caller reads it as "not measured".
var ErrUnsupported = errors.New("complexity: language not measured")

// The Kind values of a Function.
const (
	KindFunction = "function" // a free function
	KindMethod   = "method"   // a method: Go receiver, class body, PHP trait, …
	KindLambda   = "lambda"   // an anonymous unit: closure, lambda, arrow function
)

// Function is one measured unit of a buffer.
type Function struct {
	Name      string `json:"name"`      // "renderWeek", "Server.handle", "Class::method", "(anonymous)#3"
	Class     string `json:"class"`     // the qualifier of a method (#2937): "Server"; "" for a free function, a lambda, a nested def
	Kind      string `json:"kind"`      // KindFunction | KindMethod | KindLambda
	Line      int    `json:"line"`      // 0-based line of the unit's first token
	EndLine   int    `json:"endLine"`   // 0-based last line of the unit
	NLOC      int    `json:"nloc"`      // non-blank, non-comment lines in [Line, EndLine]
	Params    int    `json:"params"`    // declared parameters
	CCN       int    `json:"ccn"`       // cyclomatic complexity
	Cognitive int    `json:"cognitive"` // cognitive complexity
	Depth     int    `json:"depth"`     // deepest nesting level reached inside the unit
	// CommentLines are the comment-only lines in [Line, EndLine];
	// CommentDensity is CommentLines / (NLOC + CommentLines) as a ratio
	// (0.55), 0 when the unit has neither.
	CommentLines   int     `json:"commentLines"`
	CommentDensity float64 `json:"commentDensity"`
	// Sites are the unit's increments in source order (#2946): one per
	// branch, selector, arm, else, boolean run, jump and recursive call, so
	// 1 + Σ Sites.CCN == CCN and Σ Sites.Cognitive == Cognitive. A nested
	// unit's own structures are its own sites; the enclosing unit, whose
	// cognitive total includes them at its nesting (Sonar), records them
	// too, CCN-less.
	Sites []Site `json:"sites,omitempty"`
	// Candidates are the unit's extraction candidates (#2948,
	// candidates.go): the nesting-raising nodes whose extraction into a
	// function of their own would save the most cognitive complexity — at
	// most MaxCandidates, each saving at least MinCandidateSaving, best
	// first. A nested unit is its own function, never a candidate.
	Candidates []Candidate `json:"candidates,omitempty"`
	// Baseline is the unit's status against the accepted-debt baseline
	// (#2958, baseline.go), left by Baseline.Stamp: new until a baseline
	// classifies it. Not a measurement, so never serialised here — the
	// CLI spells it out as its own field.
	Baseline Status `json:"-"`
}

// Site is one increment of a unit's CCN or cognitive complexity (#2946):
// where it sits, what it is and what it costs.
type Site struct {
	// Line / Col are the 0-based position of the node's first token (a
	// boolean run: its first operator); EndLine the node's last line.
	Line    int `json:"line"`
	Col     int `json:"col"`
	EndLine int `json:"endLine"`
	// Kind is the node kind as the language spells it — "if", "else if",
	// "elif", "else", "for", "switch", "case", "catch", "goto", "?:" for a
	// conditional expression — a boolean run's operator ("&&", "and") or
	// "recursion".
	Kind string `json:"kind"`
	// CCN is the cyclomatic increment: 1 for a branch or an arm, the
	// operator count of a boolean run, 0 otherwise (and always 0 in the
	// enclosing unit's copy of a nested unit's site).
	CCN int `json:"ccn"`
	// Cognitive is the whole cognitive increment; Nesting the part of it
	// that is the nesting penalty (0 for a flat increment); Level the
	// nesting level the site sits at.
	Cognitive int `json:"cognitive"`
	Nesting   int `json:"nesting"`
	Level     int `json:"level"`
}

// DensityPercent is d rounded to a whole percent — the unit the UI, the CLI
// and the complexity.comment_density_warn threshold use.
func DensityPercent(d float64) int { return int(d*100 + 0.5) }

// CommentsOver reports whether f's comment density is at or above warn
// percent; a non-positive warn disables the check.
func (f Function) CommentsOver(warn int) bool {
	return warn > 0 && DensityPercent(f.CommentDensity) >= warn
}

// Excess is how far f sits above the warning thresholds (#2945):
// max(0, CCN − ccnWarn) + max(0, Cognitive − cogWarn). A unit at a
// threshold counts as over it but has no excess; a non-positive threshold
// disables its term. The thresholds are parameters, not globals, so a
// per-file threshold can feed them.
func (f Function) Excess(ccnWarn, cogWarn int) int {
	return above(f.CCN, ccnWarn) + above(f.Cognitive, cogWarn)
}

// above is max(0, v − warn), 0 for a disabled (non-positive) warn.
func above(v, warn int) int {
	if warn <= 0 || v <= warn {
		return 0
	}
	return v - warn
}

// syntaxTree is the parse seam — a variable so a test can hand Measure a nil
// tree (the !cgo shape) in a cgo build.
var syntaxTree = highlight.SyntaxTree

// Measure parses lines as language langID and returns every function unit in
// source order. It is a full parse of the text, so callers run it off the
// Update goroutine. ErrUnsupported when the language has no spec or no tree.
func Measure(langID string, lines []string) ([]Function, error) {
	l, ok := lang.ByID(langID)
	if !ok || l.Complexity == nil {
		return nil, ErrUnsupported
	}
	root := syntaxTree(langID, lines)
	if root == nil {
		return nil, ErrUnsupported
	}
	return measure(root, lines, l.Complexity, comments{line: l.LineComment, block: l.BlockComment}), nil
}

// Summary is the aggregate of a list of functions — a file, or a project.
type Summary struct {
	Functions     int     `json:"functions"`
	NLOC          int     `json:"nloc"`
	CCNSum        int     `json:"ccnSum"`
	CCNAvg        float64 `json:"ccnAvg"`
	CCNMax        int     `json:"ccnMax"`
	CogSum        int     `json:"cogSum"`
	CogAvg        float64 `json:"cogAvg"`
	CogMax        int     `json:"cogMax"`
	OverThreshold int     `json:"overThreshold"` // functions at or above any warning threshold
	// Excess sums Function.Excess over the population (#2945): the mass
	// above the CCN and cognitive thresholds, the primary optimisation
	// signal and what the diff verdict reads first.
	Excess int `json:"excess"`
	// Accepted counts the units over a CCN or cognitive threshold that the
	// baseline accepts (#2958): they are left out of OverThreshold and
	// Excess. Worsened counts the recorded units that grew past their
	// recorded metrics; they are in OverThreshold.
	Accepted int `json:"accepted"`
	Worsened int `json:"worsened"`
	// CommentLines sums the functions' comment-only lines; CommentDensity is
	// CommentLines / (NLOC + CommentLines) over the aggregate (#2934).
	CommentLines   int     `json:"commentLines"`
	CommentDensity float64 `json:"commentDensity"`
}

// Summarize aggregates fns. A function is over threshold when its CCN is at
// or above ccnWarn, its cognitive complexity at or above cogWarn or its
// comment density at or above cmtWarn percent; a non-positive threshold
// disables that metric's check. A unit the baseline accepts
// (Function.Baseline, #2958) is neither over threshold on CCN / cognitive
// nor counted in Excess — it is counted in Accepted instead; the comment
// density check is not part of the baseline. Excess sums Function.Excess
// with the CCN and cognitive thresholds. Averages are 0 for an empty list.
func Summarize(fns []Function, ccnWarn, cogWarn, cmtWarn int) Summary {
	var s Summary
	for _, f := range fns {
		s.Functions++
		s.NLOC += f.NLOC
		s.CCNSum += f.CCN
		s.CogSum += f.Cognitive
		s.CommentLines += f.CommentLines
		accepted := f.Baseline == BaselineAccepted
		if !accepted {
			s.Excess += f.Excess(ccnWarn, cogWarn)
		}
		if f.CCN > s.CCNMax {
			s.CCNMax = f.CCN
		}
		if f.Cognitive > s.CogMax {
			s.CogMax = f.Cognitive
		}
		over := f.Over(ccnWarn, cogWarn)
		switch {
		case over && accepted:
			s.Accepted++
		case over && f.Baseline == BaselineWorsened:
			s.Worsened++
		}
		if over && !accepted || f.CommentsOver(cmtWarn) {
			s.OverThreshold++
		}
	}
	if s.Functions > 0 {
		s.CCNAvg = float64(s.CCNSum) / float64(s.Functions)
		s.CogAvg = float64(s.CogSum) / float64(s.Functions)
	}
	s.CommentDensity = density(s.NLOC, s.CommentLines)
	return s
}
