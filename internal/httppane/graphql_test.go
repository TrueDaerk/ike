package httppane

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"ike/internal/httpclient"
)

// graphQLSample is a GraphQL answer that failed the way GraphQL fails: HTTP
// 200, a null data member and an errors array.
func graphQLSample(body string) *httpclient.Response {
	return &httpclient.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       []byte(body),
		Duration:   12 * time.Millisecond,
		RequestKey: "hero",
		Request: &httpclient.RequestSnapshot{
			Method:  "POST",
			URL:     "https://example.com/graphql",
			Headers: http.Header{"Content-Type": {"application/json"}},
			Body:    []byte(`{"query":"{ hero { name } }"}`),
		},
	}
}

func TestGraphQLErrorsRenderAboveTheBody(t *testing.T) {
	m := New(nil)
	m.SetSize(100, 24)
	m.Set("hero", graphQLSample(`{"data":null,"errors":[`+
		`{"message":"Cannot query field \"nope\" on type \"Query\".","locations":[{"line":1,"column":3}]},`+
		`{"message":"and another"}]}`))

	if n := len(m.GraphQLErrors()); n != 2 {
		t.Fatalf("GraphQLErrors = %d, want 2", n)
	}
	// The status row says how many, since the status code says 200.
	statusRow := m.RowText(0)
	if !strings.Contains(statusRow, "200 OK") || !strings.Contains(statusRow, "2 errors") {
		t.Errorf("status row = %q, want the error count on it", statusRow)
	}
	// The block sits above the body: the first error row must come before the
	// first body row.
	errRow, bodyRow := -1, -1
	for i := 0; i < m.Rows(); i++ {
		text := m.RowText(i)
		if errRow < 0 && strings.Contains(text, "Cannot query field") {
			errRow = i
		}
		if bodyRow < 0 && strings.Contains(text, `"data"`) {
			bodyRow = i
		}
	}
	if errRow < 0 {
		t.Fatalf("the error message never made it into the rows")
	}
	if bodyRow < 0 || errRow > bodyRow {
		t.Errorf("error row %d is not above the body row %d", errRow, bodyRow)
	}
	// The location travels with the message — it is what points the caret.
	if !strings.Contains(m.RowText(errRow), "query 1:3") {
		t.Errorf("error row = %q, want the query location", m.RowText(errRow))
	}
	if !strings.Contains(m.View(), "and another") {
		t.Errorf("the second error is not on screen:\n%s", m.View())
	}
}

// A GraphQL answer without errors renders exactly like any other JSON
// response: no block, no count, nothing added.
func TestGraphQLSuccessRendersUnchanged(t *testing.T) {
	m := New(nil)
	m.SetSize(100, 24)
	m.Set("hero", graphQLSample(`{"data":{"hero":{"name":"R2-D2"}}}`))

	if errs := m.GraphQLErrors(); len(errs) != 0 {
		t.Fatalf("GraphQLErrors = %v, want none", errs)
	}
	if strings.Contains(m.RowText(0), "error") {
		t.Errorf("status row = %q, want no error mention", m.RowText(0))
	}
	if !strings.Contains(m.View(), "R2-D2") {
		t.Errorf("the body is missing:\n%s", m.View())
	}
}

// Browsing back to a successful entry must drop the previous entry's error
// block: the rows are recomposed, and so is the count behind them.
func TestGraphQLErrorsResetOnHistoryStep(t *testing.T) {
	m := New(nil)
	m.SetSize(100, 24)
	m.Set("hero", graphQLSample(`{"data":null,"errors":[{"message":"boom"}]}`))
	m.SetHistory([]HistoryItem{
		{Resp: graphQLSample(`{"data":null,"errors":[{"message":"boom"}]}`)},
		{Resp: graphQLSample(`{"data":{"hero":{"name":"R2-D2"}}}`), At: time.Now()},
	})
	m.showHistory(1)
	if errs := m.GraphQLErrors(); len(errs) != 0 {
		t.Errorf("GraphQLErrors = %v after stepping to a clean entry, want none", errs)
	}
	if strings.Contains(m.View(), "boom") {
		t.Errorf("the previous entry's error block survived:\n%s", m.View())
	}
}
