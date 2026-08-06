package langyaml

import (
	"slices"
	"testing"

	"ike/internal/lang"
)

// TestYAMLRegistered guards #879: .yaml/.yml resolve to the yaml language with
// the yaml-language-server and # line comments.
func TestYAMLRegistered(t *testing.T) {
	for _, path := range []string{"/p/deploy.yaml", "/p/ci.yml"} {
		l, ok := lang.ByPath(path)
		if !ok || l.ID != "yaml" {
			t.Errorf("%s → %v/%v, want yaml", path, l, ok)
		}
	}
	l, _ := lang.ByID("yaml")
	if l.Server == nil || l.Server.Command != "yaml-language-server" {
		t.Errorf("server = %+v, want yaml-language-server", l.Server)
	}
	line, _, ok := lang.Comments("/p/a.yaml")
	if !ok || line != "#" {
		t.Errorf("line comment = %q/%v, want #", line, ok)
	}
}

// TestYAMLIndent: only positions where YAML requires (block scalars) or
// conventionally continues (":" opening a nested mapping) one level deeper
// auto-indent; everything else copy-indents so sibling keys keep their level.
func TestYAMLIndent(t *testing.T) {
	suffixes, ok := lang.IndentAfter("/p/deploy.yaml")
	if !ok {
		t.Fatal("yaml declares no indent suffixes")
	}
	for _, want := range []string{":", "|", ">"} {
		if !slices.Contains(suffixes, want) {
			t.Errorf("IndentAfter misses %q", want)
		}
	}
	if slices.Contains(suffixes, "-") {
		t.Error("\"-\" must not auto-indent: sequence items continue at their own level")
	}
}

// TestYAMLBase64Spans (#1620): values in a Kubernetes Secret data: block
// decode when printable; other documents stay raw.
func TestYAMLBase64Spans(t *testing.T) {
	l, ok := lang.ByID("yaml")
	if !ok || l.Spans == nil {
		t.Fatal("yaml: no Spans producer registered")
	}
	spans := l.Spans([]string{"kind: Secret", "data:", "  user: YWRtaW4="})
	if len(spans) != 1 || spans[0].Replace != "admin" {
		t.Errorf("spans = %+v, want the Secret value decoded to admin", spans)
	}
	if spans := l.Spans([]string{"kind: Deployment", "data:", "  user: YWRtaW4="}); len(spans) != 0 {
		t.Errorf("non-Secret document decoded: %+v", spans)
	}
}
