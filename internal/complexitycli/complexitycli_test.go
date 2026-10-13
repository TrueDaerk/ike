package complexitycli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/cli"
	"ike/internal/complexity"
	"ike/internal/lang"
	_ "ike/plugins/languages/go"
	_ "ike/plugins/languages/json"
)

// simpleGo has one function with CCN 1; branchyGo one with CCN 5, cog 6.
const simpleGo = `package p

func simple() int { return 1 }
`

const branchyGo = `package p

func branchy(a, b int) int {
	if a > 0 {
		if b > 0 {
			return 1
		}
	}
	for i := 0; i < a; i++ {
		if i == b {
			return i
		}
	}
	return 0
}
`

func requireTrees(t *testing.T) {
	t.Helper()
	if !complexity.Available() {
		t.Skip("no syntax trees in this build (cgo off)")
	}
}

// fixture writes a small project: two Go files, an excluded vendor tree, a
// hidden directory, a file of a language without a spec, and a project
// config excluding one Go file.
func fixture(t *testing.T) string {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	write(t, dir, "a.go", simpleGo)
	write(t, dir, "pkg/b.go", branchyGo)
	write(t, dir, "vendor/v.go", branchyGo)
	write(t, dir, ".hidden/h.go", branchyGo)
	write(t, dir, "gen/skip_me.go", branchyGo)
	write(t, dir, "data.json", `{"a": 1}`)
	write(t, dir, ".ike/settings.toml", "[complexity]\nexclude = [\"gen/**\"]\nccn_warn = 4\n")
	return dir
}

func write(t *testing.T, dir, rel, text string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, a cli.ComplexityArgs) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(a, &out, &errb)
	return out.String(), errb.String(), code
}

func TestProjectText(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}})
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if !strings.HasPrefix(lines[0], "FILE") || !strings.Contains(lines[0], "PARAMS") {
		t.Fatalf("header = %q", lines[0])
	}
	// Sorted by CCN descending: branchy first.
	if !strings.HasPrefix(lines[1], "pkg/b.go") || !strings.Contains(lines[1], "branchy") {
		t.Errorf("first row = %q, want pkg/b.go branchy", lines[1])
	}
	if !strings.HasPrefix(lines[2], "a.go") {
		t.Errorf("second row = %q, want a.go", lines[2])
	}
	for _, bad := range []string{"vendor/", ".hidden/", "gen/"} {
		if strings.Contains(out, bad) {
			t.Errorf("output lists %s:\n%s", bad, out)
		}
	}
	if !strings.Contains(out, "summary: 2 functions in 2 files") {
		t.Errorf("summary missing:\n%s", out)
	}
	// ccn_warn = 4 from the project config: branchy is over threshold.
	if !strings.Contains(out, "1 over threshold (CCN ≥ 4 or cog ≥ 15 or cmt ≥ 40%)") || !strings.Contains(lines[0], "CMT%") {
		t.Errorf("thresholds not read from the project config:\n%s", out)
	}
	if _, ok := lang.ByPath("data.json"); ok && !strings.Contains(out, "skipped: 1 file (no complexity spec: json)") {
		t.Errorf("skipped line missing:\n%s", out)
	}
}

func TestProjectMinFilter(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	out, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Min: 3})
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "branchy") || strings.Contains(out, "simple") {
		t.Errorf("--min 3 should keep only branchy:\n%s", out)
	}
	if !strings.Contains(out, "shown: 1 of 2 functions (CCN or cog ≥ 3)") {
		t.Errorf("shown line missing:\n%s", out)
	}
	// An explicit --ccn filters at that threshold too.
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}, CCN: 2})
	if strings.Contains(out, "simple") {
		t.Errorf("--ccn 2 should drop simple:\n%s", out)
	}
}

