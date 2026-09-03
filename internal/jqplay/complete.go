package jqplay

// complete.go is the typing aid behind the playground's query line (#1979):
// given the program, the cursor and the parsed input snapshot, it answers
// "what could be typed here" — the object keys that actually exist at the
// path under construction, or the jq builtins matching a half-typed name.
// Key completion is pipeline-aware (#2019): the chain under the cursor is
// resolved in the context established by the preceding pipe segments,
// `select`/`map`-style arguments and object constructions, so `.[] |
// select(.` offers the element keys; a context the static analysis cannot
// answer for stays silent rather than guessing.
//
// Everything is synchronous and bounded. The input was parsed once at open,
// so key extraction is a walk over decoded values, capped by a node budget so
// a pathological document cannot stall a keystroke; the builtin list comes
// from gojq itself — its `builtins` builtin, the same list jq prints — run
// once and memoized. The popup, its keys and its rendering live in
// internal/app; this file is pure and testable without a terminal.

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/itchyny/gojq"
)

// MaxCompletionItems caps how many candidates one completion offers. The
// popup shows eight rows; two hundred is already more than arrowing through
// makes sense for, and the cap keeps a 10 000-key object from building a list
// nobody will read.
const MaxCompletionItems = 200

// completionNodeBudget bounds how many decoded values a key extraction
// visits while descending the path. The snapshot may hold a `.jsonl` stream
// of 10 000 values each carrying large arrays; the budget stops the walk
// mid-way — offering the keys seen so far — instead of stalling the query
// line on a keystroke.
const completionNodeBudget = 10000

// Candidate is one completion offer for the query line.
type Candidate struct {
	// Label is the display text: the object key, or the builtin's name.
	Label string
	// Insert is what accepting writes over the partial being typed. For an
	// object key that is not a valid jq identifier it is the quoted form —
	// `."a b"` is how jq spells that access.
	Insert string
	// Detail is the short trailer after the label: the value's type for a
	// key, the arity notation (`/1 /2`) for a builtin.
	Detail string
	// Doc is the one-line description shown for a selected builtin; keys
	// have none.
	Doc string
}

// Complete returns the candidates for the program at rune position pos over
// the parsed input, and the rune index of the partial they replace. It
// answers three contexts: after a `.` (and deeper, `.foo.` or `.items[].`)
// the object keys that exist at that path in the snapshot, resolved in the
// pipeline context the preceding segments establish (`.[] | select(.` offers
// the element keys, #2019); at an object-construction key position (`{a`,
// jq's `{a: .a}` shorthand) the context value's keys; on a bare identifier
// the jq builtins matching it. Anywhere else — inside a string or comment,
// after `$` or `@`, on a number — it offers nothing. manual forces the
// builtin list open on an empty identifier (the explicit completion request);
// without it an empty partial stays quiet.
func Complete(program string, pos int, in *Input, manual bool) (items []Candidate, start int) {
	if in.Dialect() == DialectXMQ {
		// The xmq query line holds a CLI command line, not a jq program
		// (#2414): the offers are the xmq commands, not gojq's builtins.
		return completeXMQ(program, pos, manual)
	}
	r := []rune(program)
	if pos < 0 {
		pos = 0
	}
	if pos > len(r) {
		pos = len(r)
	}
	if insideLiteral(r, pos) {
		return nil, 0
	}
	start = pos
	for start > 0 && identRune(r[start-1]) {
		start--
	}
	partial := string(r[start:pos])
	if partial != "" && !identStart(r[start]) {
		return nil, 0 // a run starting with a digit is a number literal
	}
	if start > 0 && r[start-1] == '.' {
		if start > 1 && r[start-2] == '.' {
			return nil, 0 // `..` — keys at every depth are not a list to offer
		}
		steps, root, ok := parseChain(r, start-1, false)
		if !ok {
			return nil, 0
		}
		ctx, cok := contextAt(r, root, contextDepthCap)
		if !cok {
			return nil, 0
		}
		return filterPrefix(keyCandidates(in, append(ctx, steps...)), partial), start
	}
	if ctx, isKey, ok := constructionKeyContext(r, start); isKey {
		if !ok {
			return nil, 0
		}
		return filterPrefix(keyCandidates(in, ctx), partial), start
	}
	if partial == "" && !manual {
		return nil, 0
	}
	if start > 0 && (r[start-1] == '$' || r[start-1] == '@' || r[start-1] == '"') {
		return nil, 0 // variables, formats and string tails are not builtins
	}
	return filterPrefix(builtinCandidates(), partial), start
}

