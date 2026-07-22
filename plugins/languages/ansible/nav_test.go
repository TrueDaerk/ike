package langansible

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/complete"
	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
)

func TestHostsRefAt(t *testing.T) {
	for _, tc := range []struct {
		line string
		col  int
		want string // "" = no ref
	}{
		{"- hosts: webservers", 12, "webservers"},
		{"- hosts: webservers", 9, "webservers"},
		{"  hosts: web:db", 10, "web"},
		{"  hosts: web:db", 14, "db"},
		{"  hosts: all:!excluded", 15, "excluded"},
		{"  delegate_to: web01", 16, "web01"},
		{"  name: not a hosts line", 10, ""},
		{"- hosts: webservers", 3, ""}, // on the key, not the value
		{`  when: groups['dbservers'] | length > 0`, 18, "dbservers"},
		{"  loop: \"{{ groups.webservers }}\"", 22, "webservers"},
	} {
		got, ok := hostsRefAt(tc.line, tc.col)
		if tc.want == "" {
			if ok {
				t.Errorf("%q@%d: claimed %q, want none", tc.line, tc.col, got)
			}
			continue
		}
		if !ok || got != tc.want {
			t.Errorf("%q@%d = %q/%v, want %q", tc.line, tc.col, got, ok, tc.want)
		}
	}
}

// ansibleProject builds a project with an INI inventory and returns the root
// and an associated playbook path.
func ansibleProject(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"ansible.cfg":     "",
		"inventory/hosts": iniInventory,
		"site.yml":        "- hosts: webservers\n  tasks: []\n",
	})
	playbook := filepath.Join(root, "site.yml")
	// The editor's sniff layer records this association on open (#897).
	lang.AssociatePath(playbook, "ansible")
	return root, playbook
}

// TestHostsDefinition guards the #922 goto-definition path end-to-end through
// the registered LocalDefinition seam: hosts: value resolves to the inventory
// line; non-ansible files and unknown names pass to the server.
func TestHostsDefinition(t *testing.T) {
	root, playbook := ansibleProject(t)

	msg, ok := ilsp.LocalDefinitionAt(playbook, 0, 12, "- hosts: webservers")
	if !ok {
		t.Fatal("hosts: value not claimed")
	}
	wantPath := filepath.Join(root, "inventory", "hosts")
	if msg.Path != wantPath || msg.Line != 1 {
		t.Errorf("definition = %s:%d, want %s:1", msg.Path, msg.Line, wantPath)
	}

	if _, ok := ilsp.LocalDefinitionAt(playbook, 0, 12, "- hosts: unknownhost"); ok {
		t.Error("unknown name must pass to the server")
	}
	plain := filepath.Join(root, "plain.yml")
	if _, ok := ilsp.LocalDefinitionAt(plain, 0, 12, "- hosts: webservers"); ok {
		t.Error("non-ansible file must pass to the server")
	}
}

// TestHostsCompletion guards the #922 completion source: names offered in the
// hosts: value position, groups sorted first, prefix filtered, nothing
// offered elsewhere.
func TestHostsCompletion(t *testing.T) {
	_, playbook := ansibleProject(t)
	s := newHostsSource()
	s.Observe(host.EditorEvent{Path: playbook, Text: "- hosts: w\n  tasks: []\n"})

	items, err := s.Complete(context.Background(), complete.Request{Path: playbook, Line: 0, Col: 10, Char: "w"})
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, it := range items {
		labels = append(labels, it.Label)
	}
	joined := strings.Join(labels, ",")
	if !strings.Contains(joined, "webservers") || !strings.Contains(joined, "web01") {
		t.Errorf("prefix w: got %s, want webservers + web01", joined)
	}
	if strings.Contains(joined, "dbservers") {
		t.Errorf("prefix w must filter dbservers, got %s", joined)
	}
	// Group sorts before its member hosts.
	for _, it := range items {
		if it.Label == "webservers" && !strings.HasPrefix(it.SortText, "0") {
			t.Error("group must sort first")
		}
	}

	// Off the hosts value (the tasks line) nothing is offered.
	items, _ = s.Complete(context.Background(), complete.Request{Path: playbook, Line: 1, Col: 5})
	if len(items) != 0 {
		t.Errorf("non-hosts position offered %d items", len(items))
	}
}
