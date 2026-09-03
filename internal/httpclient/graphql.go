package httpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"ike/internal/graphql"
	"ike/internal/httpfile"
)

// graphql.go is the response half of the GraphQL support (#2423). A GraphQL
// server answers a failed operation with HTTP 200 and an `errors` array in the
// body, so the status code alone says nothing: without this, a query with a
// typo looked exactly like a successful one. GraphQLErrors lifts those
// messages out so the viewer can put them above the body and the run can count
// as failed.
//
// Detection is deliberately two-sided — the *request* must look like a GraphQL
// call and the *body* must carry a well-formed `errors` array — so an ordinary
// REST endpoint that happens to answer `{"errors":[…]}` keeps its old
// behaviour. The request side reads the as-sent snapshot (#1832) rather than a
// flag on the response, which means a browsed history entry and a re-sent
// request are recognised exactly like a fresh dispatch.

// graphQLScanLimit bounds the body GraphQLErrors parses. Past it the answer is
// no longer something a person reads in the viewer, and a JSON parse of many
// megabytes on every compose is a cost nobody asked for.
const graphQLScanLimit = 4 << 20

// GraphQLError is one entry of a GraphQL response's `errors` array.
type GraphQLError struct {
	Message string
	// Path is the response-path of the failing field, dot-joined
	// ("hero.friends.0.name"); "" when the server sent none.
	Path string
	// Location is the "line:column" of the offending query token; "" when the
	// server sent none.
	Location string
}

// String renders one error the way the viewer shows it: the message, then
// whatever locating information the server supplied.
func (e GraphQLError) String() string {
	var tail []string
	if e.Path != "" {
		tail = append(tail, "at "+e.Path)
	}
	if e.Location != "" {
		tail = append(tail, "query "+e.Location)
	}
	if len(tail) == 0 {
		return e.Message
	}
	return e.Message + " (" + strings.Join(tail, ", ") + ")"
}

// wireGraphQLBody is the shape a GraphQL answer must have to be read as one:
// a top-level object whose members are drawn from the three the specification
// defines. An extra member means this is some other API's JSON and the errors
// it carries are not GraphQL's.
type wireGraphQLBody struct {
	Data       json.RawMessage    `json:"data"`
	Errors     []wireGraphQLError `json:"errors"`
	Extensions json.RawMessage    `json:"extensions"`
}

type wireGraphQLError struct {
	Message   string `json:"message"`
	Path      []any  `json:"path"`
	Locations []struct {
		Line   int `json:"line"`
		Column int `json:"column"`
	} `json:"locations"`
}

// IsGraphQLRequest reports whether a stored request went out as a GraphQL call
// (#2423): the raw-query media type, or a JSON body carrying a "query" member
// — which is the envelope this client and every GraphQL client sends.
func IsGraphQLRequest(s *RequestSnapshot) bool {
	if s == nil {
		return false
	}
	ct := s.Headers.Get("Content-Type")
	if graphql.IsRawMedia(ct) {
		return true
	}
	if len(s.Body) == 0 || len(s.Body) > graphQLScanLimit {
		return false
	}
	var env struct {
		Query *string `json:"query"`
	}
	return json.Unmarshal(s.Body, &env) == nil && env.Query != nil
}

// IsGraphQL reports whether this exchange was a GraphQL call — the gate for
// every GraphQL-specific piece of the viewer.
func (r *Response) IsGraphQL() bool {
	return r != nil && IsGraphQLRequest(r.Request)
}

// GraphQLErrors returns the errors a GraphQL response carries, nil when there
// are none — which includes every non-GraphQL exchange, a truncated or spooled
// body (only its head is in memory, so a parse would fail on a cut object),
// and a body that is not the three-member GraphQL envelope.
func (r *Response) GraphQLErrors() []GraphQLError {
	if !r.IsGraphQL() || r.Truncated {
		return nil
	}
	body := r.Body
	if len(body) == 0 || len(body) > graphQLScanLimit {
		return nil
	}
	if r.BodySize > len(body) {
		return nil // spooled: what is in memory is only the head
	}
	var env wireGraphQLBody
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&env); err != nil {
		return nil
	}
	var out []GraphQLError
	for _, e := range env.Errors {
		if e.Message == "" {
			return nil // not the GraphQL shape after all
		}
		out = append(out, GraphQLError{
			Message:  e.Message,
			Path:     graphQLPath(e.Path),
			Location: graphQLLocation(e.Locations),
		})
	}
	return out
}

// graphQLPath dot-joins a response path; a numeric list index renders as the
// integer it is rather than as JSON's float.
func graphQLPath(path []any) string {
	parts := make([]string, 0, len(path))
	for _, p := range path {
		switch v := p.(type) {
		case string:
			parts = append(parts, v)
		case float64:
			parts = append(parts, strconv.Itoa(int(v)))
		default:
			parts = append(parts, fmt.Sprint(v))
		}
	}
	return strings.Join(parts, ".")
}

// graphQLLocation renders the first source location of an error; further ones
// are dropped, since a single "line:column" is what points the caret.
func graphQLLocation(locs []struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}) string {
	if len(locs) == 0 || locs[0].Line == 0 {
		return ""
	}
	return fmt.Sprintf("%d:%d", locs[0].Line, locs[0].Column)
}

// graphQLWarning is the one thing a GraphQL dispatch has to say up front: with
// `Content-Type: application/graphql` the body *is* the query, so a variables
// object the author wrote below it has nowhere to travel. Sending it silently
// stripped would look like a server that ignores variables.
func graphQLWarning(resolved *httpfile.Request) string {
	if resolved.GraphQL == nil || resolved.GraphQL.Variables == "" {
		return ""
	}
	ct, _ := resolved.Header("Content-Type")
	if !graphql.IsRawMedia(ct) {
		return ""
	}
	return "graphql: Content-Type application/graphql sends the query alone — the variables block was not sent"
}