// filterPrefix keeps the candidates whose label starts with partial
// (case-insensitively — builtins are lowercase, keys are whatever the
// document says), capped at MaxCompletionItems.
func filterPrefix(items []Candidate, partial string) []Candidate {
	low := strings.ToLower(partial)
	var out []Candidate
	for _, it := range items {
		if partial != "" && !strings.HasPrefix(strings.ToLower(it.Label), low) {
			continue
		}
		out = append(out, it)
		if len(out) == MaxCompletionItems {
			break
		}
	}
	return out
}

// insideLiteral reports whether pos sits inside a string or comment token —
// the two places a `.` or an identifier is text, not syntax. The cursor
// counts as inside an unterminated string (the scanner runs it to the end of
// the line), and as outside just after a closing quote.
func insideLiteral(r []rune, pos int) bool {
	for _, t := range Tokens(string(r)) {
		if t.Start >= pos {
			return false
		}
		switch t.Kind {
		case KindComment:
			if pos <= t.End {
				return true
			}
		case KindString:
			if pos < t.End {
				return true
			}
			if pos == t.End && !(t.End-t.Start >= 2 && r[t.End-1] == '"') {
				return true
			}
		}
	}
	return false
}

// --- path chain ---

// stepKind classifies one segment of a field-access chain.
type stepKind int

const (
	// stepKey descends into an object field: `.foo`, `."a b"`, `.["a b"]`.
	stepKey stepKind = iota
	// stepIndex descends into one array element: `.[0]`, `.[-1]`.
	stepIndex
	// stepIter descends into every element: `.[]` over an array, every value
	// over an object.
	stepIter
)

// step is one parsed segment of the chain before the trigger dot.
type step struct {
	kind stepKind
	key  string
	idx  int
}

// bracketScanCap bounds the backwards scan for a bracket or quote opener; a
// bracket whose content is longer than a screenful is not a plain index.
const bracketScanCap = 256

// parseChain reads the field-access chain ending at rune index end
// (exclusive), right to left: called with the trigger dot's index over
// `.foo.bar[].` it yields [key foo, key bar, iter]. root is the rune index
// where the chain's text begins — its leading dot, or end itself when the
// chain is empty and the trigger dot after a boundary is the root (`| .`) —
// the position the pipeline context resolves at (contextAt). ok is false when
// the chain is not rooted at a path — `f(x).`, `env.`, `$v.` — because those
// keys cannot be read off the snapshot. needRootDot additionally demands an
// explicit leading `.`: a pipe segment must be a path expression of its own,
// while the trigger chain may be the bare dot itself.
func parseChain(r []rune, end int, needRootDot bool) (steps []step, root int, ok bool) {
	i := end // exclusive end of the unconsumed chain text
	rooted := false
	for i > 0 {
		switch c := r[i-1]; {
		case c == '?':
			// `.foo?.` — the optional marker changes nothing about the keys.
			i--
			rooted = false
		case c == ']':
			open := matchOpen(r, i-1)
			if open < 0 {
				return nil, 0, false
			}
			st, sok := bracketStep(r[open+1 : i-1])
			if !sok {
				return nil, 0, false
			}
			steps = append(steps, st)
			i = open
			rooted = false
		case c == '"':
			open := openingQuote(r, i-1)
			if open <= 0 || r[open-1] != '.' {
				return nil, 0, false
			}
			key, kok := unquote(string(r[open:i]))
			if !kok {
				return nil, 0, false
			}
			steps = append(steps, step{kind: stepKey, key: key})
			i = open - 1 // the segment's dot is consumed with it
			rooted = true
		case identRune(c):
			s := i
			for s > 0 && identRune(r[s-1]) {
				s--
			}
			if s == 0 || r[s-1] != '.' {
				return nil, 0, false // `env.`, `f(x).` — not rooted at the input
			}
			steps = append(steps, step{kind: stepKey, key: string(r[s:i])})
			i = s - 1 // the segment's dot is consumed with it
			rooted = true
		case c == '.':
			// The chain's own root (`.[0].` consumed down to its leading dot).
			// It must sit at the program start or after a boundary.
			if i-1 > 0 && !chainBoundary(r[i-2]) {
				return nil, 0, false
			}
			reverseSteps(steps)
			return steps, i - 1, true
		default:
			if needRootDot || !chainBoundary(c) {
				return nil, 0, false
			}
			// Boundary without an explicit root dot: the trigger dot itself
			// is the root, completing in the boundary's context.
			reverseSteps(steps)
			return steps, i, true
		}
	}
	// The chain reached the program start. It is dot-rooted when the last
	// consumed segment carried its own leading dot (`.foo`); a `[0]` or `?`
	// at index zero is a construction, not a path.
	if needRootDot && !rooted {
		return nil, 0, false
	}
	reverseSteps(steps)
	return steps, 0, true
}

