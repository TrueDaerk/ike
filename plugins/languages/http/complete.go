package langhttp

import (
	"context"
	"strings"
	"sync"

	"ike/internal/complete"
	"ike/internal/fuzzy"
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
// Exclusive claims .http buffers for this source (#1302): the header/method
// catalogs are the only sensible completions there, so the buffer-word,
// project-scan and symbol sources must not merge their identifiers into the
// popup — and a request body offers nothing rather than every word in the file.
func (s *httpSource) Exclusive(path string) bool { return isHTTPFile(path) }

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
		return fuzzyItems(methods, strings.TrimLeft(before, " \t"), "method"), nil
	case ctxVersion:
		return fuzzyItems(protoVersions, lastField(before), "http version"), nil
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

// matches reports whether typed selects candidate. The test is a
// case-insensitive subsequence, the same shape the editor's popup filter uses
// (#1292): a strict prefix would only ever reach a header from its very first
// letters, so "Cen" missed "Content-Encoding" and — because an empty batch
// closes the popup — the editor never got to filter fuzzily either. Ranking
// stays with the editor, which scores matches with internal/fuzzy.
func matches(typed, candidate string) bool {
	if typed == "" {
		return true
	}
	_, ok := fuzzy.Match(typed, candidate)
	return ok
}

// fuzzyItems offers every candidate the typed text selects.
func fuzzyItems(candidates []string, typed, detail string) []ilsp.CompletionItem {
	var items []ilsp.CompletionItem
	for _, c := range candidates {
		if !matches(typed, c) {
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
func headerNameItems(typed string) []ilsp.CompletionItem {
	var items []ilsp.CompletionItem
	for _, h := range headerNames {
		if !matches(typed, h) {
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
// value matches loosely too, so "jso" still reaches "application/json" and
// "wwwform" reaches "application/x-www-form-urlencoded".
func headerValueItems(name, typed string) []ilsp.CompletionItem {
	values, ok := headerValues[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil
	}
	needle := strings.TrimSpace(typed)
	var items []ilsp.CompletionItem
	for _, v := range values {
		if !matches(needle, v) {
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

// headerNames is the request-header set worth offering by hand: the IANA
// registry's request headers plus the de-facto X- and Sec- ones people type
// against real APIs (#1292).
var headerNames = []string{
	"A-IM", "Accept", "Accept-Charset", "Accept-Datetime", "Accept-Encoding",
	"Accept-Language", "Access-Control-Request-Headers",
	"Access-Control-Request-Method", "Authorization", "Cache-Control",
	"Connection", "Content-Disposition", "Content-Encoding", "Content-Language",
	"Content-Length", "Content-Location", "Content-MD5", "Content-Range",
	"Content-Type", "Cookie", "DNT", "Date", "Expect", "Forwarded", "From",
	"Host", "Idempotency-Key", "If-Match", "If-Modified-Since", "If-None-Match",
	"If-Range", "If-Unmodified-Since", "Keep-Alive", "Max-Forwards", "Origin",
	"Pragma", "Prefer", "Proxy-Authorization", "Range", "Referer",
	"Sec-Fetch-Dest", "Sec-Fetch-Mode", "Sec-Fetch-Site", "Sec-Fetch-User",
	"Sec-WebSocket-Key", "Sec-WebSocket-Protocol", "Sec-WebSocket-Version", "TE",
	"Trailer", "Transfer-Encoding", "Upgrade", "Upgrade-Insecure-Requests",
	"User-Agent", "Via", "Warning", "X-Api-Key", "X-Api-Version",
	"X-CSRF-Token", "X-Correlation-Id", "X-Forwarded-For", "X-Forwarded-Host",
	"X-Forwarded-Proto", "X-HTTP-Method-Override", "X-Request-Id",
	"X-Requested-With",
}

// mimeTypes are the content types a request body (or an Accept) usually names.
var mimeTypes = []string{
	"application/json",
	"application/xml",
	"application/x-www-form-urlencoded",
	"application/octet-stream",
	"application/pdf",
	"application/graphql",
	"application/ld+json",
	"application/problem+json",
	"application/vnd.api+json",
	"application/yaml",
	"application/zip",
	"multipart/form-data",
	"multipart/mixed",
	"text/plain",
	"text/html",
	"text/csv",
	"text/xml",
	"text/event-stream",
	"image/png",
	"image/jpeg",
	"image/svg+xml",
	"*/*",
}

// headerValues maps a lowercase header name onto its typical values. Only
// headers with a small closed set (or a handful of values people actually
// type) are listed — a free-form header is better left alone than answered
// with a guess.
var headerValues = map[string][]string{
	"content-type":                  mimeTypes,
	"accept":                        mimeTypes,
	"authorization":                 {"Bearer ", "Basic ", "Digest ", "Token ", "ApiKey "},
	"proxy-authorization":           {"Bearer ", "Basic ", "Digest "},
	"cache-control":                 {"no-cache", "no-store", "no-transform", "only-if-cached", "max-age=0", "max-stale", "min-fresh="},
	"connection":                    {"keep-alive", "close", "Upgrade"},
	"accept-encoding":               {"gzip", "deflate", "br", "zstd", "identity", "gzip, deflate, br"},
	"content-encoding":              {"gzip", "deflate", "br", "zstd", "identity"},
	"accept-charset":                {"utf-8", "iso-8859-1"},
	"expect":                        {"100-continue"},
	"te":                            {"trailers", "compress", "deflate", "gzip"},
	"prefer":                        {"respond-async", "return=minimal", "return=representation", "wait=", "handling=strict", "handling=lenient"},
	"transfer-encoding":             {"chunked", "compress", "deflate", "gzip"},
	"x-requested-with":              {"XMLHttpRequest"},
	"x-http-method-override":        {"DELETE", "PATCH", "PUT"},
	"accept-language":               {"en", "en-US", "de", "de-DE", "*"},
	"accept-datetime":               {"Thu, 01 Jan 1970 00:00:00 GMT"},
	"pragma":                        {"no-cache"},
	"dnt":                           {"0", "1"},
	"upgrade":                       {"websocket", "h2c", "HTTP/2.0", "TLS/1.2"},
	"upgrade-insecure-requests":     {"1"},
	"if-match":                      {"*"},
	"if-none-match":                 {"*"},
	"content-disposition":           {"form-data", `form-data; name=""`, `attachment; filename=""`, "inline"},
	"access-control-request-method": methods,
	"sec-fetch-dest":                {"document", "empty", "image", "script", "style", "iframe", "font"},
	"sec-fetch-mode":                {"cors", "navigate", "no-cors", "same-origin", "websocket"},
	"sec-fetch-site":                {"cross-site", "same-origin", "same-site", "none"},
	"sec-fetch-user":                {"?1"},
	"sec-websocket-version":         {"13"},
	"range":                         {"bytes=0-", "bytes=0-1023"},
	"max-forwards":                  {"0", "1"},
}

func init() { complete.RegisterSource(newHTTPSource()) }
