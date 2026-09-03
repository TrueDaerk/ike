// Package httpfile parses plain-text .http files (#1248, epic #1247) into
// RFC 9112 request blocks. A file may contain several requests separated by
// "###" lines (the JetBrains/VS Code REST-client convention); text after the
// separator names the following request. Each block is a request line
// ("METHOD request-target [HTTP-version]"), header field lines, and — after
// an empty line — an optional body. Lines starting with "#" or "//" outside
// a body are comments.
//
// Parsing is tolerant per block: a malformed block yields a ParseError with
// its line number while the remaining blocks still parse. Placeholders
// ({{$env NAME}}, ${NAME} and the user-defined {{name}}, #1867) are kept
// verbatim by the parser and resolved separately at dispatch time via
// Request.Resolve / Request.ResolveVars / Substitute. In-file variable
// definitions (`@name=value`, #1867) are collected into File.Vars.
package httpfile

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Ext is the file extension the HTTP client handles.
const Ext = ".http"

// DefaultProto is assumed when a request line omits the HTTP version.
const DefaultProto = "HTTP/1.1"

// Header is a single header field line. Order and repetitions are preserved.
type Header struct {
	Name  string
	Value string
}

// Request is one parsed request block of a .http file.
type Request struct {
	// Name is the text after the introducing "###" separator, "" if none.
	Name string
	// Index is the zero-based position of the request within the file.
	Index int
	// Line is the 1-based line number of the request line.
	Line int
	// BlockStart/BlockEnd delimit the whole block (1-based, inclusive),
	// including the introducing "###" separator line — the range RequestAt
	// matches a cursor line against.
	BlockStart int
	BlockEnd   int

	Method  string
	Target  string // raw request-target, placeholders unresolved
	Proto   string // e.g. "HTTP/1.1"; DefaultProto when omitted
	Headers []Header
	Body    string
	// BodyFile is the path of an external body — the `< ./payload.json` form
	// (#1305) — with placeholders unresolved. Empty when the body is inline;
	// when set, Body is empty, so a consumer that only knows about Body never
	// sends the directive line verbatim.
	BodyFile string
	// BodyFileSubstitute reports the `<@ ./payload.json` spelling: the file's
	// own placeholders are substituted before it is sent.
	BodyFileSubstitute bool
	// Captures are the request's `# @capture name = <jq-expr>` directives
	// (#1993), in file order. They are evaluated against the response after
	// dispatch; the parser only collects them.
	Captures []Capture
	// GraphQL is the split of a `GRAPHQL <url>` block's body (#2423) — query,
	// variables, operation name and their line ranges. nil for every other
	// method, so `req.GraphQL != nil` is the test for "this is a GraphQL
	// request". See graphql.go.
	GraphQL *GraphQLSpec
	// WebSocket is the split of a `WEBSOCKET <url>` block's body (#2422) — the
	// initial messages with their wait-for-server flags. nil for every other
	// method, so `req.WebSocket != nil` is the test for "this is a WebSocket
	// session". See websocket.go.
	WebSocket *WebSocketSpec
	// BodyStart/BodyEnd delimit the body's lines (1-based, inclusive), with
	// the surrounding blank lines excluded — 0 when the request has no body.
	// Consumers that need the body *in* the file rather than as a string use
	// them: the editor highlights a JSON body as JSON (#1303) and indents it
	// by its own language (#1304).
	BodyStart int
	BodyEnd   int
}

// Key returns the stable identifier used to address the request across
// parses (history keying): the request name when present, otherwise the
// decimal index.
func (r *Request) Key() string {
	if r.Name != "" {
		return r.Name
	}
	return strconv.Itoa(r.Index)
}

// Header returns the value of the first header with the given name
// (case-insensitive) and whether it exists.
func (r *Request) Header(name string) (string, bool) {
	for _, h := range r.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value, true
		}
	}
	return "", false
}

// ParseError describes one malformed piece of a .http file.
type ParseError struct {
	Line int // 1-based
	Msg  string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
}

// Variable is one in-file variable definition — an `@name=value` line
// (#1867), the JetBrains/VS-Code REST-client spelling. Values keep their
// placeholders verbatim; they are substituted at dispatch time like any
// other text, so a definition may refer to another variable.
type Variable struct {
	Name  string
	Value string
	Line  int // 1-based
}

// File is the parse result of one .http file.
type File struct {
	Requests []*Request
	Errors   []*ParseError
	// Vars holds the file's `@name=value` definitions in file order (#1867).
	Vars []Variable
}

