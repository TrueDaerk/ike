package complexity

import (
	"fmt"
	"sort"
	"strings"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// walk.go is the engine proper: one iterative pre-order walk of the syntax
// tree with an explicit frame stack (grammars nest a long boolean chain or a
// deep else-if ladder hundreds of levels deep, so no recursion), a stack of
// the open function units, and the per-node rules of the package doc.

// headerFields are the fields of a Branch or Selector node that hold its
// header — condition, loop clauses, the switch subject, the catch parameter —
// rather than its body. Children in these fields are walked at the node's own
// nesting level; everything else inside the node is its body and nests.
var headerFields = map[string]bool{
	"condition": true, "initializer": true, "initialize": true, "update": true,
	"increment": true, "left": true, "right": true, "value": true, "subject": true,
	"alias": true, "parameter": true, "parameters": true, "type": true, "name": true,
	"label": true, "communication": true, "pattern": true, "guard": true,
	"conditional_expressions": true, "variable": true,
}

// parenKind is the node a boolean-operator sequence is flattened through
// (Sonar/gocognit: `a && (b || c) && d` is three sequences, the parentheses
// do not restart the count). The Go, Python, PHP and TSX grammars all use
// this kind; shell's `( … )` is a subshell and never transparent.
const parenKind = "parenthesized_expression"

// altField is the field an else or else-if sits in, in every grammar with an
// if_statement: Go, Python, PHP and TSX alike.
const altField = "alternative"

// unitState is one open function unit during the walk.
type unitState struct {
	idx     int    // index in measurer.out
	short   string // unqualified name for the recursion check; "" for an anonymous unit
	nesting int    // current nesting level inside this unit
	depth   int    // deepest nesting reached
	ccn     int
	cog     int
	sites   []Site // the increments recorded so far (#2946)
}

// frame is one node on the walk stack with the decisions taken on entering
// it: whether it raised the nesting of the units open before it, whether it
// opened a unit, and how its children are to be read.
type frame struct {
	n      *highlight.SyntaxNode
	next   int  // next child to enter
	raised bool // this node raised the nesting of every unit open before it
	unit   bool // this node opened the innermost unit
	scope  bool // a unit or a Nesting kind: the qualifier scope of the methods inside

	nestKids     bool // children outside the header fields / else position raise nesting
	headerExempt bool // with nestKids: children in headerFields do not raise (Branch, Selector)
	isBranch     bool // a Branch: its alternative-position children are else / else-if
	elseWrap     bool // an Else kind wrapping a Branch (`else if`): the Branch counts flat
}

type measurer struct {
	spec  *lang.ComplexitySpec
	src   string
	lines []string
	cm    comments

	branch, selector, arm, elseK, nesting, jump, boolOp map[string]bool

	stack    []frame
	units    []*unitState
	out      []Function
	anon     int
	consumed map[*highlight.SyntaxNode]bool // boolean operator nodes already flattened
	lineAt   []int                          // byte offset of each line's start in src (lazily built)
}

func set(kinds []string) map[string]bool {
	m := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		m[k] = true
	}
	return m
}

// measure walks root with spec and returns the units in source order.
func measure(root *highlight.SyntaxNode, lines []string, spec *lang.ComplexitySpec, cm comments) []Function {
	m := &measurer{
		spec:     spec,
		src:      strings.Join(lines, "\n"),
		lines:    lines,
		cm:       cm,
		branch:   set(spec.Branch),
		selector: set(spec.Selector),
		arm:      set(spec.Arms),
		elseK:    set(spec.Else),
		nesting:  set(spec.Nesting),
		jump:     set(spec.Jumps),
		boolOp:   set(spec.BoolOps),
		out:      []Function{},
		consumed: map[*highlight.SyntaxNode]bool{},
	}
	m.stack = append(m.stack, frame{n: root})
	for len(m.stack) > 0 {
		top := len(m.stack) - 1
		f := &m.stack[top]
		if f.next < len(f.n.Children) {
			c := f.n.Children[f.next]
			f.next++
			m.enter(c, top)
			continue
		}
		m.exit(*f)
		m.stack = m.stack[:top]
	}
	return m.out
}

