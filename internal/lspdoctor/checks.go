package lspdoctor

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ike/internal/lsp/client"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/protocol"
	"ike/internal/lsp/transport"
)

// checks.go runs the per-server probe chain (#2164). Every external effect is
// behind a Probes function so tests exercise the full chain with faked
// binaries and stderr, no real language server installed (the LSP testing
// rule, wiki/architecture/lsp.md).

// versionTimeout bounds the --version probe; a server that reads stdin
// forever instead of answering gets killed and reported as "no version".
const versionTimeout = 5 * time.Second

// initTimeout bounds the spawn + initialize round-trip, mirroring the
// manager's doubled request timeout for the handshake.
const initTimeout = 10 * time.Second

// SpawnResult is the spawn + initialize probe's evidence.
type SpawnResult struct {
	Err        string // spawn or initialize failure text; "" on success
	Stderr     string // decisive stderr line (transport.ErrorLine), when any
	ServerName string // initialize result's serverInfo.name, when it answered
}

// Probes are the doctor's external effects, injectable for tests.
type Probes struct {
	// LookPath resolves a command on the process PATH (exec.LookPath).
	LookPath func(string) (string, error)
	// FallbackDirs are the per-toolchain install dirs IKE's own launcher
	// probes after PATH (transport.FallbackDirs) — a hit here still works.
	FallbackDirs func() []string
	// WellKnownDirs are common install dirs IKE does NOT probe (Homebrew,
	// ~/.local/bin, …) — a hit here means the binary exists but the server
	// cannot start: the GUI-launch PATH gap (#1614).
	WellKnownDirs func() []string
	// Stat inspects a candidate file (os.Stat).
	Stat func(string) (os.FileInfo, error)
	// Shebang returns a script's interpreter line ("#!/usr/bin/env node"),
	// or "" for native binaries.
	Shebang func(path string) string
	// RunVersion executes `bin --version` with a timeout and returns the
	// combined output.
	RunVersion func(bin string) (string, error)
	// RuntimeVersion reports an interpreter's version (`node --version`),
	// or "" when the interpreter itself is missing.
	RuntimeVersion func(interpreter string) string
	// SpawnInit spawns the server and drives a real initialize round-trip.
	SpawnInit func(srv Server) SpawnResult
	// PATH is the process PATH env, quoted in PATH-gap evidence.
	PATH func() string
}

// RealProbes returns the production probe set.
func RealProbes() Probes {
	return Probes{
		LookPath:     exec.LookPath,
		FallbackDirs: transport.FallbackDirs,
		WellKnownDirs: func() []string {
			dirs := []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"}
			if home, err := os.UserHomeDir(); err == nil {
				dirs = append(dirs,
					filepath.Join(home, ".local", "bin"),
					filepath.Join(home, ".cargo", "bin"),
					filepath.Join(home, "bin"))
			}
			return dirs
		},
		Stat:    os.Stat,
		Shebang: readShebang,
		RunVersion: func(bin string) (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
			defer cancel()
			out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
			return strings.TrimSpace(string(out)), err
		},
		RuntimeVersion: func(interpreter string) string {
			ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
			defer cancel()
			out, err := exec.CommandContext(ctx, interpreter, "--version").CombinedOutput()
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(out))
		},
		SpawnInit: spawnInit,
		PATH:      func() string { return os.Getenv("PATH") },
	}
}

// readShebang returns a file's "#!" line, or "" when it is a native binary
// (or unreadable).
func readShebang(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 160)
	n, _ := f.Read(buf)
	head := string(buf[:n])
	if !strings.HasPrefix(head, "#!") {
		return ""
	}
	if i := strings.IndexByte(head, '\n'); i >= 0 {
		head = head[:i]
	}
	return strings.TrimSpace(head)
}

// spawnInit is the production spawn probe: start the server exactly like the
// manager does (transport.Start resolves PATH + fallback dirs) and drive a
// real initialize round-trip against the workspace root, then tear it down.
func spawnInit(srv Server) SpawnResult {
	proc, err := transport.Start(transport.Spec{
		Command: srv.Command,
		Args:    srv.Args,
		Env:     srv.Env,
		Dir:     srv.Root,
	})
	if err != nil {
		return SpawnResult{Err: err.Error()}
	}
	// Servers may issue requests (workspace/configuration) during the
	// handshake; answer null so the probe never wedges them. The holder is
	// mutex-guarded because the read loop starts inside NewConn.
	var mu sync.Mutex
	var conn *jsonrpc.Conn
	handler := jsonrpc.Handler{
		Request: func(id jsonrpc.ID, method string, params json.RawMessage) {
			mu.Lock()
			c := conn
			mu.Unlock()
			if c != nil {
				_ = c.Respond(id, nil, nil)
			}
		},
	}
	mu.Lock()
	conn = jsonrpc.NewConn(proc.Conn(), handler)
	mu.Unlock()
	defer func() {
		_ = conn.Close()
		_ = proc.Stop()
	}()
	cl := client.New(conn)
	ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
	defer cancel()
	res, err := cl.Initialize(ctx, client.InitParams{
		RootURI:   protocol.PathToURI(srv.Root),
		ProcessID: os.Getpid(),
	})
	if err != nil {
		return SpawnResult{Err: err.Error(), Stderr: transport.ErrorLine(proc.Stderr())}
	}
	name := ""
	if res.ServerInfo != nil {
		name = res.ServerInfo.Name
	}
	return SpawnResult{ServerName: name}
}

