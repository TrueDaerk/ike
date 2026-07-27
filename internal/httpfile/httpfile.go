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
// ({{$env NAME}} and ${NAME}) are kept verbatim by the parser and resolved
// separately at dispatch time via Request.Resolve / Substitute.
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

// File is the parse result of one .http file.
type File struct {
	Requests []*Request
	Errors   []*ParseError
}

// tokenRE matches an RFC 9110 token (methods, header names).
var tokenRE = regexp.MustCompile("^[!#$%&'*+\\-.^_`|~0-9A-Za-z]+$")

// protoRE matches an HTTP-version on the request line.
var protoRE = regexp.MustCompile(`^HTTP/\d(?:\.\d)?$`)

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

// parseBlock parses lines[start:end] as one request block and appends the
// result (or an error) to f. sep is the line index of the block's "###"
// separator (-1 when the block opens the file). Blocks holding only
// blanks/comments are skipped silently.
func parseBlock(f *File, lines []string, start, end int, name string, sep int) {
	i := start
	for i < end && (strings.TrimSpace(lines[i]) == "" || isComment(lines[i])) {
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

	// Header field lines until the empty line that starts the body.
	for i < end && strings.TrimSpace(lines[i]) != "" {
		if isComment(lines[i]) {
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

	// Body: everything after the blank line, trailing blank lines trimmed.
	if i < end {
		body := strings.Join(lines[i+1:end], "\n")
		req.Body = strings.Trim(body, "\n")
	}

	f.Requests = append(f.Requests, req)
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

// placeholderRE matches both supported placeholder forms:
// {{$env NAME}} and ${NAME}.
var placeholderRE = regexp.MustCompile(`\{\{\s*\$env\s+([A-Za-z_][A-Za-z0-9_]*)\s*\}\}|\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Substitute replaces every placeholder in s using lookup. When any
// placeholder has no value, it returns s unchanged together with an error
// naming all unresolved variables.
func Substitute(s string, lookup func(string) (string, bool)) (string, error) {
	missing := map[string]bool{}
	out := placeholderRE.ReplaceAllStringFunc(s, func(m string) string {
		groups := placeholderRE.FindStringSubmatch(m)
		nm := groups[1]
		if nm == "" {
			nm = groups[2]
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
	out := *r
	out.Headers = make([]Header, len(r.Headers))
	var errs []string
	sub := func(s string) string {
		v, err := Substitute(s, lookup)
		if err != nil {
			errs = append(errs, err.Error())
			return s
		}
		return v
	}
	out.Target = sub(r.Target)
	for i, h := range r.Headers {
		out.Headers[i] = Header{Name: h.Name, Value: sub(h.Value)}
	}
	out.Body = sub(r.Body)
	if len(errs) > 0 {
		return nil, fmt.Errorf("request %s: %s", r.Key(), strings.Join(dedup(errs), "; "))
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
