package jqplay

// cheatsheet.go is the playground's **language** reference (#2382). The
// keyboard has had one since #2237 (internal/app/playhelp.go); the language
// had none. Someone who does not already know jq could open the playground,
// see a query line and have no way to find out that `group_by` exists, let
// alone what a program using it looks like — the completion popup (#1979)
// only offers what you have already half-typed, so it finds what you know and
// nothing else.
//
// The sheet is three sections, and the split is deliberate:
//
//   - **syntax** — the parts of the language that are *not* functions and
//     therefore appear in no builtin list: the pipe, `.[]`, `.[]?`, slices,
//     object and array construction, `//`, string interpolation, `as`,
//     `reduce`, `if`, the update operators.
//   - **everyday programs** — one-line, complete programs for the operations
//     people actually open a playground for: pick a field, iterate, filter,
//     map, sort, group, rebuild an object, count, walk nested paths, default
//     a missing value, interpolate a string.
//   - **builtins** — every function gojq accepts, with its arities and, where
//     one is curated, its one-line description.
//
// The builtin section is **generated from Builtins()/builtinDocs**, never
// hand-listed here: a second list would drift from the engine's the first
// time gojq gains a function, and the whole point of the sheet is that it
// tells the truth about *this* playground. The long tail without a curated
// description is still listed — knowing that `truncate_stream` exists is
// worth more than the blank where its sentence would go, and the arity note
// still marks it as a function — but #2382 also grew builtinDocs by the
// commonly reached-for names it was missing, so the blanks are rare.
//
// Every entry with a program is checked by cheatsheet_test.go against
// Sample(d), the sample document that lives next to it in this package: a
// program in the sheet that does not compile — or that errors on a document
// of exactly the shape the sheet describes — fails the build. That is what
// keeps a typo here from becoming permanent, and it is why the examples are
// written against one small document rather than against prose.
//
// Both dialects are served from one list. yq speaks jq for everything typed
// into a query line, so almost every entry is shared; the handful that are
// genuinely about the *document language* — how a stream is separated, what
// an alias does, how a number's spelling survives — carry a dialect and only
// that dialect's session sees them. Never both side by side: a yq user has no
// use for a `.jsonl` sentence, and showing it would make the sheet something
// to filter rather than something to read.

import (
	"strconv"
	"strings"
)

// CheatKind classifies one cheatsheet row. It is what decides how the row is
// grouped, and — because a complete program and a bare function name are not
// the same thing to insert — what accepting the row does to the query line.
type CheatKind int

// The three kinds. The order is the order the sheet lists them in: the
// syntax first (it is the part no completion popup can offer), then the
// programs, then the reference.
const (
	// CheatSyntax is a language construct that is not a function.
	CheatSyntax CheatKind = iota
	// CheatExample is a complete one-line program for an everyday operation.
	CheatExample
	// CheatBuiltin is one function of the engine's own builtin list.
	CheatBuiltin
)

// String is the kind's badge in the picker — the short word that tells a
// reference row from a program worth copying.
func (k CheatKind) String() string {
	switch k {
	case CheatSyntax:
		return "syntax"
	case CheatExample:
		return "example"
	default:
		return "builtin"
	}
}

// Complete reports whether the entry's Program is a whole program rather than
// a fragment. A syntax entry and an example are written to be *run*; a
// builtin row carries only its name, which belongs at the caret of whatever
// program is already on the query line.
func (k CheatKind) Complete() bool { return k != CheatBuiltin }

// CheatEntry is one row of the sheet. Title is what the row is searched by —
// the operation for a syntax or example row ("sort an array by a field"), the
// function's name for a builtin, since that is what a reader is looking for
// in each case. Program is the one-liner (the function's bare name for a
// builtin), Doc the one-line explanation, Arity the `/1 /2` note builtins
// carry and nothing else does.
type CheatEntry struct {
	Kind    CheatKind
	Title   string
	Program string
	Doc     string
	Arity   string
}

// Cheatsheet returns the whole sheet for one dialect: syntax, then the
// everyday programs, then every builtin the engine accepts. The rows are in
// reading order — a caller that ranks them (the picker fuzzy-matches) may
// reorder freely; a caller that just prints them gets a sheet.
func Cheatsheet(d Dialect) []CheatEntry {
	out := make([]CheatEntry, 0, len(cheatSyntax)+len(cheatExamples)+len(Builtins()))
	out = append(out, cheatRows(d, cheatSyntax)...)
	out = append(out, cheatRows(d, cheatExamples)...)
	for _, b := range Builtins() {
		out = append(out, CheatEntry{
			Kind:    CheatBuiltin,
			Title:   b.Name,
			Program: b.Name,
			Doc:     b.Doc,
			Arity:   arityNote(b.Arities),
		})
	}
	return out
}