// enter applies the rules to c, a child of the frame at parentIdx, and pushes
// its frame.
func (m *measurer) enter(c *highlight.SyntaxNode, parentIdx int) {
	parent := m.stack[parentIdx]
	idx := parent.next - 1 // c's index among its siblings
	fr := frame{n: c}

	if nameField, isUnit := m.spec.FunctionNodes[c.Kind]; isUnit {
		// A nested unit raises the nesting of the units it sits in (Sonar:
		// a lambda nests), then opens its own unit at level zero.
		m.raise()
		fr.raised, fr.unit, fr.scope = true, true, true
		m.openUnit(c, nameField)
		m.stack = append(m.stack, fr)
		return
	}

	elsePos := parent.isBranch && m.elsePosition(c, parent.n, idx)
	switch {
	case m.nesting[c.Kind]:
		fr.scope = true
		fr.raised = true
	case parent.nestKids && !(parent.headerExempt && headerFields[c.Field]) && !elsePos:
		fr.raised = true
	}
	if fr.raised {
		m.raise()
	}

	switch {
	case m.selector[c.Kind]:
		m.cogNested(c, m.kindWord(c, "switch"))
		fr.nestKids, fr.headerExempt = true, true
	case m.arm[c.Kind]:
		m.ccn(c, m.kindWord(c, "case"))
	case m.branch[c.Kind]:
		if (parent.elseWrap && idx == 0) || elsePos {
			m.ccnCogFlat(c, elseIfWord(m.kindWord(c, "?:")))
		} else {
			m.ccnCogNested(c, m.kindWord(c, "?:"))
		}
		fr.isBranch, fr.nestKids, fr.headerExempt = true, true, true
	case m.elseK[c.Kind]:
		switch {
		case len(c.Children) == 1 && m.branch[c.Children[0].Kind]:
			fr.elseWrap = true
		case parent.isBranch:
			m.cogFlat(c, "else")
			fr.nestKids = true
		}
	case elsePos:
		// Go: a bare block in the if's alternative field is the else.
		m.cogFlat(c, "else")
		fr.nestKids = true
	}
	if m.jump[c.Kind] && len(c.Children) > 0 {
		m.cogFlat(c, m.kindWord(c, "jump"))
	}
	if c.Operator != "" && m.boolOp[c.Operator] && !m.consumed[c] {
		m.booleans(c)
	}
	if field, ok := m.spec.Calls[c.Kind]; ok {
		m.recursion(c, field)
	}
	m.stack = append(m.stack, fr)
}

// exit undoes what enter did for f: closes the unit it opened and lowers the
// nesting it raised.
func (m *measurer) exit(f frame) {
	if f.unit {
		m.closeUnit()
	}
	if f.raised {
		for _, u := range m.units {
			u.nesting--
		}
	}
}

// elsePosition reports whether c, child idx of the Branch node parent, sits
// in the else position: an Else kind or an else-if Branch in the alternative
// field (a Branch of the parent's own kind only where the language has no
// Else kind — Go's `else if`; a chained ternary nests instead), the bare else
// block of such a language, or — shell — an unfielded Else / Branch kind of
// another kind after the body.
func (m *measurer) elsePosition(c, parent *highlight.SyntaxNode, idx int) bool {
	noElseKinds := len(m.spec.Else) == 0
	switch {
	case c.Field == altField:
		switch {
		case m.elseK[c.Kind]:
			return true
		case m.branch[c.Kind]:
			return noElseKinds || c.Kind != parent.Kind
		default:
			return noElseKinds && !m.selector[c.Kind] && !m.arm[c.Kind]
		}
	case c.Field == "" && idx > 0 && c.Kind != parent.Kind:
		return m.elseK[c.Kind] || m.branch[c.Kind]
	}
	return false
}

// raise bumps the nesting of every open unit.
func (m *measurer) raise() {
	for _, u := range m.units {
		u.nesting++
		if u.nesting > u.depth {
			u.depth = u.nesting
		}
	}
}

// ccn adds one branch to the innermost unit: an arm.
func (m *measurer) ccn(c *highlight.SyntaxNode, kind string) {
	m.record(siteAt(c, kind), 1, false, 0)
}

// cogNested adds a structural increment — one plus the nesting level — to
// every open unit, each at its own level: a selector.
func (m *measurer) cogNested(c *highlight.SyntaxNode, kind string) {
	m.record(siteAt(c, kind), 0, true, 0)
}

// ccnCogNested is a branch: one CCN for the innermost unit and a
// structural increment for every open unit.
func (m *measurer) ccnCogNested(c *highlight.SyntaxNode, kind string) {
	m.record(siteAt(c, kind), 1, true, 0)
}

// ccnCogFlat is an else-if: one CCN for the innermost unit and one flat
// cognitive point for every open unit.
func (m *measurer) ccnCogFlat(c *highlight.SyntaxNode, kind string) {
	m.record(siteAt(c, kind), 1, false, 1)
}

// cogFlat adds one flat point to every open unit: an else, a jump.
func (m *measurer) cogFlat(c *highlight.SyntaxNode, kind string) {
	m.record(siteAt(c, kind), 0, false, 1)
}

// siteAt positions a Site on node c.
func siteAt(c *highlight.SyntaxNode, kind string) Site {
	return Site{Line: c.StartLine, Col: c.StartCol, EndLine: c.EndLine, Kind: kind}
}

