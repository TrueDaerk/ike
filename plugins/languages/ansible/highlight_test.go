//go:build cgo

package langansible

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// TestAnsibleSharesYAMLGrammar guards the grammar sharing (#897): the ansible
// id carries the yaml grammar, and a sniffed playbook path highlights.
func TestAnsibleSharesYAMLGrammar(t *testing.T) {
	a, _ := lang.ByID("ansible")
	y, _ := lang.ByID("yaml")
	if a.Grammar == nil {
		t.Fatal("ansible grammar is nil under cgo")
	}
	if a.Grammar != y.Grammar {
		t.Error("ansible must share yaml's grammar instance")
	}

	// A path associated to ansible (as the editor does after sniffing)
	// highlights with YAML captures.
	path := "/p/roles/web/tasks/main.yml"
	lang.AssociatePath(path, "ansible")
	spans := highlight.Highlight(path, []string{
		`- name: install nginx`,
		`  apt:`,
		`    name: nginx`,
	})
	if len(spans) == 0 {
		t.Fatal("expected spans for ansible source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 2); got != "property" { // name key
		t.Errorf("task key: got capture %q, want property", got)
	}
	if got := ix.CaptureAt(0, 8); got != "string" { // install nginx
		t.Errorf("task name value: got capture %q, want string", got)
	}
}

// TestAnsibleJinjaHighlight (#3061): Jinja in an Ansible file paints over
// the YAML string it sits in — quoted and plain scalars, and the bare
// expression of `when:` — while the string around the tokens keeps its
// YAML capture.
func TestAnsibleJinjaHighlight(t *testing.T) {
	path := "/p/roles/web/tasks/jinja.yml"
	lang.AssociatePath(path, "ansible")
	lines := []string{
		`- name: "{{ item.name | default('x') }}"`, // quoted
		`  debug:`,
		`    msg: hello {{ user }} there`, // plain
		`  when: foo is defined`,          // bare expression
		`  # {{ not_jinja }}`,             // YAML comment
	}
	ix := highlight.NewIndex(highlight.Highlight(path, lines))
	for _, c := range []struct {
		line, col int
		want      string
	}{
		{0, 9, "punctuation.special"}, // {{
		{0, 12, "variable"},           // item
		{0, 17, "property"},           // name
		{0, 22, "operator"},           // |
		{0, 24, "function"},           // default
		{0, 32, "string"},             // 'x'
		{0, 8, "string"},              // the YAML quote
		{2, 15, "punctuation.special"},
		{2, 18, "variable"}, // user
		{2, 10, "string"},   // hello
		{2, 26, "string"},   // there
		{3, 8, "variable"},  // foo
		{3, 12, "keyword"},  // is
		{3, 15, "function"}, // defined
		{4, 2, "comment"},
		{4, 5, "comment"},
	} {
		if got := ix.CaptureAt(c.line, c.col); got != c.want {
			t.Errorf("%d:%d %q: got %q, want %q", c.line, c.col, lines[c.line], got, c.want)
		}
	}
}

// TestPlainYAMLNoJinja (#3061): a YAML file the Ansible language does not
// claim keeps the Jinja as plain string text.
func TestPlainYAMLNoJinja(t *testing.T) {
	path := "/p/plain/config.yml"
	lang.AssociatePath(path, "yaml")
	ix := highlight.NewIndex(highlight.Highlight(path, []string{
		`name: "{{ item }}"`,
		`when: foo is defined`,
	}))
	if got := ix.CaptureAt(0, 10); got != "string" {
		t.Errorf("quoted jinja in plain yaml: got %q, want string", got)
	}
	if got := ix.CaptureAt(1, 10); got != "string" {
		t.Errorf("when: in plain yaml: got %q, want string", got)
	}
}