// chainBoundary reports whether c may legitimately precede a root path
// expression — a pipe, a bracket, an operator, a space. A closing paren, a
// variable or a format marker means the dot indexes some computed value the
// snapshot cannot answer for.
func chainBoundary(c rune) bool {
	return c != ')' && c != '$' && c != '@'
}

// --- pipeline context (#2019) ---

// contextDepthCap bounds the recursion of the context analysis. Each level
// consumes at least one program rune leftwards, so the cap only guards
// degenerate nesting on a pathological query line.
const contextDepthCap = 32

// contextScanCap bounds the leftward scans of the context analysis (sibling
// expressions, paren matching); a query line stretch longer than this holds
// an expression the static walk should not guess about.
const contextScanCap = 512

// contextAt resolves the path context in effect for an expression beginning
// at rune index i: the steps leading from the input root to the value `.`
// denotes there — after `.[] |` one iterate step, inside `select(`'s argument
// whatever the enclosing pipeline position established. An empty step list is
// the root itself. ok is false when the context cannot be read statically —
// an unknown function's argument, a variable, a computed expression — in
// which case completion stays silent rather than offering keys that may not
// exist at that point.
func contextAt(r []rune, i, depth int) ([]step, bool) {
	if depth <= 0 {
		return nil, false
	}
	for i > 0 && spaceRune(r[i-1]) {
		i--
	}
	if i == 0 {
		return nil, true
	}
	switch c := r[i-1]; {
	case c == '|':
		// The preceding pipe segment establishes the context.
		return pipeContext(r, i-1, depth-1)
	case c == '(':
		// A function argument or a plain group. select runs its filter on
		// the current input; map and the by-functions on each element.
		word, s := wordBefore(r, i-1)
		switch word {
		case "select":
			return contextAt(r, s, depth-1)
		case "map", "map_values", "sort_by", "group_by", "unique_by", "min_by", "max_by":
			ctx, ok := contextAt(r, s, depth-1)
			if !ok {
				return nil, false
			}
			return append(ctx, step{kind: stepIter}), true
		default:
			// A bare group inherits its context; a keyword before the paren
			// resolves through the identifier case below, and an unknown
			// function name refuses there — its argument's input is not the
			// snapshot's to answer.
			return contextAt(r, i-1, depth-1)
		}
	case c == '{':
		// Object construction: every entry runs on the construction's input.
		return contextAt(r, i-1, depth-1)
	case c == '[':
		// Construction or index brackets: either way the expression inside
		// runs on the context enclosing the whole bracketed path (`.a[.b]`
		// evaluates `.b` on the same input as `.a`).
		_, root, ok := parseChain(r, i-1, false)
		if !ok {
			return nil, false
		}
		return contextAt(r, root, depth-1)
	case c == ':':
		// A construction value (`{x: .`) shares the construction's input; a
		// colon under any other opener (a slice) is not resolved.
		pos, ok := exprContextStart(r, i-1)
		if !ok || pos == 0 || r[pos-1] != '{' {
			return nil, false
		}
		return contextAt(r, pos-1, depth-1)
	case strings.ContainsRune(",+-*/%<>=!", c):
		// Operators and the comma hand every operand the same input: the
		// context enclosing the whole expression list. (`|=` lands on the
		// pipe stop, whose left side is exactly the update's path.)
		pos, ok := exprContextStart(r, i-1)
		if !ok {
			return nil, false
		}
		return contextAt(r, pos, depth-1)
	case identRune(c):
		// Only the operator-like keywords may precede a path expression and
		// keep its input; `as`, `reduce`, `catch` and any plain function
		// name mean a context the snapshot cannot answer for.
		word, s := wordBefore(r, i)
		switch word {
		case "and", "or", "if", "then", "elif", "else", "try":
			pos, ok := exprContextStart(r, s)
			if !ok {
				return nil, false
			}
			return contextAt(r, pos, depth-1)
		}
		return nil, false
	}
	return nil, false
}

