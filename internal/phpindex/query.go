package phpindex

// query.go is the pure query side (#2667): a snapshot derived once per
// content generation from every file's extraction, holding the declaration
// tables and the reverse trait-use edges, and the consumer-scope queries
// the epic's features ask on it. Every method takes the snapshot the index
// holds at call time; results are copies and safe to keep.

import (
	"sort"
	"strings"
)

// snapshot is the derived view of one content generation.
type snapshot struct {
	depth   int
	decls   []*Decl
	byFQN   map[string][]*Decl // lower-cased FQN
	byShort map[string][]*Decl // lower-cased short name
	files   map[string][]*Decl // path → declarations, in file order
	// users maps a trait's key (lower-cased FQN) to the keys of the
	// declarations using it directly — classes, enums and traits.
	users map[string][]string
	// refs maps a declaration key to the files referring to it.
	refs  map[string][]string
	edges int
}

func key(fqn string) string { return strings.ToLower(strings.TrimPrefix(fqn, "\\")) }

// buildSnapshot derives the tables from the per-file extractions.
func buildSnapshot(files map[string]fileDecls, depth int) *snapshot {
	s := &snapshot{
		depth:   depth,
		byFQN:   map[string][]*Decl{},
		byShort: map[string][]*Decl{},
		files:   map[string][]*Decl{},
		users:   map[string][]string{},
		refs:    map[string][]string{},
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		fd := files[p]
		for i := range fd.Decls {
			d := &fd.Decls[i]
			s.decls = append(s.decls, d)
			s.byFQN[key(d.FQN)] = append(s.byFQN[key(d.FQN)], d)
			s.byShort[strings.ToLower(d.Name)] = append(s.byShort[strings.ToLower(d.Name)], d)
			s.files[p] = append(s.files[p], d)
		}
	}
	// Edges resolve against the declaration tables, so a written name with
	// no declaration falls back to every declaration of its short name.
	for _, p := range paths {
		fd := files[p]
		seen := map[string]bool{}
		for _, r := range fd.Refs {
			for _, t := range s.targets(r) {
				if !seen[t] {
					seen[t] = true
					s.refs[t] = append(s.refs[t], p)
				}
			}
		}
		for i := range fd.Decls {
			d := &fd.Decls[i]
			for _, t := range d.Traits {
				for _, tk := range s.targets(t) {
					s.users[tk] = append(s.users[tk], key(d.FQN))
					s.edges++
				}
			}
			for _, pn := range d.Extends {
				s.edges += len(s.targets(pn))
			}
			for _, in := range d.Implements {
				s.edges += len(s.targets(in))
			}
		}
	}
	return s
}

