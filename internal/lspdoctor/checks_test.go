package lspdoctor

// checks_test.go guards the check chain and the signature→diagnosis mapping
// (#2164) with faked binaries and stderr — no real language server installed,
// per the LSP testing rule. The scenarios cover every acceptance class:
// binary missing, PATH-of-GUI-process mismatch, node runtime mismatch, and
// crash-on-initialize with stderr evidence (the TOML/taplo case).

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeInfo is a minimal fs.FileInfo with a controllable mode.
type fakeInfo struct {
	name string
	mode fs.FileMode
	dir  bool
}

func (f fakeInfo) Name() string       { return f.name }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.dir }
func (f fakeInfo) Sys() any           { return nil }

// fakeProbes returns a probe set where nothing exists and nothing runs;
// tests override the pieces their scenario needs.
func fakeProbes() Probes {
	return Probes{
		LookPath:       func(string) (string, error) { return "", errors.New("not found") },
		FallbackDirs:   func() []string { return nil },
		WellKnownDirs:  func() []string { return nil },
		Stat:           func(string) (os.FileInfo, error) { return nil, errors.New("no file") },
		Shebang:        func(string) string { return "" },
		RunVersion:     func(string) (string, error) { return "", errors.New("not run") },
		RuntimeVersion: func(string) string { return "" },
		SpawnInit:      func(Server) SpawnResult { return SpawnResult{Err: "not run"} },
		PATH:           func() string { return "/usr/bin" },
	}
}

func one(t *testing.T, srv Server, p Probes) Result {
	t.Helper()
	res := Run([]Server{srv}, p)
	if len(res) != 1 {
		t.Fatalf("Run returned %d results", len(res))
	}
	return res[0]
}

// TestClassifyBinaryMissing: nowhere to be found → ClassMissing with the
// spec's install recipe as the fix.
func TestClassifyBinaryMissing(t *testing.T) {
	srv := Server{Lang: "go", Command: "gopls", Install: []string{"go", "install", "golang.org/x/tools/gopls@latest"}}
	res := one(t, srv, fakeProbes())
	if res.Class != ClassMissing {
		t.Fatalf("class = %q want %q", res.Class, ClassMissing)
	}
	if !strings.Contains(res.Fix, "go install golang.org/x/tools/gopls@latest") {
		t.Fatalf("fix must carry the install recipe: %q", res.Fix)
	}
}

// TestClassifyPathMismatch: the binary exists only in a well-known dir IKE
// does not probe → the GUI-PATH-gap diagnosis (#1614) with the dir named.
func TestClassifyPathMismatch(t *testing.T) {
	p := fakeProbes()
	p.WellKnownDirs = func() []string { return []string{"/opt/homebrew/bin"} }
	p.Stat = func(path string) (os.FileInfo, error) {
		if path == filepath.Join("/opt/homebrew/bin", "taplo") {
			return fakeInfo{name: "taplo", mode: 0o755}, nil
		}
		return nil, errors.New("no file")
	}
	res := one(t, Server{Lang: "toml", Command: "taplo"}, p)
	if res.Class != ClassPathMismatch {
		t.Fatalf("class = %q want %q", res.Class, ClassPathMismatch)
	}
	if !strings.Contains(res.Diagnosis, "/opt/homebrew/bin") || !strings.Contains(res.Diagnosis, "GUI") {
		t.Fatalf("diagnosis must name the stranded dir and the GUI-PATH gap: %q", res.Diagnosis)
	}
	if !strings.Contains(res.Fix, "[lsp.servers.toml] command") {
		t.Fatalf("fix must offer the config override: %q", res.Fix)
	}
}

// TestClassifyFallbackDirStillWorks: a hit in an IKE-probed toolchain dir is
// a warning, not a failure — transport.Resolve launches it anyway.
func TestClassifyFallbackDirStillWorks(t *testing.T) {
	p := fakeProbes()
	p.FallbackDirs = func() []string { return []string{"/home/u/go/bin"} }
	p.Stat = func(path string) (os.FileInfo, error) {
		switch path {
		case filepath.Join("/home/u/go/bin", "gopls"), "/root":
			return fakeInfo{mode: 0o755, dir: path == "/root"}, nil
		}
		return nil, errors.New("no file")
	}
	p.RunVersion = func(string) (string, error) { return "golang.org/x/tools/gopls v0.16.0", nil }
	p.SpawnInit = func(Server) SpawnResult { return SpawnResult{ServerName: "gopls"} }
	res := one(t, Server{Lang: "go", Command: "gopls", Root: "/root"}, p)
	if res.Class != ClassOK {
		t.Fatalf("class = %q want ok (diagnosis %q)", res.Class, res.Diagnosis)
	}
	found := false
	for _, c := range res.Checks {
		if c.Name == "binary" && c.Status == StatusWarn && strings.Contains(c.Detail, "/home/u/go/bin") {
			found = true
		}
	}
	if !found {
		t.Fatalf("binary check must warn about the off-PATH dir: %+v", res.Checks)
	}
}

