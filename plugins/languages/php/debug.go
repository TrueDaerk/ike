package langphp

import (
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"ike/internal/config"
	"ike/internal/dbgp/bridge"
	"ike/internal/lang"
)

// Debug contribution (0360, #701): PHP debugs through Xdebug, whose DBGp
// protocol is spoken by the in-process bridge (internal/dbgp/bridge) — no
// external adapter process, no Node. The bridge serves DAP on an in-memory
// pipe; Xdebug is pointed back at it per run via -d ini overrides, so the
// user's php.ini stays untouched.

var (
	_ lang.DebugAdapterProvider  = toolchain{}
	_ lang.DebugAdapterInProcess = toolchain{}
	_ lang.DebugAdapterInstaller = toolchain{}
)

// phpRun is a seam for tests: it runs the interpreter and returns combined
// output (used by the Xdebug probe and the version lookup).
var phpRun = func(interpreter string, args ...string) (string, error) {
	out, err := exec.Command(interpreter, args...).CombinedOutput()
	return string(out), err
}

// DebugAdapterConnect implements lang.DebugAdapterInProcess: start a DBGp
// bridge for the resolved interpreter and hand back its DAP end.
func (toolchain) DebugAdapterConnect(_ string, interpreter string) (io.ReadWriteCloser, error) {
	if interpreter == "" {
		interpreter = "php"
	}
	return bridge.New(interpreter), nil
}

// DebugAdapter implements lang.DebugAdapterProvider. PHP has no adapter
// process to spawn — the in-process connect above is always preferred — so
// the argv form reports unavailable.
func (toolchain) DebugAdapter(string, string) ([]string, bool) { return nil, false }

// DebugLaunchArgs implements lang.DebugAdapterProvider: the bridge's launch
// vocabulary is plain program/args/cwd/env (see bridge.launchArgs). A listen
// spec (#823) instead opens the bridge's persistent DBGp listener for
// php-fpm/Apache requests, parameterized from [debug.php]: port, hostname
// filter, and path mappings (Local resolved against the project root).
func (toolchain) DebugLaunchArgs(root string, spec lang.RunSpec, cwd string, env map[string]string) map[string]any {
	if spec.Listen {
		args := map[string]any{"request": "launch", "mode": "listen"}
		d := config.Get()
		if d == nil {
			return args
		}
		if d.Debug.PHP.Port > 0 {
			args["port"] = d.Debug.PHP.Port
		}
		if h := strings.TrimSpace(d.Debug.PHP.Hostname); h != "" {
			args["hostname"] = h
		}
		if len(d.Debug.PHP.PathMappings) > 0 {
			maps := make([]map[string]string, 0, len(d.Debug.PHP.PathMappings))
			for _, pm := range d.Debug.PHP.PathMappings {
				if pm.Server == "" || pm.Local == "" {
					continue
				}
				local := pm.Local
				if !filepath.IsAbs(local) {
					local = filepath.Join(root, local)
				}
				maps = append(maps, map[string]string{"server": pm.Server, "local": local})
			}
			if len(maps) > 0 {
				args["pathMappings"] = maps
			}
		}
		return args
	}
	args := map[string]any{
		"request": "launch",
		"program": spec.File,
		"cwd":     cwd,
	}
	if len(spec.Args) > 0 {
		args["args"] = spec.Args
	}
	if len(env) > 0 {
		args["env"] = env
	}
	return args
}

// DebugAdapterMissing implements lang.DebugAdapterInstaller (#589 flow): the
// adapter runtime is the Xdebug extension inside the resolved interpreter —
// `php -m` decides.
func (toolchain) DebugAdapterMissing(_ string, interpreter string) (bool, string) {
	if interpreter == "" {
		interpreter = "php"
	}
	out, err := phpRun(interpreter, "-m")
	if err != nil {
		return true, "cannot probe " + interpreter + " for Xdebug"
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.EqualFold(strings.TrimSpace(line), "xdebug") {
			return false, ""
		}
	}
	return true, "the Xdebug extension is not loaded in " + interpreter
}

// DebugAdapterInstall implements lang.DebugAdapterInstaller. Candidates run
// in order until one succeeds (tools absent from PATH are skipped by the
// runner):
//  1. pecl — builds and registers the extension where PECL and a build
//     toolchain exist.
//  2. Homebrew's precompiled extension formula matching the interpreter's
//     PHP version (shivammathur/extensions), for Homebrew-installed PHPs.
//
// When both fail the manager surfaces the last candidate as the manual
// instruction; loading still needs `zend_extension=xdebug` in the ini when
// the installer did not register it.
func (toolchain) DebugAdapterInstall(_ string, interpreter string) [][]string {
	if interpreter == "" {
		interpreter = "php"
	}
	candidates := [][]string{{"pecl", "install", "xdebug"}}
	if v := phpMinorVersion(interpreter); v != "" {
		candidates = append(candidates, []string{"brew", "install", "shivammathur/extensions/xdebug@" + v})
	}
	return candidates
}

// phpMinorVersion returns the interpreter's "major.minor" ("8.3"), or ""
// when it cannot be determined.
func phpMinorVersion(interpreter string) string {
	out, err := phpRun(interpreter, "-r", "echo PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION;")
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(out)
	if len(v) < 3 || strings.ContainsAny(v, " \n") {
		return ""
	}
	return v
}
