package phpindex

// extract.go turns one PHP text into its declarations through the syntax
// tree snapshot (highlight.SyntaxTree, #2667). It reads structure — the
// class-like declarations, their edges and their members — never the
// highlight captures, and resolves every written name to a fully qualified
// one from the file's namespace and imports. A name it cannot resolve keeps
// its written form; the snapshot matches such names by short name.

import (
	"strings"

	"ike/internal/highlight"
)

// Kind classifies a class-like declaration.
type Kind int

const (
	KindClass Kind = iota
	KindTrait
	KindInterface
	KindEnum
)

// String names the kind the way PHP spells it.
func (k Kind) String() string {
	switch k {
	case KindTrait:
		return "trait"
	case KindInterface:
		return "interface"
	case KindEnum:
		return "enum"
	}
	return "class"
}

// MemberKind classifies a member.
type MemberKind int

const (
	MemberMethod MemberKind = iota
	MemberProperty
	MemberConst
	MemberCase
)

// String names the member kind.
func (k MemberKind) String() string {
	switch k {
	case MemberProperty:
		return "property"
	case MemberConst:
		return "const"
	case MemberCase:
		return "case"
	}
	return "method"
}

// Pos is a 0-based line / rune-column position in editor coordinates.
type Pos struct {
	Line int
	Col  int
}

// Range is a start-inclusive, end-exclusive span in editor coordinates.
type Range struct {
	Start Pos
	End   Pos
}

// Contains reports whether p lies inside r.
func (r Range) Contains(p Pos) bool {
	if p.Line < r.Start.Line || p.Line > r.End.Line {
		return false
	}
	if p.Line == r.Start.Line && p.Col < r.Start.Col {
		return false
	}
	if p.Line == r.End.Line && p.Col >= r.End.Col {
		return false
	}
	return true
}

// Member is one declared member of a class-like type.
type Member struct {
	Kind MemberKind
	// Name is the member's name as written: methods and constants bare,
	// properties with their `$`, enum cases bare.
	Name       string
	Static     bool
	Visibility string // "public", "protected", "private" or "" (implicit public / var)
	// Params is the parameter list text including its parentheses
	// (methods only); ReturnType the return type text ("" when absent).
	// Type is a property's declared type.
	Params     string
	ReturnType string
	Type       string
	// Doc is the first sentence of the preceding docblock, "" when none.
	Doc string
	// AliasOf is set for a trait-use alias (`use X { foo as bar; }` makes
	// bar an alias member of the consumer): the aliased method as written
	// (`foo` or `X::foo`).
	AliasOf string
	// Declaring is the FQN of the type declaring the member; Path and
	// Range locate the whole member, NameRange its identifier.
	Declaring string
	Path      string
	Range     Range
	NameRange Range
}

// Signature renders the member the way a hover or completion detail shows
// it: `public static function find(int $id): static`, `protected $x`,
// `const K`, `case Active`.
func (m Member) Signature() string {
	var sb strings.Builder
	if m.Visibility != "" {
		sb.WriteString(m.Visibility)
		sb.WriteByte(' ')
	}
	if m.Static {
		sb.WriteString("static ")
	}
	switch m.Kind {
	case MemberMethod:
		sb.WriteString("function ")
		sb.WriteString(m.Name)
		if m.Params != "" {
			sb.WriteString(m.Params)
		} else {
			sb.WriteString("()")
		}
		if m.ReturnType != "" {
			sb.WriteString(": ")
			sb.WriteString(m.ReturnType)
		}
	case MemberProperty:
		if m.Type != "" {
			sb.WriteString(m.Type)
			sb.WriteByte(' ')
		}
		sb.WriteString(m.Name)
	case MemberConst:
		sb.WriteString("const ")
		sb.WriteString(m.Name)
	case MemberCase:
		sb.WriteString("case ")
		sb.WriteString(m.Name)
	}
	return sb.String()
}

// Decl is one class-like declaration.
type Decl struct {
	Kind Kind
	// Name is the short name, FQN the resolved fully qualified name without
	// a leading backslash (equal to Name in the global namespace).
	Name string
	FQN  string
	// Extends / Implements / Traits are resolved names (see resolve); an
	// unresolvable name stays as written.
	Extends    []string
	Implements []string
	Traits     []string
	Abstract   bool
	Final      bool
	Members    []Member
	Path       string
	Range      Range
	NameRange  Range
}

