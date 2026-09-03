// composerprov.go is the PHP ecosystem provider (#2419): composer.json
// parsing, `composer outdated --format=json` and
// `composer audit --format=json`.
package deps

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
)

type composerProvider struct{}

func (composerProvider) ID() string              { return "composer" }
func (composerProvider) ManifestNames() []string { return []string{"composer.json"} }

func (composerProvider) Tools() []Tool {
	return []Tool{
		{Name: "composer", Hint: "install Composer from https://getcomposer.org/download"},
	}
}

func (composerProvider) InstallCmd(dir string) []string { return []string{"composer", "update"} }

func (composerProvider) Bump(path string, dep Dep, version string) error {
	return bumpLine(path, dep, version)
}

// Manifest reads require and require-dev from composer.json, skipping
// platform requirements (php, ext-*).
func (composerProvider) Manifest(path string) ([]Dep, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pkg struct {
		Require    map[string]string `json:"require"`
		RequireDev map[string]string `json:"require-dev"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	var out []Dep
	for _, group := range []map[string]string{pkg.Require, pkg.RequireDev} {
		for name, constraint := range group {
			if name == "php" || strings.HasPrefix(name, "ext-") {
				continue
			}
			out = append(out, Dep{
				Name:    name,
				Current: strings.TrimLeft(constraint, "^~>=<"),
				Line:    lineOfKey(lines, name),
			})
		}
	}
	sortDeps(out)
	return out, nil
}

func (composerProvider) Outdated(ctx context.Context, dir string) (map[string]string, error) {
	out, err := runTool(ctx, dir, "composer", "outdated", "--format=json")
	if err != nil {
		return nil, err
	}
	var raw struct {
		Installed []struct {
			Name             string `json:"name"`
			Version          string `json:"version"`
			Latest           string `json:"latest"`
			DirectDependency bool   `json:"direct-dependency"`
		} `json:"installed"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &raw); err != nil {
		return nil, err
	}
	latest := map[string]string{}
	for _, e := range raw.Installed {
		if e.Latest != "" && e.Latest != e.Version {
			latest[e.Name] = e.Latest
		}
	}
	return latest, nil
}

func (composerProvider) Audit(ctx context.Context, dir string) (map[string][]Vuln, error) {
	out, err := runTool(ctx, dir, "composer", "audit", "--format=json")
	if err != nil {
		return nil, err
	}
	var raw struct {
		Advisories map[string][]struct {
			AdvisoryID string `json:"advisoryId"`
			Title      string `json:"title"`
			CVE        string `json:"cve"`
			Severity   string `json:"severity"`
			Link       string `json:"link"`
		} `json:"advisories"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &raw); err != nil {
		return nil, err
	}
	vulns := map[string][]Vuln{}
	for name, advisories := range raw.Advisories {
		for _, a := range advisories {
			id := a.CVE
			if id == "" {
				id = a.AdvisoryID
			}
			vulns[name] = append(vulns[name], Vuln{
				ID:       id,
				Severity: strings.ToLower(a.Severity),
				Summary:  a.Title,
				URL:      a.Link,
			})
		}
	}
	return vulns, nil
}
