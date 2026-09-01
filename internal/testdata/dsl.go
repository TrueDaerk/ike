package testdata

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// dsl.go parses the generator DSL (#2392): one field per line, written
// `name = expression`. An expression is a generator call from the catalog
// (`first_name()`, `int(1..1000)`, `from_list(red, green, blue)` — the same
// parameter grammars the wizard used), a quoted template string
// (`"https://{host}/api/{id}"`), or `weighted(...)` alternatives over
// arbitrary sub-expressions (`weighted(60: "active", 40: email({domain}))`).
// `{field}` interpolates another field of the same row, both inside template
// strings and inside generator arguments; evaluation follows the reference
// dependencies, and a cycle is rejected naming its path.
//
// The parser is line-oriented on purpose: every error carries the 1-based
// line it points at, which is what the dialog's inline error line shows.
// Blank lines and `#` comments are skipped.

// ParseError is a DSL problem tied to the line it sits on.
type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

// perr builds a ParseError.
func perr(line int, format string, args ...any) *ParseError {
	return &ParseError{Line: line, Msg: fmt.Sprintf(format, args...)}
}

// Program is a parsed, dependency-ordered DSL spec, ready to evaluate.
type Program struct {
	fields []progField
	// order is the evaluation order — indices into fields, topologically
	// sorted over the {field} references. Output stays in fields order.
	order []int
}

// progField is one `name = expression` line.
type progField struct {
	name string
	line int
	expr exprNode
}

// Names lists the field names in definition (= output) order.
func (p *Program) Names() []string {
	out := make([]string, len(p.fields))
	for i, f := range p.fields {
		out[i] = f.name
	}
	return out
}

// FieldLines maps each field name to the 1-based DSL line defining it — the
// dialog's autocomplete uses it to offer only the fields defined above the
// cursor.
func (p *Program) FieldLines() map[string]int {
	out := make(map[string]int, len(p.fields))
	for _, f := range p.fields {
		out[f.name] = f.line
	}
	return out
}

// ------------------------------------------------------------------- nodes

// exprNode is one parsed expression, evaluated per row.
type exprNode interface {
	eval(ec *evalCtx) (any, error)
	addRefs(out map[string]bool)
}

// tmplPart is one piece of an interpolated text: a literal or a {ref}.
type tmplPart struct {
	lit string
	ref string // field name; lit is unused when set
}

// tmpl is an interpolated text — a template string's body or a generator
// argument.
type tmpl []tmplPart

// static returns the text when the template holds no references.
func (t tmpl) static() (string, bool) {
	var b strings.Builder
	for _, p := range t {
		if p.ref != "" {
			return "", false
		}
		b.WriteString(p.lit)
	}
	return b.String(), true
}

// expand renders the template against the row's already-evaluated values.
func (t tmpl) expand(vals map[string]any) string {
	var b strings.Builder
	for _, p := range t {
		if p.ref == "" {
			b.WriteString(p.lit)
			continue
		}
		b.WriteString(plainValue(vals[p.ref]))
	}
	return b.String()
}

func (t tmpl) addRefs(out map[string]bool) {
	for _, p := range t {
		if p.ref != "" {
			out[p.ref] = true
		}
	}
}

// callNode is a generator call: a catalog kind plus its (possibly
// interpolated) argument. A static argument was validated at parse time; a
// dynamic one re-validates per row, since its text only exists then.
type callNode struct {
	kind    Kind
	arg     tmpl
	dynamic bool
}

func (n *callNode) eval(ec *evalCtx) (any, error) {
	param := n.arg.expand(ec.vals)
	if n.dynamic {
		if err := checkParam(n.kind, param); err != nil {
			return nil, err
		}
	}
	return ec.g.kindValue(n.kind, param, ec.row)
}

func (n *callNode) addRefs(out map[string]bool) { n.arg.addRefs(out) }

// strNode is a quoted template string; it always evaluates to a string.
type strNode struct {
	body tmpl
}

