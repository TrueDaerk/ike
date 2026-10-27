package lsp

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
)

// TestDropJinjaDiagnostics (#3060): in a .yml.j2 template a server
// diagnostic on the {% for %} tag is dropped, one on plain YAML stays; a
// non-template path keeps everything.
func TestDropJinjaDiagnostics(t *testing.T) {
	lines := []string{
		`items:`,
		`{% for x in xs %}`,
		`  - {{ x }}`,
		`{% endfor %}`,
		`port: oops`,
	}
	rng := func(sl, sc, el, ec int) buffer.Range {
		return buffer.Range{Start: buffer.Position{Line: sl, Col: sc}, End: buffer.Position{Line: el, Col: ec}}
	}
	diags := []Diagnostic{
		{Range: rng(1, 0, 1, 17), Message: "bogus: implicit map keys need to be followed by map values"},
		{Range: rng(2, 4, 2, 5), Message: "bogus: flow mapping in a list item"},
		{Range: rng(4, 6, 4, 10), Message: "real: expected integer"},
	}
	got := DropJinjaDiagnostics("deploy/app.yml.j2", lines, diags)
	if len(got) != 1 || got[0].Message != "real: expected integer" {
		t.Fatalf("kept %+v, want only the diagnostic off the Jinja blocks", got)
	}
	if len(diags) != 3 || !strings.HasPrefix(diags[0].Message, "bogus") {
		t.Fatalf("input set mutated: %+v", diags)
	}
	if got := DropJinjaDiagnostics("deploy/app.yml", lines, diags); len(got) != 3 {
		t.Fatalf("plain YAML kept %d diagnostics, want all 3", len(got))
	}
}