// VarMap collapses the file's definitions into the name→value map the
// resolution chain takes: a name defined twice takes its last value, the way
// a re-assignment reads.
func (f *File) VarMap() map[string]string {
	if len(f.Vars) == 0 {
		return nil
	}
	out := make(map[string]string, len(f.Vars))
	for _, v := range f.Vars {
		out[v.Name] = v.Value
	}
	return out
}

// tokenRE matches an RFC 9110 token (methods, header names).
var tokenRE = regexp.MustCompile("^[!#$%&'*+\\-.^_`|~0-9A-Za-z]+$")

// protoRE matches an HTTP-version on the request line.
var protoRE = regexp.MustCompile(`^HTTP/\d(?:\.\d)?$`)

// ValidToken reports whether s is an RFC 9110 token — the rule this parser
// applies to methods and header names. Exposed for the .http reformatter
// (#1602), which must accept exactly what the parser accepts.
func ValidToken(s string) bool { return tokenRE.MatchString(s) }

// ValidProto reports whether s is an HTTP-version the request line accepts.
func ValidProto(s string) bool { return protoRE.MatchString(s) }

const separator = "###"

// Parse splits src into request blocks and parses each one. Malformed
// blocks are reported in File.Errors; well-formed blocks parse regardless.
func Parse(src string) *File {
	f := &File{}
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")

	name := ""      // pending name from the last separator
	blockStart := 0 // first line index of the current block
	for i := 0; i <= len(lines); i++ {
		atEnd := i == len(lines)
		if !atEnd && !strings.HasPrefix(lines[i], separator) {
			continue
		}
		sep := blockStart - 1 // line index of the introducing ###, -1 for none
		parseBlock(f, lines, blockStart, i, name, sep)
		if atEnd {
			break
		}
		name = strings.TrimSpace(strings.TrimPrefix(lines[i], separator))
		blockStart = i + 1
	}
	return f
}

// isComment reports whether a line outside a body is a comment.
func isComment(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//")
}

// varDefRE matches an in-file variable definition (#1867): `@name = value`,
// with whitespace around the `=` and around the value stripped. The value may
// be empty (`@token=`) and may itself hold placeholders.
var varDefRE = regexp.MustCompile(`^[ \t]*@([A-Za-z_][A-Za-z0-9_.-]*)[ \t]*=[ \t]*(.*?)[ \t]*$`)

