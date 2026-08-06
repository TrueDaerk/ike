package langyaml

import (
	"strings"
	"testing"

	ilsp "ike/internal/lsp"
	"ike/internal/yamlanchor"
)

var anchored = []string{
	"defaults: &defaults",
	"  adapter: postgres",
	"  host: localhost",
	"development:",
	"  <<: *defaults",
	"  database: dev",
}

// TestAnchorDefinition (#1629) guards the goto path end-to-end through the
// registered LocalDefinition seam: an alias jumps to its anchor, everything
// else (anchors, plain text, non-YAML files) passes to the server.
func TestAnchorDefinition(t *testing.T) {
	msg, ok := ilsp.LocalDefinitionAt("cfg.yaml", 4, 8, anchored)
	if !ok {
		t.Fatal("alias not claimed")
	}
	if msg.Path != "cfg.yaml" || msg.Line != 0 || msg.Col != 10 {
		t.Errorf("definition = %s:%d:%d, want cfg.yaml:0:10", msg.Path, msg.Line, msg.Col)
	}
	if _, ok := ilsp.LocalDefinitionAt("cfg.yaml", 1, 4, anchored); ok {
		t.Error("plain text must pass to the server")
	}
	if _, ok := ilsp.LocalDefinitionAt("cfg.txt", 4, 8, anchored); ok {
		t.Error("a non-YAML file must pass to the server")
	}
}

// TestAnchorHover (#1629): hover on the alias previews the resolved value as
// a yaml fence titled with the anchor name; merge keys inside the anchored
// block are expanded.
func TestAnchorHover(t *testing.T) {
	text, ok := ilsp.LocalHoverAt("cfg.yaml", 4, 8, anchored)
	if !ok {
		t.Fatal("alias hover not claimed")
	}
	for _, want := range []string{"&defaults", "```yaml", "adapter: postgres", "host: localhost"} {
		if !strings.Contains(text, want) {
			t.Errorf("hover %q misses %q", text, want)
		}
	}
	if _, ok := ilsp.LocalHoverAt("cfg.yaml", 1, 4, anchored); ok {
		t.Error("plain text must pass to the server")
	}
}

// TestAnchorReferences (#1629): find-usages on the anchor lists the anchor
// and its alias with line previews; a non-mark position passes.
func TestAnchorReferences(t *testing.T) {
	refs, ok := ilsp.LocalReferencesAt("cfg.yaml", 0, 12, anchored)
	if !ok {
		t.Fatal("anchor usages not claimed")
	}
	if len(refs) != 2 || refs[0].Line != 0 || refs[1].Line != 4 {
		t.Fatalf("refs = %+v, want the anchor line 0 and alias line 4", refs)
	}
	if refs[1].Preview != "<<: *defaults" {
		t.Errorf("preview = %q", refs[1].Preview)
	}
	if _, ok := ilsp.LocalReferencesAt("cfg.yaml", 5, 4, anchored); ok {
		t.Error("plain text must pass to the server")
	}
}

// TestYamlSpansCarryAnchorPairs (#1629): the language's Spans hook emits the
// paired rainbow captures alongside the existing producers.
func TestYamlSpansCarryAnchorPairs(t *testing.T) {
	spans := yamlSpans(anchored)
	want := yamlanchor.Capture("defaults")
	n := 0
	for _, s := range spans {
		if s.Capture == want {
			n++
		}
	}
	if n != 2 {
		t.Errorf("got %d spans with capture %q, want 2 (anchor + alias)", n, want)
	}
}
