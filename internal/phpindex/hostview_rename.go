package phpindex

// hostview_rename.go is the rename half of the host-facing view (Epic 0520,
// #2672). Rename around traits is unsafe or impossible with the server
// alone: renaming `abc()` on class B leaves `$this->abc()` inside the traits
// B consumes untouched, and a rename started inside a trait body on such a
// member is refused because the server cannot resolve it. The view turns the
// reference scanner (#2671) into a rename *plan* the bridge applies through
// its multi-file edit funnel — the identifiers to rewrite, or the
// declarations that make the target ambiguous.
//
// The gates mirror hostview_refs.go: PHP buffer, php.trait_index on, a
// class-like body around the position. The side decides the rest: the
// extend side (the server accepted the rename) contributes only the rows
// inside trait bodies, which the server never edits; the index side (the
// server refused) answers only inside a trait body and contributes the
// member's whole scope.

import (
	"sort"
	"strings"

	"ike/internal/host"
)

// SetRenameTelemetry installs the callback for a rename the index took
// part in (#2672): the side and how many identifiers the index rewrote.
// Called off the UI goroutine, from TraitRenameApplied. Pass nil to remove
// it.
func (v *HostView) SetRenameTelemetry(fn func(side host.TraitRenameSide, edits int)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.reportRename = fn
}

// TraitRenameApplied implements host.TraitRenamer: it forwards the applied
// rename to the telemetry callback, skipping a rename the index added
// nothing to.
func (v *HostView) TraitRenameApplied(side host.TraitRenameSide, edits int) {
	if v == nil || edits <= 0 {
		return
	}
	v.mu.RLock()
	report := v.reportRename
	v.mu.RUnlock()
	if report != nil {
		report(side, edits)
	}
}

// TraitRenameAt implements host.TraitRenamer: the gates, MembersAt for the
// member the position names, the ambiguity check on the index side, then
// References per declaration filtered to what the side needs.
func (v *HostView) TraitRenameAt(side host.TraitRenameSide, path string, lines []string, line, col int) (host.TraitRenamePlan, bool) {
	var plan host.TraitRenamePlan
	if v == nil || v.idx == nil || path == "" || phpOnly(path) != "php" {
		return plan, false
	}
	if !v.idx.Options().Enabled {
		return plan, false // php.trait_index = false
	}
	pos := Pos{Line: line, Col: col}
	scope, ok := v.idx.ScopeAt(path, pos)
	if !ok {
		return plan, false
	}
	if side == host.TraitRenameIndex && !scope.IsTrait() {
		// Outside a trait body a refused rename means what it says.
		return plan, false
	}
	members := v.idx.MembersAt(path, strings.Join(lines, "\n"), pos)
	if len(members) == 0 {
		return plan, false
	}
	plan.OldName = bareName(members[0].Name)
	if side == host.TraitRenameIndex {
		if amb := v.ambiguousDeclarations(members); len(amb) > 0 {
			for _, m := range amb {
				plan.Ambiguous = append(plan.Ambiguous, v.member(m))
			}
			return plan, true
		}
	}
	type editKey struct {
		path      string
		line, col int
	}
	texts := map[string][]string{}
	seen := map[editKey]bool{}
	for _, m := range members {
		for _, l := range v.idx.References(m) {
			if side == host.TraitRenameExtend && !l.InTrait {
				continue // the server edits the consumer side itself
			}
			if l.Range.Start.Line != l.Range.End.Line {
				continue // an identifier never spans lines
			}
			fl, ok := texts[l.Path]
			if !ok {
				text, found := v.idx.fileText(l.Path)
				if found {
					fl = strings.Split(text, "\n")
				}
				texts[l.Path] = fl
			}
			text := identifierAt(fl, l.Range)
			if bareName(text) != plan.OldName {
				// A trait-use alias (`use X { abc as other; }`) keeps its
				// own name: only the member's own spelling is renamed.
				continue
			}
			k := editKey{l.Path, l.Range.Start.Line, l.Range.Start.Col}
			if seen[k] {
				continue
			}
			seen[k] = true
			plan.Edits = append(plan.Edits, host.TraitRenameEdit{
				Path:      l.Path,
				Line:      l.Range.Start.Line,
				Col:       l.Range.Start.Col,
				EndCol:    l.Range.End.Col,
				Text:      text,
				Declaring: l.Declaring,
				DeclName:  shortName(l.Declaring),
				InTrait:   l.InTrait,
				Decl:      l.Decl,
			})
		}
	}
	sort.SliceStable(plan.Edits, func(i, j int) bool {
		a, b := plan.Edits[i], plan.Edits[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
	return plan, len(plan.Edits) > 0
}

// identifierAt returns the text of a single-line range in lines, "" when
// the range lies outside them.
func identifierAt(lines []string, r Range) string {
	if r.Start.Line < 0 || r.Start.Line >= len(lines) {
		return ""
	}
	runes := []rune(lines[r.Start.Line])
	start, end := r.Start.Col, r.End.Col
	if start < 0 || end > len(runes) || start >= end {
		return ""
	}
	return string(runes[start:end])
}

// ambiguousDeclarations reports the declarations that make an index-driven
// rename refuse: the member resolves to several declaring types that are
// neither related (one in the other's class scope — its parent chain or
// trait closure) nor defined alike (the same signature, as two consumers
// implementing one trait contract are). Two such declarations are different
// members that merely share a name; renaming both, or guessing one, would be
// wrong either way. Nothing when the target is unambiguous.
func (v *HostView) ambiguousDeclarations(members []Member) []Member {
	s := v.idx.snapshot()
	if s == nil || len(members) < 2 {
		return nil
	}
	first := map[string]Member{}
	var order []string
	for _, m := range members {
		if _, ok := first[m.Declaring]; !ok {
			first[m.Declaring] = m
			order = append(order, m.Declaring)
		}
	}
	if len(order) < 2 {
		return nil
	}
	related := func(a, b string) bool {
		for _, k := range s.classScopeKeys(key(a)) {
			if k == key(b) {
				return true
			}
		}
		for _, k := range s.classScopeKeys(key(b)) {
			if k == key(a) {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(order); i++ {
		for j := i + 1; j < len(order); j++ {
			a, b := first[order[i]], first[order[j]]
			if related(a.Declaring, b.Declaring) || a.Signature() == b.Signature() {
				continue
			}
			out := make([]Member, 0, len(order))
			for _, d := range order {
				out = append(out, first[d])
			}
			return out
		}
	}
	return nil
}