func (n *strNode) eval(ec *evalCtx) (any, error) { return n.body.expand(ec.vals), nil }

func (n *strNode) addRefs(out map[string]bool) { n.body.addRefs(out) }

// weightedNode picks one branch per row, proportionally to the weights. The
// draw comes from the seeded instance faker — one draw, then only the winning
// branch evaluates — so a seeded run stays byte-identical regardless of which
// branch wins.
type weightedNode struct {
	weights []float64
	exprs   []exprNode
	total   float64
}

func (n *weightedNode) eval(ec *evalCtx) (any, error) {
	pick := ec.g.fake.Float64Range(0, n.total)
	acc := 0.0
	for i, w := range n.weights {
		acc += w
		if pick <= acc || i == len(n.weights)-1 {
			return n.exprs[i].eval(ec)
		}
	}
	return n.exprs[len(n.exprs)-1].eval(ec) // unreachable
}

func (n *weightedNode) addRefs(out map[string]bool) {
	for _, e := range n.exprs {
		e.addRefs(out)
	}
}

// evalCtx is one row's evaluation state: the generator (faker + catalog) and
// the values evaluated so far, keyed by field name.
type evalCtx struct {
	g    *Generator
	row  int
	vals map[string]any
}

// ------------------------------------------------------------------ parsing

// ParseDSL parses a whole spec text. The first problem is returned as a
// *ParseError carrying its line.
func ParseDSL(src string) (*Program, error) {
	p := &Program{}
	seen := map[string]int{}
	for i, raw := range strings.Split(src, "\n") {
		ln := i + 1
		s := strings.TrimSpace(raw)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		name, rest, ok := strings.Cut(s, "=")
		if !ok {
			return nil, perr(ln, "missing '=' — a field is written name = expression")
		}
		name = strings.TrimSpace(name)
		if err := checkFieldName(name); err != nil {
			return nil, perr(ln, "%s", err)
		}
		if prev, dup := seen[name]; dup {
			return nil, perr(ln, "duplicate field %q (already defined on line %d)", name, prev)
		}
		expr, err := parseExpr(ln, strings.TrimSpace(rest))
		if err != nil {
			return nil, err
		}
		seen[name] = ln
		p.fields = append(p.fields, progField{name: name, line: ln, expr: expr})
	}
	if len(p.fields) == 0 {
		return nil, perr(1, "at least one field is required, written name = expression")
	}
	if err := p.resolve(); err != nil {
		return nil, err
	}
	return p, nil
}

// ValidFieldName reports whether name may head a `name = expression` line —
// the dialog's autocomplete uses it to recognize the fields defined above the
// cursor without running the whole parser.
func ValidFieldName(name string) bool { return name != "" && checkFieldName(name) == nil }

// checkFieldName bounds what a field (and therefore a {reference}) may be
// called: letters, digits, '_', '-' and '.'. The writers sanitize further per
// format; the DSL only needs names that survive inside braces.
func checkFieldName(name string) error {
	if name == "" {
		return fmt.Errorf("a field name is required before '='")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '_', r == '-', r == '.':
		default:
			return fmt.Errorf("invalid field name %q — use letters, digits, '_', '-' or '.'", name)
		}
	}
	return nil
}

