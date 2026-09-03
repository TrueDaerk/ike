package deps

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeBin writes an executable shell script named name that emits the
// given stdout and exits with code, and returns the bin directory.
func fakeBin(t *testing.T, dir, name, stdout string, code int) {
	t.Helper()
	script := "#!/bin/sh\ncat <<'IKE_EOF'\n" + stdout + "\nIKE_EOF\nexit " + strings.TrimSpace(string(rune('0'+code))) + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// toolPath points PATH at bin only (plus /bin:/usr/bin for sh itself), so
// exec.LookPath sees exactly the fake toolchain.
func toolPath(t *testing.T, bin string) {
	t.Helper()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/bin"+string(os.PathListSeparator)+"/usr/bin")
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const goMod = `module example.com/demo

go 1.22

require (
	github.com/foo/bar v1.2.3
	github.com/baz/qux v0.1.0 // indirect
)

require golang.org/x/text v0.14.0
`

func TestGoManifestParsesRequireBlocksAndLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	write(t, path, goMod)
	deps, err := goProvider{}.Manifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 3 {
		t.Fatalf("want 3 deps, got %v", deps)
	}
	if deps[0].Name != "github.com/foo/bar" || deps[0].Current != "v1.2.3" || deps[0].Line != 6 || deps[0].Indirect {
		t.Fatalf("bad first dep: %+v", deps[0])
	}
	if !deps[1].Indirect {
		t.Fatalf("indirect marker lost: %+v", deps[1])
	}
	if deps[2].Name != "golang.org/x/text" || deps[2].Line != 10 {
		t.Fatalf("single-line require missed: %+v", deps[2])
	}
}

func TestGoOutdatedAndAudit(t *testing.T) {
	bin := t.TempDir()
	fakeBin(t, bin, "go", `{"Path":"example.com/demo","Main":true,"Version":""}
{"Path":"github.com/foo/bar","Version":"v1.2.3","Update":{"Version":"v1.3.0"}}
{"Path":"golang.org/x/text","Version":"v0.14.0"}`, 0)
	fakeBin(t, bin, "govulncheck", `{"osv":{"id":"GO-2024-1234","summary":"bad parser","database_specific":{"url":"https://pkg.go.dev/vuln/GO-2024-1234"}}}
{"finding":{"osv":"GO-2024-1234","fixed_version":"v1.3.0","trace":[{"module":"github.com/foo/bar"}]}}`, 0)
	toolPath(t, bin)

	latest, err := goProvider{}.Outdated(context.Background(), bin)
	if err != nil {
		t.Fatal(err)
	}
	if latest["github.com/foo/bar"] != "v1.3.0" || len(latest) != 1 {
		t.Fatalf("bad outdated map: %v", latest)
	}
	vulns, err := goProvider{}.Audit(context.Background(), bin)
	if err != nil {
		t.Fatal(err)
	}
	v := vulns["github.com/foo/bar"]
	if len(v) != 1 || v[0].ID != "GO-2024-1234" || v[0].Summary != "bad parser" || v[0].FixedIn != "v1.3.0" {
		t.Fatalf("bad vulns: %+v", vulns)
	}
}

const packageJSON = `{
  "name": "demo",
  "dependencies": {
    "left-pad": "^1.3.0"
  },
  "devDependencies": {
    "eslint": "~9.0.0"
  }
}
`

func TestNpmManifestOutdatedAudit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "package.json")
	write(t, path, packageJSON)
	deps, err := npmProvider{}.Manifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 2 || deps[0].Name != "eslint" || deps[0].Current != "9.0.0" || deps[0].Line != 7 {
		t.Fatalf("bad deps: %+v", deps)
	}
	if deps[1].Name != "left-pad" || deps[1].Line != 4 {
		t.Fatalf("bad deps: %+v", deps)
	}

	bin := t.TempDir()
	// npm outdated exits 1 when something is outdated — output must
	// still be parsed.
	fakeBin(t, bin, "npm", `{"left-pad":{"current":"1.3.0","wanted":"1.3.0","latest":"1.5.0"}}`, 1)
	toolPath(t, bin)
	latest, err := npmProvider{}.Outdated(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if latest["left-pad"] != "1.5.0" {
		t.Fatalf("bad latest: %v", latest)
	}

	fakeBin(t, bin, "npm", `{"vulnerabilities":{"left-pad":{"severity":"high","via":[{"title":"padding overflow","url":"https://github.com/advisories/GHSA-xxxx","severity":"high"}]}}}`, 1)
	vulns, err := npmProvider{}.Audit(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	v := vulns["left-pad"]
	if len(v) != 1 || v[0].ID != "GHSA-xxxx" || v[0].Severity != "high" || v[0].Summary != "padding overflow" {
		t.Fatalf("bad vulns: %+v", vulns)
	}
}

func TestNpmPackageManagerByLockfile(t *testing.T) {
	dir := t.TempDir()
	if pm := packageManager(dir); pm != "npm" {
		t.Fatalf("default pm = %s", pm)
	}
	write(t, filepath.Join(dir, "yarn.lock"), "")
	if pm := packageManager(dir); pm != "yarn" {
		t.Fatalf("yarn.lock pm = %s", pm)
	}
	write(t, filepath.Join(dir, "pnpm-lock.yaml"), "")
	if pm := packageManager(dir); pm != "pnpm" {
		t.Fatalf("pnpm-lock pm = %s", pm)
	}
}

const composerJSON = `{
  "require": {
    "php": ">=8.1",
    "monolog/monolog": "^2.9.0"
  }
}
`

func TestComposerManifestOutdatedAudit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "composer.json")
	write(t, path, composerJSON)
	deps, err := composerProvider{}.Manifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0].Name != "monolog/monolog" || deps[0].Current != "2.9.0" || deps[0].Line != 4 {
		t.Fatalf("php platform req must be skipped, monolog kept: %+v", deps)
	}

	bin := t.TempDir()
	fakeBin(t, bin, "composer", `{"installed":[{"name":"monolog/monolog","version":"2.9.0","latest":"3.6.0","direct-dependency":true}]}`, 0)
	toolPath(t, bin)
	latest, err := composerProvider{}.Outdated(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if latest["monolog/monolog"] != "3.6.0" {
		t.Fatalf("bad latest: %v", latest)
	}

	fakeBin(t, bin, "composer", `{"advisories":{"monolog/monolog":[{"advisoryId":"PKSA-1","title":"log injection","cve":"CVE-2024-0001","severity":"medium","link":"https://example.com/adv"}]}}`, 0)
	vulns, err := composerProvider{}.Audit(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	v := vulns["monolog/monolog"]
	if len(v) != 1 || v[0].ID != "CVE-2024-0001" || v[0].Severity != "medium" {
		t.Fatalf("bad vulns: %+v", vulns)
	}
}

const cargoToml = `[package]
name = "demo"

[dependencies]
serde = "1.0.190"
tokio = { version = "1.35", features = ["full"] }

[dev-dependencies]
insta = "1.34"
`

func TestCargoManifestOutdatedAudit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Cargo.toml")
	write(t, path, cargoToml)
	deps, err := cargoProvider{}.Manifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 3 || deps[0].Name != "serde" || deps[0].Current != "1.0.190" || deps[0].Line != 5 {
		t.Fatalf("bad deps: %+v", deps)
	}
	if deps[1].Name != "tokio" || deps[1].Current != "1.35" {
		t.Fatalf("inline-table version missed: %+v", deps[1])
	}

	bin := t.TempDir()
	fakeBin(t, bin, "cargo", `{"dependencies":[{"name":"serde","project":"1.0.190","latest":"1.0.200"}]}`, 0)
	fakeBin(t, bin, "cargo-outdated", "", 0)
	fakeBin(t, bin, "cargo-audit", "", 0)
	toolPath(t, bin)
	latest, err := cargoProvider{}.Outdated(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if latest["serde"] != "1.0.200" {
		t.Fatalf("bad latest: %v", latest)
	}

	fakeBin(t, bin, "cargo", `{"vulnerabilities":{"list":[{"advisory":{"id":"RUSTSEC-2024-0001","title":"UAF in tokio","url":"https://rustsec.org"},"package":{"name":"tokio","version":"1.35.0"},"versions":{"patched":[">=1.36.0"]}}]}}`, 1)
	vulns, err := cargoProvider{}.Audit(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	v := vulns["tokio"]
	if len(v) != 1 || v[0].ID != "RUSTSEC-2024-0001" || v[0].FixedIn != ">=1.36.0" {
		t.Fatalf("bad vulns: %+v", vulns)
	}
}

func TestCargoOutdatedSkippedWithoutSubcommand(t *testing.T) {
	bin := t.TempDir()
	fakeBin(t, bin, "cargo", "must not run", 0)
	toolPath(t, bin)
	latest, err := cargoProvider{}.Outdated(context.Background(), bin)
	if err != nil || latest != nil {
		t.Fatalf("missing cargo-outdated must be a silent skip, got %v %v", latest, err)
	}
}

const requirements = `# pinned
requests==2.31.0
Flask>=2.0
`

func TestPipManifestsOutdatedAudit(t *testing.T) {
	dir := t.TempDir()
	reqPath := filepath.Join(dir, "requirements.txt")
	write(t, reqPath, requirements)
	deps, err := pipProvider{}.Manifest(reqPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 2 || deps[0].Name != "requests" || deps[0].Current != "2.31.0" || deps[0].Line != 2 {
		t.Fatalf("bad deps: %+v", deps)
	}
	if deps[1].Name != "Flask" || deps[1].Current != "" {
		t.Fatalf("range pin must have no current: %+v", deps[1])
	}

	pyPath := filepath.Join(dir, "pyproject.toml")
	write(t, pyPath, "[project]\nname = \"demo\"\ndependencies = [\n  \"httpx==0.27.0\",\n]\n")
	deps, err = pipProvider{}.Manifest(pyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0].Name != "httpx" || deps[0].Current != "0.27.0" || deps[0].Line != 4 {
		t.Fatalf("bad pyproject deps: %+v", deps)
	}

	bin := t.TempDir()
	fakeBin(t, bin, "pip3", `[{"name":"Requests","version":"2.31.0","latest_version":"2.32.0"}]`, 0)
	fakeBin(t, bin, "pip-audit", `{"dependencies":[{"name":"requests","version":"2.31.0","vulns":[{"id":"PYSEC-2024-1","description":"cert bypass\ndetails","fix_versions":["2.32.0"]}]}]}`, 0)
	toolPath(t, bin)
	latest, err := pipProvider{}.Outdated(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if latest["requests"] != "2.32.0" {
		t.Fatalf("names must be PEP 503 normalized: %v", latest)
	}
	vulns, err := pipProvider{}.Audit(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	v := vulns["requests"]
	if len(v) != 1 || v[0].ID != "PYSEC-2024-1" || v[0].Summary != "cert bypass" || v[0].FixedIn != "2.32.0" {
		t.Fatalf("bad vulns: %+v", vulns)
	}
}

func TestBumpLineKeepsConstraintPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "package.json")
	write(t, path, packageJSON)
	dep := Dep{Name: "left-pad", Current: "1.3.0", Line: 4}
	if err := (npmProvider{}).Bump(path, dep, "1.5.0"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"left-pad": "^1.5.0"`) {
		t.Fatalf("prefix lost or bump missed:\n%s", data)
	}
}

func TestBumpLineRefusesDriftedManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	write(t, path, goMod)
	err := (goProvider{}).Bump(path, Dep{Name: "github.com/foo/bar", Current: "v9.9.9", Line: 6}, "v1.3.0")
	if err == nil {
		t.Fatal("bump must refuse when the line no longer matches")
	}
}

func TestScannerCachesByMtimeAndForceRescans(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "package.json"), packageJSON)
	bin := t.TempDir()
	fakeBin(t, bin, "npm", `{"left-pad":{"latest":"1.5.0"}}`, 0)
	toolPath(t, bin)

	s := NewScanner()
	res := s.Scan(context.Background(), root, false)
	if len(res.Manifests) != 1 || res.Manifests[0].Provider != "npm" {
		t.Fatalf("bad scan: %+v", res)
	}
	find := func(r Result, name string) Dep {
		for _, d := range r.Manifests[0].Deps {
			if d.Name == name {
				return d
			}
		}
		return Dep{}
	}
	if find(res, "left-pad").Latest != "1.5.0" {
		t.Fatalf("latest not joined: %+v", res.Manifests[0].Deps)
	}

	// New tool output without a manifest change: cache must win.
	fakeBin(t, bin, "npm", `{"left-pad":{"latest":"9.9.9"}}`, 0)
	res = s.Scan(context.Background(), root, false)
	if find(res, "left-pad").Latest != "1.5.0" {
		t.Fatalf("mtime cache bypassed: %+v", res.Manifests[0].Deps)
	}
	// Force bypasses the cache.
	res = s.Scan(context.Background(), root, true)
	if find(res, "left-pad").Latest != "9.9.9" {
		t.Fatalf("force did not rescan: %+v", res.Manifests[0].Deps)
	}
	// A manifest touch also invalidates.
	fakeBin(t, bin, "npm", `{"left-pad":{"latest":"1.5.0"}}`, 0)
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(root, "package.json"), future, future); err != nil {
		t.Fatal(err)
	}
	res = s.Scan(context.Background(), root, false)
	if find(res, "left-pad").Latest != "1.5.0" {
		t.Fatalf("mtime change ignored: %+v", res.Manifests[0].Deps)
	}
}

func TestScanReportsMissingToolsWithHints(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), goMod)
	bin := t.TempDir() // empty: no go, no govulncheck
	toolPath(t, bin)
	res := NewScanner().Scan(context.Background(), root, false)
	if len(res.Manifests) != 1 || len(res.Manifests[0].Deps) != 3 {
		t.Fatalf("manifest must still be listed without the toolchain: %+v", res)
	}
	if len(res.Missing) != 2 {
		t.Fatalf("want go+govulncheck missing, got %+v", res.Missing)
	}
	for _, mt := range res.Missing {
		if mt.Provider != "go" || mt.Tool.Hint == "" {
			t.Fatalf("missing tool without hint: %+v", mt)
		}
	}
}

func TestSnapshotDepAt(t *testing.T) {
	SetSnapshot(Result{Manifests: []ManifestDeps{{
		Path: "/p/go.mod", Provider: "go",
		Deps: []Dep{{Name: "github.com/foo/bar", Current: "v1.2.3", Latest: "v1.3.0", Line: 6}},
	}}})
	t.Cleanup(func() { SetSnapshot(Result{}) })
	d, prov, ok := DepAt("/p/go.mod", 6)
	if !ok || prov != "go" || d.Latest != "v1.3.0" {
		t.Fatalf("DepAt failed: %+v %s %v", d, prov, ok)
	}
	if _, _, ok := DepAt("/p/go.mod", 7); ok {
		t.Fatal("line 7 must not resolve")
	}
}