// VarDefinition recognises an `@name=value` line outside a body and returns
// its parts (#1867). Exposed for the .http reformatter, which must leave the
// lines the parser reads as definitions alone.
func VarDefinition(line string) (name, value string, ok bool) {
	m := varDefRE.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// parseBlock parses lines[start:end] as one request block and appends the
// result (or an error) to f. sep is the line index of the block's "###"
// separator (-1 when the block opens the file). Blocks holding only
// blanks/comments (or only variable definitions) are skipped silently.
func parseBlock(f *File, lines []string, start, end int, name string, sep int) {
	// Capture directives (#1993) are comment lines, so they are picked up
	// wherever a comment is skipped below — before the request line, between
	// folded query lines, in the header block. They belong to the block's
	// request, which does not exist yet at the first of those places.
	var captures []Capture
	noteCapture := func(idx int) {
		if c, ok := captureAt(lines[idx], idx); ok {
			captures = append(captures, c)
		}
	}

	i := start
	for i < end {
		if strings.TrimSpace(lines[i]) == "" || isComment(lines[i]) {
			noteCapture(i)
			i++
			continue
		}
		// Variable definitions (#1867) may precede the request line — a block
		// of nothing but definitions is the file header the JetBrains
		// convention puts them in.
		nm, value, ok := VarDefinition(lines[i])
		if !ok {
			break
		}
		f.Vars = append(f.Vars, Variable{Name: nm, Value: value, Line: i + 1})
		i++
	}
	if i == end {
		return // empty block, e.g. leading comments before the first ###
	}

	fail := func(line int, format string, args ...any) {
		f.Errors = append(f.Errors, &ParseError{Line: line + 1, Msg: fmt.Sprintf(format, args...)})
	}

	// Request line.
	fields := strings.Fields(lines[i])
	blockStart := sep // 0-based: the separator line, or 0 at file start
	if sep < 0 {
		blockStart = 0
	}
	req := &Request{
		Name: name, Index: len(f.Requests), Line: i + 1,
		BlockStart: blockStart + 1, BlockEnd: end, Proto: DefaultProto,
	}
	switch len(fields) {
	case 2:
		req.Method, req.Target = fields[0], fields[1]
	case 3:
		if !protoRE.MatchString(fields[2]) {
			fail(i, "invalid HTTP version %q", fields[2])
			return
		}
		req.Method, req.Target, req.Proto = fields[0], fields[1], fields[2]
	default:
		fail(i, "invalid request line %q: want \"METHOD target [HTTP-version]\"", strings.TrimSpace(lines[i]))
		return
	}
	if !tokenRE.MatchString(req.Method) {
		fail(i, "invalid method %q", req.Method)
		return
	}
	i++

	// Folded query lines (#1269): indented continuation lines starting with
	// "?" or "&" extend the request target, the JetBrains spelling of a long
	// query. Folding ends at the first header line, blank line or block end.
	var query []string
	for i < end {
		if isComment(lines[i]) {
			noteCapture(i)
			i++
			continue
		}
		t := strings.TrimSpace(lines[i])
		if t == "" || (!strings.HasPrefix(t, "?") && !strings.HasPrefix(t, "&")) {
			break
		}
		query = append(query, queryParams(t)...)
		i++
	}
	req.Target = appendQuery(req.Target, query)

	// Header field lines until the empty line that starts the body.
	for i < end && strings.TrimSpace(lines[i]) != "" {
		if isComment(lines[i]) {
			noteCapture(i)
			i++
			continue
		}
		colon := strings.Index(lines[i], ":")
		if colon <= 0 || !tokenRE.MatchString(lines[i][:colon]) {
			fail(i, "invalid header field %q", lines[i])
			return
		}
		req.Headers = append(req.Headers, Header{
			Name:  lines[i][:colon],
			Value: strings.TrimSpace(lines[i][colon+1:]),
		})
		i++
	}

	// Body: everything after the blank line, surrounding blank lines trimmed.
	if i < end {
		bs, be := i+1, end-1
		for bs <= be && lines[bs] == "" {
			bs++
		}
		for be >= bs && lines[be] == "" {
			be--
		}
		if bs <= be {
			req.Body = strings.Join(lines[bs:be+1], "\n")
			req.BodyStart, req.BodyEnd = bs+1, be+1
			if path, substitute, ok := externalBody(req.Body); ok {
				req.Body, req.BodyFile, req.BodyFileSubstitute = "", path, substitute
			}
		}
	}

	// A GRAPHQL block's body is a query plus an optional JSON variables
	// object (#2423); the split is part of the parse so highlighting,
	// completion and dispatch all read the same sections.
	if strings.EqualFold(req.Method, GraphQLMethod) {
		req.GraphQL = graphQLSpec(req.Body, req.BodyStart, req.BodyEnd)
	}

	// A WEBSOCKET block's body is its initial messages, separated by `===`
	// lines (#2422); the split is part of the parse so dispatch and any editor
	// consumer read the same sections.
	if strings.EqualFold(req.Method, WebSocketMethod) {
		req.WebSocket = webSocketSpec(req.Body)
	}

	req.Captures = captures
	f.Requests = append(f.Requests, req)
}

// externalBodyRE matches the JetBrains external-body directive: "< path"
// sends the file verbatim, "<@ path" substitutes the file's own placeholders
// first, and "<@encoding path" additionally names a source encoding.
var externalBodyRE = regexp.MustCompile(`^<(@[A-Za-z0-9_-]*)?[ \t]+(\S.*)$`)

// externalBody recognises a body that is nothing but a file directive. A body
// with any further line is inline text — a lone "<" line inside a larger
// payload must keep its literal meaning.
func externalBody(body string) (path string, substitute, ok bool) {
	if strings.ContainsAny(body, "\n") {
		return "", false, false
	}
	m := externalBodyRE.FindStringSubmatch(strings.TrimSpace(body))
	if m == nil {
		return "", false, false
	}
	return strings.TrimSpace(m[2]), m[1] != "", true
}

// queryParams splits one folded query line into "key=value" fragments
// (#1269). The leading "?"/"&" is dropped, several params may share a line
// ("? a = 1 & b = 2"), and whitespace around the separators and the tokens is
// stripped — "? v =" yields "v=", "? v" yields "v" (a valueless flag param).
// A value may itself contain "=" ("?filter=a=b"), so only the first one
// separates.
func queryParams(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimLeft(line, "?&")
	var out []string
	for _, part := range strings.Split(line, "&") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, hasValue := strings.Cut(part, "=")
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if !hasValue {
			out = append(out, key)
			continue
		}
		out = append(out, key+"="+strings.TrimSpace(value))
	}
	return out
}

// appendQuery attaches folded query params to a request target, opening the
// query with "?" or extending an existing one with "&" (#1269).
func appendQuery(target string, params []string) string {
	if len(params) == 0 {
		return target
	}
	sep := "?"
	switch {
	case strings.HasSuffix(target, "?"), strings.HasSuffix(target, "&"):
		sep = "" // the request line already opened the query
	case strings.Contains(target, "?"):
		sep = "&"
	}
	return target + sep + strings.Join(params, "&")
}