// cheatRow is one authored row before the dialect filter is applied. only
// pins a row to one document language (nil = both); the two dialect-specific
// spellings of the same idea are written as two rows rather than as one row
// with a branch, because they differ in their wording as much as in their
// program.
type cheatRow struct {
	kind    CheatKind
	only    *Dialect
	title   string
	program string
	doc     string
}

// cheatRows keeps the rows that apply to d and turns them into entries.
func cheatRows(d Dialect, rows []cheatRow) []CheatEntry {
	out := make([]CheatEntry, 0, len(rows))
	for _, r := range rows {
		if r.only != nil && *r.only != d {
			continue
		}
		out = append(out, CheatEntry{Kind: r.kind, Title: r.title, Program: r.program, Doc: r.doc})
	}
	return out
}

// jqOnly / yqOnly are the addressable dialect values cheatRow.only points at.
var (
	jqOnly = DialectJQ
	yqOnly = DialectYQ
)

// arityNote renders a builtin's arities the way jq's own `builtins` prints
// them — `/0 /1` — which is also what the completion popup shows, so the two
// views of the same function agree.
func arityNote(arities []int) string {
	parts := make([]string, len(arities))
	for i, n := range arities {
		parts[i] = "/" + strconv.Itoa(n)
	}
	return strings.Join(parts, " ")
}

// cheatSyntax is the language that is not a function: the operators and forms
// a builtin list can never mention. Every program here runs against Sample.
var cheatSyntax = []cheatRow{
	{kind: CheatSyntax, title: "the input itself", program: ".", doc: "the identity filter — the program the playground opens on"},
	{kind: CheatSyntax, title: "a field of an object", program: ".meta", doc: "`.name` reads a key; a missing key is null, not an error"},
	{kind: CheatSyntax, title: "a nested path", program: ".meta.page", doc: "field access chains — `.a.b.c` walks down"},
	{kind: CheatSyntax, title: "a key that is not an identifier", program: `.meta["total"]`, doc: `bracket form for keys with spaces or punctuation — also ."two words"`},
	{kind: CheatSyntax, title: "iterate an array or object", program: ".users[]", doc: "`.[]` emits every element — one output per element, not an array"},
	{kind: CheatSyntax, title: "one element by index", program: ".users[0]", doc: "negative indexes count from the end: `.users[-1]`"},
	{kind: CheatSyntax, title: "a slice of an array", program: ".users[1:3]", doc: "`.[from:to]` — the end is exclusive, both ends optional"},
	{kind: CheatSyntax, title: "tolerate the wrong shape", program: ".users[]?", doc: "`?` suppresses the error when the input cannot be iterated or indexed"},
	{kind: CheatSyntax, title: "pipe one filter into the next", program: ".users[] | .name", doc: "`|` feeds every output of the left side into the right side"},
	{kind: CheatSyntax, title: "two outputs per input", program: ".users[] | .name, .age", doc: "`,` runs both filters on the same input and emits both results"},
	{kind: CheatSyntax, title: "keep only what matches", program: ".users[] | select(.active)", doc: "`select(f)` passes the input through when f is truthy, else nothing"},
	{kind: CheatSyntax, title: "build an object", program: "{page: .meta.page, users: (.users | length)}", doc: "`{key: filter}` — `{name}` is short for `{name: .name}`"},
	{kind: CheatSyntax, title: "collect outputs into an array", program: "[.users[].name]", doc: "`[…]` gathers everything the filter emits into one array"},
	{kind: CheatSyntax, title: "supply a default", program: `.meta.author // "unknown"`, doc: "`//` takes the right side when the left is null, false or empty"},
	{kind: CheatSyntax, title: "interpolate a string", program: `"page \(.meta.page) of \(.meta.total)"`, doc: `\(…) embeds a filter's result in a string literal`},
	{kind: CheatSyntax, title: "a conditional", program: `if .meta.page == 1 then "first" else "rest" end`, doc: "`if … then … elif … else … end`; the else branch may be left out"},
	{kind: CheatSyntax, title: "catch an error", program: `.meta.raw | try fromjson catch "not JSON"`, doc: "`try f catch g` — g sees the error message as its input"},
	{kind: CheatSyntax, title: "bind a value to a variable", program: ".meta.total as $n | .users | length == $n", doc: "`f as $x | g` — $x is in scope for the whole right side"},
	{kind: CheatSyntax, title: "fold a stream into one value", program: "reduce .users[].age as $a (0; . + $a)", doc: "`reduce f as $x (init; update)` — the accumulator is the update's input"},
	{kind: CheatSyntax, title: "set a path", program: ".meta.page = 2", doc: "`=` assigns a constant; the whole input comes back changed"},
	{kind: CheatSyntax, title: "update a path from its own value", program: ".users[].age |= . + 1", doc: "`|=` runs the filter on the old value at that path — `+=` and friends too"},
	{kind: CheatSyntax, title: "every value at any depth", program: "[.. | numbers]", doc: "`..` visits the input and all its descendants — `.. | .a?` is the deep search"},
	{kind: CheatSyntax, title: "arithmetic and comparison", program: ".users[] | .age > 40", doc: "`+ - * / %` and `== != < <= > >=` work on the obvious types; `+` also concatenates"},
	{kind: CheatSyntax, title: "a path as data", program: `getpath(["meta", "page"])`, doc: "paths are arrays of keys and indexes — `path(f)` produces them, `getpath`/`setpath` use them"},

	// The document-language rows. Each dialect gets its own spelling: the
	// separator, and what "several documents in one buffer" means, is the one
	// thing about the playground that is genuinely not shared.
	{kind: CheatSyntax, only: &jqOnly, title: "several values in one buffer", program: ".", doc: "a JSON stream (a `.jsonl` export, a concatenated body) runs the program over every value; the results print one per line"},
	{kind: CheatSyntax, only: &yqOnly, title: "several documents in one file", program: ".", doc: "`---` separates YAML documents; the program runs over each one and the results come back `---`-separated"},
	{kind: CheatSyntax, only: &jqOnly, title: "a number keeps its exact spelling", program: ".meta.id", doc: "numbers are read as written, so a 19-digit id survives the round trip instead of rounding"},
	{kind: CheatSyntax, only: &yqOnly, title: "aliases and merge keys are resolved", program: ".service.retries", doc: "the decoder expands `*alias` and folds `<<:` before the program runs — the value, not the reference, is what you query"},
}