// IsTrait reports whether the declaration is a trait.
func (d Decl) IsTrait() bool { return d.Kind == KindTrait }

// fileDecls is one file's extraction.
type fileDecls struct {
	Decls []Decl
	// Refs are the resolved names the file refers to outside its own
	// declarations: imports, parents, interfaces and used traits.
	Refs []string
}

// extractFile is the walk's extractor: PHP hosts only.
func extractFile(path, host, text string, only func(string) bool) map[string]fileDecls {
	if host != "php" || (only != nil && !only("php")) {
		return nil
	}
	fd, ok := extractText(path, text)
	if !ok {
		return nil
	}
	return map[string]fileDecls{"php": fd}
}

// extractText extracts path's declarations from text; ok is false without a
// syntax tree (no cgo).
func extractText(path, text string) (fileDecls, bool) {
	lines := strings.Split(text, "\n")
	root := highlight.SyntaxTree("php", lines)
	if root == nil {
		return fileDecls{}, false
	}
	src := strings.Join(lines, "\n")
	e := &extractor{path: path, src: src, imports: map[string]string{}}
	e.walk(root)
	return fileDecls{Decls: e.decls, Refs: dedupe(e.refs)}, true
}

// extractor carries the walk's namespace state.
type extractor struct {
	path    string
	src     string
	ns      string
	imports map[string]string // lower-cased alias → FQN
	decls   []Decl
	refs    []string
}

func (e *extractor) text(n *highlight.SyntaxNode) string { return n.Text(e.src) }

// walk visits n's children: namespace and import statements update the
// resolution state, class-like declarations are extracted, everything else
// is descended into (a conditional class inside a function body is still a
// declaration).
func (e *extractor) walk(n *highlight.SyntaxNode) {
	for _, c := range n.Children {
		switch c.Kind {
		case "namespace_definition":
			name := ""
			if nn := c.Child("name"); nn != nil {
				name = e.text(nn)
			}
			if body := c.Child("body"); body != nil {
				saveNS, saveImports := e.ns, e.imports
				e.ns, e.imports = name, map[string]string{}
				e.walk(body)
				e.ns, e.imports = saveNS, saveImports
			} else {
				// An unbraced namespace applies to the rest of the file
				// and starts with a clean import table.
				e.ns, e.imports = name, map[string]string{}
			}
		case "namespace_use_declaration":
			e.importDecl(c)
		case "class_declaration", "trait_declaration", "interface_declaration", "enum_declaration":
			e.decls = append(e.decls, e.decl(c))
		case "comment", "php_tag", "text":
		default:
			e.walk(c)
		}
	}
}

// importDecl records `use A\B;`, `use A\B as C;` and the grouped
// `use A\{B, C as D};` forms; function and const imports are skipped.
func (e *extractor) importDecl(n *highlight.SyntaxNode) {
	if t := n.Child("type"); t != nil {
		return
	}
	prefix := ""
	var clauses []*highlight.SyntaxNode
	for _, c := range n.Children {
		switch c.Kind {
		case "namespace_name":
			prefix = e.text(c)
		case "namespace_use_clause":
			clauses = append(clauses, c)
		case "namespace_use_group":
			clauses = append(clauses, c.ChildrenOfKind("namespace_use_clause")...)
		}
	}
	for _, cl := range clauses {
		if t := cl.Child("type"); t != nil {
			continue
		}
		name := ""
		alias := ""
		for _, c := range cl.Children {
			switch {
			case c.Field == "alias":
				alias = e.text(c)
			case c.Kind == "name" || c.Kind == "qualified_name" || c.Kind == "relative_name":
				name = e.text(c)
			}
		}
		if name == "" {
			continue
		}
		fqn := strings.TrimPrefix(name, "\\")
		if prefix != "" {
			fqn = strings.TrimPrefix(prefix, "\\") + "\\" + fqn
		}
		if alias == "" {
			alias = shortName(fqn)
		}
		e.imports[strings.ToLower(alias)] = fqn
		e.refs = append(e.refs, fqn)
	}
}

