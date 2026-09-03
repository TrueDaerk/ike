// Package deps scans a project's declared dependencies per manifest file
// (go.mod, package.json, composer.json, Cargo.toml, requirements.txt,
// pyproject.toml) and enriches them with the latest available version and
// known vulnerabilities by shelling out to the ecosystem toolchain (#2419).
//
// One Provider per ecosystem implements the small Manifest/Outdated/Audit
// seam; missing toolchain binaries are reported per provider with an
// install hint instead of failing the scan. All network access happens
// inside the external tools — the package itself never dials out.
package deps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"ike/internal/lang"
)

// Vuln is one known vulnerability affecting a dependency.
type Vuln struct {
	// ID is the advisory identifier (CVE, GHSA, RUSTSEC, GO-…).
	ID string
	// Severity is the tool-reported severity, lower-cased ("critical",
	// "high", "moderate", "low", or "" when the tool gives none).
	Severity string
	// Summary is a one-line description of the advisory.
	Summary string
	// FixedIn is the earliest fixed version, "" when unknown.
	FixedIn string
	// URL points at the advisory, "" when the tool gives none.
	URL string
}

// Dep is one declared dependency of a manifest.
type Dep struct {
	// Name is the package identifier as the ecosystem spells it.
	Name string
	// Current is the declared/installed version, without constraint
	// prefixes (^, ~, >=…) so it matches toolchain output.
	Current string
	// Latest is the newest available version, "" while unknown or when
	// the dependency is up to date.
	Latest string
	// Indirect marks transitive dependencies (go.mod "// indirect").
	Indirect bool
	// Line is the 1-based manifest line the dependency is declared on.
	Line int
	// Vulns lists known vulnerabilities affecting Current.
	Vulns []Vuln
}

// Outdated reports whether a newer version than the current one is known.
func (d Dep) Outdated() bool { return d.Latest != "" && d.Latest != d.Current }

// Tool names a toolchain binary a provider shells out to, with an install
// hint shown when the binary is missing from PATH.
type Tool struct {
	// Name is the binary looked up on PATH.
	Name string
	// Hint tells the user how to install the tool.
	Hint string
	// Optional tools only disable their feature (audit/outdated) when
	// missing; required tools disable the whole provider.
	Optional bool
}

// Provider is the per-ecosystem seam: parse a manifest, ask the toolchain
// for newer versions and vulnerabilities, and rewrite a version in place.
type Provider interface {
	// ID is the stable provider key ("go", "npm", "composer", "cargo",
	// "pip"), also shown as the ecosystem label in the tool window.
	ID() string
	// ManifestNames lists the manifest base names the provider owns.
	ManifestNames() []string
	// Manifest parses one manifest file into its declared dependencies
	// (name, current version, line, direct/indirect).
	Manifest(path string) ([]Dep, error)
	// Outdated returns the latest known version per dependency name,
	// resolved by the toolchain in the manifest's directory.
	Outdated(ctx context.Context, dir string) (map[string]string, error)
	// Audit returns known vulnerabilities per dependency name.
	Audit(ctx context.Context, dir string) (map[string][]Vuln, error)
	// Bump rewrites the dependency's version in the manifest to the
	// given version, preserving constraint prefixes. It does not run
	// any install step.
	Bump(path string, dep Dep, version string) error
	// InstallCmd is the command that applies a manifest edit to the
	// environment (go mod tidy, npm install, …), run from the
	// manifest's directory after the user confirms.
	InstallCmd(dir string) []string
	// Tools lists the binaries the provider shells out to.
	Tools() []Tool
}

// MissingTool reports one absent toolchain binary of a provider.
type MissingTool struct {
	Provider string
	Tool     Tool
}

// Providers returns the built-in providers in display order.
func Providers() []Provider {
	return []Provider{goProvider{}, npmProvider{}, composerProvider{}, cargoProvider{}, pipProvider{}}
}

// providerFor matches a manifest base name to its provider.
func providerFor(base string) Provider {
	for _, p := range Providers() {
		for _, n := range p.ManifestNames() {
			if n == base {
				return p
			}
		}
	}
	return nil
}

// DetectManifests lists the project-root manifest files a provider owns,
// in stable order. Only the root directory is scanned — nested manifests
// (node_modules, vendored trees) are out of scope. Manifests are gated on
// the language registry's declarations (#2419): a manifest no registered
// language claims via DepManifests is skipped, so disabling a language
// plugin silences its ecosystem. An empty registry (tests, stripped
// builds) declares nothing and gates nothing.
func DetectManifests(root string) []string {
	declared := map[string]bool{}
	for _, name := range lang.DepManifests() {
		declared[name] = true
	}
	var out []string
	for _, p := range Providers() {
		for _, name := range p.ManifestNames() {
			if len(declared) > 0 && !declared[name] {
				continue
			}
			path := filepath.Join(root, name)
			if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
				out = append(out, path)
			}
		}
	}
	sort.Strings(out)
	return out
}

// missingTools splits a provider's tools into present and missing,
// consulting PATH via exec.LookPath.
func missingTools(p Provider) []MissingTool {
	var out []MissingTool
	for _, t := range p.Tools() {
		if _, err := exec.LookPath(t.Name); err != nil {
			out = append(out, MissingTool{Provider: p.ID(), Tool: t})
		}
	}
	return out
}

// hasTool reports whether the named binary is on PATH.
func hasTool(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// runTool executes a toolchain command in dir and returns its stdout.
// Non-zero exits still return the output: npm outdated, composer outdated
// and cargo audit exit 1 when they find something, with valid JSON on
// stdout — the caller decides whether the payload parses.
func runTool(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}

// bumpLine rewrites the first occurrence of dep.Current on the
// dependency's declaration line to version, keeping everything else on
// the line (constraint prefixes like ^, ~, >= included) untouched. This
// covers every supported manifest format because they all keep the
// version literal on the declaration line.
func bumpLine(path string, dep Dep, version string) error {
	if dep.Line < 1 || dep.Current == "" || version == "" {
		return fmt.Errorf("deps: cannot bump %s: missing line or version", dep.Name)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	if dep.Line > len(lines) {
		return fmt.Errorf("deps: %s line %d out of range", path, dep.Line)
	}
	line := lines[dep.Line-1]
	if !strings.Contains(line, dep.Current) {
		return fmt.Errorf("deps: %s line %d no longer contains %s", filepath.Base(path), dep.Line, dep.Current)
	}
	lines[dep.Line-1] = strings.Replace(line, dep.Current, version, 1)
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), fi.Mode().Perm())
}
