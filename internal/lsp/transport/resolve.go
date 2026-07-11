package transport

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// resolve.go locates a server binary beyond exec.LookPath (#370). Install
// recipes like `go install …` drop binaries into per-toolchain directories
// (go's GOBIN, npm's global bin) that are typically not on PATH in a plain
// terminal session, so a freshly installed server would stay "not found"
// forever. Resolve probes those well-known locations after PATH fails and
// returns an absolute path the process can be launched with directly.

// envOutput runs a toolchain query (`go env GOBIN`, `npm prefix -g`) and
// returns its trimmed stdout. Injectable for tests.
var envOutput = func(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return strings.TrimSpace(string(out)), err
}

// Resolve returns an absolute path for command: PATH first, then the
// well-known per-toolchain install directories (FallbackDirs). It returns the
// LookPath error unchanged when the command is nowhere to be found, so
// callers keep their errors.Is semantics.
func Resolve(command string) (string, error) {
	p, err := exec.LookPath(command)
	if err == nil {
		return p, nil
	}
	for _, dir := range FallbackDirs() {
		cand := filepath.Join(dir, command)
		if info, serr := os.Stat(cand); serr == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return cand, nil
		}
	}
	return "", err
}

// FallbackDirs lists the per-toolchain install directories install recipes
// drop binaries into, in probe order. Toolchains that are themselves missing
// contribute nothing (their recipe could not have run either).
func FallbackDirs() []string {
	var dirs []string
	add := func(dir string) {
		if dir == "" {
			return
		}
		for _, d := range dirs {
			if d == dir {
				return
			}
		}
		dirs = append(dirs, dir)
	}
	// go install target: GOBIN, else GOPATH/bin (default ~/go/bin).
	if _, err := exec.LookPath("go"); err == nil {
		if gobin, err := envOutput("go", "env", "GOBIN"); err == nil && gobin != "" {
			add(gobin)
		} else if gopath, err := envOutput("go", "env", "GOPATH"); err == nil && gopath != "" {
			// GOPATH may hold a list; the first entry is the install target.
			add(filepath.Join(filepath.SplitList(gopath)[0], "bin"))
		}
	} else if home, err := os.UserHomeDir(); err == nil {
		// go missing from PATH too, but a previous install may still sit in
		// the default GOPATH.
		add(filepath.Join(home, "go", "bin"))
	}
	// npm install -g target: <prefix>/bin (the prefix itself on Windows).
	if _, err := exec.LookPath("npm"); err == nil {
		if prefix, err := envOutput("npm", "prefix", "-g"); err == nil && prefix != "" {
			add(filepath.Join(prefix, "bin"))
			add(prefix)
		}
	}
	return dirs
}