// resolve turns a written class name into a fully qualified one: a leading
// backslash is absolute, `namespace\X` is relative to the current namespace,
// the first segment may be an import alias, and anything else lives in the
// current namespace. Names the walk cannot see (no import, no namespace)
// come back as written — the snapshot's short-name fallback matches them.
func (e *extractor) resolve(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "\\") {
		return name[1:]
	}
	if len(name) > 10 && strings.EqualFold(name[:10], "namespace\\") {
		return joinNS(e.ns, name[10:])
	}
	first, rest, hasRest := strings.Cut(name, "\\")
	if fq, ok := e.imports[strings.ToLower(first)]; ok {
		if hasRest {
			return fq + "\\" + rest
		}
		return fq
	}
	return joinNS(e.ns, name)
}

func joinNS(ns, name string) string {
	if ns == "" {
		return name
	}
	return ns + "\\" + name
}

// decl extracts one class-like declaration with its members.
func (e *extractor) decl(n *highlight.SyntaxNode) Decl {
	d := Decl{Path: e.path, Range: rangeOf(n)}
	switch n.Kind {
	case "trait_declaration":
		d.Kind = KindTrait
	case "interface_declaration":
		d.Kind = KindInterface
	case "enum_declaration":
		d.Kind = KindEnum
	}
	if nn := n.Child("name"); nn != nil {
		d.Name = e.text(nn)
		d.NameRange = rangeOf(nn)
	}
	d.FQN = joinNS(e.ns, d.Name)
	for _, c := range n.Children {
		switch c.Kind {
		case "abstract_modifier":
			d.Abstract = true
		case "final_modifier":
			d.Final = true
		case "base_clause":
			for _, nm := range c.Children {
				if r := e.resolve(e.text(nm)); r != "" {
					d.Extends = append(d.Extends, r)
					e.refs = append(e.refs, r)
				}
			}
		case "class_interface_clause":
			for _, nm := range c.Children {
				if r := e.resolve(e.text(nm)); r != "" {
					d.Implements = append(d.Implements, r)
					e.refs = append(e.refs, r)
				}
			}
		}
	}
	if body := n.Child("body"); body != nil {
		e.members(&d, body)
	}
	return d
}

// members extracts the body's members and trait uses. i-1 is consulted for
// the docblock preceding a member.
func (e *extractor) members(d *Decl, body *highlight.SyntaxNode) {
	for i, c := range body.Children {
		doc := ""
		if i > 0 && body.Children[i-1].Kind == "comment" {
			doc = docSummary(e.text(body.Children[i-1]))
		}
		switch c.Kind {
		case "use_declaration":
			e.traitUse(d, c)
		case "method_declaration":
			e.method(d, c, doc)
		case "property_declaration":
			e.property(d, c, doc)
		case "const_declaration":
			vis, static := modifiers(c, e.src)
			for _, el := range c.ChildrenOfKind("const_element") {
				nm := el.ChildOfKind("name")
				if nm == nil {
					continue
				}
				d.Members = append(d.Members, Member{
					Kind: MemberConst, Name: e.text(nm), Static: static, Visibility: vis, Doc: doc,
					Declaring: d.FQN, Path: e.path, Range: rangeOf(el), NameRange: rangeOf(nm),
				})
			}
		case "enum_case":
			nm := c.Child("name")
			if nm == nil {
				continue
			}
			d.Members = append(d.Members, Member{
				Kind: MemberCase, Name: e.text(nm), Doc: doc,
				Declaring: d.FQN, Path: e.path, Range: rangeOf(c), NameRange: rangeOf(nm),
			})
		}
	}
}

