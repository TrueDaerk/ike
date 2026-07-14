package run

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/lang"
)

// fakeToolchain contributes a deterministic run command and module form.
type fakeToolchain struct{}

func (fakeToolchain) Detect(string) (map[string]any, bool) { return nil, false }
func (fakeToolchain) Interpreter(string) (string, bool)    { return "/usr/bin/fake", true }
func (fakeToolchain) RunCommand(_ string, spec lang.RunSpec, interpreter string) ([]string, bool) {
	argv := []string{interpreter}
	if spec.Module != "" {
		argv = append(argv, "-m", spec.Module)
	} else {
		argv = append(argv, spec.File)
	}
	return append(argv, spec.Args...), true
}
func (fakeToolchain) Module(root, file string) (string, bool) {
	if filepath.Base(file) == "packaged.fake" {
		return "pkg.packaged", true
	}
	return "", false
}

func init() {
	lang.Register(lang.Language{ID: "fake", Extensions: []string{"fake"}, Toolchain: fakeToolchain{}})
}

// redirect points the store at a temp dir for the test's duration.
func redirect(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	return dir
}

// TestStoreRoundTrip verifies save/load keep configurations and last-used.
func TestStoreRoundTrip(t *testing.T) {
	redirect(t)
	s := Store{}
	s.Upsert(Config{Name: "a.fake", Kind: KindRun, Lang: "fake", File: "a.fake", Args: []string{"-x"}, Env: map[string]string{"K": "V"}})
	s.Touch("a.fake")
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if len(got.Configs) != 1 || got.LastUsed != "a.fake" {
		t.Fatalf("round trip lost data: %+v", got)
	}
	c := got.ByName("a.fake")
	if c == nil || c.Args[0] != "-x" || c.Env["K"] != "V" || c.Kind != KindRun {
		t.Fatalf("config fields lost: %+v", c)
	}
}

// TestLoadMissingOrMalformed verifies tolerant loading.
func TestLoadMissingOrMalformed(t *testing.T) {
	dir := redirect(t)
	if s := Load(); len(s.Configs) != 0 {
		t.Fatal("missing file must load empty")
	}
	if err := os.WriteFile(filepath.Join(dir, "runconfigs.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := Load(); len(s.Configs) != 0 {
		t.Fatal("malformed file must load empty")
	}
}

// TestEnsureForCreatesDefault verifies first-run default synthesis: kind run,
// no env, project-relative file, module form from the language.
func TestEnsureForCreatesDefault(t *testing.T) {
	redirect(t)
	root := t.TempDir()
	file := filepath.Join(root, "packaged.fake")
	s := Store{}
	cfg, created, ok := s.EnsureFor(root, file)
	if !ok || !created {
		t.Fatalf("EnsureFor: created=%v ok=%v", created, ok)
	}
	if cfg.Name != "packaged.fake" || cfg.Kind != KindRun || cfg.Lang != "fake" {
		t.Fatalf("default config wrong: %+v", cfg)
	}
	if cfg.File != "packaged.fake" {
		t.Fatalf("file must be project-relative, got %q", cfg.File)
	}
	if cfg.Module != "pkg.packaged" {
		t.Fatalf("module form not taken from the language: %q", cfg.Module)
	}
	if len(cfg.Env) != 0 || cfg.Cwd != "" {
		t.Fatal("defaults must carry no env and root cwd")
	}
	// Second call finds the same config, no duplicate.
	again, created2, _ := s.EnsureFor(root, file)
	if created2 || again != cfg {
		t.Fatal("EnsureFor must reuse the existing config")
	}
}

// TestEnsureForUnknownLanguage reports no config for unclaimed files.
func TestEnsureForUnknownLanguage(t *testing.T) {
	s := Store{}
	if _, _, ok := s.EnsureFor(t.TempDir(), "/x/y.unknown-ext"); ok {
		t.Fatal("unknown language must not synthesize a config")
	}
}

// TestArgvSynthesis verifies the launch command comes from the provider with
// the resolved interpreter, module form and args.
func TestArgvSynthesis(t *testing.T) {
	root := t.TempDir()
	cfg := Config{Name: "m", Kind: KindRun, Lang: "fake", File: "pkg/m.fake", Module: "pkg.m", Args: []string{"--flag"}}
	argv, ok := Argv(root, cfg, "")
	if !ok {
		t.Fatal("Argv must resolve through the provider")
	}
	want := []string{"/usr/bin/fake", "-m", "pkg.m", "--flag"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	}
	// Explicit interpreter wins over detection.
	argv, _ = Argv(root, cfg, "/custom/python")
	if argv[0] != "/custom/python" {
		t.Fatalf("explicit interpreter must win, got %v", argv)
	}
}

// TestConfigDirAndEnv verifies cwd resolution and env rendering.
func TestConfigDirAndEnv(t *testing.T) {
	root := "/proj"
	c := Config{Cwd: "sub"}
	if got := c.Dir(root); got != filepath.Join(root, "sub") {
		t.Fatalf("Dir = %q", got)
	}
	if got := (Config{}).Dir(root); got != root {
		t.Fatalf("empty cwd must resolve to root, got %q", got)
	}
	env := Config{Env: map[string]string{"B": "2", "A": "1"}}.EnvSlice()
	if len(env) != 2 || env[0] != "A=1" || env[1] != "B=2" {
		t.Fatalf("EnvSlice = %v", env)
	}
}

// TestUpsertNameCollision gives the second same-named file a path-based name.
func TestUpsertNameCollision(t *testing.T) {
	redirect(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := Store{}
	if _, _, ok := s.EnsureFor(root, filepath.Join(root, "main.fake")); !ok {
		t.Fatal("first EnsureFor failed")
	}
	cfg, created, ok := s.EnsureFor(root, filepath.Join(root, "sub", "main.fake"))
	if !ok || !created {
		t.Fatal("second EnsureFor failed")
	}
	if cfg.Name != filepath.Join("sub", "main.fake") {
		t.Fatalf("colliding name must fall back to the relative path, got %q", cfg.Name)
	}
}
