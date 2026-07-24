// Package run holds run configurations (0350, #575): named, persisted
// descriptions of how to run (or debug) a file, JetBrains-style. A
// configuration is data — file, module form, args, env, cwd — and the actual
// command line is synthesized at launch through the language registry's
// RunCommandProvider seam, so interpreter changes apply to every later run.
//
// Persistence lives per project in .ike/runconfigs.json (IKE_CONFIG_DIR
// overrides the base directory, exactly like session.json and layout.json).
package run

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ike/internal/lang"
)

// Kind tells a plain run from a debug launch; both share one configuration
// shape so a debug reuses the run's data (Epic #572 design rule).
type Kind string

const (
	KindRun   Kind = "run"
	KindDebug Kind = "debug"
)

// Config is one run configuration. File and Cwd are stored project-relative
// so the file travels with the repository; "" Cwd means the project root.
type Config struct {
	Name   string            `json:"name"`
	Kind   Kind              `json:"kind"`
	Lang   string            `json:"lang"`
	File   string            `json:"file"`
	Module string            `json:"module,omitempty"`
	Args   []string          `json:"args,omitempty"`
	Env    map[string]string `json:"env,omitempty"`
	Cwd    string            `json:"cwd,omitempty"`
	// Listen marks a listen-style debug configuration (#823): no process is
	// launched, the adapter waits for incoming connections (PHP/Xdebug web
	// debugging). File is empty then.
	Listen bool `json:"listen,omitempty"`
	// Tests marks a test-scope configuration (#1150): the argv is synthesized
	// through the language's TestSpec instead of its RunCommandProvider.
	// TestName/TestKind select exactly one test function; an empty TestName
	// runs the file's whole test scope (Go: its package). Cwd holds the test
	// file's directory so `go test` targets the right package.
	Tests    bool   `json:"tests,omitempty"`
	TestName string `json:"test_name,omitempty"`
	TestKind string `json:"test_kind,omitempty"`
}

// Store is the persisted set of configurations plus the last-used name (the
// rerun-last target).
type Store struct {
	Configs  []Config `json:"configs"`
	LastUsed string   `json:"last_used,omitempty"`
}

// File returns the path of the per-project run-configuration store, honoring
// the IKE_CONFIG_DIR override like the session and layout stores.
func File() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "runconfigs.json")
	}
	return filepath.Join(".ike", "runconfigs.json")
}

// Load reads the store; any missing or malformed file yields an empty store —
// run configurations are convenience state, never a startup error.
func Load() Store {
	data, err := os.ReadFile(File())
	if err != nil {
		return Store{}
	}
	var s Store
	if json.Unmarshal(data, &s) != nil {
		return Store{}
	}
	return s
}

// Save persists the store; errors are returned for the caller to surface as
// a notification (a failed save must not disrupt the run itself).
func Save(s Store) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := File()
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// ByName returns the configuration named name, or nil.
func (s *Store) ByName(name string) *Config {
	for i := range s.Configs {
		if s.Configs[i].Name == name {
			return &s.Configs[i]
		}
	}
	return nil
}

// ForFile returns the first configuration targeting the project-relative
// file, or nil.
func (s *Store) ForFile(file string) *Config {
	for i := range s.Configs {
		if s.Configs[i].File == file {
			return &s.Configs[i]
		}
	}
	return nil
}

// Upsert adds cfg or replaces the configuration with the same name, and
// returns the stored copy.
func (s *Store) Upsert(cfg Config) *Config {
	if existing := s.ByName(cfg.Name); existing != nil {
		*existing = cfg
		return existing
	}
	s.Configs = append(s.Configs, cfg)
	return &s.Configs[len(s.Configs)-1]
}

// Touch marks name as the last-used configuration (the rerun target).
func (s *Store) Touch(name string) { s.LastUsed = name }

// Last returns the last-used configuration, or nil.
func (s *Store) Last() *Config { return s.ByName(s.LastUsed) }

// Names lists the configuration names, sorted (pickers, tests).
func (s *Store) Names() []string {
	out := make([]string, 0, len(s.Configs))
	for _, c := range s.Configs {
		out = append(out, c.Name)
	}
	sort.Strings(out)
	return out
}

