package langyaml

import (
	"testing"

	"ike/internal/lang"
)

// TestJinjaTemplateAttachesYAMLServer (#3060): a *.yml.j2 template resolves
// to YAML and so attaches yaml-language-server like any YAML file — the
// reason its diagnostics need the Jinja filter (ilsp.DropJinjaDiagnostics).
func TestJinjaTemplateAttachesYAMLServer(t *testing.T) {
	l, ok := lang.ByPath("deploy/app.yml.j2")
	if !ok || l.ID != "yaml" || !l.HasServer() {
		t.Fatalf("ByPath(app.yml.j2) = %q (ok %v, server %v), want yaml with a server", l.ID, ok, l.HasServer())
	}
	if !lang.IsJinjaTemplate("deploy/app.yml.j2") {
		t.Fatal("app.yml.j2 is not a Jinja template")
	}
}