// targets resolves a written or resolved name to declaration keys: the
// exact FQN when declared, else every declaration sharing its short name
// (legacy code without namespaces or imports). Nothing when unknown.
func (s *snapshot) targets(name string) []string {
	k := key(name)
	if len(s.byFQN[k]) > 0 {
		return []string{k}
	}
	short := strings.ToLower(shortName(name))
	ds := s.byShort[short]
	if len(ds) == 0 {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, d := range ds {
		if dk := key(d.FQN); !seen[dk] {
			seen[dk] = true
			out = append(out, dk)
		}
	}
	return out
}

// declsOf returns the declarations under a key.
func (s *snapshot) declsOf(k string) []*Decl { return s.byFQN[k] }

// consumers walks the reverse trait-use edges from the trait's keys,
// breadth-first with a visited set, so a trait cycle terminates. The start
// keys themselves are excluded from the result.
func (s *snapshot) consumers(traitFQN string) []string {
	start := s.targets(traitFQN)
	visited := map[string]bool{}
	for _, k := range start {
		visited[k] = true
	}
	var out []string
	queue := append([]string(nil), start...)
	for len(queue) > 0 {
		k := queue[0]
		queue = queue[1:]
		for _, u := range s.users[k] {
			if visited[u] {
				continue
			}
			visited[u] = true
			out = append(out, u)
			queue = append(queue, u)
		}
	}
	return out
}

// traitClosure is every trait a declaration uses, transitively.
func (s *snapshot) traitClosure(k string, visited map[string]bool) []string {
	var out []string
	for _, d := range s.declsOf(k) {
		for _, t := range d.Traits {
			for _, tk := range s.targets(t) {
				if visited[tk] {
					continue
				}
				visited[tk] = true
				out = append(out, tk)
				out = append(out, s.traitClosure(tk, visited)...)
			}
		}
	}
	return out
}

// parentChain follows extends from k up to depth parents; a cycle or an
// undeclared parent stops it.
func (s *snapshot) parentChain(k string, depth int) []string {
	visited := map[string]bool{k: true}
	var out []string
	cur := k
	for i := 0; i < depth; i++ {
		ds := s.declsOf(cur)
		if len(ds) == 0 || len(ds[0].Extends) == 0 {
			break
		}
		next := ""
		for _, t := range s.targets(ds[0].Extends[0]) {
			if !visited[t] {
				next = t
				break
			}
		}
		if next == "" {
			break
		}
		visited[next] = true
		out = append(out, next)
		cur = next
	}
	return out
}

// scopeKeys lists the declaration keys of a trait's consumer scope in
// precedence order: consumers (nearest first), the trait itself, sibling
// traits, then the consumers' parent chains.
func (s *snapshot) scopeKeys(traitFQN string) []string {
	self := s.targets(traitFQN)
	cons := s.consumers(traitFQN)
	inScope := map[string]bool{}
	var out []string
	add := func(k string) {
		if !inScope[k] {
			inScope[k] = true
			out = append(out, k)
		}
	}
	for _, k := range cons {
		add(k)
	}
	for _, k := range self {
		add(k)
	}
	for _, k := range cons {
		for _, t := range s.traitClosure(k, map[string]bool{}) {
			add(t)
		}
	}
	for _, k := range cons {
		for _, p := range s.parentChain(k, s.depth) {
			add(p)
			for _, t := range s.traitClosure(p, map[string]bool{}) {
				add(t)
			}
		}
	}
	return out
}

// membersOf lists the members of every declaration under the keys, in order.
func (s *snapshot) membersOf(keys []string) []Member {
	var out []Member
	for _, k := range keys {
		for _, d := range s.declsOf(k) {
			out = append(out, d.Members...)
		}
	}
	return out
}

// --- public query API ---

// ScopeAt returns the innermost class-like declaration around pos in path
// (an indexed file or an observed buffer), and whether there is one. The
// declaration's IsTrait tells the trait features whether they apply.
func (x *Index) ScopeAt(path string, pos Pos) (Decl, bool) {
	s := x.snapshot()
	if s == nil {
		return Decl{}, false
	}
	var best *Decl
	for _, d := range s.files[cleanPath(path)] {
		if !d.Range.Contains(pos) {
			continue
		}
		if best == nil || d.Range.Start.Line > best.Range.Start.Line ||
			(d.Range.Start.Line == best.Range.Start.Line && d.Range.Start.Col >= best.Range.Start.Col) {
			best = d
		}
	}
	if best == nil {
		return Decl{}, false
	}
	return *best, true
}

// ConsumersOf lists the FQNs of every declaration using the trait, directly
// or through other traits; cycle-safe, sorted.
func (x *Index) ConsumersOf(traitFQN string) []string {
	s := x.snapshot()
	if s == nil {
		return nil
	}
	return s.fqns(s.consumers(traitFQN))
}

// SiblingTraitsOf lists the other traits the trait's consumers use — the
// members a `$this` inside the trait also sees — sorted.
func (x *Index) SiblingTraitsOf(traitFQN string) []string {
	s := x.snapshot()
	if s == nil {
		return nil
	}
	self := map[string]bool{}
	for _, k := range s.targets(traitFQN) {
		self[k] = true
	}
	cons := s.consumers(traitFQN)
	consumer := map[string]bool{}
	for _, k := range cons {
		consumer[k] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, k := range cons {
		for _, t := range s.traitClosure(k, map[string]bool{}) {
			if !self[t] && !consumer[t] && !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return s.fqns(out)
}

// ParentChain lists the FQNs of the class's parents, nearest first, up to
// depth (php.index.parent_depth when depth < 0); stops at an undeclared
// parent or a cycle.
func (x *Index) ParentChain(classFQN string, depth int) []string {
	s := x.snapshot()
	if s == nil {
		return nil
	}
	if depth < 0 {
		depth = s.depth
	}
	var out []string
	for _, k := range s.targets(classFQN) {
		out = append(out, s.parentChain(k, depth)...)
		break
	}
	return s.fqnsOrdered(out)
}

// VisibleMembers is the trait's consumer scope: the union of its consumers'
// members, its own, its sibling traits' and the consumers' parent chains'
// (to php.index.parent_depth), deduplicated by (kind, name) with the
// nearest declaration winning — a consumer's own method over the trait's,
// the trait's over a parent's, mirroring PHP's precedence.
func (x *Index) VisibleMembers(traitFQN string) []Member {
	s := x.snapshot()
	if s == nil {
		return nil
	}
	type mk struct {
		kind MemberKind
		name string
	}
	seen := map[mk]bool{}
	var out []Member
	for _, m := range s.membersOf(s.scopeKeys(traitFQN)) {
		k := mk{m.Kind, m.Name}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, m)
	}
	return out
}

// Lookup returns every declaration of a member in the trait's consumer
// scope, nearest first — several when consumers declare it independently.
// A property name matches with or without its `$`.
func (x *Index) Lookup(traitFQN string, kind MemberKind, name string) []Member {
	s := x.snapshot()
	if s == nil {
		return nil
	}
	if kind == MemberProperty && !strings.HasPrefix(name, "$") {
		name = "$" + name
	}
	var out []Member
	for _, m := range s.membersOf(s.scopeKeys(traitFQN)) {
		if m.Kind == kind && m.Name == name {
			out = append(out, m)
		}
	}
	return out
}

// LookupAccess is Lookup for a member access resolved from the syntax tree
// (#2670): the access carries the declaration kinds a member of that shape
// may have, and the first kind that resolves wins — `self::K` finds the
// constant `K` or, failing that, the enum case `K`.
func (x *Index) LookupAccess(traitFQN string, a Access) []Member {
	for _, k := range a.Kinds {
		if ms := x.Lookup(traitFQN, k, a.Name); len(ms) > 0 {
			return ms
		}
	}
	return nil
}

// DeclarationsNamed lists the declarations matching name: a qualified name
// exactly, a bare name by short name; sorted by path.
func (x *Index) DeclarationsNamed(name string) []Decl {
	s := x.snapshot()
	if s == nil {
		return nil
	}
	var ds []*Decl
	if strings.Contains(name, "\\") {
		ds = s.byFQN[key(name)]
	} else {
		ds = s.byShort[strings.ToLower(name)]
	}
	out := make([]Decl, 0, len(ds))
	for _, d := range ds {
		out = append(out, *d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// FilesReferencing lists the files that import, extend, implement or use
// the declaration, sorted. The declaring file is included only when it
// refers to the name itself.
func (x *Index) FilesReferencing(fqn string) []string {
	s := x.snapshot()
	if s == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, k := range s.targets(fqn) {
		for _, p := range s.refs[k] {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// fqns maps declaration keys to their display FQNs, sorted.
func (s *snapshot) fqns(keys []string) []string {
	out := s.fqnsOrdered(keys)
	sort.Strings(out)
	return out
}

// fqnsOrdered maps declaration keys to their display FQNs, keeping order.
func (s *snapshot) fqnsOrdered(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if ds := s.declsOf(k); len(ds) > 0 {
			out = append(out, ds[0].FQN)
		}
	}
	return out
}
