package httpfile

import (
	"strings"

	"ike/internal/graphql"
)

// graphql.go adds the `GRAPHQL <url>` request block (#2423), the JetBrains
// spelling of a GraphQL call: the body is the query/mutation text, and an
// optional JSON `variables` object follows it after a blank line.
//
//	### hero
//	GRAPHQL https://example.com/graphql
//
//	query Hero($episode: Episode) {
//	  hero(episode: $episode) { name }
//	}
//
//	{
//	  "episode": "EMPIRE"
//	}
//
// The parser only *splits* the block — Request.GraphQL holds the query, the
// variables and the operation name, with the line ranges the editor needs to
// highlight and complete each section. The rewrite into the request that goes
// on the wire happens in ResolveVars, once placeholders are substituted:
// method POST, body `{"query":…,"variables":…,"operationName":…}` and
// `Content-Type: application/json` unless the author set one. An author who
// sets `Content-Type: application/graphql` gets the raw query as the body
// instead, which is what that media type means.
//
// The GraphQL language itself — splitting, naming the operation, building the
// envelope — lives in internal/graphql, so the editor's highlighting and
// completion producers read exactly what the dispatcher sends.

// GraphQLMethod is the request-line method that marks a GraphQL block.
const GraphQLMethod = graphql.Method

// GraphQLSpec is the split of one GRAPHQL block's body (#2423). Line numbers
// are 1-based and inclusive, 0 when the section is absent — they address the
// sections *in the file*, which is what the region/span producers and the
// completion source need.
type GraphQLSpec struct {
	// Query is the operation text: everything up to the variables' blank-line
	// separator, or the whole body when there are none.
	Query string
	// Variables is the JSON object text, "" when the block declares none.
	Variables string
	// OperationName is the name of the first named operation in Query, "" for
	// an anonymous one. It travels in the envelope so a document holding
	// several operations still selects one.
	OperationName string

	QueryStart, QueryEnd int
	VarsStart, VarsEnd   int
}

// IsGraphQLMedia reports whether a Content-Type value is the raw-query media
// type `application/graphql`.
func IsGraphQLMedia(contentType string) bool { return graphql.IsRawMedia(contentType) }

// graphQLSpec splits a GRAPHQL block's body. bodyStart/bodyEnd are the body's
// 1-based line range in the file (0 when the block has no body), so the
// returned spec can address the two sections there.
func graphQLSpec(body string, bodyStart, bodyEnd int) *GraphQLSpec {
	spec := &GraphQLSpec{}
	if body == "" {
		return spec
	}
	query, variables, varsLine := graphql.SplitBody(strings.Split(body, "\n"))
	spec.Query, spec.Variables = query, variables
	spec.OperationName = graphql.OperationName(query)
	if bodyStart > 0 {
		spec.QueryStart = bodyStart
		spec.QueryEnd = bodyStart + len(strings.Split(query, "\n")) - 1
		if varsLine >= 0 {
			spec.VarsStart, spec.VarsEnd = bodyStart+varsLine, bodyEnd
		}
	}
	return spec
}

// applyGraphQL rewrites a *resolved* GRAPHQL block into the request that goes
// on the wire (#2423): POST, the JSON envelope as the body, and
// `Content-Type: application/json` when the author set none. With
// `Content-Type: application/graphql` the raw query is the body instead — that
// media type carries the query itself, so there is no envelope to put
// variables in.
//
// An external body (`< ./hero.graphql`, #1305) keeps its directive here: the
// file is only read at dispatch, which builds the envelope from its contents.
func (r *Request) applyGraphQL() error {
	if r.GraphQL == nil {
		return nil
	}
	r.Method = "POST"
	ct, ok := r.Header("Content-Type")
	if !ok {
		ct = "application/json"
		r.Headers = append(r.Headers, Header{Name: "Content-Type", Value: ct})
	}
	if r.BodyFile != "" {
		return nil
	}
	if graphql.IsRawMedia(ct) {
		r.Body = r.GraphQL.Query
		return nil
	}
	body, err := graphql.Envelope(r.GraphQL.Query, r.GraphQL.Variables, r.GraphQL.OperationName)
	if err != nil {
		return err
	}
	r.Body = string(body)
	return nil
}
