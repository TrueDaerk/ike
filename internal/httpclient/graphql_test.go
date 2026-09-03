package httpclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ike/internal/httpfile"
)

// graphql_test.go covers both halves of #2423 from the dispatcher's side: what
// a GRAPHQL block puts on the wire, and what the viewer is told about the
// answer that comes back.

func TestDispatchGraphQLSendsTheEnvelope(t *testing.T) {
	var gotMethod, gotType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotType = r.Method, r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":{"hero":{"name":"R2-D2"}}}`)
	}))
	defer srv.Close()

	src := "GRAPHQL " + srv.URL + "/graphql\n\n" +
		"query Hero($episode: Episode) {\n  hero(episode: $episode) { name }\n}\n\n" +
		`{"episode":"EMPIRE"}` + "\n"
	req := httpfile.Parse(src).Requests[0]
	resp, err := Dispatch(context.Background(), req, Options{DisableConfig: true})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotType)
	}
	var env struct {
		Query         string          `json:"query"`
		Variables     json.RawMessage `json:"variables"`
		OperationName string          `json:"operationName"`
	}
	if err := json.Unmarshal(gotBody, &env); err != nil {
		t.Fatalf("the body is not the JSON envelope: %v (%s)", err, gotBody)
	}
	if !strings.Contains(env.Query, "hero(episode: $episode)") {
		t.Errorf("query = %q", env.Query)
	}
	if string(env.Variables) != `{"episode":"EMPIRE"}` {
		t.Errorf("variables = %s", env.Variables)
	}
	if env.OperationName != "Hero" {
		t.Errorf("operationName = %q, want Hero", env.OperationName)
	}
	// The snapshot is what re-send repeats and what the viewer reads to decide
	// this was a GraphQL exchange at all.
	if !resp.IsGraphQL() {
		t.Error("the response was not recognised as a GraphQL exchange")
	}
	if len(resp.GraphQLErrors()) != 0 {
		t.Errorf("errors = %v, want none", resp.GraphQLErrors())
	}
}

func TestDispatchGraphQLRawMediaSendsTheQueryAndWarns(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":{}}`)
	}))
	defer srv.Close()

	src := "GRAPHQL " + srv.URL + "\nContent-Type: application/graphql\n\n" +
		"{ hero { name } }\n\n" + `{"unused":1}` + "\n"
	resp, err := Dispatch(context.Background(), httpfile.Parse(src).Requests[0],
		Options{DisableConfig: true})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if string(gotBody) != "{ hero { name } }" {
		t.Errorf("body = %q, want the raw query", gotBody)
	}
	// Variables have nowhere to travel under that media type; dropping them
	// silently would look like a server that ignores them.
	if !hasWarning(resp.Warnings, "variables block was not sent") {
		t.Errorf("warnings = %v, want a note about the dropped variables", resp.Warnings)
	}
}

func TestDispatchGraphQLBodyFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "hero.graphql", "query Hero { hero { name } }\n")
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	src := "GRAPHQL " + srv.URL + "\n\n< ./hero.graphql\n"
	_, err := Dispatch(context.Background(), httpfile.Parse(src).Requests[0],
		Options{DisableConfig: true, BaseDir: dir})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var env struct {
		Query         string `json:"query"`
		OperationName string `json:"operationName"`
	}
	if err := json.Unmarshal(gotBody, &env); err != nil {
		t.Fatalf("the external body was not enveloped: %v (%s)", err, gotBody)
	}
	if env.OperationName != "Hero" || !strings.Contains(env.Query, "hero") {
		t.Errorf("envelope = %+v", env)
	}
}

func TestGraphQLErrorsAreLiftedOutOfTheBody(t *testing.T) {
	resp := graphQLResponse(`{"data":null,"errors":[
		{"message":"Cannot query field \"nope\" on type \"Query\".",
		 "locations":[{"line":2,"column":3}],"path":["hero","friends",0,"name"]},
		{"message":"second"}]}`)
	errs := resp.GraphQLErrors()
	if len(errs) != 2 {
		t.Fatalf("errors = %d, want 2", len(errs))
	}
	if errs[0].Path != "hero.friends.0.name" {
		t.Errorf("path = %q", errs[0].Path)
	}
	if errs[0].Location != "2:3" {
		t.Errorf("location = %q", errs[0].Location)
	}
	if got := errs[0].String(); !strings.Contains(got, "at hero.friends.0.name") ||
		!strings.Contains(got, "query 2:3") {
		t.Errorf("String() = %q", got)
	}
	if got := errs[1].String(); got != "second" {
		t.Errorf("String() of a bare message = %q", got)
	}
}

// A response with an empty errors array is a success, not a failure.
func TestGraphQLErrorsIgnoresEmptyAndAbsentArrays(t *testing.T) {
	for _, body := range []string{`{"data":{"a":1}}`, `{"data":null,"errors":[]}`} {
		if errs := graphQLResponse(body).GraphQLErrors(); len(errs) != 0 {
			t.Errorf("%s yielded %v, want no errors", body, errs)
		}
	}
}

// An ordinary REST endpoint answering {"errors":[…]} keeps its old behaviour:
// the request was never a GraphQL call.
func TestGraphQLErrorsIgnoreNonGraphQLExchanges(t *testing.T) {
	resp := &Response{
		StatusCode: 200,
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       []byte(`{"errors":[{"message":"boom"}]}`),
		Request: &RequestSnapshot{Method: "POST", URL: "https://example.com/things",
			Headers: http.Header{"Content-Type": {"application/json"}},
			Body:    []byte(`{"name":"x"}`)},
	}
	if resp.IsGraphQL() {
		t.Fatal("a plain JSON POST was read as a GraphQL request")
	}
	if errs := resp.GraphQLErrors(); errs != nil {
		t.Errorf("errors = %v, want none", errs)
	}
}

// A body carrying members outside data/errors/extensions is some other API's
// JSON, whatever the request looked like.
func TestGraphQLErrorsRejectForeignEnvelopes(t *testing.T) {
	resp := graphQLResponse(`{"errors":[{"message":"boom"}],"status":"failed"}`)
	if errs := resp.GraphQLErrors(); errs != nil {
		t.Errorf("errors = %v, want none for a non-GraphQL envelope", errs)
	}
}

// graphQLResponse composes a response whose request looks like a GraphQL call,
// so only the body under test decides the outcome.
func graphQLResponse(body string) *Response {
	return &Response{
		Status:     "200 OK",
		StatusCode: 200,
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       []byte(body),
		Request: &RequestSnapshot{
			Method:  "POST",
			URL:     "https://example.com/graphql",
			Headers: http.Header{"Content-Type": {"application/json"}},
			Body:    []byte(`{"query":"{ hero { name } }"}`),
		},
	}
}

func hasWarning(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}
