package langansible

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/lang"
)

// TestAnsibleRegistered guards #897: the language registers with the Ansible
// server, shares the yaml grammar capabilities, and deliberately claims no
// extensions — detection is the sniffer's job.
func TestAnsibleRegistered(t *testing.T) {
	l, ok := lang.ByID("ansible")
	if !ok {
		t.Fatal("ansible not registered")
	}
	if l.Server == nil || l.Server.Command != "ansible-language-server" {
		t.Errorf("server = %+v, want ansible-language-server", l.Server)
	}
	if len(l.Extensions) != 0 || len(l.Filenames) != 0 {
		t.Errorf("ansible must not claim extensions/filenames (would steal plain yaml), got %+v", l)
	}
	if l.LineComment != "#" {
		t.Errorf("line comment = %q, want #", l.LineComment)
	}
	y, _ := lang.ByID("yaml")
	if len(l.FoldNodes) == 0 || len(y.FoldNodes) != len(l.FoldNodes) {
		t.Error("ansible should mirror yaml's fold nodes (same grammar)")
	}
}

// TestAnsiblePathHeuristics: the path patterns from the issue claim role
// trees, playbooks and inventory-style directories; unrelated paths pass.
func TestAnsiblePathHeuristics(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/p/roles/web/tasks/main.yml", true},
		{"/p/roles/web/handlers/main.yml", true},
		{"/p/roles/db/defaults/main.yml", true},
		{"/p/roles/db/vars/main.yml", true},
		{"/p/roles/db/meta/main.yml", true},
		{"/p/roles/web/tasks/setup/users.yml", true}, // any depth below the group dir
		{"/p/playbooks/site.yml", true},
		{"/p/group_vars/all.yml", true},
		{"/p/host_vars/web01.yml", true},
		{"/p/inventory/hosts.yml", true},
		{"/p/roles/web/files/config.yml", false}, // files/ is not a well-known group
		{"/p/app/config.yml", false},
		{"/p/docker-compose.yml", false},
	} {
		if got := isAnsiblePath(tc.path); got != tc.want {
			t.Errorf("isAnsiblePath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestAnsibleRootMarkers: project markers up the tree claim any .yml; a plain
// project without markers stays yaml (the sniffer passes).
func TestAnsibleRootMarkers(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "sub", "dir")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	if id, ok := sniff(filepath.Join(deep, "x.yml")); ok {
		t.Fatalf("no markers yet, sniffed %q", id)
	}

	if err := os.WriteFile(filepath.Join(root, "ansible.cfg"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if id, ok := sniff(filepath.Join(deep, "x.yml")); !ok || id != "ansible" {
		t.Errorf("ansible.cfg marker: sniff = %q/%v, want ansible", id, ok)
	}
	// Non-YAML files are never claimed, marker or not.
	if _, ok := sniff(filepath.Join(deep, "x.txt")); ok {
		t.Error("non-yaml file must not be claimed")
	}

	// requirements.yml alone is not enough — it needs a roles/ dir beside it.
	root2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(root2, "requirements.yml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := sniff(filepath.Join(root2, "x.yml")); ok {
		t.Error("requirements.yml without roles/ must not claim")
	}
	if err := os.Mkdir(filepath.Join(root2, "roles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if id, ok := sniff(filepath.Join(root2, "x.yml")); !ok || id != "ansible" {
		t.Errorf("requirements.yml + roles/: sniff = %q/%v, want ansible", id, ok)
	}
}

// TestSniffThroughRegistry: the registered sniffer resolves through lang.Sniff
// and plain .yml outside any Ansible context stays yaml via ByPath.
func TestSniffThroughRegistry(t *testing.T) {
	if l, ok := lang.Sniff("/p/roles/web/tasks/main.yml"); !ok || l.ID != "ansible" {
		t.Errorf("Sniff(role tree) = %v/%v, want ansible", l.ID, ok)
	}
	plain := filepath.Join(t.TempDir(), "compose.yml")
	if l, ok := lang.Sniff(plain); ok {
		t.Errorf("Sniff(plain yml) claimed %s, want pass", l.ID)
	}
	if l, ok := lang.ByPath(plain); !ok || l.ID != "yaml" {
		t.Errorf("plain .yml = %v/%v, want yaml", l.ID, ok)
	}
}