// cheatExamples is the everyday-program section: the operations the issue's
// own list names, one complete line each. They read as a recipe book — the
// point is not that `map` exists but that `.users | map(.name)` is what
// "pull one field out of every element" looks like.
var cheatExamples = []cheatRow{
	{kind: CheatExample, title: "pick one field", program: ".meta.total", doc: "the shortest useful program: read a value out of the document"},
	{kind: CheatExample, title: "iterate an array", program: ".users[]", doc: "one output per element — the start of most pipelines"},
	{kind: CheatExample, title: "filter elements", program: ".users[] | select(.age > 40)", doc: "keep the elements a condition holds for"},
	{kind: CheatExample, title: "filter and keep the array", program: ".users | map(select(.age > 40))", doc: "`map(select(…))` filters *inside* the array instead of unwrapping it"},
	{kind: CheatExample, title: "map one field out of every element", program: ".users | map(.name)", doc: "`map(f)` applies f to every element and rebuilds the array"},
	{kind: CheatExample, title: "sort by a field", program: ".users | sort_by(.age)", doc: "`sort_by(f)` orders by f's value; `sort` orders the values themselves"},
	{kind: CheatExample, title: "sort descending", program: ".users | sort_by(.age) | reverse", doc: "there is no descending sort — sort, then reverse"},
	{kind: CheatExample, title: "group by a field", program: ".users | group_by(.active)", doc: "an array of groups, each group an array, ordered by the key"},
	{kind: CheatExample, title: "count per group", program: ".users | group_by(.active) | map({active: .[0].active, n: length})", doc: "group, then measure each group — the histogram shape"},
	{kind: CheatExample, title: "count values", program: "[.users[].tags[]] | group_by(.) | map({tag: .[0], n: length})", doc: "collect, group by the value itself, count each group"},
	{kind: CheatExample, title: "count elements", program: ".users | length", doc: "`length` is elements, keys, codepoints or absolute value, by type"},
	{kind: CheatExample, title: "rebuild an object", program: ".users | map({user: .name, years: .age})", doc: "rename and drop fields by constructing the object you want"},
	{kind: CheatExample, title: "object to a lookup table", program: ".users | map({key: .name, value: .age}) | from_entries", doc: "`from_entries` turns {key, value} pairs into an object — `to_entries` goes back"},
	{kind: CheatExample, title: "the keys of an object", program: ".counts | keys", doc: "sorted; `keys_unsorted` keeps the document's own order"},
	{kind: CheatExample, title: "map over an object's values", program: ".counts | map_values(. * 10)", doc: "keys stay, values are rewritten — `with_entries` reaches the keys too"},
	{kind: CheatExample, title: "drop a field", program: "del(.meta.raw)", doc: "`del(path)` removes what the path expression matched"},
	{kind: CheatExample, title: "walk a nested path", program: ".users[].tags[]", doc: "iterate, then iterate again — nesting is just more `[]`"},
	{kind: CheatExample, title: "flatten and deduplicate", program: "[.users[].tags[]] | unique", doc: "`unique` sorts and drops duplicates; `unique_by(f)` keeps one per key"},
	{kind: CheatExample, title: "find a field at any depth", program: `[.. | objects | select(has("name")) | .name]`, doc: "`..` plus a type guard is the search when you do not know the path"},
	{kind: CheatExample, title: "sum numbers", program: "[.users[].age] | add", doc: "`add` sums numbers, concatenates strings and arrays, merges objects"},
	{kind: CheatExample, title: "the largest element", program: ".users | max_by(.age) | .name", doc: "`min_by`/`max_by` return the element, not the key"},
	{kind: CheatExample, title: "join into one string", program: `.users | map(.name) | join(", ")`, doc: "`join` needs strings — `map(tostring)` first if they are not"},
	{kind: CheatExample, title: "a line per element", program: `.users[] | "\(.name) is \(.age)"`, doc: "interpolation is how a document becomes a report"},
	{kind: CheatExample, title: "match a regular expression", program: `.users[] | select(.name | test("^a"))`, doc: "`test` is the boolean; `match`, `capture`, `scan`, `sub`, `gsub` do the rest"},
	{kind: CheatExample, title: "default a missing value", program: `.users[] | {name, team: (.team // "none")}`, doc: "`//` inside a construction fills the gaps a partial document leaves"},
	{kind: CheatExample, title: "the first few outputs", program: "limit(2; .users[])", doc: "`limit(n; f)` stops f after n outputs — `first(f)` is limit(1; f)"},
	{kind: CheatExample, title: "every path in the document", program: "[paths] | length", doc: "`paths` enumerates the document's structure; `paths(scalars)` only the leaves"},

	// Dialect-specific programs: the same two functions, but what they are
	// *for* differs enough that one wording would be wrong in both sessions.
	{kind: CheatExample, only: &jqOnly, title: "a JSON string field as data", program: ".meta.raw | fromjson", doc: "`fromjson` parses a string that holds JSON — the escaped-payload case"},
	{kind: CheatExample, only: &yqOnly, title: "a JSON string inside the YAML", program: ".meta.raw | fromjson", doc: "`fromjson` parses a quoted scalar that holds JSON — the embedded-config case"},
	{kind: CheatExample, only: &jqOnly, title: "compact the value onto one line", program: "tojson", doc: "`tojson` renders the value as a JSON string — the one-line form of a pretty result"},
	{kind: CheatExample, only: &yqOnly, title: "render the value as JSON", program: "tojson", doc: "`tojson` escapes the value into a JSON string, since the result buffer writes YAML"},
}

