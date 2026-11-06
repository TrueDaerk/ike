package httpclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// tripleQuoteServer records the body of the last request it received.
func tripleQuoteServer(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestDispatchSendsTripleQuotedLiteralAsJSONString(t *testing.T) {
	srv, got := tripleQuoteServer(t)
	src := strings.Join([]string{
		"POST " + srv.URL + "/abc",
		"Content-Type: application/json",
		"",
		`{`,
		`  "property": """my multi-`,
		`                 line property`,
		`                 with triple quotes""",`,
		`  "raw": """C:\dir "x" {{who}}"""`,
		`}`,
	}, "\n")
	opts := noConfig
	opts.Lookup = func(n string) (string, bool) { return "me", n == "who" }
	resp, err := Dispatch(context.Background(), parseOne(t, src), opts)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]string
	if err := json.Unmarshal([]byte(*got), &v); err != nil {
		t.Fatalf("sent body is not valid JSON: %v\n%s", err, *got)
	}
	if want := "my multi-\nline property\nwith triple quotes"; v["property"] != want {
		t.Errorf("property = %q, want %q", v["property"], want)
	}
	if want := `C:\dir "x" me`; v["raw"] != want {
		t.Errorf("raw = %q, want %q", v["raw"], want)
	}
	snap := resp.Request
	if curl := snap.Curl(); strings.Contains(curl, `"""`) || !strings.Contains(curl, `"my multi-\nline property\nwith triple quotes"`) {
		t.Errorf("curl export does not carry the converted body: %s", curl)
	}
	// httpie gets one field per member, holding the decoded string
	if httpie := snap.HTTPie(); strings.Contains(httpie, `"""`) || !strings.Contains(httpie, "'property=my multi-\nline property\nwith triple quotes'") {
		t.Errorf("httpie export does not carry the converted body: %s", httpie)
	}
}

func TestDispatchKeepsTripleQuotesInPlainTextBody(t *testing.T) {
	srv, got := tripleQuoteServer(t)
	body := "say \"\"\"\n  hi\n\"\"\""
	src := "POST " + srv.URL + "/abc\nContent-Type: text/plain\n\n" + body
	if _, err := Dispatch(context.Background(), parseOne(t, src), noConfig); err != nil {
		t.Fatal(err)
	}
	if *got != body {
		t.Errorf("sent %q, want %q", *got, body)
	}
}
