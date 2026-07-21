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