// TestClassifyNotExecutable: exists, exec bit missing → chmod fix.
func TestClassifyNotExecutable(t *testing.T) {
	p := fakeProbes()
	p.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	p.Stat = func(path string) (os.FileInfo, error) {
		if path == "/srv/bin/pyright-langserver" {
			return fakeInfo{mode: 0o644}, nil
		}
		return nil, errors.New("no file")
	}
	res := one(t, Server{Lang: "python", Command: "/srv/bin/pyright-langserver"}, p)
	if res.Class != ClassNotExecutable {
		t.Fatalf("class = %q want %q", res.Class, ClassNotExecutable)
	}
	if !strings.Contains(res.Fix, "chmod +x /srv/bin/pyright-langserver") {
		t.Fatalf("fix = %q", res.Fix)
	}
}

// TestClassifyArchMismatch: spawn dies with an exec-format complaint → the
// Rosetta/architecture diagnosis.
func TestClassifyArchMismatch(t *testing.T) {
	p := fakeProbes()
	p.LookPath = func(string) (string, error) { return "/usr/local/bin/gopls", nil }
	p.Stat = func(string) (os.FileInfo, error) { return fakeInfo{mode: 0o755}, nil }
	p.RunVersion = func(string) (string, error) { return "", errors.New("fork/exec: exec format error") }
	p.SpawnInit = func(Server) SpawnResult { return SpawnResult{Err: "fork/exec /usr/local/bin/gopls: exec format error"} }
	res := one(t, Server{Lang: "go", Command: "gopls", Install: []string{"go", "install", "gopls@latest"}}, p)
	if res.Class != ClassArchMismatch {
		t.Fatalf("class = %q want %q", res.Class, ClassArchMismatch)
	}
	if !strings.Contains(res.Diagnosis, "architecture") {
		t.Fatalf("diagnosis = %q", res.Diagnosis)
	}
}

// TestClassifyNodeRuntimeMissing: a node-script server whose node is not on
// IKE's PATH → runtime mismatch before any spawn attempt.
func TestClassifyNodeRuntimeMissing(t *testing.T) {
	p := fakeProbes()
	p.LookPath = func(string) (string, error) { return "/usr/local/bin/typescript-language-server", nil }
	p.Stat = func(string) (os.FileInfo, error) { return fakeInfo{mode: 0o755}, nil }
	p.Shebang = func(string) string { return "#!/usr/bin/env node" }
	p.RuntimeVersion = func(string) string { return "" }
	res := one(t, Server{Lang: "typescript", Command: "typescript-language-server"}, p)
	if res.Class != ClassRuntimeMismatch {
		t.Fatalf("class = %q want %q", res.Class, ClassRuntimeMismatch)
	}
	if !strings.Contains(res.Diagnosis, "node") {
		t.Fatalf("diagnosis = %q", res.Diagnosis)
	}
}

// TestClassifyNodeRuntimeTooOld: the spawn's stderr carries an engine
// complaint → runtime mismatch naming the node version IKE sees.
func TestClassifyNodeRuntimeTooOld(t *testing.T) {
	p := fakeProbes()
	p.LookPath = func(string) (string, error) { return "/usr/local/bin/typescript-language-server", nil }
	p.Stat = func(string) (os.FileInfo, error) { return fakeInfo{mode: 0o755}, nil }
	p.Shebang = func(string) string { return "#!/usr/bin/env node" }
	p.RuntimeVersion = func(string) string { return "v14.2.0" }
	p.RunVersion = func(string) (string, error) { return "", errors.New("exit status 1") }
	p.SpawnInit = func(Server) SpawnResult {
		return SpawnResult{Err: "jsonrpc: connection closed", Stderr: "error: Unsupported engine: wanted node >=18"}
	}
	res := one(t, Server{Lang: "typescript", Command: "typescript-language-server", Install: []string{"npm", "install", "-g", "typescript-language-server"}}, p)
	if res.Class != ClassRuntimeMismatch {
		t.Fatalf("class = %q want %q (diagnosis %q)", res.Class, ClassRuntimeMismatch, res.Diagnosis)
	}
	if !strings.Contains(res.Diagnosis, "v14.2.0") {
		t.Fatalf("diagnosis must name the node version: %q", res.Diagnosis)
	}
	if !strings.Contains(res.Fix, "upgrade node") {
		t.Fatalf("fix = %q", res.Fix)
	}
}

