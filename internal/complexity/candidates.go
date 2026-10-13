package complexity

import (
	"sort"

	"ike/internal/highlight"
)

// candidates.go computes the extraction candidates of a unit (#2948): for
// every nesting-raising node — a Branch, a Selector, an else body — what
// the file would save in cognitive complexity if that node were extracted
// into a function of its own, and the metrics the two halves would have.
//
// The arithmetic follows from the cognitive rules alone. A structural node
// (an `if`, a `for`, a `switch`, a nested `?:`) at level L costs `1 + L`;
// extracted whole, header included, it sits at level 0 of the new unit and
// costs 1, and every site inside it keeps its flat part and loses L of its
// nesting penalty. An else body (a flat node: `else`, shell `elif`) cannot
// take its keyword along — the `else { helper() }` stays — so its own flat
// point stays with the rest and the body's sites, which sat at L + 1, lose
// L + 1. An else-if is part of its chain and is no candidate of its own:
// the chain head's candidate covers it. A nested unit is already its own
// function, so neither it nor the candidates inside it belong to the
// enclosing unit.
//
// The sums are O(sites) per unit: unitState keeps running totals of the
// sites appended so far (CCN, cognitive, and how many carry a nesting
// penalty), a candidate snapshots them when its node is entered and reads
// the difference when it is left. Every structural site inside a candidate
// at level L sits at level ≥ L + 1 with a penalty equal to its level, so
// "loses drop, never below 0" is `drop` for each of those and 0 for a flat
// site — the count is all the sum needs (TestCandidateInvariants recomputes
// it site by site over the goldens).

// Candidate is one extraction proposal of a unit (#2948): the node to
// extract and what the two resulting functions would measure.
type Candidate struct {
	// Line / EndLine are the node's 0-based range, header line to last line.
	Line    int `json:"line"`
	EndLine int `json:"endLine"`
	// Kind is the node's site kind: "if", "for", "switch", "else", …
	Kind string `json:"kind"`
	// CogSaving is the cognitive complexity the file loses: the enclosing
	// unit's loss minus the new unit's cost.
	CogSaving int `json:"cogSaving"`
	// NewCCN / NewCog are the metrics the extracted function would have.
	NewCCN int `json:"newCCN"`
	NewCog int `json:"newCog"`
	// RestCCN / RestCog are the metrics the enclosing unit would keep.
	// RestCCN + NewCCN == CCN + 1 and RestCog + NewCog == Cognitive −
	// CogSaving always hold.
	RestCCN int `json:"restCCN"`
	RestCog int `json:"restCog"`
}

// MinCandidateSaving is the smallest CogSaving a candidate is reported
// with; MaxCandidates how many a unit keeps (the best by saving, ties by
// range).
const (
	MinCandidateSaving = 3
	MaxCandidates      = 3
)

// The filter in effect: the constants, loosened by a test to see every
// candidate of a unit.
var candidateMinSaving, candidateMax = MinCandidateSaving, MaxCandidates

// candOpen is a candidate whose node the walk is inside.
type candOpen struct {
	u     *unitState
	c     Candidate
	own   Site // the node's own site (zero for a body candidate)
	body  bool // a flat node: the body is extracted, the node's point stays
	level int  // the nesting level the node sits at
	// The unit's running sums when the node was entered, own site included.
	ccn, cog, nested int
}

// openCandidate starts a candidate for node c of the innermost unit, whose
// site was just recorded; body marks an else body, whose own flat point is
// not part of the extraction. nil outside every unit.
func (m *measurer) openCandidate(c *highlight.SyntaxNode, body bool) *candOpen {
	if len(m.units) == 0 {
		return nil
	}
	u := m.units[len(m.units)-1]
	o := &candOpen{u: u, body: body, level: u.nesting, ccn: u.sumCCN, cog: u.sumCog, nested: u.sumNested}
	o.c = Candidate{Line: c.StartLine, EndLine: c.EndLine, Kind: "else"}
	if !body {
		if len(u.sites) == 0 {
			return nil
		}
		o.own = u.sites[len(u.sites)-1]
		o.c.Kind = o.own.Kind
	}
	return o
}

// closeCandidate reads the sites recorded inside the node and settles the
// saving and the new unit's metrics; the rest is settled by finishCandidates
// once the unit's totals are known.
func (m *measurer) closeCandidate(o *candOpen) {
	u := o.u
	inCCN, inCog, inNested := u.sumCCN-o.ccn, u.sumCog-o.cog, u.sumNested-o.nested
	c := o.c
	if o.body {
		drop := o.level + 1
		c.CogSaving = drop * inNested
		c.NewCCN = 1 + inCCN
		c.NewCog = inCog - drop*inNested
	} else {
		drop := o.own.Nesting
		c.CogSaving = drop + drop*inNested
		c.NewCCN = 1 + o.own.CCN + inCCN
		c.NewCog = 1 + inCog - drop*inNested
	}
	u.cands = append(u.cands, c)
}

// finishCandidates completes the rest metrics against the unit's totals
// and keeps the best MaxCandidates with a saving of at least
// MinCandidateSaving — saving descending, then range ascending.
func finishCandidates(cands []Candidate, ccn, cog int) []Candidate {
	var out []Candidate
	for _, c := range cands {
		if c.CogSaving < candidateMinSaving {
			continue
		}
		c.RestCCN = ccn - (c.NewCCN - 1)
		c.RestCog = cog - c.CogSaving - c.NewCog
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.CogSaving != b.CogSaving {
			return a.CogSaving > b.CogSaving
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.EndLine < b.EndLine
	})
	if len(out) > candidateMax {
		out = out[:candidateMax]
	}
	return out
}