// record applies one increment (#2946): ccn to the innermost unit, and to
// every open unit either the structural `1 + nesting` (nested) or flat
// cognitive points — each unit at its own level — and appends the site to
// every unit it changed.
func (m *measurer) record(s Site, ccn int, nested bool, flat int) {
	last := len(m.units) - 1
	for i, u := range m.units {
		site := s
		site.Level = u.nesting
		if i == last {
			site.CCN = ccn
			u.ccn += ccn
		}
		if nested {
			site.Cognitive, site.Nesting = 1+u.nesting, u.nesting
		} else {
			site.Cognitive = flat
		}
		if site.CCN == 0 && site.Cognitive == 0 {
			continue
		}
		u.cog += site.Cognitive
		u.sites = append(u.sites, site)
	}
}

// siteWords are the leading keywords a site is named by — the node kind as
// the language spells it. A node led by none of them takes the fallback of
// its role: "?:" for a conditional expression, "case" for a shell or PHP
// match arm, "else" for Go's bare else block.
var siteWords = set([]string{
	"if", "elif", "elseif", "else", "unless", "for", "foreach", "while", "until", "do",
	"switch", "select", "match", "case", "when", "default", "try", "except", "catch",
	"goto", "break", "continue",
})

// kindWord names the site of node c: its leading keyword when it is one of
// siteWords, else fallback.
func (m *measurer) kindWord(c *highlight.SyntaxNode, fallback string) string {
	text := c.Text(m.src)
	i := 0
	for i < len(text) && (text[i] >= 'a' && text[i] <= 'z' || text[i] >= 'A' && text[i] <= 'Z' || text[i] == '_') {
		i++
	}
	if w := strings.ToLower(text[:i]); siteWords[w] {
		return w
	}
	return fallback
}

// elseIfWord names a branch in the else position: Go's and TypeScript's
// `if` reads "else if"; a language's own spelling (elif, elseif) stays.
func elseIfWord(w string) string {
	if w == "if" {
		return "else if"
	}
	return w
}

// position converts a byte offset of src to a 0-based line and byte column.
func (m *measurer) position(off int) (line, col int) {
	if m.lineAt == nil {
		m.lineAt = make([]int, 0, len(m.lines))
		at := 0
		for _, l := range m.lines {
			m.lineAt = append(m.lineAt, at)
			at += len(l) + 1
		}
	}
	line = sort.Search(len(m.lineAt), func(i int) bool { return m.lineAt[i] > off }) - 1
	if line < 0 {
		line = 0
	}
	return line, off - m.lineAt[line]
}

// openUnit starts the unit for node c and appends its record to out.
func (m *measurer) openUnit(c *highlight.SyntaxNode, nameField string) {
	f := Function{Kind: KindFunction, Line: c.StartLine, EndLine: c.EndLine}
	short := ""
	if nameField != "" {
		short = c.Child(nameField).Text(m.src)
	}
	switch {
	case short == "":
		m.anon++
		f.Name = fmt.Sprintf("(anonymous)#%d", m.anon)
		f.Kind = KindLambda
	default:
		f.Name = short
		if q := m.qualifier(c); q != "" {
			sep := m.spec.MethodSep
			if sep == "" {
				sep = "."
			}
			f.Name = q + sep + short
			f.Class = q
			f.Kind = KindMethod
		} else if strings.Contains(c.Kind, "method") {
			f.Kind = KindMethod
		}
	}
	if m.spec.ParamsField != "" {
		f.Params = countParams(c.Child(m.spec.ParamsField))
	}
	f.NLOC, f.CommentLines = nloc(m.lines, c.StartLine, c.EndLine, m.cm)
	f.CommentDensity = density(f.NLOC, f.CommentLines)
	m.out = append(m.out, f)
	m.units = append(m.units, &unitState{idx: len(m.out) - 1, short: short, ccn: 1})
}

// closeUnit writes the innermost unit's metrics back and pops it.
func (m *measurer) closeUnit() {
	n := len(m.units)
	u := m.units[n-1]
	m.units = m.units[:n-1]
	f := &m.out[u.idx]
	f.CCN = u.ccn
	f.Cognitive = u.cog
	f.Depth = u.depth
	// Pre-order walk is source order but for a boolean run, recorded at its
	// tree's root before the structures inside its operands: sort once.
	sort.SliceStable(u.sites, func(i, j int) bool {
		a, b := u.sites[i], u.sites[j]
		return a.Line < b.Line || a.Line == b.Line && a.Col < b.Col
	})
	f.Sites = u.sites
}