// cheatSampleJSON is the document the jq sheet's programs are written — and
// tested — against. It is deliberately small and boring: three records with a
// number, a boolean and an array field, a `meta` object with a JSON-in-JSON
// string and an id too long for a float, and a counts object to demonstrate
// the object-shaped operations. Every field it has is used by at least one
// entry, and every entry only uses fields it has.
const cheatSampleJSON = `{
  "users": [
    {"name": "ada",   "age": 36, "tags": ["math", "eng"],  "active": true},
    {"name": "linus", "age": 54, "tags": ["kernel"],       "active": false},
    {"name": "grace", "age": 45, "tags": ["navy", "eng"],  "active": true}
  ],
  "meta": {"page": 1, "total": 3, "id": 9007199254740993, "raw": "{\"ok\": true}"},
  "counts": {"eng": 2, "kernel": 1, "math": 1, "navy": 1}
}`

// cheatSampleYAML is the same document in YAML, plus the two things only YAML
// has: an anchor and a merge key, which the yq-only entries are about. The
// shared shape is what lets one list of programs serve both dialects.
const cheatSampleYAML = `defaults: &defaults
  retries: 3
  timeout: 30

users:
  - name: ada
    age: 36
    tags: [math, eng]
    active: true
  - name: linus
    age: 54
    tags: [kernel]
    active: false
  - name: grace
    age: 45
    tags: [navy, eng]
    active: true

meta:
  page: 1
  total: 3
  raw: '{"ok": true}'

counts:
  eng: 2
  kernel: 1
  math: 1
  navy: 1

service:
  <<: *defaults
  name: api
`

// Sample is the cheatsheet's example document in the dialect's own language —
// what every program in the sheet is written against, and what the sheet's
// test evaluates them over. The picker shows it as the sheet's preamble, so
// a reader can see where `.users` and `.meta.page` come from instead of
// having to guess which document the examples imagine.
func Sample(d Dialect) string {
	if d == DialectYQ {
		return cheatSampleYAML
	}
	return cheatSampleJSON
}
