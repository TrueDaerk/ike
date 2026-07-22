package langansible

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTree writes files (relative path → content) under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const iniInventory = `# staging
[webservers]
web01 ansible_host=10.0.0.1
web02

[dbservers]
db01

[production:children]
webservers
dbservers

[webservers:vars]
http_port=80
`

func TestInventoryINI(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"ansible.cfg":     "",
		"inventory/hosts": iniInventory,
	})
	ix := BuildInventoryIndex(root)

	for _, tc := range []struct {
		name, kind string
		line       int
	}{
		{"webservers", "group", 1},
		{"web01", "host", 2},
		{"web02", "host", 3},
		{"dbservers", "group", 5},
		{"production", "group", 8},
	} {
		d, ok := ix.Lookup(tc.name)
		if !ok {
			t.Errorf("%s: not indexed", tc.name)
			continue
		}
		if d.Kind != tc.kind || d.Line != tc.line {
			t.Errorf("%s = %s@%d, want %s@%d", tc.name, d.Kind, d.Line, tc.kind, tc.line)
		}
	}
	// A :vars entry is not a host.
	if _, ok := ix.Lookup("http_port=80"); ok {
		t.Error(":vars content indexed as host")
	}
	if _, ok := ix.Lookup("http_port"); ok {
		t.Error(":vars content indexed as host")
	}
}

const yamlInventory = `all:
  children:
    webservers:
      hosts:
        web01:
          ansible_host: 10.0.0.1
        web02:
    dbservers:
      hosts:
        db01:
`

func TestInventoryYAML(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"ansible.cfg":               "",
		"inventory/staging.yml":     yamlInventory,
		"group_vars/monitoring.yml": "interval: 30\n",
		"host_vars/bastion.yml":     "port: 22\n",
	})
	ix := BuildInventoryIndex(root)

	for _, tc := range []struct{ name, kind string }{
		{"webservers", "group"},
		{"dbservers", "group"},
		{"web01", "host"},
		{"web02", "host"},
		{"db01", "host"},
		{"monitoring", "group"}, // from group_vars/
		{"bastion", "host"},     // from host_vars/
	} {
		d, ok := ix.Lookup(tc.name)
		if !ok || d.Kind != tc.kind {
			t.Errorf("%s = %v/%v, want kind %s", tc.name, d, ok, tc.kind)
		}
	}
	// A host variable key must not read as a host.
	if _, ok := ix.Lookup("ansible_host"); ok {
		t.Error("host variable key indexed as host")
	}
}

func TestProjectRoot(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "roles", "web", "tasks")
	writeTree(t, root, map[string]string{"ansible.cfg": ""})
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ProjectRoot(deep); got != root {
		t.Errorf("ProjectRoot = %s, want %s", got, root)
	}
}