// Default synthesizes the default configuration for the absolute file at
// root (0350): kind run, no env, cwd = project root, the language's module
// form when the file lies in a package (Python `-m`), a unique name from the
// file's base name. ok=false when no registered language claims the file.
func Default(root, file string) (Config, bool) {
	l, found := lang.ByPath(file)
	if !found {
		return Config{}, false
	}
	rel := relTo(root, file)
	cfg := Config{
		Name:   filepath.Base(file),
		Kind:   KindRun,
		Lang:   l.ID,
		File:   rel,
		Module: lang.ModuleFor(l.ID, root, file),
	}
	return cfg, true
}

// TestConfig synthesizes the test-scope configuration for the absolute file
// at root (#1150): one test function when t is non-nil, the file's whole test
// scope otherwise. Cwd is the file's directory (Go: `go test` targets the
// package). The name is stable per target — "TestX (pkg/dir)" or
// "tests: pkg/dir" — so Upsert folds repeated runs of the same test into one
// configuration. ok=false when no registered language claims the file or its
// language declares no test runner.
func TestConfig(root, file string, t *lang.TestMatch) (Config, bool) {
	l, found := lang.ByPath(file)
	if !found || !lang.HasTests(file) {
		return Config{}, false
	}
	rel := relTo(root, file)
	scope := filepath.ToSlash(filepath.Dir(rel))
	cwd := filepath.Dir(rel)
	if cwd == "." {
		cwd = ""
	}
	cfg := Config{
		Kind:  KindRun,
		Lang:  l.ID,
		File:  rel,
		Cwd:   cwd,
		Tests: true,
	}
	if t != nil {
		cfg.TestName, cfg.TestKind = t.Name, t.Kind
		cfg.Name = t.Name + " (" + scope + ")"
	} else {
		cfg.Name = "tests: " + scope
	}
	return cfg, true
}

// EnsureFor returns the store's configuration for the absolute file,
// creating and persisting the default one on first run (created=true then).
// A failed persist still returns the in-memory configuration — the run must
// not fail because .ike is unwritable.
func (s *Store) EnsureFor(root, file string) (cfg *Config, created bool, ok bool) {
	rel := relTo(root, file)
	if existing := s.ForFile(rel); existing != nil {
		return existing, false, true
	}
	def, found := Default(root, file)
	if !found {
		return nil, false, false
	}
	// Keep names unique: a second file with the same base name gets its
	// relative path as the name.
	if s.ByName(def.Name) != nil {
		def.Name = def.File
	}
	return s.Upsert(def), true, true
}

// Argv synthesizes the command line for cfg at root through the language's
// RunCommandProvider — or, for a test-scope configuration (#1150), through
// its TestSpec templates; explicit is the user's configured interpreter for
// the language ("" when none). ok=false when the language contributes no run
// command.
func Argv(root string, cfg Config, explicit string) ([]string, bool) {
	if cfg.Tests {
		file := absTo(root, cfg.File)
		if cfg.TestName == "" {
			return lang.TestFileArgv(root, file, explicit)
		}
		return lang.TestArgv(root, file, lang.TestMatch{Name: cfg.TestName, Kind: cfg.TestKind}, explicit)
	}
	spec := lang.RunSpec{
		File:   absTo(root, cfg.File),
		Module: cfg.Module,
		Args:   cfg.Args,
	}
	return lang.RunArgv(cfg.Lang, root, spec, explicit)
}

// Dir resolves cfg's working directory against root ("" = root).
func (c Config) Dir(root string) string {
	if c.Cwd == "" {
		return root
	}
	return absTo(root, c.Cwd)
}

// EnvSlice renders the env map as KEY=VALUE pairs (sorted, deterministic).
func (c Config) EnvSlice() []string {
	if len(c.Env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(c.Env))
	for k := range c.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+c.Env[k])
	}
	return out
}

// relTo stores paths project-relative when possible, absolute otherwise.
func relTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

// absTo resolves a stored (possibly relative) path against root.
func absTo(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}