// RequestAt returns the request whose block (separator line through last
// body line) contains the 1-based cursor line, mirroring how run-at-cursor
// resolves its target.
func (f *File) RequestAt(line int) (*Request, bool) {
	for _, r := range f.Requests {
		if line >= r.BlockStart && line <= r.BlockEnd {
			return r, true
		}
	}
	return nil, false
}

// placeholderRE matches the three supported placeholder forms:
// {{$env NAME}} and ${NAME} — resolved from the process environment — and the
// user-defined {{name}} (#1867), resolved from the variable chain.
var placeholderRE = regexp.MustCompile(`\{\{\s*\$env\s+([A-Za-z_][A-Za-z0-9_]*)\s*\}\}|\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\{\{\s*([A-Za-z_][A-Za-z0-9_.-]*)\s*\}\}`)

// Substitute replaces every placeholder in s using lookup for all three
// forms. When any placeholder has no value, it returns s unchanged together
// with an error naming all unresolved variables.
func Substitute(s string, lookup func(string) (string, bool)) (string, error) {
	return SubstituteVars(s, &Vars{Lookup: lookup})
}

// SubstituteVars is Substitute with the full variable chain (#1867):
// {{$env NAME}} and ${NAME} keep resolving from the process environment
// alone, while {{name}} walks in-file definitions → environment file →
// process environment.
func SubstituteVars(s string, v *Vars) (string, error) {
	return substitute(s, v.EnvValue, func(name string) (string, bool) { return v.value(name, nil) })
}

// substitute is the shared replacement pass: env resolves the {{$env NAME}}
// and ${NAME} forms, user the {{name}} form.
func substitute(s string, env, user func(string) (string, bool)) (string, error) {
	missing := map[string]bool{}
	out := placeholderRE.ReplaceAllStringFunc(s, func(m string) string {
		groups := placeholderRE.FindStringSubmatch(m)
		lookup, nm := env, groups[1]
		if nm == "" {
			nm = groups[2]
		}
		if nm == "" {
			lookup, nm = user, groups[3]
		}
		if v, ok := lookup(nm); ok {
			return v
		}
		missing[nm] = true
		return m
	})
	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for nm := range missing {
			names = append(names, nm)
		}
		sort.Strings(names)
		return s, fmt.Errorf("unresolved placeholders: %s", strings.Join(names, ", "))
	}
	return out, nil
}

// Resolve returns a copy of the request with all placeholders in target,
// header values, and body substituted via lookup (typically os.LookupEnv).
// Any unresolved placeholder fails the whole request so a broken request is
// never dispatched.
func (r *Request) Resolve(lookup func(string) (string, bool)) (*Request, error) {
	return r.ResolveVars(&Vars{Lookup: lookup})
}

// ResolveVars is Resolve with the full variable chain (#1867).
func (r *Request) ResolveVars(vars *Vars) (*Request, error) {
	out := *r
	out.Headers = make([]Header, len(r.Headers))
	var errs []string
	sub := func(s string) string {
		v, err := SubstituteVars(s, vars)
		if err != nil {
			errs = append(errs, err.Error())
			return s
		}
		return v
	}
	out.Target = sub(r.Target)
	out.BodyFile = sub(r.BodyFile)
	for i, h := range r.Headers {
		out.Headers[i] = Header{Name: h.Name, Value: sub(h.Value)}
	}
	out.Body = sub(r.Body)
	if len(errs) > 0 {
		return nil, fmt.Errorf("request %s: %s", r.Key(), strings.Join(dedup(errs), "; "))
	}
	// A GRAPHQL block becomes its wire form here (#2423), after substitution:
	// the envelope must carry the *resolved* query and variables, and every
	// consumer of a resolved request — dispatch, the curl and httpie exports —
	// then sees the POST that actually goes out.
	if out.GraphQL != nil {
		out.GraphQL = graphQLSpec(out.Body, r.BodyStart, r.BodyEnd)
		if err := out.applyGraphQL(); err != nil {
			return nil, fmt.Errorf("request %s: %v", r.Key(), err)
		}
	}
	// A WEBSOCKET block re-splits after substitution (#2422), so the messages
	// that go out carry the *resolved* text.
	if out.WebSocket != nil {
		out.WebSocket = webSocketSpec(out.Body)
	}
	return &out, nil
}

// dedup removes duplicate strings, preserving order.
func dedup(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
