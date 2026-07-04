package lang

// Toolchain resolves project-specific language-server settings from the workspace
// root — most importantly the interpreter path so a version-aware server (pyright,
// etc.) checks against the project's actual toolchain instead of a global default.
//
// IKE only *detects* (active venv, .python-version, pyproject requires-python, …)
// and hands the result to the server; it never reimplements the server's version
// logic. Detect runs at server spawn (and on restart) with the resolved root.
// ok=false means "nothing to inject" — the server keeps its own defaults. The
// returned settings are merged into ServerSpec.Settings, with any explicit user
// setting winning over a detected one.
type Toolchain interface {
	Detect(root string) (settings map[string]any, ok bool)
}
