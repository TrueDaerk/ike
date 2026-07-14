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

// DebugAdapterInstall implements lang.DebugAdapterInstaller: pip inside the
// interpreter first, uv as the fallback — uv-managed pythons ship without
// pip, and `uv pip install --python <interpreter>` targets exactly that
// environment.
func (toolchain) DebugAdapterInstall(_ string, interpreter string) [][]string {
	if interpreter == "" {
		interpreter = "python3"
	}
	return [][]string{
		{interpreter, "-m", "pip", "install", "debugpy"},
		{"uv", "pip", "install", "--python", interpreter, "debugpy"},
	}
}

// DebugLaunchArgs implements lang.DebugAdapterProvider: module form when the
// spec carries one (mirroring `-m` runs), else program form. Output is
// redirected through DAP output events so the debug UI owns it.
func (toolchain) DebugLaunchArgs(_ string, spec lang.RunSpec, cwd string, env map[string]string) map[string]any {
	args := map[string]any{
		"request":        "launch",
		"console":        "internalConsole",
		"redirectOutput": true,
		"justMyCode":     true,
		"cwd":            cwd,
		"args":           spec.Args,
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