func TestProjectJSON(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	out, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, JSON: true})
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var got struct {
		Summary   map[string]any   `json:"summary"`
		Functions []map[string]any `json:"functions"`
		Skipped   map[string]any   `json:"skipped"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	for _, k := range []string{"files", "functions", "nloc", "ccn_sum", "ccn_avg", "ccn_max", "cog_sum", "cog_avg", "cog_max", "over_threshold", "ccn_warn", "cog_warn"} {
		if _, ok := got.Summary[k]; !ok {
			t.Errorf("summary lacks %q", k)
		}
	}
	if got.Summary["ccn_max"] != float64(5) || got.Summary["files"] != float64(2) {
		t.Errorf("summary = %v", got.Summary)
	}
	if len(got.Functions) != 2 {
		t.Fatalf("functions = %v", got.Functions)
	}
	f := got.Functions[0]
	want := map[string]any{"file": "pkg/b.go", "name": "branchy", "kind": "function", "line": float64(3), "ccn": float64(5), "params": float64(2)}
	for k, v := range want {
		if f[k] != v {
			t.Errorf("functions[0].%s = %v, want %v", k, f[k], v)
		}
	}
	if _, ok := got.Skipped["languages"]; !ok {
		t.Errorf("skipped lacks languages: %v", got.Skipped)
	}
}

// chattyGo has one function whose body is 60 % comment (2 code lines, 3
// comment lines) under a doc comment that does not count (#2934).
const chattyGo = `package p

// chatty is documented above its header; that comment stays out.
func chatty() {
	// one
	// two
	/* three */
}
`

// TestProjectCmtFilter: --cmt N filters like --ccn / --cog, the JSON
// carries comment_lines / comment_density on functions and the summary,
// and the text table has a CMT% column (#2934).
func TestProjectCmtFilter(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	write(t, dir, "chatty.go", chattyGo)
	out, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Cmt: 40, JSON: true})
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var got struct {
		Summary   map[string]any   `json:"summary"`
		Functions []map[string]any `json:"functions"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	// branchy stays for CCN ≥ 4 (project config), chatty for cmt ≥ 40;
	// simple drops.
	names := []string{}
	for _, f := range got.Functions {
		names = append(names, f["name"].(string))
	}
	if strings.Join(names, ",") != "branchy,chatty" {
		t.Fatalf("--cmt 40 rows = %v", names)
	}
	c := got.Functions[1]
	if c["comment_lines"] != float64(3) || c["comment_density"] != 0.6 || c["nloc"] != float64(2) {
		t.Errorf("chatty = %v", c)
	}
	if b := got.Functions[0]; b["comment_lines"] != float64(0) || b["comment_density"] != float64(0) {
		t.Errorf("branchy = %v", b)
	}
	for _, k := range []string{"comment_lines", "comment_density", "cmt_warn"} {
		if _, ok := got.Summary[k]; !ok {
			t.Errorf("summary lacks %q", k)
		}
	}
	if got.Summary["cmt_warn"] != float64(40) || got.Summary["comment_lines"] != float64(3) || got.Summary["over_threshold"] != float64(2) {
		t.Errorf("summary = %v", got.Summary)
	}
	// A threshold the function stays under drops it; the table shows CMT%.
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}, Cmt: 61})
	if strings.Contains(out, "chatty") || !strings.Contains(out, "CMT%") {
		t.Errorf("--cmt 61 should drop chatty:\n%s", out)
	}
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}})
	if !strings.Contains(out, "60\n") {
		t.Errorf("the table must show chatty's 60:\n%s", out)
	}
}

// TestNoCgoBuild: a build without syntax trees refuses with a clear message
// and exit 2 instead of printing an empty report.
func TestNoCgoBuild(t *testing.T) {
	orig := available
	available = func() bool { return false }
	defer func() { available = orig }()
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{t.TempDir()}})
	if code != ExitError || out != "" || !strings.Contains(stderr, "cgo") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, stderr)
	}
}

func TestUsageAndDirErrors(t *testing.T) {
	out, _, code := run(t, cli.ComplexityArgs{Help: true})
	if code != ExitOK || !strings.Contains(out, "usage: ike complexity") {
		t.Errorf("help: exit %d, %q", code, out)
	}
	requireTrees(t)
	_, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{filepath.Join(t.TempDir(), "missing")}})
	if code != ExitError || stderr == "" {
		t.Errorf("missing dir: exit %d, stderr %q", code, stderr)
	}
}

// gitFixture commits the fixture project, then edits it: branchy gains a
// branch, simple is removed, added() is new.
func gitFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := fixture(t)
	g := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	g("init", "-q", "-b", "main")
	g("add", "-A")
	g("commit", "-qm", "base")
	write(t, dir, "pkg/b.go", strings.Replace(branchyGo, "\treturn 0\n", "\tif a == b && b > 2 {\n\t\treturn 2\n\t}\n\treturn 0\n", 1))
	write(t, dir, "a.go", "package p\n")
	write(t, dir, "fresh/c.go", "package p\n\nfunc added(x int) int {\n\tif x > 0 {\n\t\treturn x\n\t}\n\treturn 0\n}\n")
	return dir
}

