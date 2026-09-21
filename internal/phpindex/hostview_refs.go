package phpindex

// hostview_refs.go is the references half of the host-facing view (Epic
// 0520, #2671): the LSP bridge's find-usages merge asks it, after the server
// answered, which index-derived usages that answer is missing. The gates
// mirror hostview.go — PHP buffer, php.trait_index on, a class-like body
// around the position — with one more: an *empty* server answer is only
// complemented inside a trait body, where the server is known to be blind.
// Everywhere else an empty answer means what it says.

import (
	"strings"

	"ike/internal/host"
)

// SetReferencesTelemetry installs the callback for a find-usages answer the
// index complemented (#2671): the server's location count and how many rows
// the index added. Called off the UI goroutine, from TraitReferencesAt, only
// when the index contributed. Pass nil to remove it.
func (v *HostView) SetReferencesTelemetry(fn func(server, index int)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.reportRefs = fn
}

// TraitReferencesAt implements host.TraitIndex: the gates, MembersAt for the
// member the position names, then References per declaration, merged and
// filtered by what the server answer is missing. A highlight lookup is
// restricted to the file and recorded nowhere.
func (v *HostView) TraitReferencesAt(op host.TraitLookup, path string, lines []string, line, col, serverHits int) []host.TraitReference {
	if v == nil || v.idx == nil || path == "" || phpOnly(path) != "php" {
		return nil
	}
	if !v.idx.Options().Enabled {
		return nil // php.trait_index = false
	}
	pos := Pos{Line: line, Col: col}
	scope, ok := v.idx.ScopeAt(path, pos)
	if !ok {
		return nil
	}
	if serverHits == 0 && !scope.IsTrait() {
		// Outside a trait body an empty server answer means what it says.
		return nil
	}
	members := v.idx.MembersAt(path, strings.Join(lines, "\n"), pos)
	if len(members) == 0 {
		return nil
	}
	seen := map[host.TraitReference]bool{}
	var out []host.TraitReference
	for _, m := range members {
		var locs []Location
		if op == host.TraitHighlight {
			locs = v.idx.ReferencesIn(m, path)
		} else {
			locs = v.idx.References(m)
		}
		for _, l := range locs {
			if serverHits > 0 && !l.InTrait {
				// The server reported the consumer side itself; only the
				// rows inside trait bodies are its blind spot.
				continue
			}
			r := host.TraitReference{Path: l.Path, Line: l.Range.Start.Line, Col: l.Range.Start.Col, Decl: l.Decl}
			if seen[r] {
				continue
			}
			seen[r] = true
			out = append(out, r)
		}
	}
	if len(out) > 0 && op == host.TraitReferences {
		v.mu.RLock()
		report := v.reportRefs
		v.mu.RUnlock()
		if report != nil {
			report(serverHits, len(out))
		}
	}
	return out
}