// pipeContext resolves the context established by the pipe at rune index
// pipe: the segment left of it parsed as a simple path chain (`.foo`, `.[]?`)
// prepended onto that segment's own context, or a `select(...)` call passing
// its input through unchanged. Anything else — a function call, a variable,
// arithmetic, a construction — refuses: its output shape is not the
// snapshot's to answer.
func pipeContext(r []rune, pipe, depth int) ([]step, bool) {
	end := pipe
	for end > 0 && spaceRune(r[end-1]) {
		end--
	}
	if end == 0 {
		return nil, false
	}
	if r[end-1] == ')' {
		open := matchParen(r, end-1)
		if open < 0 {
			return nil, false
		}
		word, s := wordBefore(r, open)
		if word != "select" {
			return nil, false
		}
		return contextAt(r, s, depth)
	}
	steps, root, ok := parseChain(r, end, true)
	if !ok {
		return nil, false
	}
	ctx, ok := contextAt(r, root, depth)
	if !ok {
		return nil, false
	}
	return append(ctx, steps...), true
}

// exprContextStart scans left from rune index i (exclusive) over complete
// sibling expressions — balanced brackets and strings are skipped whole — to
// the position whose context the expression at i shares: just after the
// enclosing `|`, `(`, `{` or `[`, or the program start. A `;` at depth zero
// is the argument separator of a function the analysis does not model, and a
// scan past contextScanCap gives up.
func exprContextStart(r []rune, i int) (int, bool) {
	depth := 0
	k, n := i-1, 0
	for ; k >= 0 && n < contextScanCap; k, n = k-1, n+1 {
		switch r[k] {
		case ')', ']', '}':
			depth++
		case '(', '[', '{':
			if depth == 0 {
				return k + 1, true
			}
			depth--
		case '|':
			if depth == 0 {
				return k + 1, true
			}
		case ';':
			if depth == 0 {
				return 0, false
			}
		case '"':
			q := openingQuote(r, k)
			if q < 0 {
				return 0, false
			}
			k = q
		}
	}
	if k < 0 {
		return 0, true
	}
	return 0, false
}

// constructionKeyContext detects the object-construction key position — an
// identifier right after `{`, or after a `,` whose enclosing opener is `{` —
// where jq's shorthand (`{a}` means `{a: .a}`) makes the context value's keys
// the thing to offer instead of builtins. isKey reports the position, ok
// whether the construction's context resolved; an unresolved key position
// stays silent, it never offers functions.
func constructionKeyContext(r []rune, start int) (ctx []step, isKey, ok bool) {
	i := start
	for i > 0 && spaceRune(r[i-1]) {
		i--
	}
	if i == 0 {
		return nil, false, false
	}
	switch r[i-1] {
	case '{':
		ctx, ok = contextAt(r, i-1, contextDepthCap)
		return ctx, true, ok
	case ',':
		pos, sok := exprContextStart(r, i-1)
		if !sok || pos == 0 || r[pos-1] != '{' {
			return nil, false, false
		}
		ctx, ok = contextAt(r, pos-1, contextDepthCap)
		return ctx, true, ok
	}
	return nil, false, false
}

