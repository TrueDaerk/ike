package httpdiff

import (
	"net/http"
	"strings"
	"testing"

	"ike/internal/httphistory"
)

// TestIgnoreHeaderMatching covers the noise filter's rules (#2247): header
// names are case-insensitive, a trailing "*" is a family, and a bare "*"
// never hides everything.
func TestIgnoreHeaderMatching(t *testing.T) {
	patterns := []string{"date", "X-Request-Id", "x-amz-*", "", "*"}
	cases := []struct {
		name string
		want bool
	}{
		{"Date", true},
		{"DATE", true},
		{"x-request-id", true},
		{"X-Amz-Cf-Pop", true},
		{"x-amz-", true},
		{"Content-Type", false},
		{"x-amzn-requestid", false}, // "amzn" is not the "amz-" family
		{"", false},
	}
	for _, c := range cases {
		if got := IgnoreHeader(c.name, patterns); got != c.want {
			t.Errorf("IgnoreHeader(%q) = %v, want %v", c.name, got, c.want)
		}
	}
	if IgnoreHeader("Date", nil) {
		t.Error("an empty pattern list must keep every header")
	}
	if IgnoreHeader("Anything", []string{"*"}) {
		t.Error("a bare \"*\" must not hide every header")
	}
}

// TestHeadersTextFilteredDropsNoise: the filtered render leaves the volatile
// headers out and keeps the rest sorted (#2247).
func TestHeadersTextFilteredDropsNoise(t *testing.T) {
	h := http.Header{
		"Date":         {"Mon, 01 Jan 2024 00:00:00 GMT"},
		"X-Request-Id": {"abc123"},
		"Content-Type": {"application/json"},
		"Etag":         {"\"v1\""},
	}
	got := HeadersTextFiltered(h, []string{"date", "x-request-id"})
	want := "Content-Type: application/json\nEtag: \"v1\"\n"
	if got != want {
		t.Errorf("filtered headers:\n%q\nwant\n%q", got, want)
	}
	if HeadersTextFiltered(h, nil) != HeadersText(h) {
		t.Error("an empty filter must render like HeadersText")
	}
}

// TestTextFilteredHidesVolatileHeaders is the acceptance criterion of the
// noise filter (#2247): two runs differing only in Date and a request id
// render identically, while a real header change still shows.
func TestTextFilteredHidesVolatileHeaders(t *testing.T) {
	entry := func(date, id, body string) httphistory.Entry {
		return httphistory.Entry{
			Proto: "HTTP/1.1", Status: "200 OK", StatusCode: 200,
			Headers: http.Header{
				"Date":         {date},
				"X-Request-Id": {id},
				"Content-Type": {"application/json"},
			},
			Body: []byte(body),
		}
	}
	ignore := []string{"date", "x-request-id"}
	a := TextFiltered(entry("Mon, 01 Jan 2024 00:00:00 GMT", "one", `{"n":1}`), ignore)
	b := TextFiltered(entry("Tue, 02 Jan 2024 00:00:00 GMT", "two", `{"n":1}`), ignore)
	if a != b {
		t.Errorf("volatile headers must not differ:\n%s\nvs\n%s", a, b)
	}
	if strings.Contains(a, "X-Request-Id") || strings.Contains(a, "Date:") {
		t.Errorf("filtered headers must not be rendered:\n%s", a)
	}
	if !strings.Contains(a, "Content-Type: application/json") {
		t.Errorf("unfiltered headers must survive:\n%s", a)
	}
	c := TextFiltered(entry("Mon, 01 Jan 2024 00:00:00 GMT", "one", `{"n":2}`), ignore)
	if a == c {
		t.Error("a changed body must still differ")
	}
	// Unfiltered, the same pair differs — which is the noise the filter exists for.
	if Text(entry("Mon, 01 Jan 2024 00:00:00 GMT", "one", `{"n":1}`)) ==
		Text(entry("Tue, 02 Jan 2024 00:00:00 GMT", "two", `{"n":1}`)) {
		t.Error("without the filter the volatile headers must differ")
	}
}
