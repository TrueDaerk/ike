package httppane

// bench_test.go pins the per-interaction cost of a large open response
// (#2386): with a big body on show, View runs after *every* message, so its
// cost is what every click and keystroke pays. The pathological shape is the
// user's Elasticsearch case — a ~2 MB minified JSON as one single line with
// non-ASCII and plenty of hex identifiers — spooled to a 1 MiB in-memory
// head, which sits under both the pretty-print and highlight caps and
// therefore renders as one giant highlighted row. The many-short-lines shape
// benchmarks alongside so a fix for one form cannot silently regress the
// other.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"ike/internal/httpclient"
)

// largeSingleLineJSON builds a minified single-line JSON document of at least
// n bytes, with hex-hash identifiers and non-ASCII text like an Elasticsearch
// hit list.
func largeSingleLineJSON(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"took":3,"hits":[`)
	for i := 0; b.Len() < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"_id":"%08x%08x%08x","_score":1.0,"_source":{"name":"Grüße München %d","täg":"üöä-%d","trace":"c0ffee%06x"}}`,
			i*2654435761, i+7, i*31, i, i, i)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

// largeResponse is a spooled 2 MB response whose in-memory head is the first
// SpoolThreshold bytes of one minified line — the #2386 starting case. The
// truncated head keeps json.Indent from pretty-printing, so the body composes
// as a single giant row.
func largeResponse() *httpclient.Response {
	body := largeSingleLineJSON(2 << 20)
	return &httpclient.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       body[:httpclient.SpoolThreshold],
		BodySize:   len(body),
		SpoolPath:  "elsewhere.spool", // never read: only Spooled()/BodyBytes gates
		Duration:   80 * time.Millisecond,
		RequestKey: "search",
	}
}

// largeManyLinesResponse is the same volume as many short lines (NDJSON-ish).
func largeManyLinesResponse() *httpclient.Response {
	var b strings.Builder
	for i := 0; b.Len() < httpclient.SpoolThreshold; i++ {
		fmt.Fprintf(&b, `{"_id":"%08x%08x","msg":"Grüße %d","trace":"c0ffee%06x"}`+"\n", i*2654435761, i+7, i, i)
	}
	body := []byte(b.String())
	return &httpclient.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		Headers:    http.Header{"Content-Type": {"application/x-ndjson"}},
		Body:       body,
		BodySize:   len(body) * 2,
		SpoolPath:  "elsewhere.spool",
		Duration:   80 * time.Millisecond,
		RequestKey: "search",
	}
}

// benchModel composes resp in a viewer sized like a real pane, with the
// syntax pass landed — the steady state the user sits in while the response
// is open.
func benchModel(b *testing.B, resp *httpclient.Response) *Model {
	b.Helper()
	m := New(nil)
	m.SetSize(220, 50)
	m.Set("search", resp)
	m.FinishHighlight()
	b.ResetTimer()
	return &m
}

func BenchmarkViewLargeSingleLine(b *testing.B) {
	m := benchModel(b, largeResponse())
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkViewLargeSingleLinePanned(b *testing.B) {
	m := benchModel(b, largeResponse())
	m.ScrollX(4096) // deep into the line: the window sits mid-body
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkViewLargeManyLines(b *testing.B) {
	m := benchModel(b, largeManyLinesResponse())
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkMousePressLargeSingleLine(b *testing.B) {
	m := benchModel(b, largeResponse())
	for i := 0; i < b.N; i++ {
		m.MousePress(40, 8)
		m.MouseRelease()
	}
}

func BenchmarkScrollXLargeSingleLine(b *testing.B) {
	m := benchModel(b, largeResponse())
	for i := 0; i < b.N; i++ {
		m.ScrollX(hScrollStep)
		m.ScrollX(-hScrollStep)
	}
}