// matchParen finds the `(` opening the paren closed at rune index close,
// tracking nesting and skipping strings, within contextScanCap.
func matchParen(r []rune, close int) int {
	depth := 0
	for k, n := close-1, 0; k >= 0 && n < contextScanCap; k, n = k-1, n+1 {
		switch r[k] {
		case ')':
			depth++
		case '(':
			if depth == 0 {
				return k
			}
			depth--
		case '"':
			q := openingQuote(r, k)
			if q < 0 {
				return -1
			}
			k = q
		}
	}
	return -1
}

// wordBefore reads the identifier ending just left of rune index i (spaces
// skipped), returning it with its start index; empty when none.
func wordBefore(r []rune, i int) (string, int) {
	for i > 0 && spaceRune(r[i-1]) {
		i--
	}
	s := i
	for s > 0 && identRune(r[s-1]) {
		s--
	}
	if s == i || !identStart(r[s]) {
		return "", i
	}
	return string(r[s:i]), s
}

// spaceRune reports a plain blank the context scans skip over.
func spaceRune(c rune) bool { return c == ' ' || c == '\t' }

// matchOpen finds the `[` opening the bracket closed at rune index close,
// skipping over a quoted string inside (`.["a[b"]`). It refuses nesting and
// anything longer than bracketScanCap — such a bracket holds an expression,
// not an index.
func matchOpen(r []rune, close int) int {
	for k, n := close-1, 0; k >= 0 && n < bracketScanCap; k, n = k-1, n+1 {
		switch r[k] {
		case '[':
			return k
		case ']':
			return -1
		case '"':
			q := openingQuote(r, k)
			if q < 0 {
				return -1
			}
			k = q
		}
	}
	return -1
}

// openingQuote finds the `"` opening the string closed at rune index close;
// a quote preceded by a backslash is content, not the opener.
func openingQuote(r []rune, close int) int {
	for k, n := close-1, 0; k >= 0 && n < bracketScanCap; k, n = k-1, n+1 {
		if r[k] == '"' && (k == 0 || r[k-1] != '\\') {
			return k
		}
	}
	return -1
}

// bracketStep classifies a bracket's content: empty is iteration, an integer
// is an index, a quoted string is a key; anything else is an expression the
// static walk cannot follow.
func bracketStep(content []rune) (step, bool) {
	s := strings.TrimSpace(string(content))
	if s == "" {
		return step{kind: stepIter}, true
	}
	if n, err := strconv.Atoi(s); err == nil {
		return step{kind: stepIndex, idx: n}, true
	}
	if key, ok := unquote(s); ok {
		return step{kind: stepKey, key: key}, true
	}
	return step{}, false
}

// unquote decodes a quoted jq string literal (JSON escapes) into its value.
func unquote(q string) (string, bool) {
	var s string
	if err := json.Unmarshal([]byte(q), &s); err != nil {
		return "", false
	}
	return s, true
}

// reverseSteps flips the backwards-collected chain into document order.
func reverseSteps(steps []step) {
	for l, r := 0, len(steps)-1; l < r; l, r = l+1, r-1 {
		steps[l], steps[r] = steps[r], steps[l]
	}
}

// --- key extraction ---

