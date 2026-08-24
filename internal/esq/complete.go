package esq

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"ike/internal/complete"
	"ike/internal/fuzzy"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// complete.go is the console's completion source (#1927): inside a query
// buffer (<index>.es.json) it offers the common Query-DSL keys plus the
// field names of the buffer's index, derived from the index mapping. The
// source claims its files exclusively (#1302), so a query buffer never mixes
// mapping fields with buffer-word noise.

// dslKeys are the Query-DSL and aggregation keys worth offering anywhere a
// key is being typed. Deliberately flat: modelling which keys nest under
// which would chase a moving spec, and the fuzzy filter narrows the list to a
// handful after two keystrokes anyway.
var dslKeys = []string{
	"query", "bool", "must", "must_not", "should", "filter",
	"minimum_should_match", "boost",
	"match", "match_all", "match_none", "match_phrase", "match_phrase_prefix",
	"multi_match", "term", "terms", "range", "exists", "prefix", "wildcard",
	"regexp", "fuzzy", "ids", "values", "fields", "query_string",
	"simple_query_string", "nested", "path", "operator", "analyzer", "slop",
	"gte", "gt", "lte", "lt", "format", "time_zone",
	"sort", "order", "asc", "desc", "size", "from", "_source",
	"track_total_hits", "search_after",
	"aggs", "aggregations", "avg", "sum", "min", "max", "stats",
	"cardinality", "value_count", "percentiles", "date_histogram", "histogram",
	"calendar_interval", "fixed_interval", "interval", "field", "missing",
	"top_hits", "filters", "composite", "sources", "after",
	"highlight", "pre_tags", "post_tags",
}

// successTTL and errorTTL bound the mapping cache: a fetched field list stays
// warm for a while (reindexing mid-session is rare), a failed fetch retries
// soon (the cluster may just have been restarting).
const (
	successTTL = 5 * time.Minute
	errorTTL   = 10 * time.Second
)

// CompletionSource implements complete.Source for query buffers. Register it
// once on the app's engine; it resolves endpoints against the live config on
// every fetch.
type CompletionSource struct {
	mu    sync.Mutex
	lines map[string][]string // buffer text per open query file (Observe)
}

type mapEntry struct {
	fields []Field
	err    error
	at     time.Time
}

// The field cache is package state, not source state: the console pane primes
// it when an index loads (PrimeFields), the completion source reads and
// refills it — the two never hold a reference to each other.
var (
	fieldMu    sync.Mutex
	fieldCache = map[string]mapEntry{}
)

// NewCompletionSource builds the source.
func NewCompletionSource() *CompletionSource {
	return &CompletionSource{lines: map[string][]string{}}
}

// Name tags the source's batches.
func (s *CompletionSource) Name() string { return "esquery" }

// Priority slots the source with the other structural sources; with the
// exclusive claim active it competes with nobody anyway.
func (s *CompletionSource) Priority() int { return ilsp.PrioritySymbols }

// Exclusive claims query buffers outright (#1302): only this source runs
// there, so the popup holds mapping fields and DSL keys, not buffer words.
func (s *CompletionSource) Exclusive(path string) bool { return IsQueryPath(path) }

// TriggerChar re-triggers on the JSON string delimiters that matter here: an
// opening quote starts a key, a dot descends into an object field's children.
func (s *CompletionSource) TriggerChar(ch string) bool { return ch == "\"" || ch == "." }

// Observe stashes the buffer text of query files — on the UI goroutine, so
// stash-and-return only; extraction happens in Complete.
func (s *CompletionSource) Observe(ev host.EditorEvent) {
	// Gated on the real path, not the language name: a query buffer's index
	// is encoded in its file name (QueryRef), so a file-less buffer merely
	// *treated* as one has nothing to complete against (#2048).
	if ev.Kind != host.EditorChange || !IsQueryPath(ev.Path) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.Large || ev.Text == "" {
		delete(s.lines, ev.BufKey())
		return
	}
	s.lines[ev.BufKey()] = strings.Split(ev.Text, "\n")
}

// PrimeFields seeds the mapping cache from outside — the console pane fetches
// the mapping when an index loads and hands the fields over, so the common
// completion path never waits on the cluster.
func PrimeFields(endpoint, index string, fields []Field) {
	fieldMu.Lock()
	defer fieldMu.Unlock()
	fieldCache[endpoint+"\x00"+index] = mapEntry{fields: fields, at: time.Now()}
}

// resetFieldCache clears the package cache (tests).
func resetFieldCache() {
	fieldMu.Lock()
	defer fieldMu.Unlock()
	fieldCache = map[string]mapEntry{}
}

