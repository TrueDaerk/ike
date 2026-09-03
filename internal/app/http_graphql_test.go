package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/graphql"
	"ike/internal/pane"
)

// http_graphql_test.go drives the two schema commands (#2423) against a fake
// introspection server, plus the failure the GraphQL support exists for: an
// operation that fails with HTTP 200.

const introspectionAnswer = `{"data":{"__schema":{
  "queryType": {"name": "Query"},
  "mutationType": null,
  "subscriptionType": null,
  "types": [
    {"kind":"OBJECT","name":"Query","description":"The root.","fields":[
      {"name":"hero","description":null,"isDeprecated":false,
       "args":[{"name":"episode","description":null,"defaultValue":null,
                "type":{"kind":"ENUM","name":"Episode","ofType":null}}],
       "type":{"kind":"OBJECT","name":"Character","ofType":null}}],
     "inputFields":null,"interfaces":[],"enumValues":null,"possibleTypes":null},
    {"kind":"OBJECT","name":"Character","description":null,"fields":[
      {"name":"name","description":null,"isDeprecated":false,"args":[],
       "type":{"kind":"SCALAR","name":"String","ofType":null}}],
     "inputFields":null,"interfaces":[],"enumValues":null,"possibleTypes":null},
    {"kind":"ENUM","name":"Episode","description":null,"fields":null,
     "inputFields":null,"interfaces":null,
     "enumValues":[{"name":"EMPIRE","description":null,"isDeprecated":false}],
     "possibleTypes":null}
  ]}}}`

// graphQLServer answers an introspection query with the fixture above and
// records the request it saw, so the test can check that the block's own
// headers travelled.
func graphQLServer(t *testing.T) (*httptest.Server, *http.Header) {
	t.Helper()
	var seen http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		var env struct {
			OperationName string `json:"operationName"`
		}
		_ = json.Unmarshal(body, &env)
		w.Header().Set("Content-Type", "application/json")
		if env.OperationName != "IntrospectionQuery" {
			io.WriteString(w, `{"errors":[{"message":"not an introspection query"}]}`)
			return
		}
		io.WriteString(w, introspectionAnswer)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// openGraphQLFile writes a GRAPHQL request file and opens it with the caret in
// the block.
func openGraphQLFile(t *testing.T, m Model, url string) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "api.http")
	src := "### hero\nGRAPHQL " + url + "\nAuthorization: Bearer t0ken\n\n" +
		"query Hero($episode: Episode) {\n  hero(episode: $episode) { name }\n}\n\n" +
		`{"episode":"EMPIRE"}` + "\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	return out.(Model)
}

func TestGraphQLIntrospectCachesTheSchema(t *testing.T) {
	srv, seen := graphQLServer(t)
	m := httpApp(t)
	m = openGraphQLFile(t, m, srv.URL+"/graphql")

	out, cmd := m.Update(HTTPGraphQLIntrospectMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("http.graphqlIntrospect on a GRAPHQL block must dispatch")
	}
	msg := drainGraphQLSchema(t, cmd)
	if msg.Err != nil {
		t.Fatalf("introspection: %v", msg.Err)
	}
	// The block's credentials travel: a schema behind a login is the norm.
	if got := seen.Get("Authorization"); got != "Bearer t0ken" {
		t.Errorf("Authorization = %q, want the block's own header", got)
	}
	if got := seen.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	out, _ = m.Update(msg)
	m = out.(Model)
	cached, ok := graphql.NewCache().For(srv.URL + "/graphql")
	if !ok {
		t.Fatal("the schema was not cached")
	}
	if _, ok := cached.FieldByName("Query", "hero"); !ok {
		t.Errorf("the cached schema lost Query.hero: %+v", cached)
	}
}