// keyCandidates walks the parsed snapshot along steps and collects the
// object keys present at the end, each with its value's type as the detail.
// Arrays and objects are descended through `[]` steps; the node budget bounds
// the whole walk, so a huge snapshot yields the keys seen so far rather than
// a stalled keystroke. A key that is not a valid jq identifier inserts its
// quoted form.
func keyCandidates(in *Input, steps []step) []Candidate {
	if in == nil {
		return nil
	}
	budget := completionNodeBudget
	nodes := in.values
	for _, st := range steps {
		var next []any
		for _, n := range nodes {
			if budget <= 0 {
				break
			}
			budget--
			switch st.kind {
			case stepKey:
				if obj, ok := n.(map[string]any); ok {
					if v, exists := obj[st.key]; exists {
						next = append(next, v)
					}
				}
			case stepIndex:
				if arr, ok := n.([]any); ok {
					idx := st.idx
					if idx < 0 {
						idx += len(arr)
					}
					if 0 <= idx && idx < len(arr) {
						next = append(next, arr[idx])
					}
				}
			case stepIter:
				switch c := n.(type) {
				case []any:
					for _, v := range c {
						if budget <= 0 {
							break
						}
						budget--
						next = append(next, v)
					}
				case map[string]any:
					// `.[]` over an object iterates its values; sorted keys
					// keep the budget cut deterministic.
					for _, k := range sortedKeys(c) {
						if budget <= 0 {
							break
						}
						budget--
						next = append(next, c[k])
					}
				}
			}
		}
		nodes = next
	}
	// Collection runs on its own budget: descending a huge array may have
	// spent the walk's entirely, and ending up at the target nodes only to
	// read no keys off them would make the popup silently empty.
	budget = completionNodeBudget
	types := map[string]string{}
	for _, n := range nodes {
		if budget <= 0 {
			break
		}
		budget--
		obj, ok := n.(map[string]any)
		if !ok {
			continue
		}
		for k, v := range obj {
			if _, seen := types[k]; !seen {
				types[k] = gojq.TypeOf(v)
			}
		}
	}
	keys := make([]string, 0, len(types))
	for k := range types {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Candidate, 0, len(keys))
	for _, k := range keys {
		out = append(out, Candidate{Label: k, Insert: insertKey(k), Detail: types[k]})
	}
	return out
}

// sortedKeys returns obj's keys in order, for a deterministic iteration.
func sortedKeys(obj map[string]any) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// insertKey is the text accepting a key writes after the dot: the key itself
// when it is a valid jq identifier, its quoted form otherwise — `."a b"` is
// how jq spells that access.
func insertKey(k string) string {
	if identKey(k) {
		return k
	}
	b, err := json.Marshal(k)
	if err != nil {
		return k
	}
	return string(b)
}

// identKey reports whether k can follow a dot bare.
func identKey(k string) bool {
	for i, c := range k {
		if i == 0 && !identStart(c) {
			return false
		}
		if i > 0 && !identRune(c) {
			return false
		}
	}
	return k != ""
}

// --- builtins ---

// Builtin is one jq builtin function, with every arity it is defined for.
type Builtin struct {
	Name    string
	Arities []int
	// Doc is the one-line description, curated for the common builtins;
	// empty for the long tail.
	Doc string
}

// Builtins returns gojq's builtin functions, sorted by name. The list comes
// from gojq itself — its `builtins` builtin, evaluated once and memoized —
// so it names exactly what the playground's engine accepts, whatever version
// ships.
func Builtins() []Builtin { return builtinList() }

var builtinList = sync.OnceValue(func() []Builtin {
	q, err := gojq.Parse("builtins")
	if err != nil {
		return nil
	}
	v, _ := q.Run(nil).Next()
	arr, _ := v.([]any)
	byName := map[string][]int{}
	for _, e := range arr {
		s, _ := e.(string)
		name, arity, found := strings.Cut(s, "/")
		if !found || name == "" {
			continue
		}
		n, err := strconv.Atoi(arity)
		if err != nil {
			continue
		}
		byName[name] = append(byName[name], n)
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Builtin, 0, len(names))
	for _, name := range names {
		arities := byName[name]
		sort.Ints(arities)
		// The list must only offer what the playground's engine accepts: a
		// few entries (`input`, `debug/1`) name functions the plain compiler
		// rejects without special options the playground does not pass. One
		// probe compile per entry, once, filters them for whatever gojq
		// version ships.
		arities = compilable(name, arities)
		if len(arities) == 0 {
			continue
		}
		out = append(out, Builtin{Name: name, Arities: arities, Doc: builtinDocs[name]})
	}
	return out
})

// compilable keeps the arities at which a call to name actually compiles
// under the playground's plain gojq.Compile.
func compilable(name string, arities []int) []int {
	var out []int
	for _, arity := range arities {
		program := name
		if arity > 0 {
			program += "(" + strings.TrimRight(strings.Repeat(".;", arity), ";") + ")"
		}
		q, err := gojq.Parse(program)
		if err != nil {
			continue
		}
		if _, err := gojq.Compile(q); err != nil {
			continue
		}
		out = append(out, arity)
	}
	return out
}

