package langhttp

import (
	"strings"
	"testing"

	"ike/internal/lang"
)

// registerFake makes a language id resolvable without linking its grammar, so
// the mapping tests do not depend on which language plugins a build links.
func registerFake(t *testing.T, id string, exts ...string) {
	t.Helper()
	lang.Register(lang.Language{ID: id, Extensions: exts})
}

func TestBodyLanguageFromContentType(t *testing.T) {
	registerFake(t, "json", "json")
	registerFake(t, "xml", "xml")
	registerFake(t, "html", "html")
	registerFake(t, "yaml", "yaml")
	registerFake(t, "typescript", "ts", "js")
	registerFake(t, "graphql", "graphql", "gql")

	for _, tc := range []struct {
		ct   string
		want string
	}{
		{"application/json", "json"},
		{"application/json; charset=utf-8", "json"},
		{"  APPLICATION/JSON  ", "json"},
		{"application/vnd.api+json", "json"},
		{"application/vnd.api+json; charset=utf-8", "json"},
		{"text/xml", "xml"},
		{"application/soap+xml", "xml"},
		{"text/html;charset=iso-8859-1", "html"},
		{"application/x-yaml", "yaml"},
		{"application/javascript", "typescript"},
		{"application/graphql", "graphql"},
		{"application/graphql; charset=utf-8", "graphql"},
	} {
		got, ok := bodyLanguage(tc.ct)
		if !ok || got != tc.want {
			t.Errorf("bodyLanguage(%q) = %q,%v; want %q", tc.ct, got, ok, tc.want)
		}
	}
	for _, ct := range []string{"", "application/octet-stream", "text/plain", "nonsense", "multipart/form-data"} {
		if got, ok := bodyLanguage(ct); ok {
			t.Errorf("bodyLanguage(%q) = %q, want no language", ct, got)
		}
	}
}

func TestBodyRegionsSpanEveryTypedBody(t *testing.T) {
	registerFake(t, "json", "json")
	registerFake(t, "xml", "xml")

	src := strings.Join([]string{
		"POST https://example.com/a",     // 0
		"Content-Type: application/json", // 1
		"",                               // 2
		`{`,                              // 3
		`  "a": 1`,                       // 4
		`}`,                              // 5
		"",                               // 6
		"### second",                     // 7
		"POST https://example.com/b",     // 8
		"Content-Type: text/xml",         // 9
		"",                               // 10
		"<root/>",                        // 11
		"",                               // 12
		"### third — no content type",    // 13
		"POST https://example.com/c",     // 14
		"",                               // 15
		"raw payload",                    // 16
	}, "\n")
	lines := strings.Split(src, "\n")

	got := bodyRegions(lines)
	if len(got) != 2 {
		t.Fatalf("regions = %+v, want exactly the two typed bodies", got)
	}
	if got[0].Lang != "json" || got[0].StartLine != 3 || got[0].EndLine != 5 {
		t.Errorf("json region = %+v, want json lines 3–5", got[0])
	}
	if got[0].EndCol != len(lines[5]) {
		t.Errorf("region must end at the end of its last line, EndCol = %d", got[0].EndCol)
	}
	if got[1].Lang != "xml" || got[1].StartLine != 11 || got[1].EndLine != 11 {
		t.Errorf("xml region = %+v, want xml line 11", got[1])
	}
}

// TestBodyRegionsIgnoreBodilessRequests: a request without a body — or with a
// Content-Type but nothing after the blank line — contributes no region.
func TestBodyRegionsIgnoreBodilessRequests(t *testing.T) {
	registerFake(t, "json", "json")
	lines := []string{
		"GET https://example.com/a",
		"Content-Type: application/json",
		"",
		"",
	}
	if got := bodyRegions(lines); len(got) != 0 {
		t.Fatalf("regions = %+v, want none", got)
	}
}

// TestRegionAtResolvesTheBodyLanguage guards the editor-side seam (#1304): the
// registry answers "which language is line N" for a .http buffer.
func TestRegionAtResolvesTheBodyLanguage(t *testing.T) {
	registerFake(t, "json", "json")
	lines := []string{
		"POST https://example.com/a",
		"Content-Type: application/json",
		"",
		"{",
		`  "a": 1`,
		"}",
	}
	if r, ok := lang.RegionAt("http", lines, 4); !ok || r.Lang != "json" {
		t.Fatalf("RegionAt(body line) = %+v,%v; want a json region", r, ok)
	}
	if _, ok := lang.RegionAt("http", lines, 1); ok {
		t.Fatal("a header line must not resolve to the body language")
	}
}

// TestBodyRegionsSkipExternalBodies guards #1305: `< ./payload.json` is a
// directive line, not JSON, however the request is typed.
func TestBodyRegionsSkipExternalBodies(t *testing.T) {
	registerFake(t, "json", "json")
	lines := []string{
		"POST https://example.com/a",
		"Content-Type: application/json",
		"",
		"< ./payload.json",
	}
	if got := bodyRegions(lines); len(got) != 0 {
		t.Fatalf("regions = %+v, want none for an external body", got)
	}
}
