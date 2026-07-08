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
	got := pythonCandidates(root, run, look)
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
