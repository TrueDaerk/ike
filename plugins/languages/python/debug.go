package langpython

import (
	"os/exec"

	"ike/internal/lang"
)

// Debug-adapter contribution (0350, #578): Python debugs through debugpy's
// DAP adapter (`python -m debugpy.adapter` on stdio), launched with the same
// resolved interpreter run commands use — so breakpoints hit inside the
// project's venv, not a global python.

var (
	_ lang.DebugAdapterProvider  = toolchain{}
	_ lang.DebugAdapterInstaller = toolchain{}
)

// DebugAdapter implements lang.DebugAdapterProvider. debugpy must be
// installed in the interpreter's environment; a missing module surfaces as
// the adapter process failing to start, which the session reports.
func (toolchain) DebugAdapter(_ string, interpreter string) ([]string, bool) {
	if interpreter == "" {
		interpreter = "python3"
	}
	return []string{interpreter, "-m", "debugpy.adapter"}, true
}

// DebugAdapterMissing implements lang.DebugAdapterInstaller (#589): the
// adapter runtime is the debugpy package inside the resolved interpreter's
// environment — a plain import probe decides.
func (toolchain) DebugAdapterMissing(_ string, interpreter string) (bool, string) {
	if interpreter == "" {
		interpreter = "python3"
	}
	if err := exec.Command(interpreter, "-c", "import debugpy").Run(); err != nil {
		return true, "debugpy is not installed in " + interpreter
	}
	return false, ""
}

// DebugAdapterInstall implements lang.DebugAdapterInstaller. Candidates run in
// order until one succeeds:
//  1. pip inside the interpreter — works in a virtualenv that ships pip.
//  2. uv targeting the interpreter — for uv-created venvs, which have no pip.
//  3. pip with --break-system-packages — for an externally-managed interpreter
//     (PEP 668: Homebrew/system python) where a plain install is refused.
//  4. uv with --break-system-packages — for a uv-managed standalone python,
//     which is both pip-less and marked externally managed.
//
// The last two override the environment guard on purpose: when a project has no
// virtualenv, the detected interpreter is the only environment the debug
// adapter can run in, and debugpy is a developer tool. Without these fallbacks
// auto-install fails outright for every non-venv interpreter (#589).
func (toolchain) DebugAdapterInstall(_ string, interpreter string) [][]string {
	if interpreter == "" {
		interpreter = "python3"
	}
	return [][]string{
		{interpreter, "-m", "pip", "install", "debugpy"},
		{"uv", "pip", "install", "--python", interpreter, "debugpy"},
		{interpreter, "-m", "pip", "install", "--break-system-packages", "debugpy"},
		{"uv", "pip", "install", "--break-system-packages", "--python", interpreter, "debugpy"},
	}
}

// DebugLaunchArgs implements lang.DebugAdapterProvider: module form when the
// spec carries one (mirroring `-m` runs), else program form. Output is
// redirected through DAP output events so the debug UI owns it.
func (toolchain) DebugLaunchArgs(_ string, spec lang.RunSpec, cwd string, env map[string]string) map[string]any {
	args := map[string]any{
		"request": "launch",
		// integratedTerminal makes debugpy launch the debuggee via the
		// runInTerminal reverse request, giving it a real tty so input() works
		// (#625). Output then flows to that terminal rather than DAP output
		// events. redirectOutput stays off — the terminal owns the streams.
		"console":    "integratedTerminal",
		"justMyCode": true,
		"cwd":        cwd,
	}
	// Emit "args" only when non-empty: a nil slice marshals to JSON null,
	// which debugpy's vectorizing array validator turns into [null] and
	// rejects with `"args"[0] must be str`. Absent, it defaults to [].
	if len(spec.Args) > 0 {
		args["args"] = spec.Args
	}
	if spec.Module != "" {
		args["module"] = spec.Module
	} else {
		args["program"] = spec.File
	}
	if len(env) > 0 {
		args["env"] = env
	}
	return args
}
