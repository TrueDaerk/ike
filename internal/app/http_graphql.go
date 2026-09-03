package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/graphql"
	"ike/internal/host"
	"ike/internal/httpclient"
	"ike/internal/httpfile"
)

// http_graphql.go hosts the two schema commands of the GraphQL support
// (#2423). Writing a query blind is the part of a GraphQL call that hurts:
// the server knows every field, argument and type, and says so through the
// introspection query — but only if somebody asks.
//
//   - http.graphqlIntrospect asks, over the request's own URL and headers (so
//     an authenticated endpoint answers), and caches the schema per host under
//     the project state directory. The completion source in
//     plugins/languages/http reads that cache; nothing else has to happen for
//     fields and arguments to start completing.
//   - http.graphqlSchema opens the cached schema as SDL in a scratch file —
//     the readable form of the same data, for the questions a popup cannot
//     answer ("what else is on this type?").
//
// Both act on the GRAPHQL block under the caret: its URL is the endpoint and
// its headers are the credentials, so neither needs a prompt.

// HTTPGraphQLIntrospectMsg runs http.graphqlIntrospect (palette, #2423):
// introspect the endpoint of the GRAPHQL block under the caret and cache the
// schema for completion.
type HTTPGraphQLIntrospectMsg struct{}

// HTTPGraphQLSchemaMsg runs http.graphqlSchema (palette, #2423): open the
// cached schema of the block's endpoint as SDL in a scratch file.
type HTTPGraphQLSchemaMsg struct{}

// httpGraphQLSchemaMsg carries an introspection dispatch back onto the update
// loop. The exchange runs off-loop like every other dispatch; only the storing
// and the notice belong on the loop.
type httpGraphQLSchemaMsg struct {
	Host   string
	Schema *graphql.Schema
	Err    error
}

// graphqlRequestAtCursor resolves the GRAPHQL block under the caret through
// the same variable chain a dispatch uses, so the endpoint and the headers are
// the ones a run would actually use. Every failure explains itself and yields
// false; what is named is the command, so a palette entry never fails mutely.
func (m *Model) graphqlRequestAtCursor(command string) (*httpfile.Request, bool) {
	ed := m.activeEditor()
	if ed == nil || !isHTTPBuffer(ed) {
		m.host.Notify(host.Info, command+": not an .http file")
		return nil, false
	}
	f := httpfile.Parse(ed.Text())
	line, _ := ed.CursorPos()
	req, ok := f.RequestAt(line + 1)
	if !ok {
		m.host.Notify(host.Info, command+": no request under the cursor")
		return nil, false
	}
	if req.GraphQL == nil {
		m.host.Notify(host.Info, command+": "+requestLabel(req)+" is not a GRAPHQL block — write the request line as \"GRAPHQL <url>\"")
		return nil, false
	}
	vars, hint, err := m.httpVars(httpSource(ed), f)
	if err != nil {
		m.host.Notify(host.Error, command+": "+err.Error())
		return nil, false
	}
	vars.Lookup = os.LookupEnv // closes the chain, exactly as dispatch does
	resolved, err := req.ResolveVars(vars)
	if err != nil {
		notice := command + ": " + err.Error()
		if hint != "" {
			notice += " — " + hint
		}
		m.host.Notify(host.Error, notice)
		return nil, false
	}
	return resolved, true
}

// introspectGraphQLSchema runs http.graphqlIntrospect (#2423): post the
// introspection query to the block's endpoint with its headers, and cache what
// comes back. The exchange runs off-loop, like every other dispatch.
func (m *Model) introspectGraphQLSchema() tea.Cmd {
	resolved, ok := m.graphqlRequestAtCursor("http.graphqlIntrospect")
	if !ok {
		return nil
	}
	endpoint, ok := graphql.HostOf(resolved.Target)
	if !ok {
		m.host.Notify(host.Error, "http.graphqlIntrospect: "+resolved.Target+" has no host to cache a schema under")
		return nil
	}
	probe := introspectionRequest(resolved)
	m.host.Notify(host.Info, "http.graphqlIntrospect: asking "+endpoint+" for its schema…")
	return func() tea.Msg {
		resp, err := httpclient.Dispatch(context.Background(), probe, httpclient.Options{})
		if err != nil {
			return httpGraphQLSchemaMsg{Host: endpoint, Err: err}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return httpGraphQLSchemaMsg{Host: endpoint,
				Err: fmt.Errorf("%s answered %s", endpoint, resp.Status)}
		}
		body, err := resp.FullBody()
		if err != nil {
			return httpGraphQLSchemaMsg{Host: endpoint, Err: err}
		}
		schema, err := graphql.ParseIntrospection(body)
		return httpGraphQLSchemaMsg{Host: endpoint, Schema: schema, Err: err}
	}
}

// introspectionRequest builds the probe: the block's endpoint and headers,
// with the introspection query as the body. The author's own Content-Type is
// dropped — the probe is always the JSON envelope, even for a block that sends
// `application/graphql` — while every other header (Authorization above all)
// travels, since a schema behind a login is the common case.
func introspectionRequest(resolved *httpfile.Request) *httpfile.Request {
	probe := &httpfile.Request{
		Name:   "graphql introspection",
		Method: "POST",
		Target: resolved.Target,
		Proto:  httpfile.DefaultProto,
	}
	for _, h := range resolved.Headers {
		if strings.EqualFold(h.Name, "Content-Type") || strings.EqualFold(h.Name, "Content-Length") {
			continue
		}
		probe.Headers = append(probe.Headers, h)
	}
	probe.Headers = append(probe.Headers, httpfile.Header{Name: "Content-Type", Value: "application/json"})
	// Built here rather than through the GRAPHQL block path: the query is
	// ours, it holds no placeholders and it carries no variables.
	body, _ := graphql.Envelope(graphql.IntrospectionQuery, "", "IntrospectionQuery")
	probe.Body = string(body)
	return probe
}

// storeGraphQLSchema lands a finished introspection: cache it, and say what
// the endpoint offers. A failure names the endpoint — "the server refused" and
// "the request never arrived" are different problems.
func (m *Model) storeGraphQLSchema(msg httpGraphQLSchemaMsg) {
	if msg.Err != nil {
		m.host.Notify(host.Error, "http.graphqlIntrospect: "+msg.Host+": "+msg.Err.Error())
		return
	}
	cache := graphql.NewCache()
	if err := cache.Store(msg.Host, msg.Schema); err != nil {
		m.host.Notify(host.Error, "http.graphqlIntrospect: "+err.Error())
		return
	}
	m.host.Notify(host.Info, fmt.Sprintf("http.graphqlIntrospect: cached %d types of %s in %s — fields and arguments now complete inside the query",
		len(msg.Schema.Types), msg.Host, cache.Path(msg.Host)))
}

// openGraphQLSchema runs http.graphqlSchema (#2423): the cached schema of the
// block's endpoint as SDL, in a scratch file. Nothing cached explains the one
// step that is missing rather than opening an empty buffer.
func (m Model) openGraphQLSchema() (tea.Model, tea.Cmd) {
	resolved, ok := m.graphqlRequestAtCursor("http.graphqlSchema")
	if !ok {
		return m, nil
	}
	cache := graphql.NewCache()
	schema, ok := cache.For(resolved.Target)
	if !ok {
		where := resolved.Target
		if endpoint, ok := graphql.HostOf(resolved.Target); ok {
			where = endpoint
		}
		m.host.Notify(host.Info, "http.graphqlSchema: no cached schema for "+where+" — run http.graphqlIntrospect first")
		return m, nil
	}
	return m.newScratch("graphql", schema.SDL())
}
