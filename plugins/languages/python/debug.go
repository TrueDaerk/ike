package langpython

import "ike/internal/lang"

// Debug-adapter contribution (0350, #578): Python debugs through debugpy's
// DAP adapter (`python -m debugpy.adapter` on stdio), launched with the same
// resolved interpreter run commands use — so breakpoints hit inside the
// project's venv, not a global python.

var _ lang.DebugAdapterProvider = toolchain{}

// DebugAdapter implements lang.DebugAdapterProvider. debugpy must be
// installed in the interpreter's environment; a missing module surfaces as
// the adapter process failing to start, which the session reports.
func (toolchain) DebugAdapter(_ string, interpreter string) ([]string, bool) {
	if interpreter == "" {
		interpreter = "python3"
	}
	return []string{interpreter, "-m", "debugpy.adapter"}, true
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