// Complete offers DSL keys and mapping fields while the cursor sits inside a
// JSON string — the only spot in a query body where a name is being typed.
func (s *CompletionSource) Complete(ctx context.Context, req complete.Request) ([]ilsp.CompletionItem, error) {
	endpoint, index, ok := QueryRef(req.Path)
	if !ok {
		return nil, nil
	}
	line, ok := s.lineAt(req.BufKey(), req.Line)
	if !ok {
		return nil, nil
	}
	typed, inString := stringPrefix(line, req.Col)
	if !inString {
		return nil, nil
	}
	// The editor replaces only the identifier tail on accept; the part of the
	// typed string before it (e.g. "user." of "user.add") rides along as
	// ReplacePrefix so a dotted field lands whole.
	replace := typed[:len(typed)-len(identTail(typed))]

	var items []ilsp.CompletionItem
	fields := s.fields(ctx, endpoint, index)
	for _, f := range fields {
		if !matches(typed, f.Name) {
			continue
		}
		items = append(items, ilsp.CompletionItem{
			Label: f.Name, Detail: f.Type, InsertText: f.Name,
			FilterText: f.Name, SortText: "0" + f.Name, ReplacePrefix: replace,
		})
	}
	for _, k := range dslKeys {
		if !matches(typed, k) {
			continue
		}
		items = append(items, ilsp.CompletionItem{
			Label: k, Detail: "dsl", InsertText: k,
			FilterText: k, SortText: "1" + k, ReplacePrefix: replace,
		})
	}
	return items, nil
}

// fields returns the index's fields, from cache or fetched under ctx. A miss
// blocks at most for the engine's dispatch timeout; the result (or the error)
// is cached so the next keystroke is instant either way.
func (s *CompletionSource) fields(ctx context.Context, endpoint, index string) []Field {
	key := endpoint + "\x00" + index
	fieldMu.Lock()
	e, hit := fieldCache[key]
	fieldMu.Unlock()
	if hit {
		ttl := successTTL
		if e.err != nil {
			ttl = errorTTL
		}
		if time.Since(e.at) < ttl {
			return e.fields
		}
	}
	ep, ok := FindEndpoint(endpoint)
	if !ok {
		return e.fields
	}
	_, fields, err := NewClient(ep).Mapping(ctx, index)
	if err != nil {
		// Keep the last good field list under an error TTL: a hiccup should
		// not blank completion that worked a second ago.
		fieldMu.Lock()
		fieldCache[key] = mapEntry{fields: e.fields, err: err, at: time.Now()}
		fieldMu.Unlock()
		return e.fields
	}
	fieldMu.Lock()
	fieldCache[key] = mapEntry{fields: fields, at: time.Now()}
	fieldMu.Unlock()
	return fields
}

// lineAt returns the request line from the stashed buffer text, falling back
// to the file on disk for a buffer opened this session but never edited.
func (s *CompletionSource) lineAt(path string, line int) (string, bool) {
	s.mu.Lock()
	lines, ok := s.lines[path]
	s.mu.Unlock()
	if !ok {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", false
		}
		lines = strings.Split(string(raw), "\n")
	}
	if line < 0 || line >= len(lines) {
		return "", false
	}
	return lines[line], true
}

// stringPrefix reports whether col sits inside a JSON string literal on line,
// and the string's text up to the cursor. Quote counting honours backslash
// escapes; an odd count of opening quotes before the cursor means "inside".
func stringPrefix(line string, col int) (string, bool) {
	runes := []rune(line)
	if col > len(runes) {
		col = len(runes)
	}
	start := -1
	escaped := false
	for i := 0; i < col; i++ {
		switch {
		case escaped:
			escaped = false
		case runes[i] == '\\':
			escaped = true
		case runes[i] == '"':
			if start < 0 {
				start = i + 1
			} else {
				start = -1
			}
		}
	}
	if start < 0 {
		return "", false
	}
	return string(runes[start:col]), true
}

// identTail is the identifier suffix of typed — the span the editor replaces
// on accept without help.
func identTail(typed string) string {
	runes := []rune(typed)
	i := len(runes)
	for i > 0 {
		r := runes[i-1]
		if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			i--
			continue
		}
		break
	}
	return string(runes[i:])
}

// matches gates candidates with the same loose subsequence filter the .http
// source settled on: a strict prefix would close the popup before the editor
// ever got to filter fuzzily.
func matches(typed, candidate string) bool {
	if typed == "" {
		return true
	}
	_, ok := fuzzy.Match(typed, candidate)
	return ok
}
