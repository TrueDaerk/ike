package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/lang"
)

// fakeTC is a registered test toolchain: it "detects" a fixed interpreter and
// maps explicit choices into settings.
type fakeTC struct{ detected string }

func (f fakeTC) Detect(string) (map[string]any, bool) {
	return map[string]any{"x": f.detected}, true
}
func (f fakeTC) Interpreter(string) (string, bool) { return f.detected, f.detected != "" }

func TestInterpreterExplicitBeatsDetection(t *testing.T) {
	lang.Register(lang.Language{ID: "tctest", Extensions: []string{"tctest"}, Toolchain: fakeTC{detected: "/detected/bin/x"}})
	if p, src := lang.Interpreter("tctest", ".", ""); p != "/detected/bin/x" || src != "detected" {
		t.Fatalf("detection: %q %q", p, src)
	}
	if p, src := lang.Interpreter("tctest", ".", "/explicit/x"); p != "/explicit/x" || src != "config" {
		t.Fatalf("explicit must win: %q %q", p, src)
	}
	if p, src := lang.Interpreter("no-such-lang", ".", ""); p != "" || src != "" {
		t.Fatalf("unknown language: %q %q", p, src)
	}
}

func TestParseUvPythonList(t *testing.T) {
	out := `cpython-3.13.0-macos-aarch64-none                 <download available>
cpython-3.12.4-macos-aarch64-none                 /Users/x/.local/share/uv/python/cpython-3.12.4-macos-aarch64-none/bin/python3.12
cpython-3.11.9-macos-aarch64-none                 /opt/homebrew/bin/python3.11
`
	got := parseUvPythonList(out)
	want := []string{
		"/Users/x/.local/share/uv/python/cpython-3.12.4-macos-aarch64-none/bin/python3.12",
		"/opt/homebrew/bin/python3.11",
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("parseUvPythonList = %v, want %v", got, want)
	}
}

