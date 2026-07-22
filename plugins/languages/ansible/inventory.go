package langansible

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// inventory.go is the IKE-side Ansible inventory index (#922):
// ansible-language-server resolves modules but not the hosts and groups a
// playbook references (`hosts: webservers`, `delegate_to:`), so IKE indexes
// the inventory sources of the detected project itself and answers
// goto-definition and completion for them.
//
// Indexed sources, relative to the project root (found via the same markers
// the #897 sniffer uses):
//
//   - INI inventories: inventory/ (all files), plus ./hosts and ./inventory
//     files — [group] headers define groups, non-section lines define hosts
//   - YAML inventories (*.yml/*.yaml in inventory/): nested `children:` keys
//     define groups, keys under `hosts:` define hosts
//   - group_vars/<name>(.yml) and host_vars/<name>(.yml): the base name
//     defines a group / host at line 1 of that file
//
// The index is best-effort text scanning — no ansible invocation, no plugin
// resolution; a name ansible would compute dynamically simply stays unknown.

// InvDef is one host or group definition site.
type InvDef struct {
	Name string
	Kind string // "host" or "group"
	Path string // absolute file path
	Line int    // 0-based line of the definition
}

// InventoryIndex holds the project's host/group definitions, first
// definition wins per (kind, name).
type InventoryIndex struct {
	Root string
	defs map[string]InvDef // key: kind + "\x00" + name
}

// Defs returns all definitions sorted by name (groups first, then hosts) —
// stable for completion listings and tests.
func (ix *InventoryIndex) Defs() []InvDef {
	out := make([]InvDef, 0, len(ix.defs))
	for _, d := range ix.defs {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == "group"
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Lookup resolves a host-pattern token to its definition: exact group first
// (the common `hosts:` case), then host.
func (ix *InventoryIndex) Lookup(name string) (InvDef, bool) {
	if d, ok := ix.defs["group\x00"+name]; ok {
		return d, true
	}
	d, ok := ix.defs["host\x00"+name]
	return d, ok
}

func (ix *InventoryIndex) add(d InvDef) {
	key := d.Kind + "\x00" + d.Name
	if _, exists := ix.defs[key]; !exists {
		ix.defs[key] = d
	}
}

// ProjectRoot walks up from dir to the Ansible project root — the first
// directory carrying one of the #897 markers — falling back to the nearest
// .git directory, then dir itself.
func ProjectRoot(dir string) string {
	gitRoot := ""
	for d := dir; ; {
		if fileExists(filepath.Join(d, "ansible.cfg")) || fileExists(filepath.Join(d, "galaxy.yml")) ||
			(fileExists(filepath.Join(d, "requirements.yml")) && dirExists(filepath.Join(d, "roles"))) {
			return d
		}
		if gitRoot == "" && dirExists(filepath.Join(d, ".git")) {
			gitRoot = d
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	if gitRoot != "" {
		return gitRoot
	}
	return dir
}

// BuildInventoryIndex scans the project root's inventory sources.
func BuildInventoryIndex(root string) *InventoryIndex {
	ix := &InventoryIndex{Root: root, defs: map[string]InvDef{}}

	// inventory/ directory: every regular file, format by extension.
	invDir := filepath.Join(root, "inventory")
	if entries, err := os.ReadDir(invDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			p := filepath.Join(invDir, e.Name())
			switch strings.ToLower(filepath.Ext(e.Name())) {
			case ".yml", ".yaml":
				ix.scanYAMLInventory(p)
			default:
				ix.scanINIInventory(p)
			}
		}
	}
	// Root-level classic inventory files.
	for _, name := range []string{"hosts", "inventory"} {
		if p := filepath.Join(root, name); fileExists(p) {
			ix.scanINIInventory(p)
		}
	}
	// group_vars/ and host_vars/: the base name is the definition.
	ix.scanVarsDir(filepath.Join(root, "group_vars"), "group")
	ix.scanVarsDir(filepath.Join(root, "host_vars"), "host")
	return ix
}

// scanINIInventory indexes an INI-style inventory: [group] and
// [group:children] headers define groups; a non-empty, non-comment line
// outside a :vars section defines the host named by its first field. Hosts
// listed under a :children section are group references, also indexed as
// groups (a child group may only exist as such a reference).
func (ix *InventoryIndex) scanINIInventory(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	section := "" // current section suffix: "", "children", "vars"
	for i, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") {
			continue
		}
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			name := t[1 : len(t)-1]
			section = ""
			if i := strings.IndexByte(name, ':'); i >= 0 {
				name, section = name[:i], name[i+1:]
			}
			ix.add(InvDef{Name: name, Kind: "group", Path: path, Line: i})
			continue
		}
		if section == "vars" {
			continue
		}
		name := strings.Fields(t)[0]
		kind := "host"
		if section == "children" {
			kind = "group"
		}
		ix.add(InvDef{Name: name, Kind: kind, Path: path, Line: i})
	}
}

// scanYAMLInventory indexes a YAML inventory by indentation-aware scanning:
// a key directly under a `hosts:` block is a host, a key directly under a
// `children:` block is a group. Good enough for the standard layout without
// pulling a YAML parser into the plugin.
func (ix *InventoryIndex) scanYAMLInventory(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	type frame struct {
		indent int
		kind   string // "hosts", "children" or ""
	}
	var stack []frame
	for i, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		key, rest, isKey := strings.Cut(t, ":")
		if !isKey || strings.ContainsAny(key, "{[\"'") {
			continue
		}
		for len(stack) > 0 && indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		parent := ""
		if len(stack) > 0 {
			parent = stack[len(stack)-1].kind
		}
		switch parent {
		case "hosts":
			ix.add(InvDef{Name: key, Kind: "host", Path: path, Line: i})
		case "children":
			ix.add(InvDef{Name: key, Kind: "group", Path: path, Line: i})
		}
		if strings.TrimSpace(rest) == "" {
			switch key {
			case "hosts", "children":
				stack = append(stack, frame{indent: indent, kind: key})
			default:
				// A group/host key opening its own mapping: neutral frame so
				// its children are not mistaken for hosts of the outer block.
				stack = append(stack, frame{indent: indent, kind: ""})
			}
		}
	}
}

// scanVarsDir indexes group_vars/ or host_vars/: `<name>.yml`, `<name>.yaml`
// or a `<name>/` directory each define name at the top of the file (the
// directory form points at its first file).
func (ix *InventoryIndex) scanVarsDir(dir, kind string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(dir, name)
		if e.IsDir() {
			sub, err := os.ReadDir(path)
			if err != nil || len(sub) == 0 {
				continue
			}
			ix.add(InvDef{Name: name, Kind: kind, Path: filepath.Join(path, sub[0].Name()), Line: 0})
			continue
		}
		if ext := strings.ToLower(filepath.Ext(name)); ext == ".yml" || ext == ".yaml" {
			name = strings.TrimSuffix(name, filepath.Ext(name))
		}
		ix.add(InvDef{Name: name, Kind: kind, Path: path, Line: 0})
	}
}