// resolve wires the {field} references: every reference must name a defined
// field, and the dependency graph must be acyclic. The evaluation order is a
// stable topological sort (definition order among the ready fields), so a
// spec without references evaluates top to bottom.
func (p *Program) resolve() error {
	index := make(map[string]int, len(p.fields))
	for i, f := range p.fields {
		index[f.name] = i
	}
	deps := make([][]int, len(p.fields))
	for i, f := range p.fields {
		refs := map[string]bool{}
		f.expr.addRefs(refs)
		names := make([]string, 0, len(refs))
		for r := range refs {
			names = append(names, r)
		}
		sort.Strings(names)
		for _, r := range names {
			j, ok := index[r]
			if !ok {
				return perr(f.line, "reference {%s} does not match any field", r)
			}
			deps[i] = append(deps[i], j)
		}
	}
	done := make([]bool, len(p.fields))
	p.order = p.order[:0]
	for len(p.order) < len(p.fields) {
		progressed := false
		for i := range p.fields {
			if done[i] {
				continue
			}
			ready := true
			for _, d := range deps[i] {
				if !done[d] {
					ready = false
					break
				}
			}
			if ready {
				p.order = append(p.order, i)
				done[i] = true
				progressed = true
			}
		}
		if !progressed {
			return p.cycleError(deps, done)
		}
	}
	return nil
}

// cycleError names one reference cycle among the unresolved fields, walking
// the dependency edges until a field repeats: "a → b → a".
func (p *Program) cycleError(deps [][]int, done []bool) error {
	start := -1
	for i := range p.fields {
		if !done[i] {
			start = i
			break
		}
	}
	var path []int
	onPath := map[int]bool{}
	cur := start
	for !onPath[cur] {
		onPath[cur] = true
		path = append(path, cur)
		next := -1
		for _, d := range deps[cur] {
			if !done[d] {
				next = d
				break
			}
		}
		cur = next
	}
	// Trim the lead-in so the message shows only the cycle itself.
	for len(path) > 0 && path[0] != cur {
		delete(onPath, path[0])
		path = path[1:]
	}
	names := make([]string, 0, len(path)+1)
	for _, i := range path {
		names = append(names, p.fields[i].name)
	}
	names = append(names, p.fields[cur].name)
	return perr(p.fields[path[0]].line, "fields reference each other in a cycle: %s", strings.Join(names, " → "))
}

// exprScan is a cursor over one expression's runes.
type exprScan struct {
	r  []rune
	i  int
	ln int
}

// parseExpr parses one full expression and refuses trailing garbage.
func parseExpr(ln int, s string) (exprNode, error) {
	sc := &exprScan{r: []rune(s), ln: ln}
	n, err := sc.expr()
	if err != nil {
		return nil, err
	}
	sc.skipSpace()
	if !sc.eof() {
		return nil, perr(ln, "unexpected %q after the expression", string(sc.r[sc.i:]))
	}
	return n, nil
}

func (sc *exprScan) eof() bool { return sc.i >= len(sc.r) }

func (sc *exprScan) peek() rune {
	if sc.eof() {
		return 0
	}
	return sc.r[sc.i]
}

func (sc *exprScan) skipSpace() {
	for !sc.eof() && (sc.r[sc.i] == ' ' || sc.r[sc.i] == '\t') {
		sc.i++
	}
}

// expr parses one expression at the cursor: a quoted template string, a
// generator call, or weighted(...).
func (sc *exprScan) expr() (exprNode, error) {
	sc.skipSpace()
	if sc.eof() {
		return nil, perr(sc.ln, "an expression is required — a generator call like first_name(), a quoted string or weighted(…)")
	}
	switch c := sc.peek(); {
	case c == '"':
		return sc.stringLit()
	case c == '{':
		return nil, perr(sc.ln, "a bare {reference} is not an expression — quote it (\"{%s}\") to copy another field", sc.braceHint())
	case isIdentStart(c):
		return sc.callOrWeighted()
	}
	return nil, perr(sc.ln, "expected a generator call like first_name(), a quoted string or weighted(…)")
}

// braceHint reads the name inside a bare {…} for the error message.
func (sc *exprScan) braceHint() string {
	rest := string(sc.r[sc.i:])
	rest = strings.TrimPrefix(rest, "{")
	if j := strings.IndexAny(rest, "}\" "); j >= 0 {
		rest = rest[:j]
	}
	if rest == "" {
		return "field"
	}
	return rest
}

func isIdentStart(c rune) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdentRune(c rune) bool {
	return isIdentStart(c) || c >= '0' && c <= '9'
}