func TestDiffText(t *testing.T) {
	requireTrees(t)
	dir := gitFixture(t)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Diff: true})
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	for _, want := range []string{
		"complexity vs HEAD (",
		"changed (1):",
		"CCN 5→7 (+2)",
		"added (1):",
		"fresh/c.go",
		"removed (1):",
		"simple",
		"totals: before 2 functions, ΣCCN 6,",
		"verdict: MORE COMPLEX (ΣCCN +3,",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("diff text lacks %q:\n%s", want, out)
		}
	}
}

func TestDiffJSON(t *testing.T) {
	requireTrees(t)
	dir := gitFixture(t)
	out, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Diff: true, Rev: "main", JSON: true})
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var got struct {
		Rev     string           `json:"rev"`
		Base    map[string]any   `json:"base"`
		Head    map[string]any   `json:"head"`
		Changed []map[string]any `json:"changed"`
		Added   []map[string]any `json:"added"`
		Removed []map[string]any `json:"removed"`
		Verdict string           `json:"verdict"`
		Delta   map[string]any   `json:"delta"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got.Rev != "main" || got.Verdict != "more_complex" {
		t.Errorf("rev %q verdict %q", got.Rev, got.Verdict)
	}
	if len(got.Changed) != 1 || len(got.Added) != 1 || len(got.Removed) != 1 {
		t.Fatalf("changed/added/removed = %d/%d/%d\n%s", len(got.Changed), len(got.Added), len(got.Removed), out)
	}
	d := got.Changed[0]["delta"].(map[string]any)
	if got.Changed[0]["name"] != "branchy" || d["ccn"] != float64(2) {
		t.Errorf("changed[0] = %v", got.Changed[0])
	}
	if got.Base["ccn_sum"] != float64(6) || got.Head["ccn_sum"] != float64(9) || got.Delta["ccn"] != float64(3) {
		t.Errorf("base %v head %v delta %v", got.Base, got.Head, got.Delta)
	}
	// The comment density rides every side (#2934).
	for name, m := range map[string]map[string]any{
		"before": got.Changed[0]["before"].(map[string]any), "after": got.Changed[0]["after"].(map[string]any),
		"delta": d, "base": got.Base, "head": got.Head, "top delta": got.Delta, "added": got.Added[0],
	} {
		if _, ok := m["comment_lines"]; !ok {
			t.Errorf("%s lacks comment_lines: %v", name, m)
		}
		if _, ok := m["comment_density"]; !ok {
			t.Errorf("%s lacks comment_density: %v", name, m)
		}
	}
}

func TestDiffErrors(t *testing.T) {
	requireTrees(t)
	dir := gitFixture(t)
	_, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Diff: true, Rev: "no-such-ref"})
	if code != ExitError || !strings.Contains(stderr, "no-such-ref") {
		t.Errorf("bad ref: exit %d, stderr %q", code, stderr)
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", os.TempDir())
	plain := t.TempDir()
	write(t, plain, "a.go", simpleGo)
	_, stderr, code = run(t, cli.ComplexityArgs{Paths: []string{plain}, Diff: true})
	if code != ExitError || stderr == "" {
		t.Errorf("not a repository: exit %d, stderr %q", code, stderr)
	}
}

// projectJSON runs the JSON report and returns summary.files, the file of
// every function row and the skipped languages.
func projectJSON(t *testing.T, a cli.ComplexityArgs) (files int, rows, skippedLangs []string) {
	t.Helper()
	a.JSON = true
	out, stderr, code := run(t, a)
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	var got struct {
		Summary struct {
			Files int `json:"files"`
		} `json:"summary"`
		Functions []struct {
			File string `json:"file"`
		} `json:"functions"`
		Skipped struct {
			Languages []string `json:"languages"`
		} `json:"skipped"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	for _, f := range got.Functions {
		rows = append(rows, f.File)
	}
	return got.Summary.Files, rows, got.Skipped.Languages
}

// TestSelectSingleFile: one named file reports only its functions (#2936);
// outside a git repository the paths render relative to the working
// directory.
func TestSelectSingleFile(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	t.Chdir(dir)
	files, rows, _ := projectJSON(t, cli.ComplexityArgs{Paths: []string{"pkg/b.go"}})
	if files != 1 || len(rows) != 1 || rows[0] != "pkg/b.go" {
		t.Errorf("files %d, rows %v", files, rows)
	}
	out, _, _ := run(t, cli.ComplexityArgs{Paths: []string{"pkg/b.go"}})
	if !strings.Contains(out, "summary: 1 functions in 1 files") || strings.Contains(out, "a.go") {
		t.Errorf("text report:\n%s", out)
	}
}