// TestClassifyCrashOnInitializeTaplo: the motivating TOML case — the PATH
// taplo crashes ("not part of this build") while the npm install the old hint
// suggested already exists in a fallback dir. The doctor must produce the
// better diagnosis: LSP-capable build advice plus the shadowed-copy pointer,
// instead of repeating the install hint.
func TestClassifyCrashOnInitializeTaplo(t *testing.T) {
	p := fakeProbes()
	p.LookPath = func(string) (string, error) { return "/opt/homebrew/bin/taplo", nil }
	p.FallbackDirs = func() []string { return []string{"/home/u/.npm-global/bin"} }
	p.Stat = func(path string) (os.FileInfo, error) {
		switch path {
		case filepath.Join("/home/u/.npm-global/bin", "taplo"), "/opt/homebrew/bin/taplo":
			return fakeInfo{mode: 0o755}, nil
		}
		return nil, errors.New("no file")
	}
	p.RunVersion = func(string) (string, error) { return "taplo 0.9.0", nil }
	p.SpawnInit = func(Server) SpawnResult {
		return SpawnResult{
			Err:    "jsonrpc: connection closed",
			Stderr: "ERROR operation failed error=the LSP is not part of this build, please consult the documentation",
		}
	}
	res := one(t, Server{Lang: "toml", Command: "taplo", Install: []string{"npm", "install", "-g", "@taplo/cli"}}, p)
	if res.Class != ClassCrashInit {
		t.Fatalf("class = %q want %q", res.Class, ClassCrashInit)
	}
	if !strings.Contains(res.Diagnosis, "not part of this build") {
		t.Fatalf("diagnosis must carry the stderr evidence: %q", res.Diagnosis)
	}
	if !strings.Contains(res.Fix, "npm install -g @taplo/cli") ||
		!strings.Contains(res.Fix, "/home/u/.npm-global/bin/taplo") {
		t.Fatalf("fix must give the LSP-capable-build advice and the shadowed copy: %q", res.Fix)
	}
}

// TestClassifyBadRoot: an unusable workspace root fails before any spawn.
func TestClassifyBadRoot(t *testing.T) {
	p := fakeProbes()
	p.LookPath = func(string) (string, error) { return "/usr/bin/gopls", nil }
	p.Stat = func(path string) (os.FileInfo, error) {
		if path == "/usr/bin/gopls" {
			return fakeInfo{mode: 0o755}, nil
		}
		return nil, errors.New("no file")
	}
	p.RunVersion = func(string) (string, error) { return "gopls v0.16.0", nil }
	res := one(t, Server{Lang: "go", Command: "gopls", Root: "/gone"}, p)
	if res.Class != ClassBadRoot {
		t.Fatalf("class = %q want %q", res.Class, ClassBadRoot)
	}
}

// TestRunWithFakeBinaryCrash exercises the real probe chain end-to-end with a
// faked server binary that dies with stderr — RealProbes against a scripted
// executable, no language server installed.
func TestRunWithFakeBinaryCrash(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fakelsp")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo fakelsp 1.0; exit 0; fi\necho 'fatal: fakelsp cannot start, config invalid' >&2\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	root := t.TempDir()
	res := one(t, Server{Lang: "fake", Command: "fakelsp", Root: root}, RealProbes())
	if res.Class != ClassCrashInit {
		t.Fatalf("class = %q want %q (diagnosis %q)", res.Class, ClassCrashInit, res.Diagnosis)
	}
	if !strings.Contains(res.Diagnosis, "fakelsp cannot start") {
		t.Fatalf("diagnosis must carry the captured stderr: %q", res.Diagnosis)
	}
}
