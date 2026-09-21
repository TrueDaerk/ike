package phpindex

// hostview.go adapts the index to the host's trait-index seam (Epic 0520,
// #2670): the read-only view the LSP bridge consults as a navigation and
// hover *fallback* inside trait bodies, after the server answered empty or
// could not be asked at all.
//
// The view is where the epic's PHP knowledge is gathered, so nothing of it
// leaks into the plugin: the buffer must be PHP, php.trait_index must be on,
// the position must sit inside a trait body (ScopeAt + IsTrait) and the
// syntax tree must name a member of `$this` / `self` / `static` there
// (MemberAccessAt). Only then does LookupAccess run. Every gate failing
// answers the same way — nothing — so the bridge keeps the status message it
// always had, and a build without the PHP grammar (no cgo, no tree) makes
// the whole fallback inert.
//
// It lives beside the index rather than in the app because both the app's
// wiring and the bridge's own tests want the real adapter; only the
// telemetry callback is the app's business (SetTelemetry, the #2668 shape).

import (
	"strings"
	"sync"

	"ike/internal/host"
)

// HostView implements host.TraitIndex over an Index. A nil index answers
// nothing, so a caller never needs to special-case a project without one.
type HostView struct {
	idx *Index

	mu sync.RWMutex
	// report is called with the feature and the declaration count of every
	// non-empty answer (telemetry ops php.trait.definition / .hover); nil
	// until SetTelemetry.
	report func(op host.TraitLookup, n int)
	// reportRefs is called with the server and index counts of every
	// find-usages answer the index complemented (telemetry op
	// php.trait.references, #2671); nil until SetReferencesTelemetry.
	reportRefs func(server, index int)
	// reportRename is called with the side and the index's edit count of
	// every applied rename the index took part in (telemetry op
	// php.trait.rename, #2672); nil until SetRenameTelemetry.
	reportRename func(side host.TraitRenameSide, edits int)
}

// NewHostView returns the host-facing view of the index.
func NewHostView(idx *Index) *HostView { return &HostView{idx: idx} }

// SetTelemetry installs the callback for a non-empty answer (#2670). It is
// called off the UI goroutine, from TraitMembersAt. Pass nil to remove it.
func (v *HostView) SetTelemetry(fn func(op host.TraitLookup, n int)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.report = fn
}

// TraitMembersAt implements host.TraitIndex.
func (v *HostView) TraitMembersAt(op host.TraitLookup, path string, lines []string, line, col int) []host.TraitMember {
	members := v.lookup(path, lines, line, col)
	if len(members) == 0 {
		return nil
	}
	out := make([]host.TraitMember, 0, len(members))
	for _, m := range members {
		out = append(out, v.member(m))
	}
	v.mu.RLock()
	report := v.report
	v.mu.RUnlock()
	if report != nil {
		report(op, len(out))
	}
	return out
}

// lookup runs the gates, cheapest first, and the index query.
func (v *HostView) lookup(path string, lines []string, line, col int) []Member {
	if v == nil || v.idx == nil || path == "" || phpOnly(path) != "php" {
		return nil
	}
	if !v.idx.Options().Enabled {
		return nil // php.trait_index = false
	}
	pos := Pos{Line: line, Col: col}
	scope, ok := v.idx.ScopeAt(path, pos)
	if !ok || !scope.IsTrait() {
		return nil // only a trait body has a consumer scope
	}
	access, ok := MemberAccessAt(strings.Join(lines, "\n"), pos)
	if !ok {
		return nil
	}
	return v.idx.LookupAccess(scope.FQN, access)
}

// member renders one member as the host's flat view, resolving the declaring
// type's kind so a hover card can say "class B".
func (v *HostView) member(m Member) host.TraitMember {
	out := host.TraitMember{
		Name:      m.Name,
		Signature: m.Signature(),
		Doc:       m.Doc,
		Declaring: m.Declaring,
		DeclName:  shortName(m.Declaring),
		Path:      m.Path,
		Line:      m.NameRange.Start.Line,
		Col:       m.NameRange.Start.Col,
	}
	for _, d := range v.idx.DeclarationsNamed(m.Declaring) {
		if d.FQN == m.Declaring {
			out.DeclKind = d.Kind.String()
			break
		}
	}
	return out
}