// qualifier returns the class or receiver name a method unit c is qualified
// by: the type of a Go `receiver`, else the `name` of the nearest enclosing
// scope when that scope is a Nesting kind (a class body) rather than a unit.
func (m *measurer) qualifier(c *highlight.SyntaxNode) string {
	if recv := c.Child("receiver"); recv != nil && len(recv.Children) > 0 {
		t := recv.Children[0].Child("type")
		if t == nil {
			t = recv.Children[0]
		}
		name := strings.TrimLeft(t.Text(m.src), "*")
		if i := strings.IndexByte(name, '['); i >= 0 {
			name = name[:i]
		}
		return name
	}
	for i := len(m.stack) - 1; i >= 0; i-- {
		fr := m.stack[i]
		if !fr.scope {
			continue
		}
		if fr.unit {
			return ""
		}
		return fr.n.Child("name").Text(m.src)
	}
	return ""
}

// booleans flattens the boolean-operator tree rooted at n in source order —
// through parentheses, stopping at any other operand — and counts: one CCN
// per operator for the innermost unit, one cognitive point per run of like
// operators for every open unit. Every operator node of the tree is marked
// consumed so the walk does not count it again.
func (m *measurer) booleans(n *highlight.SyntaxNode) {
	type item struct {
		n     *highlight.SyntaxNode
		stage int // 0: descend left, 1: emit and descend right, 2: done
	}
	var ops []string
	var at []int // byte offset of each operator
	stack := []item{{n: n}}
	for len(stack) > 0 {
		top := len(stack) - 1
		it := stack[top]
		switch it.stage {
		case 0:
			stack[top].stage = 1
			m.consumed[it.n] = true
			if l := m.boolOperand(it.n, 0); l != nil {
				stack = append(stack, item{n: l})
			}
		case 1:
			stack[top].stage = 2
			ops = append(ops, it.n.Operator)
			at = append(at, m.operatorOffset(it.n))
			if r := m.boolOperand(it.n, len(it.n.Children)-1); r != nil {
				stack = append(stack, item{n: r})
			}
		default:
			stack = stack[:top]
		}
	}
	// One site per run of like operators (#2946): its CCN is the run's
	// operator count, its cognitive point is flat, at the first operator.
	for i := 0; i < len(ops); {
		j := i + 1
		for j < len(ops) && ops[j] == ops[i] {
			j++
		}
		line, col := m.position(at[i])
		m.record(Site{Line: line, Col: col, EndLine: n.EndLine, Kind: ops[i]}, j-i, false, 1)
		i = j
	}
}

// operatorOffset is the byte offset of n's operator token: the first
// occurrence of it between n's first and last child (the operands), the
// node's start when the children do not bracket it.
func (m *measurer) operatorOffset(n *highlight.SyntaxNode) int {
	if len(n.Children) >= 2 {
		l, r := n.Children[0], n.Children[len(n.Children)-1]
		if l.EndByte <= r.StartByte && r.StartByte <= len(m.src) {
			if i := strings.Index(m.src[l.EndByte:r.StartByte], n.Operator); i >= 0 {
				return l.EndByte + i
			}
		}
	}
	return n.StartByte
}

// boolOperand returns child i of n when it is itself a boolean-operator node,
// looking through parentheses; nil otherwise.
func (m *measurer) boolOperand(n *highlight.SyntaxNode, i int) *highlight.SyntaxNode {
	if len(n.Children) < 2 || i < 0 || i >= len(n.Children) {
		return nil
	}
	c := n.Children[i]
	for c.Kind == parenKind && len(c.Children) == 1 {
		c = c.Children[0]
	}
	if c.Operator != "" && m.boolOp[c.Operator] {
		return c
	}
	return nil
}

// recursion adds one cognitive point to every open unit whose name the call
// c targets: the callee's last identifier segment (`s.handle` → handle,
// `self::m` → m, `$this->m` → m) equals the unit's short name.
func (m *measurer) recursion(c *highlight.SyntaxNode, field string) {
	callee := c.Child(field)
	if callee == nil {
		return
	}
	seg := lastSegment(callee.Text(m.src))
	if seg == "" {
		return
	}
	for _, u := range m.units {
		if u.short == seg {
			u.cog++
			u.sites = append(u.sites, Site{Line: c.StartLine, Col: c.StartCol, EndLine: c.EndLine, Kind: "recursion", Cognitive: 1, Level: u.nesting})
		}
	}
}

// lastSegment returns the identifier after the last `.`, `->`, `::` or `?->`
// of a callee text, without a leading `$`.
func lastSegment(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexAny(s, ".>:"); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimPrefix(s, "$")
}

// countParams counts the parameters of a parameter-list node: one per named
// child, except a child declaring several names (Go's `a, b int`), which
// counts once per `name` field.
func countParams(list *highlight.SyntaxNode) int {
	if list == nil {
		return 0
	}
	n := 0
	for _, p := range list.Children {
		names := 0
		for _, c := range p.Children {
			if c.Field == "name" {
				names++
			}
		}
		if names > 1 {
			n += names
		} else {
			n++
		}
	}
	return n
}