// callOrWeighted parses `ident(...)`.
func (sc *exprScan) callOrWeighted() (exprNode, error) {
	start := sc.i
	for !sc.eof() && isIdentRune(sc.r[sc.i]) {
		sc.i++
	}
	ident := string(sc.r[start:sc.i])
	sc.skipSpace()
	if sc.peek() != '(' {
		if ident == WeightedName {
			return nil, perr(sc.ln, "weighted needs parentheses: weighted(60: \"a\", 40: \"b\")")
		}
		if _, ok := Info(Kind(ident)); ok {
			return nil, perr(sc.ln, "generator %q must be called with parentheses: %s()", ident, ident)
		}
		return nil, perr(sc.ln, "unknown value %q — expected a generator call like first_name(), a quoted string or weighted(…)", ident)
	}
	if ident == WeightedName {
		return sc.weighted()
	}
	info, ok := Info(Kind(ident))
	if !ok {
		return nil, perr(sc.ln, "unknown generator %q", ident)
	}
	sc.i++ // consume '('
	arg, err := sc.argText()
	if err != nil {
		return nil, err
	}
	body, err := parseTmpl(sc.ln, strings.TrimSpace(arg))
	if err != nil {
		return nil, err
	}
	static, isStatic := body.static()
	if info.Param == "" {
		if len(body) > 0 && (!isStatic || static != "") {
			return nil, perr(sc.ln, "%s() takes no argument", ident)
		}
		return &callNode{kind: info.Kind}, nil
	}
	if isStatic {
		if err := checkParam(info.Kind, static); err != nil {
			return nil, perr(sc.ln, "%s(): %s", ident, err)
		}
		return &callNode{kind: info.Kind, arg: tmpl{{lit: static}}}, nil
	}
	return &callNode{kind: info.Kind, arg: body, dynamic: true}, nil
}

// argText consumes the argument text up to the call's closing paren,
// tracking nesting and quotes so a quoted ')' does not end the call.
func (sc *exprScan) argText() (string, error) {
	start := sc.i
	depth := 1
	inStr := false
	for !sc.eof() {
		c := sc.r[sc.i]
		switch {
		case inStr:
			if c == '\\' {
				sc.i++ // the escaped rune is consumed below
			} else if c == '"' {
				inStr = false
			}
		case c == '"':
			inStr = true
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				out := string(sc.r[start:sc.i])
				sc.i++
				return out, nil
			}
		}
		sc.i++
	}
	return "", perr(sc.ln, "missing ')' to close the call")
}

// weighted parses `weighted(w1: expr1, w2: expr2, …)` after the identifier;
// the cursor sits on '('.
func (sc *exprScan) weighted() (exprNode, error) {
	sc.i++ // consume '('
	n := &weightedNode{}
	for {
		sc.skipSpace()
		if sc.eof() {
			return nil, perr(sc.ln, "missing ')' to close weighted(…)")
		}
		if sc.peek() == ')' && len(n.exprs) == 0 {
			return nil, perr(sc.ln, "weighted(…) needs at least one branch, written weight: expression")
		}
		start := sc.i
		for !sc.eof() && (sc.r[sc.i] == '.' || sc.r[sc.i] >= '0' && sc.r[sc.i] <= '9') {
			sc.i++
		}
		wText := string(sc.r[start:sc.i])
		w, err := strconv.ParseFloat(wText, 64)
		if err != nil {
			return nil, perr(sc.ln, "weighted(…): %q is not a weight — a branch is written weight: expression", firstToken(string(sc.r[start:])))
		}
		if w <= 0 {
			return nil, perr(sc.ln, "weighted(…): weights must be positive, got %s", wText)
		}
		sc.skipSpace()
		if sc.peek() != ':' {
			return nil, perr(sc.ln, "weighted(…): expected ':' after the weight %s", wText)
		}
		sc.i++
		expr, err := sc.expr()
		if err != nil {
			return nil, err
		}
		n.weights = append(n.weights, w)
		n.exprs = append(n.exprs, expr)
		n.total += w
		sc.skipSpace()
		switch sc.peek() {
		case ',':
			sc.i++
		case ')':
			sc.i++
			return n, nil
		default:
			if sc.eof() {
				return nil, perr(sc.ln, "missing ')' to close weighted(…)")
			}
			return nil, perr(sc.ln, "weighted(…): expected ',' or ')' after a branch, got %q", string(sc.peek()))
		}
	}
}