func TestGraphQLIntrospectRejectsNonGraphQLBlocks(t *testing.T) {
	m := httpApp(t)
	path := filepath.Join(t.TempDir(), "req.http")
	if err := os.WriteFile(path, []byte("GET https://example.com/things\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	if _, cmd := m.Update(HTTPGraphQLIntrospectMsg{}); cmd != nil {
		if _, ok := findGraphQLSchema(cmd); ok {
			t.Fatal("a plain GET block must not be introspected")
		}
	}
}

// drainGraphQLSchema pulls the introspection result out of the command, which
// comes back batched with the notification timer of the "asking…" notice.
func drainGraphQLSchema(t *testing.T, cmd tea.Cmd) httpGraphQLSchemaMsg {
	t.Helper()
	msg, ok := findGraphQLSchema(cmd)
	if !ok {
		t.Fatal("no introspection result produced")
	}
	return msg
}

func findGraphQLSchema(cmd tea.Cmd) (httpGraphQLSchemaMsg, bool) {
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case httpGraphQLSchemaMsg:
			return msg, true
		case tea.BatchMsg:
			queue = append(queue, msg...)
		}
	}
	return httpGraphQLSchemaMsg{}, false
}

func TestGraphQLSchemaOpensSDLScratch(t *testing.T) {
	srv, _ := graphQLServer(t)
	m := httpApp(t)
	m = openGraphQLFile(t, m, srv.URL+"/graphql")

	// Nothing cached yet: the command explains the missing step instead of
	// opening an empty buffer.
	out, _ := m.Update(HTTPGraphQLSchemaMsg{})
	m = out.(Model)
	if got := m.activeEditor().Path(); strings.HasSuffix(got, ".graphql") {
		t.Fatalf("a scratch opened without a cached schema: %s", got)
	}

	out, cmd := m.Update(HTTPGraphQLIntrospectMsg{})
	m = out.(Model)
	out, _ = m.Update(drainGraphQLSchema(t, cmd))
	m = out.(Model)

	out, _ = m.Update(HTTPGraphQLSchemaMsg{})
	m = out.(Model)
	ed := m.activeEditor()
	if ed == nil || !strings.HasSuffix(ed.Path(), ".graphql") {
		t.Fatalf("http.graphqlSchema did not open a .graphql scratch: %v", ed)
	}
	text := ed.Text()
	for _, want := range []string{"schema {", "type Query {", "hero(episode: Episode): Character", "enum Episode {"} {
		if !strings.Contains(text, want) {
			t.Errorf("the SDL scratch is missing %q:\n%s", want, text)
		}
	}
}

// A GraphQL operation that failed answers with HTTP 200 (#2423): the viewer
// must still show it as a failure, and the completion notice must say so.
func TestGraphQLErrorsSurfaceInTheViewer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":null,"errors":[{"message":"Unknown argument \"episode\"."}]}`)
	}))
	defer srv.Close()

	m := httpApp(t)
	m = openGraphQLFile(t, m, srv.URL+"/graphql")
	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("http.run on a GRAPHQL block must dispatch")
	}
	resp := drainHTTPResponse(t, cmd)
	if resp.Err != nil {
		t.Fatal(resp.Err)
	}
	if resp.Resp.StatusCode != 200 {
		t.Fatalf("status = %d, want the 200 a failed GraphQL call answers with", resp.Resp.StatusCode)
	}
	if errs := resp.Resp.GraphQLErrors(); len(errs) != 1 {
		t.Fatalf("GraphQL errors = %v, want one", errs)
	}
	out, _ = m.Update(resp)
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.HTTPKey) {
		t.Fatal("viewer must open")
	}
	view := m.httpPanel().View()
	if !strings.Contains(view, "Unknown argument") {
		t.Errorf("the error block is not on screen:\n%s", view)
	}
	if !strings.Contains(m.httpPanel().RowText(0), "1 error") {
		t.Errorf("status row = %q, want the error count", m.httpPanel().RowText(0))
	}
}

// The palette must reach both commands, since neither carries a chord.
func TestGraphQLCommandsAreRegistered(t *testing.T) {
	m := newSized()
	for _, id := range []string{"http.graphqlIntrospect", "http.graphqlSchema"} {
		if _, ok := m.reg.Command(id); !ok {
			t.Errorf("%s must be a registry command", id)
		}
	}
}
