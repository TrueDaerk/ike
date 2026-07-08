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

// InterpreterDetector is an optional Toolchain extension exposing the resolved
// interpreter binary path (Roadmap 0160's toolchain settings page, 0170's
// terminal shims and future run configurations read it — one source of truth).
type InterpreterDetector interface {
	// Interpreter reports the interpreter Detect would hand to the server.
	Interpreter(root string) (path string, ok bool)
}

// ExplicitSettings is an optional Toolchain extension mapping an explicitly
// configured interpreter path ([lang.<id>] interpreter, Roadmap 0160) into the
// same server-settings shape Detect produces, so an explicit choice flows to
// the language server exactly like a detected one.
type ExplicitSettings interface {
	Explicit(path string) map[string]any
}

// Interpreter resolves langID's effective interpreter at root: an explicit
// config value always wins over toolchain detection. source is "config",
// "detected", or "" when nothing resolves.
func Interpreter(langID, root, explicit string) (path, source string) {
	if explicit != "" {
		return explicit, "config"
	}
	if l, ok := ByID(langID); ok && l.Toolchain != nil {
		if d, ok := l.Toolchain.(InterpreterDetector); ok {
			if p, found := d.Interpreter(root); found {
				return p, "detected"
			}
		}
	}
	return "", ""
}
