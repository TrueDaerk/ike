// Package langansible registers Ansible (#897) as its own language id so
// playbooks get the Ansible language server instead of plain
// yaml-language-server. Ansible files are YAML — the Tree-sitter grammar is
// shared with the yaml plugin (blank-imported below, so its init runs first) —
// and a plain extension cannot tell them apart, so detection is a context
// sniffer (lang.RegisterSniffer, the #893/#897 sniff layer): role-tree and
// playbook path patterns first, then Ansible project root markers up the
// directory tree; everything else stays plain yaml.
//
// Completion comes from @ansible/ansible-language-server: module FQCNs,
// module options/sub-options, keywords, hover docs, plus ansible-lint
// diagnostics when installed. The server shells out to ansible for its
// module data — full completion needs an ansible install on PATH.
// Role references and import_tasks/include_tasks paths resolve through the
// ordinary LSP definition path.
//
// Self-registers via init(); blank-imported in cmd/ike/main.go.
package langansible

import (
	"os"
	"path/filepath"
	"strings"

	"ike/internal/lang"
	"ike/plugins/languages/register"

	// The yaml plugin must register first: ansible shares its grammar.
	_ "ike/plugins/languages/yaml"
)

func init() {
	yaml, _ := lang.ByID("yaml")
	register.Language(lang.Language{
		ID:      "ansible",
		Grammar: yaml.Grammar,
		Server: &lang.ServerSpec{
			Language:    "ansible",
			Command:     "ansible-language-server",
			Args:        []string{"--stdio"},
			RootMarkers: []string{"ansible.cfg", "galaxy.yml", ".git"},
			Install:     []string{"npm", "install", "-g", "@ansible/ansible-language-server"},
		},
		LineComment: "#",
		IndentAfter: yaml.IndentAfter,
		ScopeNodes:  yaml.ScopeNodes,
		FoldNodes:   yaml.FoldNodes,
	})
	lang.RegisterSniffer(sniff)
}

// sniff claims a .yml/.yaml file for ansible when its path or its project
// context says so; everything else passes and stays plain yaml.
func sniff(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yml", ".yaml":
	default:
		return "", false
	}
	if isAnsiblePath(path) || hasAnsibleRoot(filepath.Dir(path)) {
		return "ansible", true
	}
	return "", false
}

// ansibleDirs are the path segments that mark an Ansible layout on their own.
var ansibleDirs = map[string]bool{
	"playbooks":  true,
	"group_vars": true,
	"host_vars":  true,
	"inventory":  true,
}

// roleSubdirs are the well-known directories inside roles/<name>/.
var roleSubdirs = map[string]bool{
	"tasks":    true,
	"handlers": true,
	"defaults": true,
	"vars":     true,
	"meta":     true,
}

// isAnsiblePath reports whether the path alone identifies an Ansible file:
// roles/<role>/(tasks|handlers|defaults|vars|meta)/…, or any playbooks/,
// group_vars/, host_vars/ or inventory/ segment.
func isAnsiblePath(path string) bool {
	segs := strings.Split(filepath.ToSlash(filepath.Dir(path)), "/")
	for i, s := range segs {
		if ansibleDirs[s] {
			return true
		}
		// roles/<role>/<well-known>: the file may sit any depth below.
		if s == "roles" && i+2 < len(segs) && roleSubdirs[segs[i+2]] {
			return true
		}
	}
	return false
}

// hasAnsibleRoot walks from dir upwards looking for Ansible project markers:
// ansible.cfg or galaxy.yml, or requirements.yml next to a roles/ directory.
func hasAnsibleRoot(dir string) bool {
	for {
		if fileExists(filepath.Join(dir, "ansible.cfg")) || fileExists(filepath.Join(dir, "galaxy.yml")) {
			return true
		}
		if fileExists(filepath.Join(dir, "requirements.yml")) && dirExists(filepath.Join(dir, "roles")) {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
