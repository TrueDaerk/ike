package graphql

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// query.go holds the GraphQL *language* pieces the .http client needs, kept
// apart from the schema model: splitting a request body into query and
// variables, naming the operation, and building the JSON envelope that goes on
// the wire. internal/httpfile drives all three for a `GRAPHQL <url>` block
// (#2423); the highlighting and completion producers in
// plugins/languages/http read the same helpers, so the editor and the
// dispatcher always agree on where the query ends.

// Method is the .http request-line method that marks a GraphQL block.
const Method = "GRAPHQL"

// RawMedia is the media type whose body *is* the query text, with no envelope
// and therefore no place for variables.
const RawMedia = "application/graphql"

// IsRawMedia reports whether a Content-Type value is RawMedia. Parameters are
// ignored; `application/graphql+json` is deliberately not included — that is
// the media type of the JSON envelope the default path already sends.
func IsRawMedia(contentType string) bool {
	media, _, _ := strings.Cut(contentType, ";")
	return strings.EqualFold(strings.TrimSpace(media), RawMedia)
}

// SplitBody separates a GraphQL request body's query text from the trailing
// JSON variables object. The rule is the last blank line whose remainder is a
// `{…}` block: scanning from the end means a query with blank lines of its own
// — two operations, a paragraph inside a selection set — keeps them, because
// the chunk behind such a blank line does not start with "{". varsLine is the
// 0-based index of the variables' first line within lines, -1 when the body
// declares none.
func SplitBody(lines []string) (query, variables string, varsLine int) {
	for i := len(lines) - 1; i > 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			continue
		}
		head := strings.TrimRight(strings.Join(lines[:i], "\n"), " \t\n")
		tail := strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		if head == "" || !strings.HasPrefix(tail, "{") || !strings.HasSuffix(tail, "}") {
			continue
		}
		return head, strings.Join(lines[i+1:], "\n"), i + 1
	}
	return strings.Join(lines, "\n"), "", -1
}

// operationRE matches a named operation definition's keyword and name.
var operationRE = regexp.MustCompile(`\b(query|mutation|subscription)\s+([A-Za-z_][A-Za-z0-9_]*)`)

// OperationName returns the name of the first named operation in a query
// document, "" when the document is the anonymous `{ … }` shorthand or names
// nothing. Only the text before the first selection set is examined, so a
// *field* called "query" inside a selection set is never read as an operation
// keyword.
func OperationName(query string) string {
	head := StripComments(query)
	if brace := strings.Index(head, "{"); brace >= 0 {
		head = head[:brace]
	}
	m := operationRE.FindStringSubmatch(head)
	if m == nil {
		return ""
	}
	return m[2]
}

// OperationKeyword returns the operation type a query document declares —
// "query", "mutation" or "subscription" — defaulting to "query" for the
// anonymous shorthand, which is what the specification says it is.
func OperationKeyword(query string) string {
	head := StripComments(query)
	if brace := strings.Index(head, "{"); brace >= 0 {
		head = head[:brace]
	}
	for _, kw := range []string{"mutation", "subscription", "query"} {
		if regexp.MustCompile(`\b` + kw + `\b`).MatchString(head) {
			return kw
		}
	}
	return "query"
}

// StripComments blanks out "#" comments and the contents of string literals,
// keeping every byte offset and newline intact so callers can still address
// the original text by line and column. The highlighting and completion
// producers both need it: a "{" inside a comment is not a selection set.
func StripComments(src string) string {
	out := []byte(src)
	inString, inComment, escaped := false, false, false
	for i := 0; i < len(out); i++ {
		c := out[i]
		switch {
		case c == '\n':
			inComment, inString, escaped = false, false, false
		case inComment:
			out[i] = ' '
		case inString:
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
				continue
			}
			out[i] = ' '
		case c == '"':
			inString = true
		case c == '#':
			inComment = true
			out[i] = ' '
		}
	}
	return string(out)
}

// envelope is the JSON body a GraphQL request sends. The field order is the
// marshalled order, so the payload is byte-stable across runs.
type envelope struct {
	Query         string          `json:"query"`
	Variables     json.RawMessage `json:"variables,omitempty"`
	OperationName string          `json:"operationName,omitempty"`
}

// Envelope builds the JSON request body of a GraphQL call. Empty variables and
// an anonymous operation are left out rather than sent as null, which some
// servers reject. Variables that are not valid JSON fail here, so a typo in the
// variables object is reported instead of posted.
func Envelope(query, variables, operationName string) ([]byte, error) {
	env := envelope{Query: query, OperationName: operationName}
	if v := strings.TrimSpace(variables); v != "" {
		if !json.Valid([]byte(v)) {
			return nil, fmt.Errorf("graphql variables are not valid JSON")
		}
		env.Variables = json.RawMessage(v)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// A query is full of "<", ">" and "&"; escaping them would make the
	// payload unreadable in the request snapshot and in a curl export.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(env); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// BodyFor builds the wire body of a GraphQL request from a whole request body
// (query plus optional variables) and the request's Content-Type: the JSON
// envelope, or — for `application/graphql` — the query text alone.
func BodyFor(body, contentType string) ([]byte, error) {
	query, variables, _ := SplitBody(strings.Split(body, "\n"))
	if IsRawMedia(contentType) {
		return []byte(query), nil
	}
	return Envelope(query, variables, OperationName(query))
}
