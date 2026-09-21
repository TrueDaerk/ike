package phpindex

// references.go is the index-derived reference scanner (Epic 0520, #2671).
// Find usages is blind in both directions around traits: inside trait A the
// server cannot resolve `$this->abc()`, so it reports nothing; on `abc()`
// declared in class B it never reports the calls inside the traits B
// consumes. The scanner answers both from the index: it walks the files of
// the member's scope, parses each one and returns every access of the
// member's exact name on `$this` / `self` / `static` that sits inside a
// declaration belonging to that scope, plus the declaration itself.
//
// Matching is tree-based (the same access classification MemberAccessAt
// uses), never a text search — a same-named member on an unrelated class,
// a string literal or a comment mentioning the name never matches. An open
// buffer is scanned in its observed text, not its on-disk copy, so the
// answer matches what the editor shows.
//
// The scanner is independent from the LSP bridge: the rename issue reuses
// it directly to compute the edits the server cannot see.

import (
	"os"
	"sort"
	"strings"

	"ike/internal/highlight"
)

// Location is one reference the scanner found: the member identifier's
// range in editor coordinates, and where it sits.
type Location struct {
	Path  string
	Range Range
	// Declaring is the FQN of the class-like declaration the reference lies
	// in; InTrait says that declaration is a trait — the rows the consumer
	// side of find usages appends to a server answer.
	Declaring string
	InTrait   bool
	// Decl marks a declaration of the member (or an alias clause naming it)
	// rather than an access.
	Decl bool
}

// References lists every index-derived usage of the member (#2671): its
// declaration(s) and the `$this` / `self` / `static` accesses of its exact
// name inside the declarations of its scope. The scope is the declaring type,
// its traits transitively and its subclasses within php.index.parent_depth;
// for a trait member additionally every consumer and sibling trait. A
// trait-use alias (`use X { foo as bar; }`) counts under both names.
// Results are sorted by path and position and carry no duplicates.
func (x *Index) References(m Member) []Location {
	return x.references(m, nil)
}

// ReferencesIn is References restricted to one file — the document-highlight
// case, where only the current buffer's occurrences matter.
func (x *Index) ReferencesIn(m Member, path string) []Location {
	path = cleanPath(path)
	return x.references(m, func(p string) bool { return p == path })
}

func (x *Index) references(m Member, only func(string) bool) []Location {
	// Fold pending buffer edits first, so the declaration ranges the scan
	// maps positions against describe the text it parses.
	x.Flush()
	s := x.snapshot()
	if s == nil {
		return nil
	}
	scope := s.referenceScope(m)
	if len(scope.keys) == 0 {
		return nil
	}
	var out []Location
	for _, p := range scope.paths {
		if only != nil && !only(p) {
			continue
		}
		text, ok := x.fileText(p)
		if !ok {
			continue
		}
		out = append(out, scanFile(s, p, text, m, scope)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Range.Start.Line != out[j].Range.Start.Line {
			return out[i].Range.Start.Line < out[j].Range.Start.Line
		}
		return out[i].Range.Start.Col < out[j].Range.Start.Col
	})
	// Dedupe by (path, start): a declaration the tree walk found twice
	// (alias clause and alias member) collapses to one row.
	uniq := out[:0]
	for i, l := range out {
		if i > 0 && l.Path == out[i-1].Path && l.Range.Start == out[i-1].Range.Start {
			continue
		}
		uniq = append(uniq, l)
	}
	return uniq
}

