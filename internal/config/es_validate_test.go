package config

import "testing"

// ESURLError is the shared connection check (#1927): the lenient config
// validator and the strict settings form must agree on what an endpoint URL
// is, so the messages are pinned here.
func TestESURLError(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"http://localhost:9200", ""},
		{"https://es.example.com", ""},
		{" https://es.example.com ", ""}, // surrounding whitespace is tolerated
		{"", "url is required"},
		{"   ", "url is required"},
		{"localhost:9200", "url must start with http:// or https://"},
		{"ftp://x", "url must start with http:// or https://"},
		{"http://", "url has no host"},
		{"http://%zz", "url does not parse: parse \"http://%zz\": invalid URL escape \"%zz\""},
	}
	for _, c := range cases {
		if got := ESURLError(c.url); got != c.want {
			t.Errorf("ESURLError(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

// [[elasticsearch.endpoints]] validation (#1927): entries the console could
// never connect with — no name, duplicate name, unusable URL — are dropped
// with a diagnostic; both auth schemes at once degrade to basic auth. The
// settings form rejects all of these outright, but a config file on disk
// must still load.
func TestValidateESEndpoints(t *testing.T) {
	c := defaults()
	c.Elasticsearch.Endpoints = []ESEndpoint{
		{Name: "ok", URL: "http://localhost:9200"},
		{Name: "", URL: "http://x"},
		{Name: "ok", URL: "http://dupe"},
		{Name: "badurl", URL: "not a url"},
		{Name: "both", URL: "https://x", Username: "u", Password: "p", APIKey: "k"},
	}
	diags := validate(c)
	if got := len(c.Elasticsearch.Endpoints); got != 2 {
		t.Fatalf("kept %d endpoints, want 2 (ok, both): %+v", got, c.Elasticsearch.Endpoints)
	}
	if c.Elasticsearch.Endpoints[0].Name != "ok" || c.Elasticsearch.Endpoints[1].Name != "both" {
		t.Fatalf("kept the wrong entries: %+v", c.Elasticsearch.Endpoints)
	}
	if c.Elasticsearch.Endpoints[1].APIKey != "" || c.Elasticsearch.Endpoints[1].Username != "u" {
		t.Fatalf("both-auth entry should keep basic auth and drop api_key: %+v", c.Elasticsearch.Endpoints[1])
	}
	bad := 0
	for _, d := range diags {
		if d.Field == "elasticsearch.endpoints" {
			bad++
		}
	}
	if bad != 4 {
		t.Fatalf("want 4 endpoint diagnostics (no name, dupe, bad url, both auth), got %d: %v", bad, diags)
	}
}
