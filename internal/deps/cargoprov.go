// cargoprov.go is the Rust ecosystem provider (#2419): Cargo.toml
// parsing, `cargo outdated --format json` (when the cargo-outdated
// subcommand is installed) and `cargo audit --json` (cargo-audit).
package deps

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
)

type cargoProvider struct{}

func (cargoProvider) ID() string              { return "cargo" }
func (cargoProvider) ManifestNames() []string { return []string{"Cargo.toml"} }

func (cargoProvider) Tools() []Tool {
	return []Tool{
		{Name: "cargo", Hint: "install Rust (which ships cargo) via https://rustup.rs"},
		{Name: "cargo-outdated", Hint: "cargo install cargo-outdated", Optional: true},
		{Name: "cargo-audit", Hint: "cargo install cargo-audit", Optional: true},
	}
}

func (cargoProvider) InstallCmd(dir string) []string { return []string{"cargo", "update"} }

func (cargoProvider) Bump(path string, dep Dep, version string) error {
	return bumpLine(path, dep, version)
}

// Manifest reads [dependencies]/[dev-dependencies]/[build-dependencies]
// tables from Cargo.toml with a line-oriented TOML subset: `name = "v"`
// and `name = { version = "v", … }` entries.
func (cargoProvider) Manifest(path string) ([]Dep, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Dep
	inDeps := false
	for i, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			sec := strings.Trim(t, "[]")
			inDeps = sec == "dependencies" || sec == "dev-dependencies" || sec == "build-dependencies" ||
				strings.HasSuffix(sec, ".dependencies") || strings.HasSuffix(sec, ".dev-dependencies")
			continue
		}
		if !inDeps || t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		name, rest, ok := strings.Cut(t, "=")
		if !ok {
			continue
		}
		name = strings.Trim(strings.TrimSpace(name), `"`)
		version := tomlVersion(strings.TrimSpace(rest))
		if name == "" || version == "" {
			continue
		}
		out = append(out, Dep{Name: name, Current: version, Line: i + 1})
	}
	return out, nil
}

// tomlVersion extracts the version literal from `"1.2"` or
// `{ version = "1.2", features = […] }`; "" for path/git-only entries.
func tomlVersion(v string) string {
	if strings.HasPrefix(v, `"`) {
		return strings.Trim(v, `"`)
	}
	if strings.HasPrefix(v, "{") {
		if _, rest, ok := strings.Cut(v, "version"); ok {
			if _, val, ok := strings.Cut(rest, `"`); ok {
				if ver, _, ok := strings.Cut(val, `"`); ok {
					return ver
				}
			}
		}
	}
	return ""
}

func (cargoProvider) Outdated(ctx context.Context, dir string) (map[string]string, error) {
	if !hasTool("cargo-outdated") {
		return nil, nil
	}
	out, err := runTool(ctx, dir, "cargo", "outdated", "--format", "json")
	if err != nil {
		return nil, err
	}
	var raw struct {
		Dependencies []struct {
			Name    string `json:"name"`
			Project string `json:"project"`
			Latest  string `json:"latest"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &raw); err != nil {
		return nil, err
	}
	latest := map[string]string{}
	for _, e := range raw.Dependencies {
		if e.Latest != "" && e.Latest != "---" && e.Latest != e.Project {
			latest[e.Name] = e.Latest
		}
	}
	return latest, nil
}

func (cargoProvider) Audit(ctx context.Context, dir string) (map[string][]Vuln, error) {
	if !hasTool("cargo-audit") {
		return nil, nil
	}
	out, err := runTool(ctx, dir, "cargo", "audit", "--json")
	if err != nil {
		return nil, err
	}
	var raw struct {
		Vulnerabilities struct {
			List []struct {
				Advisory struct {
					ID    string `json:"id"`
					Title string `json:"title"`
					URL   string `json:"url"`
				} `json:"advisory"`
				Package struct {
					Name string `json:"name"`
				} `json:"package"`
				Versions struct {
					Patched []string `json:"patched"`
				} `json:"versions"`
			} `json:"list"`
		} `json:"vulnerabilities"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &raw); err != nil {
		return nil, err
	}
	vulns := map[string][]Vuln{}
	for _, e := range raw.Vulnerabilities.List {
		v := Vuln{ID: e.Advisory.ID, Summary: e.Advisory.Title, URL: e.Advisory.URL}
		if len(e.Versions.Patched) > 0 {
			v.FixedIn = strings.Join(e.Versions.Patched, ", ")
		}
		vulns[e.Package.Name] = append(vulns[e.Package.Name], v)
	}
	return vulns, nil
}