// traitUse records `use A, C;` edges and the alias members of a
// `{ foo as bar; }` list. An `insteadof` rule and a visibility-only `as`
// add no member.
func (e *extractor) traitUse(d *Decl, n *highlight.SyntaxNode) {
	for _, c := range n.Children {
		switch c.Kind {
		case "name", "qualified_name", "relative_name":
			if r := e.resolve(e.text(c)); r != "" {
				d.Traits = append(d.Traits, r)
				e.refs = append(e.refs, r)
			}
		case "use_list":
			for _, cl := range c.ChildrenOfKind("use_as_clause") {
				var names []*highlight.SyntaxNode
				target := ""
				vis := ""
				for _, part := range cl.Children {
					switch part.Kind {
					case "class_constant_access_expression":
						target = e.text(part)
					case "name":
						names = append(names, part)
					case "visibility_modifier":
						vis = e.text(part)
					}
				}
				// `foo as bar` → names [foo, bar]; `X::foo as bar` → target + [bar];
				// `foo as protected` → names [foo] only: no alias.
				var alias *highlight.SyntaxNode
				switch {
				case target != "" && len(names) == 1:
					alias = names[0]
				case target == "" && len(names) == 2:
					target, alias = e.text(names[0]), names[1]
				}
				if alias == nil {
					continue
				}
				d.Members = append(d.Members, Member{
					Kind: MemberMethod, Name: e.text(alias), Visibility: vis, AliasOf: target,
					Declaring: d.FQN, Path: e.path, Range: rangeOf(cl), NameRange: rangeOf(alias),
				})
			}
		}
	}
}

// method extracts a method plus the promoted constructor properties in its
// parameter list.
func (e *extractor) method(d *Decl, n *highlight.SyntaxNode, doc string) {
	nm := n.Child("name")
	if nm == nil {
		return
	}
	vis, static := modifiers(n, e.src)
	m := Member{
		Kind: MemberMethod, Name: e.text(nm), Static: static, Visibility: vis, Doc: doc,
		Declaring: d.FQN, Path: e.path, Range: rangeOf(n), NameRange: rangeOf(nm),
	}
	params := n.Child("parameters")
	if params != nil {
		m.Params = collapseSpace(e.text(params))
	}
	if rt := n.Child("return_type"); rt != nil {
		m.ReturnType = e.text(rt)
	}
	d.Members = append(d.Members, m)
	for _, p := range params.ChildrenOfKind("property_promotion_parameter") {
		pn := p.Child("name")
		if pn == nil {
			continue
		}
		pm := Member{
			Kind: MemberProperty, Name: e.text(pn),
			Declaring: d.FQN, Path: e.path, Range: rangeOf(p), NameRange: rangeOf(pn),
		}
		if v := p.Child("visibility"); v != nil {
			pm.Visibility = e.text(v)
		}
		if t := p.Child("type"); t != nil {
			pm.Type = e.text(t)
		}
		d.Members = append(d.Members, pm)
	}
}

// property extracts every element of a property declaration.
func (e *extractor) property(d *Decl, n *highlight.SyntaxNode, doc string) {
	vis, static := modifiers(n, e.src)
	typ := ""
	if t := n.Child("type"); t != nil {
		typ = e.text(t)
	}
	for _, el := range n.ChildrenOfKind("property_element") {
		nm := el.Child("name")
		if nm == nil {
			continue
		}
		d.Members = append(d.Members, Member{
			Kind: MemberProperty, Name: e.text(nm), Static: static, Visibility: vis, Type: typ, Doc: doc,
			Declaring: d.FQN, Path: e.path, Range: rangeOf(el), NameRange: rangeOf(nm),
		})
	}
}

// modifiers reads a member's visibility and static modifiers.
func modifiers(n *highlight.SyntaxNode, src string) (vis string, static bool) {
	for _, c := range n.Children {
		switch c.Kind {
		case "visibility_modifier":
			vis = c.Text(src)
		case "static_modifier":
			static = true
		}
	}
	return vis, static
}

// docSummary is the first prose line of a docblock: `/** Does x. */` →
// "Does x."; annotation lines (`@return`) and decoration are skipped. A
// plain comment yields "".
func docSummary(comment string) string {
	if !strings.HasPrefix(comment, "/**") {
		return ""
	}
	body := strings.TrimSuffix(strings.TrimPrefix(comment, "/**"), "*/")
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if line == "" || strings.HasPrefix(line, "@") {
			continue
		}
		return line
	}
	return ""
}

func rangeOf(n *highlight.SyntaxNode) Range {
	return Range{Start: Pos{n.StartLine, n.StartCol}, End: Pos{n.EndLine, n.EndCol}}
}

// collapseSpace joins a multi-line parameter list into one line.
func collapseSpace(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.ReplaceAll(s, "( ", "(")
	s = strings.ReplaceAll(s, " )", ")")
	return strings.ReplaceAll(s, ",)", ")")
}

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	out := xs[:0]
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