// firstToken trims a snippet for an error message.
func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if j := strings.IndexAny(s, ":,)"); j >= 0 {
		s = s[:j]
	}
	s = strings.TrimSpace(s)
	if len(s) > 20 {
		s = s[:20] + "…"
	}
	return s
}

// stringLit parses a quoted template string. Escapes: \" \\ \{ \} \n \t;
// {name} interpolates a field.
func (sc *exprScan) stringLit() (exprNode, error) {
	sc.i++ // consume the opening quote
	var body tmpl
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			body = append(body, tmplPart{lit: lit.String()})
			lit.Reset()
		}
	}
	for {
		if sc.eof() {
			return nil, perr(sc.ln, "missing closing quote")
		}
		c := sc.r[sc.i]
		sc.i++
		switch c {
		case '"':
			flush()
			return &strNode{body: body}, nil
		case '\\':
			if sc.eof() {
				return nil, perr(sc.ln, "missing closing quote")
			}
			e := sc.r[sc.i]
			sc.i++
			switch e {
			case '"', '\\', '{', '}':
				lit.WriteRune(e)
			case 'n':
				lit.WriteByte('\n')
			case 't':
				lit.WriteByte('\t')
			default:
				return nil, perr(sc.ln, `unknown escape \%s — use \" \\ \{ \} \n or \t`, string(e))
			}
		case '{':
			ref, err := sc.refName()
			if err != nil {
				return nil, err
			}
			flush()
			body = append(body, tmplPart{ref: ref})
		default:
			lit.WriteRune(c)
		}
	}
}

// refName reads a {reference}'s name; the cursor sits after '{'.
func (sc *exprScan) refName() (string, error) {
	start := sc.i
	for !sc.eof() && sc.r[sc.i] != '}' && sc.r[sc.i] != '"' {
		sc.i++
	}
	if sc.eof() || sc.r[sc.i] != '}' {
		return "", perr(sc.ln, "missing '}' to close the field reference")
	}
	name := strings.TrimSpace(string(sc.r[start:sc.i]))
	sc.i++
	if err := checkFieldName(name); err != nil {
		return "", perr(sc.ln, "bad field reference: %s", err)
	}
	return name, nil
}

// parseTmpl reads a generator argument as an interpolated text: plain
// characters plus {name} references. Argument text has no escapes — the
// param grammars (ranges, domains, lists) never need a literal brace.
func parseTmpl(ln int, s string) (tmpl, error) {
	var out tmpl
	var lit strings.Builder
	r := []rune(s)
	i := 0
	for i < len(r) {
		c := r[i]
		if c != '{' {
			lit.WriteRune(c)
			i++
			continue
		}
		j := i + 1
		for j < len(r) && r[j] != '}' {
			j++
		}
		if j >= len(r) {
			return nil, perr(ln, "missing '}' to close the field reference")
		}
		name := strings.TrimSpace(string(r[i+1 : j]))
		if err := checkFieldName(name); err != nil {
			return nil, perr(ln, "bad field reference: %s", err)
		}
		if lit.Len() > 0 {
			out = append(out, tmplPart{lit: lit.String()})
			lit.Reset()
		}
		out = append(out, tmplPart{ref: name})
		i = j + 1
	}
	if lit.Len() > 0 {
		out = append(out, tmplPart{lit: lit.String()})
	}
	return out, nil
}
