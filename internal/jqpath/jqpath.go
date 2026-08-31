// Package jqpath maps a jq query to source locations in a JSON or YAML buffer
// (#2363): the structural mode of the in-file search. Where the jq playground
// (internal/jqplay) answers "what does this query produce", jqpath answers
// "where in the document are the nodes it selects" — the query runs wrapped in
// jq's `path(...)`, so every output is a path array, and each path resolves
// against a position-annotated parse of the same text to the span the node
// occupies in the source.
//
// Like internal/docpath it is a leaf package — no Tree-sitter, no CGo — but
// unlike docpath it parses the whole document: a query can select any node,
// so every node needs a position. JSON goes through a hand-written positioned
// parser (encoding/json discards offsets); YAML goes through yaml.v3's node
// tree, whose Line/Column mark every node. Both keep the decoded values in
// exactly the shapes internal/jqplay feeds gojq (json.Number and all), so a
// query behaves here the way it behaves in the playground.
//
// A match span is the node's first source line: a scalar highlights its token,
// a container highlights from its opening token to the end of that line. jq's
// path() also emits paths to locations a document does not contain (`.missing`
// on `{}` is the path ["missing"]); those resolve to no source node and yield
// no match — the mode finds what is there, not what could be.
package jqpath

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/itchyny/gojq"

	"ike/internal/docpath"
)

// MaxMatches caps how many spans one query reports, aligned with the search
// tally cap (search.MaxMatches): past it the counter reads "999+" anyway, and
// an unbounded `..` over a large document should not build an unbounded list.
const MaxMatches = 999

// Span is one match: rune columns [Start, End) on a 0-based line — the same
// shape internal/editor/search.Span has, redeclared so the search package can
// depend on jqpath without a cycle.
type Span struct {
	Line       int
	Start, End int
}

// node is one document value with its source position. val holds the decoded
// value in the playground's shapes for gojq; obj/arr index the children by the
// path elements gojq emits.
type node struct {
	val any
	obj map[string]*node
	arr []*node
	// span is the node's highlight: its first source line, with End == -1
	// standing for "through the end of the line" until finish resolves it.
	span Span
}

// Find runs program over text (parsed per langID's document language) and
// returns the source spans of the nodes it selects, in reading order, deduped.
// capped reports that the list stopped at MaxMatches. The error is the parse
// or evaluation failure to show inline; zero matches is not an error.
func Find(ctx context.Context, langID, text, program string) (spans []Span, capped bool, err error) {
	program = strings.TrimSpace(program)
	if program == "" {
		return nil, false, nil
	}
	if !docpath.IsLang(langID) {
		return nil, false, errors.New("structural search needs a JSON or YAML buffer")
	}
	// Parse the raw program first so a syntax error reads against what the
	// user typed, not against the path(...) wrapper.
	if _, perr := gojq.Parse(program); perr != nil {
		return nil, false, fmt.Errorf("jq: %s", perr)
	}
	// The trailing newline guards the closing paren against a `#` comment at
	// the end of the program, which would otherwise swallow it.
	query, perr := gojq.Parse("path(" + program + "\n)")
	if perr != nil {
		return nil, false, errors.New("jq: not a query path() accepts")
	}
	code, cerr := gojq.Compile(query)
	if cerr != nil {
		return nil, false, fmt.Errorf("jq: %s", cerr)
	}

	var docs []*node
	if docpath.IsYAML(langID) {
		docs, err = parseYAMLNodes(text)
	} else {
		docs, err = parseJSONNodes(text)
	}
	if err != nil {
		return nil, false, err
	}

	seen := map[Span]bool{}
	for _, doc := range docs {
		iter := code.RunWithContext(ctx, doc.val)
		for {
			out, ok := iter.Next()
			if !ok {
				break
			}
			if rerr, ok := out.(error); ok {
				var halt *gojq.HaltError
				if errors.As(rerr, &halt) && halt.Value() == nil {
					break // `halt`: a clean stop, not a diagnostic
				}
				if ctx.Err() != nil {
					return nil, false, errors.New("jq: the query did not finish in time")
				}
				return nil, false, fmt.Errorf("jq: %s", rerr)
			}
			path, ok := out.([]any)
			if !ok {
				continue
			}
			n, ok := resolve(doc, path)
			if !ok {
				continue // a path the document does not contain (null leaf)
			}
			if seen[n.span] {
				continue
			}
			if len(seen) >= MaxMatches {
				capped = true
				break
			}
			seen[n.span] = true
			spans = append(spans, n.span)
		}
		if ctx.Err() != nil {
			return nil, false, errors.New("jq: the query did not finish in time")
		}
		if capped {
			break
		}
	}
	finish(spans, text)
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].Line != spans[j].Line {
			return spans[i].Line < spans[j].Line
		}
		return spans[i].Start < spans[j].Start
	})
	return spans, capped, nil
}

// resolve walks one gojq path array down the node tree. ok is false when the
// path steps into a child the source does not hold.
func resolve(n *node, path []any) (*node, bool) {
	for _, el := range path {
		switch e := el.(type) {
		case string:
			child, ok := n.obj[e]
			if !ok {
				return nil, false
			}
			n = child
		case int:
			if e < 0 || e >= len(n.arr) {
				return nil, false
			}
			n = n.arr[e]
		default:
			return nil, false
		}
	}
	return n, true
}

// finish resolves the End == -1 sentinel ("through the end of the line") that
// container and multi-line spans carry, against the actual source lines with
// trailing whitespace trimmed.
func finish(spans []Span, text string) {
	if len(spans) == 0 {
		return
	}
	lines := strings.Split(text, "\n")
	for i, s := range spans {
		if s.End >= 0 {
			continue
		}
		end := 0
		if s.Line < len(lines) {
			end = utf8.RuneCountInString(strings.TrimRight(lines[s.Line], " \t"))
		}
		if end <= s.Start {
			end = s.Start + 1
		}
		spans[i].End = end
	}
}
