package langhttp

import (
	"sort"
	"strings"

	"ike/internal/graphql"
	"ike/internal/httpfile"
	ilsp "ike/internal/lsp"
)

// graphql_complete.go answers completion inside a GRAPHQL block's query
// section (#2423) from the schema `http.graphqlIntrospect` cached for the
// block's endpoint. Without it the query section is the one place in a request
// file where nothing at all completes — and it is the place where the names
// are least guessable, since they come from a schema the file never mentions.
//
// Nothing here dispatches: the cache is the whole input, so a completion never
// waits on a network round-trip. No cached schema simply means no items, which
// closes the popup rather than filling it with noise.

// graphQLQueryAt reports whether line (0-based) sits inside the *query* section
// of a GRAPHQL block, and returns the block. The variables object below it is
// JSON and completes nothing schema-aware; the request line and headers are the
// ordinary .http contexts.
func graphQLQueryAt(lines []string, line int) (*httpfile.Request, bool) {
	f := httpfile.Parse(strings.Join(lines, "\n"))
	for _, r := range f.Requests {
		if r.GraphQL == nil || r.GraphQL.QueryStart == 0 || r.BodyFile != "" {
			continue
		}
		if line+1 >= r.GraphQL.QueryStart && line+1 <= r.GraphQL.QueryEnd {
			return r, true
		}
	}
	return nil, false
}

// graphQLItems is the completion batch for a caret inside a query section.
// lines is the whole buffer, line/before the caret's line and the text on it up
// to the caret.
func graphQLItems(req *httpfile.Request, lines []string, line int, before string) []ilsp.CompletionItem {
	schema, ok := graphqlSchemaFor(req.Target)
	if !ok {
		return nil
	}
	caret := graphql.Analyze(schema, graphQLTextBefore(req, lines, line, before))
	switch caret.Kind {
	case graphql.CaretField:
		return graphQLFieldItems(schema, caret)
	case graphql.CaretArgument:
		return graphQLArgumentItems(schema, caret)
	case graphql.CaretType:
		return graphQLTypeItems(schema, caret)
	}
	return nil
}

// graphQLTextBefore assembles the query text from its first line up to the
// caret — the input the position walk needs. A query is analysed from its
// start every time rather than incrementally: it is a handful of lines, and
// anything cached would have to be invalidated on every keystroke anyway.
func graphQLTextBefore(req *httpfile.Request, lines []string, line int, before string) string {
	start := req.GraphQL.QueryStart - 1
	if start < 0 || line < start || line >= len(lines) {
		return before
	}
	var b strings.Builder
	for i := start; i < line; i++ {
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}
	b.WriteString(before)
	return b.String()
}

// graphQLFieldItems offers the fields of the selection set's type, plus the
// "__typename" meta field every selection set accepts. The detail column
// carries the field's own type, so the popup answers "and what is that?" in the
// same glance.
func graphQLFieldItems(s *graphql.Schema, caret graphql.Caret) []ilsp.CompletionItem {
	t, ok := s.TypeByName(caret.Type)
	if !ok {
		return nil
	}
	var items []ilsp.CompletionItem
	for _, f := range t.Fields {
		if !matches(caret.Prefix, f.Name) {
			continue
		}
		detail := f.Type
		if f.Deprecated {
			// Still offered — the schema says it exists — but never silently:
			// a deprecated field is a migration waiting to happen.
			detail += " (deprecated)"
		}
		items = append(items, ilsp.CompletionItem{
			Label: f.Name, FilterText: f.Name, InsertText: f.Name,
			Detail: detail, Doc: firstLine(f.Description), SortText: f.Name,
		})
	}
	if matches(caret.Prefix, "__typename") {
		items = append(items, ilsp.CompletionItem{
			Label: "__typename", FilterText: "__typename", InsertText: "__typename",
			Detail: "String!", Doc: "The name of the concrete type of this object.",
			SortText: "zz__typename",
		})
	}
	return items
}

// graphQLArgumentItems offers the arguments of the field whose "(" is open,
// inserting the ": " separator so the caret lands where the value goes — the
// header-name rule, one grammar down.
func graphQLArgumentItems(s *graphql.Schema, caret graphql.Caret) []ilsp.CompletionItem {
	f, ok := s.FieldByName(caret.Type, caret.Field)
	if !ok {
		return nil
	}
	var items []ilsp.CompletionItem
	for _, a := range f.Args {
		if !matches(caret.Prefix, a.Name) {
			continue
		}
		detail := a.Type
		if a.Default != "" {
			detail += " = " + a.Default
		}
		items = append(items, ilsp.CompletionItem{
			Label: a.Name, FilterText: a.Name, InsertText: a.Name + ": ",
			Detail: detail, Doc: firstLine(a.Description), SortText: a.Name,
		})
	}
	return items
}

// graphQLTypeItems offers every named type of the schema — the positions where
// one belongs: the ":" of a variable definition and the "... on" of a type
// condition. Introspection's own meta types are left out; they are never what
// is being written.
func graphQLTypeItems(s *graphql.Schema, caret graphql.Caret) []ilsp.CompletionItem {
	var items []ilsp.CompletionItem
	for i := range s.Types {
		t := &s.Types[i]
		if strings.HasPrefix(t.Name, "__") || !matches(caret.Prefix, t.Name) {
			continue
		}
		items = append(items, ilsp.CompletionItem{
			Label: t.Name, FilterText: t.Name, InsertText: t.Name,
			Detail: strings.ToLower(t.Kind), Doc: firstLine(t.Description), SortText: t.Name,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Label < items[j].Label })
	return items
}

// firstLine trims a schema description down to what fits the popup's doc line.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
