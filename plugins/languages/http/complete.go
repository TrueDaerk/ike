package langhttp

import (
	"context"
	"strings"
	"sync"

	"ike/internal/complete"
	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
)

// complete.go contributes completion inside .http request files (#1268,
// roadmap 0410): method keywords and the HTTP version on the request line,
// header names in the header block (inserting the ": " separator), and the
// typical values of the headers people actually type by hand. Everything is
// position-aware — a request file is a tiny grammar, and offering header
// names inside a JSON body would be worse than offering nothing.

// httpSource implements complete.Source and complete.EventObserver: the
// buffer text is needed because the context (request line / header block /
// body) can only be decided by looking at the lines above the cursor.
type httpSource struct {
	mu    sync.RWMutex
	lines map[string][]string
}

func newHTTPSource() *httpSource { return &httpSource{lines: map[string][]string{}} }

// Name implements complete.Source.
func (s *httpSource) Name() string { return "http" }

// Priority implements complete.Source: above the word echo — a header name is
// a better answer than a word that happens to appear in the file — and below
// a language server, which .http files do not have anyway.
func (s *httpSource) Priority() int { return ilsp.PriorityEmmet }

// Observe implements complete.EventObserver: keep the current text of every
// open .http buffer.
func (s *httpSource) Observe(ev host.EditorEvent) {
	if ev.Kind != host.EditorChange || !isHTTPFile(ev.Path) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.Large || ev.Text == "" {
		delete(s.lines, ev.Path)
		return
	}
	s.lines[ev.Path] = strings.Split(ev.Text, "\n")
}

// isHTTPFile reports whether path is a request file, via the registry so the
// .rest extension follows automatically.
func isHTTPFile(path string) bool {
	l, ok := lang.ByPath(path)
	return ok && l.ID == "http"
}

func (s *httpSource) linesFor(path string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lines[path]
}

// Complete implements complete.Source.
func (s *httpSource) Complete(_ context.Context, req complete.Request) ([]ilsp.CompletionItem, error) {
	if !isHTTPFile(req.Path) {
		return nil, nil
	}
	lines := s.linesFor(req.Path)
	if req.Line < 0 || req.Line >= len(lines) {
		return nil, nil
	}
	line := lines[req.Line]
	col := clampCol(line, req.Col)
	before := string([]rune(line)[:col])

	switch classify(lines, req.Line, before) {
	case ctxMethod:
		return prefixItems(methods, strings.TrimLeft(before, " \t"), "method"), nil
	case ctxVersion:
		return prefixItems(protoVersions, lastField(before), "http version"), nil
	case ctxHeaderName:
		return headerNameItems(strings.TrimLeft(before, " \t")), nil
	case ctxHeaderValue:
		name, value, _ := strings.Cut(before, ":")
		return headerValueItems(name, value), nil
	}
	return nil, nil
}

// completion contexts inside a request block.
type ctxKind int

const (
	ctxNone ctxKind = iota
	ctxMethod
	ctxVersion
	ctxHeaderName
	ctxHeaderValue
)

// classify decides what the cursor position can complete. The rules mirror
// the parser (internal/httpfile): a block starts at a "###" separator, its
// first non-blank non-comment line is the request line, header field lines
// follow until the first blank line, and everything after that blank line is
// body — where nothing is completed. Comments complete nothing either.
func classify(lines []string, at int, before string) ctxKind {
	if at < 0 || at >= len(lines) {
		return ctxNone
	}
	if isCommentLine(lines[at]) || isSeparator(lines[at]) {
		return ctxNone
	}
	// Walk up to the block start, remembering whether a blank line (the body
	// separator) and the request line were passed.
	requestLine := -1
	sawBlank := false
	for i := at - 1; i >= 0; i-- {
		l := lines[i]
		if isSeparator(l) {
			break
		}
		if strings.TrimSpace(l) == "" {
			// A blank line before any request line is just leading space.
			sawBlank = true
			continue
		}
		if isCommentLine(l) || isFoldedQueryLine(l) {
			continue
		}
		requestLine = i
		if sawBlank {
			// The blank sat between the request block and this line: body.
			return ctxNone
		}
		break
	}
	if requestLine < 0 {
		// This line is the block's request line.
		if len(strings.Fields(before)) >= 2 && endsInField(before) {
			return ctxVersion
		}
		if strings.ContainsAny(strings.TrimSpace(before), " \t") {
			return ctxNone // typing the target: nothing local to offer
		}
		return ctxMethod
	}
	// A header field line: before the colon the name completes, after it the
	// value. A folded query continuation line completes nothing.
	if isFoldedQueryLine(before) {
		return ctxNone
	}
	if strings.Contains(before, ":") {
		return ctxHeaderValue
	}
	return ctxHeaderName
}

func isSeparator(line string) bool { return strings.HasPrefix(line, "###") }

func isCommentLine(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//")
}

// isFoldedQueryLine reports the JetBrains query continuation form (#1269).
func isFoldedQueryLine(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "?") || strings.HasPrefix(t, "&")
}