func TestPythonCandidatesVenvAndUv(t *testing.T) {
	root := t.TempDir()
	venvPy := filepath.Join(root, ".venv", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(venvPy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(venvPy, []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}
	uvPy := filepath.Join(root, "uv-python3.12")
	if err := os.WriteFile(uvPy, []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIRTUAL_ENV", "") // no active venv in the test env
	run := func(name string, args ...string) string {
		if name == "uv" {
			return "cpython-3.12  " + uvPy + "\ncpython-3.13  <download available>\n"
		}
		return ""
	}
	look := func(string) string { return "" }
	got := pythonCandidates(root, run, look, noResolve)
	if len(got) != 2 || got[0] != venvPy || got[1] != uvPy {
		t.Fatalf("candidates = %v, want [%s %s]", got, venvPy, uvPy)
	}
}

func TestToolchainChooseWritesProjectConfigAndRestarts(t *testing.T) {
	restoreConfig(t)
	lang.Register(lang.Language{ID: "tcpage", Extensions: []string{"tcpage"}, Toolchain: fakeTC{detected: "/detected/bin/x"}})
	proj := t.TempDir()
	opts := config.Options{UserPath: filepath.Join(t.TempDir(), "settings.toml"), ProjectRoot: proj}
	restarted := false
	p := NewToolchainPage(opts, proj, func() tea.Cmd {
		restarted = true
		return nil
	})
	p.run = func(name string, args ...string) string { return "X 1.2.3\nextra" }
	p.look = func(string) string { return "" }

	// Select the tcpage row.
	for i, l := range p.languages() {
		if l.ID == "tcpage" {
			p.sel = i
		}
	}
	interp := filepath.Join(proj, "bin-x")
	if err := os.WriteFile(interp, []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}
	p.candidates, p.picking, p.pick = []string{interp}, true, 0
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("choosing a candidate must produce commands")
	}
	// Run the batched commands: write-reload + probe (+ restart already ran).
	drainBatch(t, p, cmd)
	if !restarted {
		t.Fatal("choosing an interpreter must offer the LSP restart")
	}
	if got := config.Get().Lang["tcpage"]["interpreter"]; got != interp {
		t.Fatalf("project config interpreter = %q, want %q", got, interp)
	}
	if got := config.Origin(opts, "lang.tcpage.interpreter"); got != "project" {
		t.Fatalf("interpreter origin = %q, want project", got)
	}
	// The page now resolves the explicit value and renders the probed version.
	if path, src := p.interpreter("tcpage"); path != interp || src != "config" {
		t.Fatalf("effective interpreter = %q %q", path, src)
	}
	if v := p.View(120, 40); !strings.Contains(v, "X 1.2.3") || !strings.Contains(v, "@config") {
		t.Fatalf("view missing probe/source:\n%s", v)
	}
	// Reset falls back to detection.
	drainBatch(t, p, p.Update(tea.KeyPressMsg{Text: "r", Code: 'r'}))
	if path, src := p.interpreter("tcpage"); path != "/detected/bin/x" || src != "detected" {
		t.Fatalf("reset must fall back to detection, got %q %q", path, src)
	}
}

// drainBatch executes a (possibly batched) command tree, feeding config
// reloads into Set and version probes into the page.
func drainBatch(t *testing.T, p *ToolchainPage, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	switch m := msg.(type) {
	case config.ConfigReloadedMsg:
		config.Set(m.Config)
	case VersionMsg:
		p.Receive(m)
	case tea.BatchMsg:
		for _, c := range m {
			drainBatch(t, p, c)
		}
	}
}

// TestToolchainFooterPinned guards #537: the key hints render in a footer
// pinned to the bottom, and moving the selection does not shift other rows.
func TestToolchainFooterPinned(t *testing.T) {
	restoreConfig(t)
	lang.Register(lang.Language{ID: "tcfoot1", Extensions: []string{"tcfoot1"}, Toolchain: fakeTC{detected: "/bin/a"}})
	lang.Register(lang.Language{ID: "tcfoot2", Extensions: []string{"tcfoot2"}, Toolchain: fakeTC{detected: "/bin/b"}})
	p := NewToolchainPage(testOpts(t), t.TempDir(), nil)
	p.look = func(string) string { return "" }
	p.run = func(string, ...string) string { return "" }
	const h = 10
	lines := strings.Split(p.View(120, h), "\n")
	if len(lines) != h {
		t.Fatalf("view height = %d, want %d", len(lines), h)
	}
	if !strings.Contains(lines[h-3], "enter pick interpreter") { // 3-line footer: hint(2, #553) + status
		t.Fatalf("hint must be pinned above the status line:\n%s", strings.Join(lines, "\n"))
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	lines = strings.Split(p.View(120, h), "\n")
	if len(lines) != h || !strings.Contains(lines[h-3], "enter pick interpreter") {
		t.Fatalf("footer must stay pinned after a selection move:\n%s", strings.Join(lines, "\n"))
	}
}

// TestDefaultCandidatesWellKnownDirs guards #538: languages without specific
// discovery get PATH plus the well-known install directories as candidates.
func TestDefaultCandidatesWellKnownDirs(t *testing.T) {
	prev := wellKnownBinDirs
	t.Cleanup(func() { wellKnownBinDirs = prev })
	dir := t.TempDir()
	fake := filepath.Join(dir, "xlang")
	if err := os.WriteFile(fake, []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}
	wellKnownBinDirs = []string{filepath.Join(dir, "missing"), dir}

	// PATH miss: the well-known location is still offered.
	got := defaultCandidates("xlang", ".", func(string) string { return "" }, noResolve)
	if len(got) != 1 || got[0] != fake {
		t.Fatalf("candidates = %v, want [%s]", got, fake)
	}
	// PATH hit first, deduplicated against the same well-known path.
	got = defaultCandidates("xlang", ".", func(string) string { return fake }, noResolve)
	if len(got) != 1 || got[0] != fake {
		t.Fatalf("candidates must dedupe PATH vs well-known, got %v", got)
	}
	got = defaultCandidates("xlang", ".", func(string) string { return "/elsewhere/xlang" }, noResolve)
	if len(got) != 2 || got[0] != "/elsewhere/xlang" || got[1] != fake {
		t.Fatalf("PATH must come first, got %v", got)
	}
}

// TestToolchainFooterHintWraps guards #553: on a narrow column the python
// hint wraps over the footer's two hint lines instead of clipping mid-word
// ("· u u" for "u uv install").
func TestToolchainFooterHintWraps(t *testing.T) {
	restoreConfig(t)
	f := &fakeEnv{binaries: map[string]string{}}
	p := pythonPage(t, f)
	const w, h = 76, 12
	v := p.View(w, h)
	if !strings.Contains(v, "u uv install") {
		t.Fatalf("the full hint must survive wrapping:\n%s", v)
	}
	lines := strings.Split(v, "\n")
	if len(lines) != h {
		t.Fatalf("view height = %d, want %d", len(lines), h)
	}
}

// noResolve is the identity resolveShim for tests not exercising #650.
func noResolve(_, p string) string { return p }

// TestPythonCandidatesResolveShims guards #650: version-manager shims returned
// by PATH lookup (and the hardcoded pyenv shim) are resolved to the real
// interpreter, and identical resolutions dedupe to one entry.
func TestPythonCandidatesResolveShims(t *testing.T) {
	t.Setenv("VIRTUAL_ENV", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	shim := filepath.Join(home, ".pyenv", "shims", "python")
	if err := os.MkdirAll(filepath.Dir(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shim, []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(home, "versions", "3.12.4", "python3.12")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	run := func(string, ...string) string { return "" }
	look := func(name string) string {
		if name == "python3" {
			return shim // PATH also serves the shim
		}
		return ""
	}
	resolve := func(gotRoot, p string) string {
		if gotRoot != root {
			t.Errorf("resolve root = %q, want %q", gotRoot, root)
		}
		if strings.Contains(p, string(filepath.Separator)+".pyenv"+string(filepath.Separator)+"shims"+string(filepath.Separator)) {
			return real
		}
		return p
	}
	got := pythonCandidates(root, run, look, resolve)
	if len(got) != 1 || got[0] != real {
		t.Fatalf("candidates = %v, want the single resolved path [%s]", got, real)
	}

	// Resolution failure keeps the shim (identity resolve).
	got = pythonCandidates(root, run, look, noResolve)
	if len(got) != 1 || got[0] != shim {
		t.Fatalf("candidates = %v, want unresolved shim [%s]", got, shim)
	}
}

// TestPhpAndDefaultCandidatesResolveShims guards #650 for the other discovery
// helpers: PATH hits go through the resolver.
func TestPhpAndDefaultCandidatesResolveShims(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real-bin")
	if err := os.WriteFile(real, []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolve := func(_, p string) string {
		if strings.Contains(p, "shims") {
			return real
		}
		return p
	}

	// The well-known php fallbacks may exist on the host; only the PATH hit
	// (first entry) matters here.
	got := phpCandidates(".", func(name string) string { return "/x/.asdf/shims/php" }, resolve)
	if len(got) == 0 || got[0] != real {
		t.Fatalf("php candidates = %v, want resolved %s first", got, real)
	}
	for _, p := range got {
		if strings.Contains(p, "shims") {
			t.Fatalf("shim leaked into candidates: %v", got)
		}
	}

	prev := wellKnownBinDirs
	t.Cleanup(func() { wellKnownBinDirs = prev })
	wellKnownBinDirs = nil
	got = defaultCandidates("go", ".", func(string) string { return "/x/mise/shims/go" }, resolve)
	if len(got) != 1 || got[0] != real {
		t.Fatalf("default candidates = %v, want [%s]", got, real)
	}
}
