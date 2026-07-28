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
