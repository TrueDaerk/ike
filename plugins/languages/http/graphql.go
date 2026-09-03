package langhttp

import (
	"strings"

	"ike/internal/graphql"
	"ike/internal/httpfile"
	"ike/internal/lang"
)

// graphql.go is the editor half of the `GRAPHQL <url>` block (#2423): where
// its two sections are, how they are highlighted, and — through
// graphqlSchemaFor — which cached schema the completion source answers from.
//
// Highlighting takes whichever of two routes this build offers. When a
// "graphql" language is registered (a tree-sitter grammar for it linked in),
// the query section becomes an embedded region and that grammar paints it, the
// way a JSON body is painted by the JSON grammar. When it is not — which is
// every build today — the minimal lexer below paints the same lines: keywords,
// type conditions, "$variables", field names, arguments, strings and numbers.
// The two never overlap: the lexer stands down as soon as a region claims the
// lines.

// graphQLRegions claims the two sections of a GRAPHQL block: the query text
// and the JSON variables object. The block has no Content-Type of its own to
// resolve — the envelope's application/json describes the *wire* body, not
// what is written here — so the sections are claimed directly.
func graphQLRegions(spec *httpfile.GraphQLSpec, lines []string) []lang.Region {
	var out []lang.Region
	if id, ok := resolveTag("graphql"); ok {
		if r, ok := lineRegion(id, spec.QueryStart, spec.QueryEnd, lines); ok {
			out = append(out, r)
		}
	}
	if id, ok := resolveTag("json"); ok {
		if r, ok := lineRegion(id, spec.VarsStart, spec.VarsEnd, lines); ok {
			out = append(out, r)
		}
	}
	return out
}

// lineRegion converts a 1-based inclusive line range into a region covering
// those whole lines; a range that is absent (0) or out of bounds yields none.
func lineRegion(id string, from, to int, lines []string) (lang.Region, bool) {
	if from == 0 || to == 0 {
		return lang.Region{}, false
	}
	start, end := from-1, to-1
	if start < 0 || end < start || end >= len(lines) {
		return lang.Region{}, false
	}
	return lang.Region{Lang: id, StartLine: start, EndLine: end, EndCol: len(lines[end])}, true
}

// graphQLHighlighted reports whether a registered "graphql" language claims a
// GRAPHQL block's query section in this build.
func graphQLHighlighted() bool {
	_, ok := resolveTag("graphql")
	return ok
}

// graphQLKeywords are the words the lexer paints as keywords. "on" is included
// for the `... on Type` type condition; "true"/"false"/"null" are the literal
// constants an argument value may hold.
var graphQLKeywords = map[string]string{
	"query": "keyword", "mutation": "keyword", "subscription": "keyword",
	"fragment": "keyword", "on": "keyword",
	"true": "constant.builtin", "false": "constant.builtin", "null": "constant.builtin",
}

// graphQLSpans is the minimal lexer (#2423): one pass per query line of every
// GRAPHQL block, emitting the captures the themes already style. It runs only
// where no grammar took over, so a build that later vendors tree-sitter-graphql
// silently switches to it without a change here. f is the caller's own parse of
// the buffer — the span producer already has one, and a second would double the
// per-keystroke cost of every request file.
func graphQLSpans(f *httpfile.File, lines []string) []lang.Span {
	if graphQLHighlighted() {
		return nil
	}
	var out []lang.Span
	for _, r := range f.Requests {
		if r.GraphQL == nil || r.GraphQL.QueryStart == 0 || r.BodyFile != "" {
			continue
		}
		for li := r.GraphQL.QueryStart - 1; li < r.GraphQL.QueryEnd && li < len(lines); li++ {
			if li < 0 {
				continue
			}
			out = append(out, graphQLLineSpans(li, lines[li])...)
		}
	}
	return out
}

// graphQLLineSpans lexes one query line. Columns are rune offsets, which for
// the ASCII a query is written in equal byte offsets — except inside a string
// or a comment, both of which are emitted as one span, so a multi-byte rune
// there can never split one.
func graphQLLineSpans(li int, line string) []lang.Span {
	runes := []rune(line)
	var out []lang.Span
	for i := 0; i < len(runes); {
		c := runes[i]
		switch {
		case c == '#':
			out = append(out, lang.Span{Line: li, StartCol: i, EndCol: len(runes), Capture: "comment"})
			return out
		case c == '"':
			j := i + 1
			for j < len(runes) && runes[j] != '"' {
				if runes[j] == '\\' {
					j++
				}
				j++
			}
			end := min(j+1, len(runes))
			out = append(out, lang.Span{Line: li, StartCol: i, EndCol: end, Capture: "string"})
			i = end
		case c == '$':
			j := i + 1
			for j < len(runes) && isGraphQLNameRune(runes[j]) {
				j++
			}
			out = append(out, lang.Span{Line: li, StartCol: i, EndCol: j, Capture: "variable"})
			i = j
		case c == '@':
			j := i + 1
			for j < len(runes) && isGraphQLNameRune(runes[j]) {
				j++
			}
			out = append(out, lang.Span{Line: li, StartCol: i, EndCol: j, Capture: "attribute"})
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < len(runes) && (runes[j] == '.' || runes[j] == '-' || runes[j] == '+' ||
				(runes[j] >= '0' && runes[j] <= '9') || runes[j] == 'e' || runes[j] == 'E') {
				j++
			}
			out = append(out, lang.Span{Line: li, StartCol: i, EndCol: j, Capture: "number"})
			i = j
		case isGraphQLNameStart(c):
			j := i
			for j < len(runes) && isGraphQLNameRune(runes[j]) {
				j++
			}
			out = append(out, lang.Span{Line: li, StartCol: i, EndCol: j,
				Capture: graphQLNameCapture(string(runes[i:j]), runes, j)})
			i = j
		case strings.ContainsRune("{}()[]:!=|&", c):
			out = append(out, lang.Span{Line: li, StartCol: i, EndCol: i + 1, Capture: "punctuation"})
			i++
		default:
			i++
		}
	}
	return out
}

// graphQLNameCapture classifies one name: a keyword, an argument name (a name
// followed by ":"), a type name (capitalised, the convention every schema
// follows) or a field.
func graphQLNameCapture(name string, runes []rune, end int) string {
	if capture, ok := graphQLKeywords[name]; ok {
		return capture
	}
	for j := end; j < len(runes); j++ {
		if runes[j] == ' ' || runes[j] == '\t' {
			continue
		}
		if runes[j] == ':' {
			return "property" // "argument:" / "alias:"
		}
		break
	}
	if c := rune(name[0]); c >= 'A' && c <= 'Z' {
		return "type"
	}
	return "function"
}

func isGraphQLNameStart(c rune) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isGraphQLNameRune(c rune) bool { return isGraphQLNameStart(c) || (c >= '0' && c <= '9') }

// graphqlSchemaFor resolves the cached schema a request file's block completes
// against (#2423): the block's own endpoint, or the project's single cached
// schema when the target still carries an unresolved `{{host}}` — which is the
// common shape of a request file with one endpoint.
func graphqlSchemaFor(target string) (*graphql.Schema, bool) {
	return graphql.NewCache().For(target)
}
