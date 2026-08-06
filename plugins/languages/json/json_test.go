package langjson

import (
	"testing"

	"ike/internal/lang"
)

// TestJSONRegistered guards #878: json and ndjson resolve by extension, json
// carries the vscode-json-language-server, ndjson deliberately has no server
// (the JSON server flags every line of a multi-document stream as an error).
func TestJSONRegistered(t *testing.T) {
	for _, tc := range []struct {
		path string
		id   string
	}{
		{"/p/config.json", "json"},
		{"/p/settings.jsonc", "json"},
		{"/p/events.ndjson", "ndjson"},
		{"/p/events.jsonl", "ndjson"},
	} {
		l, ok := lang.ByPath(tc.path)
		if !ok {
			t.Errorf("%s: no language registered", tc.path)
			continue
		}
		if l.ID != tc.id {
			t.Errorf("%s → %s, want %s", tc.path, l.ID, tc.id)
		}
	}

	j, _ := lang.ByID("json")
	if j.Server == nil || j.Server.Command != "vscode-json-language-server" {
		t.Errorf("json server = %+v, want vscode-json-language-server", j.Server)
	}
	// #1505: the server only advertises documentFormattingProvider when the
	// initializationOptions carry provideFormatter: true — without it,
	// Reformat File has no provider for JSON.
	if j.Server != nil {
		if v, ok := j.Server.Settings["provideFormatter"].(bool); !ok || !v {
			t.Errorf("json server settings = %v, want provideFormatter: true", j.Server.Settings)
		}
	}
	n, _ := lang.ByID("ndjson")
	if n.Server != nil {
		t.Errorf("ndjson must not have a server, got %+v", n.Server)
	}

	// Strict JSON has no comment syntax; toggling must stay unavailable.
	if _, _, ok := lang.Comments("/p/config.json"); ok {
		t.Error("json must not declare comment syntax")
	}
}

// TestSpaceAfterColon guards the typing aid's language rule (#1326): a JSON
// member's colon carries its space.
func TestSpaceAfterColon(t *testing.T) {
	for _, id := range []string{"json", "ndjson"} {
		l, ok := lang.ByID(id)
		if !ok {
			t.Fatalf("%s not registered", id)
		}
		if len(l.SpaceAfter) != 1 || l.SpaceAfter[0] != ':' {
			t.Errorf("%s SpaceAfter = %q, want [':']", id, string(l.SpaceAfter))
		}
	}
}

// TestEpochSpans (#1618): both JSON languages register the timestamp span
// producer, and it decodes value-position epoch numbers only.
func TestEpochSpans(t *testing.T) {
	for _, id := range []string{"json", "ndjson"} {
		l, ok := lang.ByID(id)
		if !ok || l.Spans == nil {
			t.Fatalf("%s: no Spans producer registered", id)
		}
	}
	spans := epochSpans([]string{
		`{"created_at": 1722945600, "id": 42, "1722945600": "key"}`,
		`  "note": "written at 1722945600"`,
	})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1: %+v", len(spans), spans)
	}
	s := spans[0]
	if s.Line != 0 || s.StartCol != 15 || s.EndCol != 25 {
		t.Errorf("span = line %d cols [%d,%d), want line 0 cols [15,25)", s.Line, s.StartCol, s.EndCol)
	}
	if s.Replace != "2024-08-06 12:00:00Z" {
		t.Errorf("stand-in = %q, want the decoded UTC form", s.Replace)
	}
}