// fileText returns the text the scanner parses for path: the observed
// buffer when the file is open, else the on-disk content.
func (x *Index) fileText(path string) (string, bool) {
	x.mu.Lock()
	d := x.buffers[path]
	x.mu.Unlock()
	if d != nil {
		return d.text, true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// refScope is the set of declarations whose bodies may reference the member,
// the files holding them, and the names the member goes by there.
type refScope struct {
	keys  map[string]bool
	paths []string
	// names are the bare member names to match (a property without its
	// `$`): the member's own plus every alias a consumer gave it, or the
	// aliased original when the member is itself an alias.
	names map[string]bool
}

// referenceScope derives the member's scope from the snapshot: the declaring
// type, its trait closure and its subclasses (to depth) with theirs; for a
// trait member the consumers, their trait closures and their subclasses too.
func (s *snapshot) referenceScope(m Member) refScope {
	sc := refScope{keys: map[string]bool{}, names: map[string]bool{bareName(m.Name): true}}
	self := key(m.Declaring)
	if len(s.declsOf(self)) == 0 {
		return sc
	}
	add := func(k string) {
		if sc.keys[k] {
			return
		}
		sc.keys[k] = true
		for _, t := range s.traitClosure(k, map[string]bool{}) {
			sc.keys[t] = true
		}
	}
	roots := []string{self}
	for _, d := range s.declsOf(self) {
		if d.IsTrait() {
			roots = append(roots, s.consumers(m.Declaring)...)
			break
		}
	}
	for _, r := range roots {
		add(r)
		for _, c := range s.subclasses(r, s.depth) {
			add(c)
		}
	}
	// Alias names: `use X { foo as bar; }` on a scope declaration makes
	// `bar` a name of X::foo — and an alias member's own usages include the
	// original it stands for.
	if m.Kind == MemberMethod {
		if m.AliasOf != "" {
			sc.names[aliasTarget(m.AliasOf)] = true
		}
		for k := range sc.keys {
			for _, d := range s.declsOf(k) {
				for _, a := range d.Members {
					if a.AliasOf != "" && aliasTarget(a.AliasOf) == m.Name {
						sc.names[a.Name] = true
					}
				}
			}
		}
	}
	seen := map[string]bool{}
	for k := range sc.keys {
		for _, d := range s.declsOf(k) {
			if !seen[d.Path] {
				seen[d.Path] = true
				sc.paths = append(sc.paths, d.Path)
			}
		}
	}
	sort.Strings(sc.paths)
	return sc
}

// subclasses walks the reverse extends edges from k, breadth-first to depth
// levels, cycle-safe; k itself is excluded.
func (s *snapshot) subclasses(k string, depth int) []string {
	visited := map[string]bool{k: true}
	var out []string
	level := []string{k}
	for i := 0; i < depth && len(level) > 0; i++ {
		var next []string
		for _, p := range level {
			for _, c := range s.children[p] {
				if visited[c] {
					continue
				}
				visited[c] = true
				out = append(out, c)
				next = append(next, c)
			}
		}
		level = next
	}
	return out
}

// aliasTarget is the method name an alias stands for: `foo` or `X::foo`.
func aliasTarget(target string) string {
	if i := strings.LastIndex(target, "::"); i >= 0 {
		return target[i+2:]
	}
	return target
}

// bareName strips a property's `$`, so `$this->x` and `self::$x` compare
// equal to the declared `$x`.
func bareName(name string) string { return strings.TrimPrefix(name, "$") }

// scanFile parses one file and collects the member's declarations and
// accesses inside the scope's declarations.
func scanFile(s *snapshot, path, text string, m Member, sc refScope) []Location {
	lines := strings.Split(text, "\n")
	root := highlight.SyntaxTree("php", lines)
	if root == nil {
		return nil
	}
	sf := &fileScan{s: s, path: path, src: strings.Join(lines, "\n"), m: m, sc: sc, decls: s.files[path]}
	sf.walk(root)
	return sf.out
}

type fileScan struct {
	s     *snapshot
	path  string
	src   string
	m     Member
	sc    refScope
	decls []*Decl
	out   []Location
}

// walk descends the whole tree except anonymous classes, whose `$this` is
// their own.
func (f *fileScan) walk(n *highlight.SyntaxNode) {
	for _, c := range n.Children {
		switch c.Kind {
		case "anonymous_class", "comment":
			continue
		case "method_declaration":
			if f.m.Kind == MemberMethod {
				f.declName(c.Child("name"))
			}
		case "property_element":
			if f.m.Kind == MemberProperty {
				f.declName(c.Child("name"))
			}
		case "property_promotion_parameter":
			if f.m.Kind == MemberProperty {
				f.declName(c.Child("name"))
			}
		case "const_element":
			if f.m.Kind == MemberConst {
				f.declName(c.ChildOfKind("name"))
			}
		case "enum_case":
			if f.m.Kind == MemberCase {
				f.declName(c.Child("name"))
			}
		case "use_as_clause":
			// `foo as bar` / `X::foo as bar`: every name of the clause that
			// is one of the member's names is a declaration-side reference.
			if f.m.Kind == MemberMethod {
				for _, part := range c.Children {
					switch part.Kind {
					case "name":
						f.declName(part)
					case "class_constant_access_expression":
						f.declName(lastOfKind(part, "name"))
					}
				}
			}
			continue
		default:
			if a, receiver, ok := accessOf(c, f.src); ok && receiver != nil && f.matches(a) {
				if name := accessName(c); name != nil {
					f.add(name, false)
				}
			}
		}
		f.walk(c)
	}
}

// declName records a declaration identifier when it names the member.
func (f *fileScan) declName(name *highlight.SyntaxNode) {
	if name == nil || !f.sc.names[bareName(name.Text(f.src))] {
		return
	}
	f.add(name, true)
}

// matches reports whether an access names the member with a compatible
// shape: a call for a method, an arrow or `::$` access for a property, a
// `::` access for a constant or an enum case.
func (f *fileScan) matches(a Access) bool {
	if !f.sc.names[bareName(a.Name)] || len(a.Kinds) == 0 {
		return false
	}
	switch f.m.Kind {
	case MemberConst, MemberCase:
		for _, k := range a.Kinds {
			if k == f.m.Kind {
				return true
			}
		}
		return false
	}
	return a.Kinds[0] == f.m.Kind
}

// accessName returns the member identifier node of an access node.
func accessName(n *highlight.SyntaxNode) *highlight.SyntaxNode {
	if n.Kind == "class_constant_access_expression" {
		return lastOfKind(n, "name")
	}
	return n.Child("name")
}

// add records the identifier when it sits inside a scope declaration.
func (f *fileScan) add(name *highlight.SyntaxNode, decl bool) {
	d := innermostDecl(f.decls, Pos{name.StartLine, name.StartCol})
	if d == nil || !f.sc.keys[key(d.FQN)] {
		return
	}
	f.out = append(f.out, Location{
		Path:      f.path,
		Range:     rangeOf(name),
		Declaring: d.FQN,
		InTrait:   d.IsTrait(),
		Decl:      decl,
	})
}

// innermostDecl is the innermost declaration of the list containing pos.
func innermostDecl(decls []*Decl, pos Pos) *Decl {
	var best *Decl
	for _, d := range decls {
		if !d.Range.Contains(pos) {
			continue
		}
		if best == nil || d.Range.Start.Line > best.Range.Start.Line ||
			(d.Range.Start.Line == best.Range.Start.Line && d.Range.Start.Col >= best.Range.Start.Col) {
			best = d
		}
	}
	return best
}

// MembersAt resolves the member the position names in any class-like body
// (#2671): the declaration whose identifier holds the position, or the
// `$this` / `self` / `static` access under it, looked up in the enclosing
// declaration's scope — the consumer scope for a trait, the type itself,
// its traits and its parent chain for a class or enum. Nearest first.
func (x *Index) MembersAt(path, text string, pos Pos) []Member {
	s := x.snapshot()
	if s == nil {
		return nil
	}
	scope, ok := x.ScopeAt(path, pos)
	if !ok {
		return nil
	}
	for _, m := range scope.Members {
		if m.NameRange.Contains(pos) || (m.NameRange.End == pos) {
			return []Member{m}
		}
	}
	access, ok := MemberAccessAt(text, pos)
	if !ok {
		return nil
	}
	if scope.IsTrait() {
		return x.LookupAccess(scope.FQN, access)
	}
	keys := s.classScopeKeys(key(scope.FQN))
	for _, kind := range access.Kinds {
		name := access.Name
		if kind == MemberProperty && !strings.HasPrefix(name, "$") {
			name = "$" + name
		}
		var out []Member
		for _, m := range s.membersOf(keys) {
			if m.Kind == kind && m.Name == name {
				out = append(out, m)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// classScopeKeys lists what `$this` sees inside a class or enum body: the
// type itself, its traits transitively, then its parent chain (to depth)
// with each parent's traits.
func (s *snapshot) classScopeKeys(k string) []string {
	out := []string{k}
	out = append(out, s.traitClosure(k, map[string]bool{})...)
	for _, p := range s.parentChain(k, s.depth) {
		out = append(out, p)
		out = append(out, s.traitClosure(p, map[string]bool{})...)
	}
	return out
}
