// pipprov.go is the Python ecosystem provider (#2419): requirements.txt
// and PEP 621 pyproject.toml parsing, `pip list --outdated` (or uv) and
// `pip-audit --format=json` when installed. It mirrors the marketplace
// Python-environment outdated check (internal/settings/pythonpkg.go) but
// targets the project's own manifests.
package deps

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type pipProvider struct{}

func (pipProvider) ID() string { return "pip" }
func (pipProvider) ManifestNames() []string {
	return []string{"requirements.txt", "pyproject.toml"}
}

func (pipProvider) Tools() []Tool {
	return []Tool{
		{Name: "pip3", Hint: "install Python 3 (which ships pip) from https://python.org"},
		{Name: "pip-audit", Hint: "pip install pip-audit", Optional: true},
	}
}

func (pipProvider) InstallCmd(dir string) []string {
	if _, err := os.Stat(filepath.Join(dir, "requirements.txt")); err == nil {
		return []string{"pip3", "install", "-r", "requirements.txt"}
	}
	return []string{"pip3", "install", "-e", "."}
}

func (pipProvider) Bump(path string, dep Dep, version string) error {
	return bumpLine(path, dep, version)
}

func (p pipProvider) Manifest(path string) ([]Dep, error) {
	if filepath.Base(path) == "pyproject.toml" {
		return pyprojectDeps(path)
	}
	return requirementsDeps(path)
}

// requirementsDeps parses `name==1.2` style pins; unpinned or complex
// requirement lines are listed without a current version.
func requirementsDeps(path string) ([]Dep, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Dep
	for i, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "-") {
			continue
		}
		if c, _, ok := strings.Cut(t, "#"); ok {
			t = strings.TrimSpace(c)
		}
		out = append(out, requirementDep(t, i+1))
	}
	return out, nil
}

// pyprojectDeps parses the PEP 621 `dependencies = [...]` array of the
// [project] table with a line-oriented TOML subset.
func pyprojectDeps(path string) ([]Dep, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Dep
	inProject, inList := false, false
	for i, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			inProject = strings.Trim(t, "[]") == "project"
			inList = false
			continue
		}
		if inProject && strings.HasPrefix(t, "dependencies") && strings.Contains(t, "[") {
			inList = !strings.Contains(t, "]")
			continue
		}
		if !inList {
			continue
		}
		if strings.HasPrefix(t, "]") {
			inList = false
			continue
		}
		spec := strings.Trim(strings.TrimSuffix(t, ","), `"'`)
		if spec == "" {
			continue
		}
		out = append(out, requirementDep(spec, i+1))
	}
	return out, nil
}

// requirementDep splits one PEP 508 requirement into name and pinned
// version (only == and ~= pins yield a current version).
func requirementDep(spec string, line int) Dep {
	name := spec
	for _, sep := range []string{"==", "~=", ">=", "<=", ">", "<", "!=", ";", "["} {
		if i := strings.Index(name, sep); i >= 0 {
			name = name[:i]
		}
	}
	d := Dep{Name: strings.TrimSpace(name), Line: line}
	for _, pin := range []string{"==", "~="} {
		if _, v, ok := strings.Cut(spec, pin); ok {
			v = strings.TrimSpace(v)
			if i := strings.IndexAny(v, ";, "); i >= 0 {
				v = v[:i]
			}
			d.Current = v
			break
		}
	}
	return d
}

func (pipProvider) Outdated(ctx context.Context, dir string) (map[string]string, error) {
	var out []byte
	var err error
	// Prefer uv when present, matching the marketplace Python check.
	if hasTool("uv") {
		out, err = runTool(ctx, dir, "uv", "pip", "list", "--outdated", "--format", "json")
	} else {
		out, err = runTool(ctx, dir, "pip3", "list", "--outdated", "--format=json")
	}
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Name   string `json:"name"`
		Latest string `json:"latest_version"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &raw); err != nil {
		return nil, err
	}
	latest := map[string]string{}
	for _, e := range raw {
		if e.Latest != "" {
			latest[normalizePyPkg(e.Name)] = e.Latest
		}
	}
	return latest, nil
}

func (pipProvider) Audit(ctx context.Context, dir string) (map[string][]Vuln, error) {
	if !hasTool("pip-audit") {
		return nil, nil
	}
	out, err := runTool(ctx, dir, "pip-audit", "--format=json")
	if err != nil {
		return nil, err
	}
	var raw struct {
		Dependencies []struct {
			Name  string `json:"name"`
			Vulns []struct {
				ID          string   `json:"id"`
				Description string   `json:"description"`
				FixVersions []string `json:"fix_versions"`
			} `json:"vulns"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &raw); err != nil {
		return nil, err
	}
	vulns := map[string][]Vuln{}
	for _, d := range raw.Dependencies {
		for _, v := range d.Vulns {
			vulns[normalizePyPkg(d.Name)] = append(vulns[normalizePyPkg(d.Name)], Vuln{
				ID:      v.ID,
				Summary: firstLine(v.Description),
				FixedIn: strings.Join(v.FixVersions, ", "),
			})
		}
	}
	return vulns, nil
}

// normalizePyPkg lower-cases and folds -/_/. runs per PEP 503 so manifest
// names match tool output.
func normalizePyPkg(name string) string {
	name = strings.ToLower(name)
	for _, r := range []string{"_", "."} {
		name = strings.ReplaceAll(name, r, "-")
	}
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	return name
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// sortDeps orders dependencies by name for stable map-sourced listings.
func sortDeps(deps []Dep) {
	sort.Slice(deps, func(i, j int) bool { return deps[i].Name < deps[j].Name })
}