// evidence is everything the check chain captured for one server; classify
// maps it to a diagnosis.
type evidence struct {
	srv            Server
	path           string // resolved path ("" = nowhere)
	onPath         bool   // resolved via PATH
	fallbackDir    string // hit in an IKE-probed fallback dir (still works)
	strandedDir    string // hit only in a well-known dir IKE does not probe
	otherCopies    []string// further copies of the binary in probed dirs
	envPATH        string
	notExecutable  bool
	shebang        string // script interpreter line, "" for native binaries
	runtime        string // interpreter binary ("node"), from the shebang
	runtimeVersion string // interpreter --version output, "" when missing
	versionOut     string
	versionErr     string
	rootErr        string
	spawn          SpawnResult
	spawnRan       bool
}

// Run executes the check chain for every server concurrently and returns the
// results in input order.
func Run(servers []Server, p Probes) []Result {
	out := make([]Result, len(servers))
	var wg sync.WaitGroup
	for i := range servers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out[i] = runOne(servers[i], p)
		}(i)
	}
	wg.Wait()
	return out
}

// runOne gathers one server's evidence and classifies it.
func runOne(srv Server, p Probes) Result {
	ev := evidence{srv: srv, envPATH: p.PATH()}

	// 1. Binary resolution: PATH, then IKE's fallback dirs, then the
	// well-known dirs IKE does not probe (the PATH-gap detector, #1614).
	if path, err := p.LookPath(srv.Command); err == nil {
		ev.path, ev.onPath = path, true
	} else {
		for _, dir := range p.FallbackDirs() {
			if hit := executableAt(p, dir, srv.Command); hit != "" {
				ev.path, ev.fallbackDir = hit, dir
				break
			}
		}
		if ev.path == "" {
			for _, dir := range p.WellKnownDirs() {
				if hit := executableAt(p, dir, srv.Command); hit != "" {
					ev.path, ev.strandedDir = hit, dir
					break
				}
			}
		}
	}
	// Other copies in probed dirs: a failing PATH binary may shadow a
	// working install (the TOML/taplo case — Homebrew's taplo without the
	// LSP feature shadows the npm one).
	if ev.path != "" {
		for _, dir := range append(p.FallbackDirs(), p.WellKnownDirs()...) {
			if hit := executableAt(p, dir, srv.Command); hit != "" && hit != ev.path {
				ev.otherCopies = append(ev.otherCopies, hit)
			}
		}
	}

	// 2. File sanity: exists but not executable?
	if ev.path == "" {
		if info, err := p.Stat(srv.Command); err == nil && !info.IsDir() && info.Mode()&0o111 == 0 {
			ev.path, ev.notExecutable = srv.Command, true
		}
	} else if info, err := p.Stat(ev.path); err == nil && info.Mode()&0o111 == 0 {
		ev.notExecutable = true
	}

	// 3. Runtime: a node script's health depends on the node on PATH.
	if ev.path != "" && !ev.notExecutable {
		ev.shebang = p.Shebang(ev.path)
		if strings.Contains(ev.shebang, "node") {
			ev.runtime = "node"
			ev.runtimeVersion = p.RuntimeVersion("node")
		}
	}

	// 4. Version probe (evidence only — some servers ship without --version).
	if ev.path != "" && !ev.notExecutable {
		out, err := p.RunVersion(ev.path)
		ev.versionOut = out
		if err != nil {
			ev.versionErr = err.Error()
		}
	}

	// 5. Workspace root sanity.
	if srv.Root != "" {
		if info, err := p.Stat(srv.Root); err != nil || !info.IsDir() {
			ev.rootErr = srv.Root + " is not a readable directory"
		}
	}

	// 6. Spawn + initialize round-trip — the decisive check, skipped when
	// the binary cannot run at all.
	if ev.path != "" && !ev.notExecutable && ev.rootErr == "" {
		ev.spawn = p.SpawnInit(srv)
		ev.spawnRan = true
	}

	return classify(ev)
}

// executableAt returns dir/command when it exists there as an executable
// regular file.
func executableAt(p Probes, dir, command string) string {
	cand := filepath.Join(dir, command)
	if info, err := p.Stat(cand); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		return cand
	}
	return ""
}
