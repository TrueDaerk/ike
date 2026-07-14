package lang

// Debug-adapter seam (0350, #578): a language plugin contributes how to
// spawn its DAP adapter and how to phrase the launch request for a run
// configuration. The interpreter resolution is shared with run commands and
// the LSP toolchain (Interpreter — explicit config beats detection).

// DebugAdapterProvider is an optional Toolchain extension for languages that
// support DAP debugging.
type DebugAdapterProvider interface {
	// DebugAdapter returns the argv spawning the adapter process speaking
	// DAP on stdio. ok=false means debugging is unavailable (e.g. the
	// adapter package is not installed).
	DebugAdapter(root, interpreter string) (argv []string, ok bool)
	// DebugLaunchArgs builds the DAP launch-request arguments for spec.
	DebugLaunchArgs(root string, spec RunSpec, cwd string, env map[string]string) map[string]any
}

// DebugAdapter resolves langID's adapter argv at root; explicit is the
// configured interpreter ([lang.<id>] interpreter).
func DebugAdapter(langID, root, explicit string) (argv []string, ok bool) {
	p, found := debugProvider(langID)
	if !found {
		return nil, false
	}
	interpreter, _ := Interpreter(langID, root, explicit)
	return p.DebugAdapter(root, interpreter)
}

// DebugLaunchArgs builds langID's launch request for spec; ok=false when the
// language contributes no adapter.
func DebugLaunchArgs(langID, root string, spec RunSpec, cwd string, env map[string]string) (map[string]any, bool) {
	p, found := debugProvider(langID)
	if !found {
		return nil, false
	}
	return p.DebugLaunchArgs(root, spec, cwd, env), true
}

// SupportsDebug reports whether langID contributes a DAP adapter.
func SupportsDebug(langID string) bool {
	_, found := debugProvider(langID)
	return found
}

func debugProvider(langID string) (DebugAdapterProvider, bool) {
	l, found := ByID(langID)
	if !found || l.Toolchain == nil {
		return nil, false
	}
	p, ok := l.Toolchain.(DebugAdapterProvider)
	return p, ok
}