// endsInField reports whether the cursor sits inside (or right after) a
// token rather than in the whitespace behind one — "GET /x " starts the
// third field, "GET /x" is still the second.
func endsInField(before string) bool {
	return before != "" && !strings.HasSuffix(before, " ") && !strings.HasSuffix(before, "\t")
}

// lastField is the whitespace-delimited token the cursor is typing.
func lastField(before string) string {
	fields := strings.Fields(before)
	if len(fields) == 0 || !endsInField(before) {
		return ""
	}
	return fields[len(fields)-1]
}

func clampCol(line string, col int) int {
	if n := len([]rune(line)); col > n {
		return n
	}
	if col < 0 {
		return 0
	}
	return col
}

// prefixItems offers every candidate matching prefix case-insensitively.
func prefixItems(candidates []string, prefix, detail string) []ilsp.CompletionItem {
	lower := strings.ToLower(prefix)
	var items []ilsp.CompletionItem
	for _, c := range candidates {
		if !strings.HasPrefix(strings.ToLower(c), lower) {
			continue
		}
		items = append(items, ilsp.CompletionItem{
			Label: c, FilterText: c, InsertText: c, Detail: detail, SortText: c,
		})
	}
	return items
}

// headerNameItems completes header names, inserting the ": " separator so the
// cursor lands where the value goes.
func headerNameItems(prefix string) []ilsp.CompletionItem {
	lower := strings.ToLower(prefix)
	var items []ilsp.CompletionItem
	for _, h := range headerNames {
		if !strings.HasPrefix(strings.ToLower(h), lower) {
			continue
		}
		items = append(items, ilsp.CompletionItem{
			Label:      h,
			FilterText: h,
			InsertText: h + ": ",
			Detail:     "header",
			SortText:   h,
		})
	}
	return items
}

// headerValueItems completes the typical values of a known header. The typed
// value prefix filters by substring, not by prefix: a user types "json" to
// reach "application/json".
func headerValueItems(name, typed string) []ilsp.CompletionItem {
	values, ok := headerValues[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil
	}
	needle := strings.ToLower(strings.TrimSpace(typed))
	var items []ilsp.CompletionItem
	for _, v := range values {
		if needle != "" && !strings.Contains(strings.ToLower(v), needle) {
			continue
		}
		items = append(items, ilsp.CompletionItem{
			Label: v, FilterText: v, InsertText: v, Detail: "value", SortText: v,
		})
	}
	return items
}

// methods are the RFC 9110 request methods plus PATCH.
var methods = []string{
	"CONNECT", "DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT", "TRACE",
}

// protoVersions are the versions the parser accepts on a request line.
var protoVersions = []string{"HTTP/1.0", "HTTP/1.1", "HTTP/2", "HTTP/3"}

// headerNames is the request-header set worth offering by hand.
var headerNames = []string{
	"Accept", "Accept-Charset", "Accept-Encoding", "Accept-Language",
	"Authorization", "Cache-Control", "Connection", "Content-Disposition",
	"Content-Encoding", "Content-Language", "Content-Length", "Content-Type",
	"Cookie", "Expect", "Forwarded", "From", "Host", "If-Match",
	"If-Modified-Since", "If-None-Match", "If-Unmodified-Since", "Origin",
	"Prefer", "Proxy-Authorization", "Range", "Referer", "TE", "Transfer-Encoding",
	"Upgrade", "User-Agent", "Via", "X-Api-Key", "X-Correlation-Id",
	"X-Forwarded-For", "X-Request-Id", "X-Requested-With",
}

// mimeTypes are the content types a request body (or an Accept) usually names.
var mimeTypes = []string{
	"application/json",
	"application/xml",
	"application/x-www-form-urlencoded",
	"application/octet-stream",
	"application/pdf",
	"application/graphql",
	"multipart/form-data",
	"text/plain",
	"text/html",
	"text/csv",
	"text/xml",
	"*/*",
}

// headerValues maps a lowercase header name onto its typical values.
var headerValues = map[string][]string{
	"content-type":      mimeTypes,
	"accept":            mimeTypes,
	"authorization":     {"Bearer ", "Basic ", "Digest ", "Token "},
	"cache-control":     {"no-cache", "no-store", "no-transform", "only-if-cached", "max-age=0", "max-stale", "min-fresh="},
	"connection":        {"keep-alive", "close"},
	"accept-encoding":   {"gzip", "deflate", "br", "identity", "gzip, deflate, br"},
	"content-encoding":  {"gzip", "deflate", "br", "identity"},
	"accept-charset":    {"utf-8", "iso-8859-1"},
	"expect":            {"100-continue"},
	"te":                {"trailers", "compress", "deflate", "gzip"},
	"prefer":            {"respond-async", "return=minimal", "return=representation", "wait="},
	"transfer-encoding": {"chunked", "compress", "deflate", "gzip"},
	"x-requested-with":  {"XMLHttpRequest"},
	"accept-language":   {"en", "en-US", "de", "de-DE", "*"},
}

func init() { complete.RegisterSource(newHTTPSource()) }
