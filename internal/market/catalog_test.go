package market

import (
	"strings"
	"testing"
)

const goodEntry = `{
	"name": "example",
	"version": "1.2.0",
	"description": "demo plugin",
	"homepage": "https://example.com",
	"capabilities": ["commands", "notify"],
	"artifact": {
		"url": "https://example.com/example-1.2.0.wasm",
		"sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	}
}`

func index(entries ...string) string {
	return `{"version": 1, "plugins": [` + strings.Join(entries, ",") + `]}`
}

func TestParseIndexValid(t *testing.T) {
	idx, diags, err := ParseIndex([]byte(index(goodEntry)))
	if err != nil {
		t.Fatalf("ParseIndex: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("diags = %v, want none", diags)
	}
	if len(idx.Plugins) != 1 {
		t.Fatalf("plugins = %d, want 1", len(idx.Plugins))
	}
	e := idx.Plugins[0]
	if e.Name != "example" || e.ParsedVersion() != (Version{1, 2, 0}) {
		t.Errorf("entry = %+v", e)
	}
}

func TestParseIndexBadJSON(t *testing.T) {
	if _, _, err := ParseIndex([]byte("{nope")); err == nil {
		t.Fatal("want error for bad JSON")
	}
}

func TestParseIndexUnsupportedVersion(t *testing.T) {
	if _, _, err := ParseIndex([]byte(`{"version": 2, "plugins": []}`)); err == nil {
		t.Fatal("want error for unsupported index version")
	}
}

func TestParseIndexSkipsBadEntries(t *testing.T) {
	mutate := func(from, to string) string { return strings.Replace(goodEntry, from, to, 1) }
	bad := []struct {
		label string
		entry string
	}{
		{"missing name", mutate(`"name": "example"`, `"name": ""`)},
		{"path in name", mutate(`"name": "example"`, `"name": "../evil"`)},
		{"bad version", mutate(`"version": "1.2.0"`, `"version": "latest"`)},
		{"unknown capability", mutate(`"commands"`, `"filesystem"`)},
		{"duplicate capability", mutate(`"notify"`, `"commands"`)},
		{"http url", mutate("https://example.com/example-1.2.0.wasm", "http://example.com/example-1.2.0.wasm")},
		{"short sha", mutate(`"sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`, `"sha256": "abc"`)},
	}
	for _, c := range bad {
		idx, diags, err := ParseIndex([]byte(index(c.entry, mutate(`"name": "example"`, `"name": "other"`))))
		if err != nil {
			t.Errorf("%s: unexpected index error: %v", c.label, err)
			continue
		}
		if len(diags) != 1 {
			t.Errorf("%s: diags = %v, want 1", c.label, diags)
		}
		if len(idx.Plugins) != 1 || idx.Plugins[0].Name != "other" {
			t.Errorf("%s: valid entries = %+v, want only %q", c.label, idx.Plugins, "other")
		}
	}
}

func TestParseIndexDuplicateName(t *testing.T) {
	idx, diags, err := ParseIndex([]byte(index(goodEntry, goodEntry)))
	if err != nil {
		t.Fatalf("ParseIndex: %v", err)
	}
	if len(idx.Plugins) != 1 || len(diags) != 1 {
		t.Fatalf("plugins=%d diags=%v, want 1 kept + 1 diagnostic", len(idx.Plugins), diags)
	}
	if !strings.Contains(diags[0], "duplicate name") {
		t.Errorf("diag = %q, want duplicate-name mention", diags[0])
	}
}
