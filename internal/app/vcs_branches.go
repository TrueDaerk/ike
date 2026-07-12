package app

import (
	"sort"

	"ike/internal/fuzzy"
	"ike/internal/palette"
	"ike/internal/vcs"
)

// vcs_branches.go is the branch picker behind vcs.branches (Roadmap 0320,
// #467): the palette opens locked to a mode listing the local branches, the
// selection dispatches the checkout. The branch list is fetched fresh on
// every open and parked on the shared vcs state for the mode's getter.

// branchesPrefix selects the branch mode inside the palette; the root model
// only opens it locked, so the rune has no user-facing prefix story.
const branchesPrefix = '+'

// OpenBranchPickerMsg starts the vcs.branches flow.
type OpenBranchPickerMsg struct{}

// CheckoutBranchMsg is emitted when a picker item is activated.
type CheckoutBranchMsg struct{ Name string }

// branchMode is the palette Mode listing local branches; list is injected so
// the mode reads the shared vcs state without holding the model.
type branchMode struct {
	list func() []vcs.Branch
}

func newBranchMode(list func() []vcs.Branch) *branchMode { return &branchMode{list: list} }

// Prefix implements palette.Mode.
func (b *branchMode) Prefix() rune { return branchesPrefix }

// Placeholder implements palette.Mode.
func (b *branchMode) Placeholder() string { return "Switch branch…" }

// Results implements palette.Mode: branches fuzzy-matched on name, the
// current branch marked and ranked first on an empty query.
func (b *branchMode) Results(query string, _ palette.Context) []palette.Item {
	type scored struct {
		branch vcs.Branch
		score  int
		spans  []int
	}
	var out []scored
	for _, br := range b.list() {
		if r, ok := fuzzy.Match(query, br.Name); ok {
			s := scored{branch: br, score: r.Score, spans: r.Positions}
			if br.Current {
				s.score++ // current wins ties, tops the empty query
			}
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	items := make([]palette.Item, 0, len(out))
	for _, s := range out {
		detail := ""
		if s.branch.Current {
			detail = "current"
		}
		items = append(items, palette.Item{
			Title:  s.branch.Name,
			Detail: detail,
			Spans:  s.spans,
			Score:  s.score,
			Msg:    CheckoutBranchMsg{Name: s.branch.Name},
		})
	}
	return items
}