// TestSelectFilesAndDirs: several files plus a directory are the union,
// each file measured once even when named twice or inside the directory.
func TestSelectFilesAndDirs(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	write(t, dir, "pkg/c.go", simpleGo)
	t.Chdir(dir)
	files, rows, _ := projectJSON(t, cli.ComplexityArgs{Paths: []string{"a.go", "a.go", "pkg", "pkg/b.go"}})
	if files != 3 || len(rows) != 3 {
		t.Fatalf("files %d, rows %v", files, rows)
	}
	want := map[string]bool{"a.go": true, "pkg/b.go": true, "pkg/c.go": true}
	for _, r := range rows {
		if !want[r] {
			t.Errorf("unexpected or repeated row file %q", r)
		}
		delete(want, r)
	}
	if len(want) != 0 {
		t.Errorf("missing rows %v", want)
	}
	files, _, _ = projectJSON(t, cli.ComplexityArgs{Paths: []string{"a.go", "pkg/c.go"}})
	if files != 2 {
		t.Errorf("two files: summary.files = %d", files)
	}
}

// TestSelectExcludedButNamed: a named file below an excluded or hidden
// directory is measured; the walk of its parent still skips the others.
func TestSelectExcludedButNamed(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	files, rows, _ := projectJSON(t, cli.ComplexityArgs{Paths: []string{dir, filepath.Join(dir, "gen/skip_me.go"), filepath.Join(dir, ".hidden/h.go")}})
	got := strings.Join(rows, " ")
	if files != 4 || !strings.Contains(got, "gen/skip_me.go") || !strings.Contains(got, ".hidden/h.go") || strings.Contains(got, "vendor/") {
		t.Errorf("summary.files = %d (want 4), rows %v", files, rows)
	}
}

// TestSelectUnmeasurableFile: a named file without a spec, or that no
// language claims, lands on the skipped line instead of aborting.
func TestSelectUnmeasurableFile(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	write(t, dir, "notes.zzz", "hello")
	t.Chdir(dir)
	files, rows, langs := projectJSON(t, cli.ComplexityArgs{Paths: []string{"a.go", "data.json", "notes.zzz"}})
	if files != 1 || len(rows) != 1 {
		t.Errorf("files %d rows %v", files, rows)
	}
	joined := strings.Join(langs, ",")
	if !strings.Contains(joined, ".zzz") {
		t.Errorf("skipped languages %v lack .zzz", langs)
	}
	if _, ok := lang.ByPath("data.json"); ok && !strings.Contains(joined, "json") {
		t.Errorf("skipped languages %v lack json", langs)
	}
	out, _, code := run(t, cli.ComplexityArgs{Paths: []string{"notes.zzz"}})
	if code != ExitOK || !strings.Contains(out, "skipped: 1 file (no complexity spec: .zzz)") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

// TestSelectUnknownPath: a missing path is a usage error naming it.
func TestSelectUnknownPath(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	t.Chdir(dir)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{"a.go", "nope/missing.go"}})
	if code != ExitError || out != "" || !strings.Contains(stderr, "nope/missing.go") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, stderr)
	}
}

// TestDiffSelectFile: --diff with a named file reports only that file's
// changes and an unchanged named file an empty "unchanged" diff. The root
// is the git root, so paths stay root-relative from a subdirectory.
func TestDiffSelectFile(t *testing.T) {
	requireTrees(t)
	dir := gitFixture(t)
	t.Chdir(filepath.Join(dir, "pkg"))
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{"b.go"}, Diff: true})
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if !strings.Contains(out, "changed (1):") || !strings.Contains(out, "pkg/b.go") ||
		strings.Contains(out, "added (") || strings.Contains(out, "removed (") {
		t.Errorf("diff restricted to pkg/b.go:\n%s", out)
	}
	// vendor/v.go is committed and untouched.
	out, _, code = run(t, cli.ComplexityArgs{Paths: []string{filepath.Join(dir, "vendor/v.go")}, Diff: true, JSON: true})
	var got struct {
		Changed []any  `json:"changed"`
		Added   []any  `json:"added"`
		Removed []any  `json:"removed"`
		Verdict string `json:"verdict"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != ExitOK {
		t.Fatalf("exit %d, %v\n%s", code, err, out)
	}
	if len(got.Changed)+len(got.Added)+len(got.Removed) != 0 || got.Verdict != "unchanged" {
		t.Errorf("unchanged file: %+v", got)
	}
	// A named untracked file below the directory git collapses still counts.
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{filepath.Join(dir, "fresh/c.go")}, Diff: true})
	if !strings.Contains(out, "added (1):") || !strings.Contains(out, "fresh/c.go") || strings.Contains(out, "changed (") {
		t.Errorf("untracked named file:\n%s", out)
	}
}
