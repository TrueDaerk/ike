package search

// structural.go is the structural search mode (#2363): a Query whose matches
// are the document nodes a jq query selects, not text occurrences. The spans
// come precomputed from internal/jqpath and are cached per document version,
// so every consumer of a Query — line highlighting, the tally, n/N stepping —
// works unchanged: LineMatches serves the cached spans, and everything else
// is built on LineMatches.
//
// The state lives behind a pointer, like the editor's tally store: the many
// value copies of a Query that one search produces share one evaluation, and
// SyncStructural re-evaluates through the shared pointer when the document
// version moved.

import (
	"context"
	"strings"
	"time"

	"ike/internal/editor/buffer"
	"ike/internal/jqpath"
)

// StructuralTimeout bounds one evaluation. The editor evaluates synchronously
// on the search line's keystrokes, so a pathological query must end as an
// error hint quickly rather than freeze the pane; the jq playground's 5s
// budget belongs to its async path.
const StructuralTimeout = time.Second

// jqState is the shared evaluation state of a structural query.
type jqState struct {
	langID string
	valid  bool
	ver    int // document version the spans were computed for
	spans  []Span
	byLine map[int][]Span
	capped bool
	err    string
}

// CompileStructural builds a structural Query for a jq program over a buffer
// of language langID (per internal/docpath's registry). An empty langID marks
// a buffer without a document language; the error surfaces on the first Sync.
// The query holds no matches until SyncStructural runs.
func CompileStructural(langID, program string) Query {
	return Query{Pattern: strings.TrimSpace(program), jq: &jqState{langID: langID}}
}

// IsStructural reports whether the query is a structural (jq) one.
func (q Query) IsStructural() bool { return q.jq != nil }

// StructuralErr returns the query's evaluation error, "" when it is none or
// the query is not structural.
func (q Query) StructuralErr() string {
	if q.jq == nil {
		return ""
	}
	return q.jq.err
}

// SyncStructural (re-)evaluates a structural query against the buffer when
// the document version moved since the last evaluation; a no-op for text
// queries. version is the editor's document version, the same counter every
// other per-edit cache keys on.
func (q Query) SyncStructural(b *buffer.Buffer, version int) {
	st := q.jq
	if st == nil || st.valid && st.ver == version {
		return
	}
	st.valid, st.ver = true, version
	st.spans, st.byLine, st.capped, st.err = nil, nil, false, ""
	if q.Pattern == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), StructuralTimeout)
	defer cancel()
	spans, capped, err := jqpath.Find(ctx, st.langID, b.String(), q.Pattern)
	if err != nil {
		st.err = err.Error()
		return
	}
	st.capped = capped
	st.spans = make([]Span, len(spans))
	st.byLine = make(map[int][]Span, len(spans))
	for i, s := range spans {
		sp := Span{Line: s.Line, Start: s.Start, End: s.End}
		st.spans[i] = sp
		st.byLine[s.Line] = append(st.byLine[s.Line], sp)
	}
}

// structuralLineMatches serves LineMatches from the cached spans.
func (q Query) structuralLineMatches(i int) []Span {
	if q.jq == nil {
		return nil
	}
	return q.jq.byLine[i]
}

// structuralScan serves ScanMatches: the span list is already capped at
// jqpath.MaxMatches, which is search.MaxMatches.
func (q Query) structuralScan() ([]Span, bool) {
	return q.jq.spans, q.jq.capped
}
