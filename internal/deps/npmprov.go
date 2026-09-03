// npmprov.go is the JavaScript ecosystem provider (#2419): package.json
// parsing, with the package manager (npm, pnpm, yarn) detected by
// lockfile for the outdated/audit/install commands.
package deps

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type npmProvider struct{}

func (npmProvider) ID() string              { return "npm" }
func (npmProvider) ManifestNames() []string { return []string{"package.json"} }

func (npmProvider) Tools() []Tool {
	return []Tool{
		{Name: "npm", Hint: "install Node.js (which ships npm) from https://nodejs.org"},
	}
}

// packageManager picks the package manager by lockfile: pnpm-lock.yaml →
// pnpm, yarn.lock → yarn, anything else → npm.
func packageManager(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "pnpm-lock.yaml")); err == nil {
		return "pnpm"
	}
	if _, err := os.Stat(filepath.Join(dir, "yarn.lock")); err == nil {
		return "yarn"
	}
	return "npm"
}

func (npmProvider) InstallCmd(dir string) []string {
	return []string{packageManager(dir), "install"}
}

func (npmProvider) Bump(path string, dep Dep, version string) error {
	return bumpLine(path, dep, version)
}

// Manifest reads dependencies and devDependencies from package.json,
// locating each entry's line by scanning for its quoted key.
func (npmProvider) Manifest(path string) ([]Dep, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	var out []Dep
	for _, group := range []map[string]string{pkg.Dependencies, pkg.DevDependencies} {
		for name, constraint := range group {
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

// lineOfKey finds the 1-based line declaring the given JSON object key.
func lineOfKey(lines []string, key string) int {
	needle := `"` + key + `"`
	for i, l := range lines {
		if strings.Contains(l, needle+":") || strings.Contains(l, needle+" :") {
			return i + 1
		}
	}
	return 0
}

func (npmProvider) Outdated(ctx context.Context, dir string) (map[string]string, error) {
	pm := packageManager(dir)
	if pm == "yarn" {
		return yarnOutdated(ctx, dir)
	}
	// npm and pnpm both emit {"name": {"current", "wanted", "latest"}}.
	out, err := runTool(ctx, dir, pm, "outdated", "--json")
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		Latest string `json:"latest"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &raw); err != nil {
		return nil, err
	}
	latest := map[string]string{}
	for name, e := range raw {
		if e.Latest != "" {
			latest[name] = e.Latest
		}
	}
	return latest, nil
}

// yarnOutdated parses yarn classic's NDJSON stream, keeping the "table"
// row whose body columns are [name, current, wanted, latest, …].
func yarnOutdated(ctx context.Context, dir string) (map[string]string, error) {
	out, err := runTool(ctx, dir, "yarn", "outdated", "--json")
	if err != nil {
		return nil, err
	}
	latest := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var row struct {
			Type string `json:"type"`
			Data struct {
				Body [][]string `json:"body"`
			} `json:"data"`
		}
		if json.Unmarshal(sc.Bytes(), &row) != nil || row.Type != "table" {
			continue
		}
		for _, cols := range row.Data.Body {
			if len(cols) >= 4 && cols[3] != "" {
				latest[cols[0]] = cols[3]
			}
		}
	}
	return latest, nil
}

func (npmProvider) Audit(ctx context.Context, dir string) (map[string][]Vuln, error) {
	pm := packageManager(dir)
	if pm == "yarn" {
		return yarnAudit(ctx, dir)
	}
	out, err := runTool(ctx, dir, pm, "audit", "--json")
	if err != nil {
		return nil, err
	}
	// npm v7+ / pnpm shape: {"vulnerabilities": {"name": {...}}}.
	var raw struct {
		Vulnerabilities map[string]struct {
			Severity string `json:"severity"`
			FixAvail json.RawMessage `json:"fixAvailable"`
			Via      []json.RawMessage `json:"via"`
		} `json:"vulnerabilities"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &raw); err != nil {
		return nil, err
	}
	vulns := map[string][]Vuln{}
	for name, e := range raw.Vulnerabilities {
		for _, via := range e.Via {
			// via entries mix advisory objects and plain strings naming
			// the vulnerable transitive package; only objects carry data.
			var adv struct {
				Title    string `json:"title"`
				URL      string `json:"url"`
				Severity string `json:"severity"`
			}
			if json.Unmarshal(via, &adv) != nil || adv.Title == "" {
				continue
			}
			vulns[name] = append(vulns[name], Vuln{
				ID:       advisoryID(adv.URL),
				Severity: strings.ToLower(adv.Severity),
				Summary:  adv.Title,
				URL:      adv.URL,
			})
		}
		if len(vulns[name]) == 0 {
			vulns[name] = append(vulns[name], Vuln{Severity: strings.ToLower(e.Severity), Summary: "vulnerable (see " + pm + " audit)"})
		}
	}
	return vulns, nil
}

// yarnAudit parses yarn classic's NDJSON auditAdvisory rows.
func yarnAudit(ctx context.Context, dir string) (map[string][]Vuln, error) {
	out, err := runTool(ctx, dir, "yarn", "audit", "--json")
	if err != nil {
		return nil, err
	}
	vulns := map[string][]Vuln{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var row struct {
			Type string `json:"type"`
			Data struct {
				Advisory struct {
					ModuleName         string `json:"module_name"`
					Title              string `json:"title"`
					Severity           string `json:"severity"`
					URL                string `json:"url"`
					PatchedVersions    string `json:"patched_versions"`
					GithubAdvisoryID   string `json:"github_advisory_id"`
				} `json:"advisory"`
			} `json:"data"`
		}
		if json.Unmarshal(sc.Bytes(), &row) != nil || row.Type != "auditAdvisory" {
			continue
		}
		a := row.Data.Advisory
		vulns[a.ModuleName] = append(vulns[a.ModuleName], Vuln{
			ID:       a.GithubAdvisoryID,
			Severity: strings.ToLower(a.Severity),
			Summary:  a.Title,
			FixedIn:  a.PatchedVersions,
			URL:      a.URL,
		})
	}
	return vulns, nil
}

// advisoryID extracts the trailing advisory id (GHSA-…) from an URL.
func advisoryID(url string) string {
	if url == "" {
		return ""
	}
	return url[strings.LastIndex(url, "/")+1:]
}
