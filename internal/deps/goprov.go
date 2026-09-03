// goprov.go is the Go ecosystem provider (#2419): go.mod parsing,
// `go list -m -u -json all` for newer versions and `govulncheck -json`
// for vulnerabilities.
package deps

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
)

type goProvider struct{}

func (goProvider) ID() string              { return "go" }
func (goProvider) ManifestNames() []string { return []string{"go.mod"} }

func (goProvider) Tools() []Tool {
	return []Tool{
		{Name: "go", Hint: "install the Go toolchain from https://go.dev/dl"},
		{Name: "govulncheck", Hint: "go install golang.org/x/vuln/cmd/govulncheck@latest", Optional: true},
	}
}

func (goProvider) InstallCmd(dir string) []string { return []string{"go", "mod", "tidy"} }

func (goProvider) Bump(path string, dep Dep, version string) error {
	return bumpLine(path, dep, version)
}

// Manifest hand-parses the require directives of a go.mod: both the
// block form and single-line requires, honouring "// indirect" markers.
func (goProvider) Manifest(path string) ([]Dep, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Dep
	inBlock := false
	for i, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case inBlock && t == ")":
			inBlock = false
			continue
		case !inBlock && strings.HasPrefix(t, "require ("):
			inBlock = true
			continue
		case !inBlock && strings.HasPrefix(t, "require "):
			t = strings.TrimSpace(strings.TrimPrefix(t, "require "))
		case !inBlock:
			continue
		}
		if t == "" || strings.HasPrefix(t, "//") {
			continue
		}
		fields := strings.Fields(t)
		if len(fields) < 2 {
			continue
		}
		out = append(out, Dep{
			Name:     fields[0],
			Current:  fields[1],
			Indirect: strings.Contains(t, "// indirect"),
			Line:     i + 1,
		})
	}
	return out, nil
}

// goListModule is the subset of `go list -m -u -json` output we read.
type goListModule struct {
	Path     string `json:"Path"`
	Version  string `json:"Version"`
	Main     bool   `json:"Main"`
	Indirect bool   `json:"Indirect"`
	Update   *struct {
		Version string `json:"Version"`
	} `json:"Update"`
}

func (goProvider) Outdated(ctx context.Context, dir string) (map[string]string, error) {
	out, err := runTool(ctx, dir, "go", "list", "-m", "-u", "-json", "all")
	if err != nil {
		return nil, err
	}
	latest := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var m goListModule
		if err := dec.Decode(&m); err != nil {
			return nil, err
		}
		if !m.Main && m.Update != nil && m.Update.Version != "" {
			latest[m.Path] = m.Update.Version
		}
	}
	return latest, nil
}

// govulnMsg is the subset of the govulncheck -json stream we read: the
// OSV catalogue entries plus the findings that reference them.
type govulnMsg struct {
	OSV *struct {
		ID       string `json:"id"`
		Summary  string `json:"summary"`
		Database struct {
			URL string `json:"url"`
		} `json:"database_specific"`
	} `json:"osv"`
	Finding *struct {
		OSV          string `json:"osv"`
		FixedVersion string `json:"fixed_version"`
		Trace        []struct {
			Module string `json:"module"`
		} `json:"trace"`
	} `json:"finding"`
}

func (goProvider) Audit(ctx context.Context, dir string) (map[string][]Vuln, error) {
	if !hasTool("govulncheck") {
		return nil, nil
	}
	out, err := runTool(ctx, dir, "govulncheck", "-json", "./...")
	if err != nil {
		return nil, err
	}
	osvs := map[string]Vuln{}
	// module -> osv id set, so multi-finding traces stay deduplicated.
	hits := map[string]map[string]bool{}
	fixed := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var msg govulnMsg
		if err := dec.Decode(&msg); err != nil {
			return nil, err
		}
		if msg.OSV != nil {
			osvs[msg.OSV.ID] = Vuln{ID: msg.OSV.ID, Summary: msg.OSV.Summary, URL: msg.OSV.Database.URL}
		}
		if f := msg.Finding; f != nil && len(f.Trace) > 0 {
			mod := f.Trace[len(f.Trace)-1].Module
			if mod == "" {
				continue
			}
			if hits[mod] == nil {
				hits[mod] = map[string]bool{}
			}
			hits[mod][f.OSV] = true
			if f.FixedVersion != "" {
				fixed[f.OSV] = strings.TrimPrefix(f.FixedVersion, mod+"@")
			}
		}
	}
	vulns := map[string][]Vuln{}
	for mod, ids := range hits {
		for id := range ids {
			v := osvs[id]
			if v.ID == "" {
				v.ID = id
			}
			v.FixedIn = fixed[id]
			vulns[mod] = append(vulns[mod], v)
		}
	}
	return vulns, nil
}