// builtinCandidates renders the builtin list as candidates, the arity
// notation (`/1 /2`) as the detail — the same shape jq's own `builtins`
// prints, so it reads as jq to a jq user.
func builtinCandidates() []Candidate {
	bs := Builtins()
	out := make([]Candidate, 0, len(bs))
	for _, b := range bs {
		parts := make([]string, len(b.Arities))
		for i, n := range b.Arities {
			parts[i] = "/" + strconv.Itoa(n)
		}
		out = append(out, Candidate{
			Label:  b.Name,
			Insert: b.Name,
			Detail: strings.Join(parts, " "),
			Doc:    b.Doc,
		})
	}
	return out
}

// builtinDocs is the curated one-line description per common builtin, shown
// under the popup for the selected item — jq-manual phrasing, one line each.
// The long tail of builtins completes without a description; the arity detail
// still marks them as functions.
var builtinDocs = map[string]string{
	"add":            "sum / concatenate the elements of the input array",
	"all":            "true if every element (or f applied to it) is truthy",
	"any":            "true if some element (or f applied to it) is truthy",
	"ascii_downcase": "lowercase the ASCII letters of a string",
	"ascii_upcase":   "uppercase the ASCII letters of a string",
	"capture":        "match a regex, named groups as an object",
	"contains":       "true if the input completely contains the argument",
	"debug":          "pass the input through, printing it to stderr",
	"del":            "delete the paths matched by the path expression",
	"empty":          "produce no output at all",
	"endswith":       "true if the input string ends with the argument",
	"env":            "the environment variables as an object",
	"error":          "raise an error from the input or the argument",
	"explode":        "string to an array of codepoints",
	"first":          "the first element, or the first output of f",
	"flatten":        "flatten nested arrays (to the given depth)",
	"floor":          "round the number down",
	"from_entries":   "array of {key, value} objects back to an object",
	"fromjson":       "parse the input string as JSON",
	"getpath":        "the value at the given path array",
	"group_by":       "group array elements by f, sorted by the key",
	"gsub":           "replace every regex match in the string",
	"has":            "true if the object has the key / the array the index",
	"implode":        "array of codepoints to a string",
	"in":             "true if the input is a key of the argument",
	"index":          "the first index at which the argument occurs",
	"indices":        "every index at which the argument occurs",
	"input":          "consume and output the next input value",
	"inside":         "true if the input is completely contained",
	"isnan":          "true if the number is NaN",
	"join":           "join array elements with the separator string",
	"keys":           "the object's keys sorted / the array's indices",
	"keys_unsorted":  "the object's keys in document order",
	"last":           "the last element, or the last output of f",
	"length":         "elements, keys, codepoints or absolute value",
	"limit":          "at most n outputs of f",
	"ltrimstr":       "strip the prefix string when present",
	"map":            "apply f to every element of the array",
	"map_values":     "apply f to every value of the object",
	"match":          "regex match details as objects",
	"max":            "the maximum element of the array",
	"max_by":         "the element with the maximum f",
	"min":            "the minimum element of the array",
	"min_by":         "the element with the minimum f",
	"not":            "logical negation of the input",
	"now":            "the current time in seconds since the epoch",
	"path":           "the path array of the path expression's outputs",
	"paths":          "every path in the input (matching f)",
	"range":          "numbers from (0 or) lower up to the bound",
	"recurse":        "the input and all its descendants (via f)",
	"repeat":         "apply f to its own output, forever",
	"reverse":        "reverse the array or string",
	"rindex":         "the last index at which the argument occurs",
	"rtrimstr":       "strip the suffix string when present",
	"scan":           "every regex match as a string (or groups)",
	"select":         "keep the input when f is truthy, else nothing",
	"sort":           "sort the array",
	"sort_by":        "sort the array by f",
	"split":          "split a string on a separator (or regex)",
	"splits":         "the pieces of a regex split, streamed",
	"sqrt":           "square root of the number",
	"startswith":     "true if the input string starts with the argument",
	"sub":            "replace the first regex match in the string",
	"test":           "true if the regex matches the string",
	"to_entries":     "object to an array of {key, value} objects",
	"todate":         "seconds since the epoch to an ISO 8601 string",
	"fromdate":       "ISO 8601 string to seconds since the epoch",
	"tojson":         "render the input as a JSON string",
	"tonumber":       "parse the input as a number",
	"tostring":       "the input as a string (JSON for non-strings)",
	"type":           "the input's type name as a string",
	"unique":         "sort the array and drop duplicates",
	"unique_by":      "keep one element per distinct f",
	"until":          "apply f until cond becomes true",
	"values":         "the object's / array's values, or non-null inputs",
	"while":          "emit the input while cond holds, applying f",
	"with_entries":   "map f over the object's {key, value} entries",

	// #2382 grew the list. The cheatsheet lists *every* builtin, so a name
	// without a sentence is a visible blank rather than a quiet omission in a
	// popup nobody opens on it; these are the ones a reader browsing the
	// reference actually reaches for. The long tail that is left — the libm
	// wrappers (`j0`, `lgamma`, `nexttoward`), `modulemeta` — is still listed
	// with its arities and no description, which is the honest state of it.
	"abs":             "the absolute value of the number",
	"arrays":          "keep the input only when it is an array",
	"booleans":        "keep the input only when it is true or false",
	"bsearch":         "binary-search the sorted array, ~insertion point when absent",
	"builtins":        "the names of every builtin, as name/arity strings",
	"ceil":            "round the number up",
	"combinations":    "one output per combination of the input's iterables",
	"delpaths":        "delete every path in the given array of paths",
	"exp":             "e raised to the number",
	"finites":         "keep the input only when it is a finite number",
	"format":          "apply a @format (text, json, csv, sh, …) by name",
	"fromdateiso8601": "ISO 8601 string to seconds since the epoch",
	"fromstream":      "rebuild values from a streamed-event sequence",
	"gmtime":          "seconds since the epoch to a UTC broken-down time array",
	"halt":            "stop the program right now, without an error",
	"halt_error":      "stop the program, reporting the input as the error",
	"infinite":        "the number larger than any other",
	"isempty":         "true if f produces no output at all",
	"isfinite":        "true if the number is neither infinite nor NaN",
	"isinfinite":      "true if the number is an infinity",
	"isnormal":        "true if the number is normal (not zero, NaN or infinite)",
	"iterables":       "keep the input only when it is an array or object",
	"localtime":       "seconds since the epoch to a local broken-down time array",
	"log":             "the natural logarithm of the number",
	"log10":           "the base-10 logarithm of the number",
	"log2":            "the base-2 logarithm of the number",
	"ltrim":           "strip leading whitespace from the string",
	"mktime":          "a broken-down UTC time array back to seconds since the epoch",
	"nan":             "the not-a-number value",
	"normals":         "keep the input only when it is a normal number",
	"nth":             "the nth element, or the nth output of f",
	"nulls":           "keep the input only when it is null",
	"numbers":         "keep the input only when it is a number",
	"objects":         "keep the input only when it is an object",
	"pick":            "keep only the paths the path expression matched",
	"pow":             "the first argument raised to the second",
	"round":           "round the number to the nearest integer",
	"rtrim":           "strip trailing whitespace from the string",
	"scalars":         "keep the input only when it is not an array or object",
	"setpath":         "set the value at the given path array",
	"skip":            "drop the first n outputs of f",
	"strftime":        "format a broken-down time array with a strftime pattern",
	"strings":         "keep the input only when it is a string",
	"strptime":        "parse a string into a broken-down time array",
	"todateiso8601":   "seconds since the epoch to an ISO 8601 string",
	"toboolean":       "parse the input string as a boolean",
	"tostream":        "stream the input as [path, leaf] events",
	"transpose":       "transpose the array of arrays, padding with null",
	"trim":            "strip whitespace from both ends of the string",
	"trimstr":         "strip the argument from both ends when present",
	"trunc":           "drop the number's fractional part",
	"truncate_stream": "drop the first n path components of a stream",
	"utf8bytelength":  "the string's length in UTF-8 bytes",
	"walk":            "apply f to every value of the input, bottom up",
	"IN":              "true if the input equals one of the source's outputs",
	"INDEX":           "index the stream into an object keyed by the expression",
	"JOIN":            "join the stream against an index object",
}
