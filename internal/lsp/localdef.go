package lsp

import "sync"

// localdef.go is the IKE-side definition seam (#922): a language plugin whose
// navigation targets no server resolves (Ansible inventory hosts/groups)
// registers a LocalDefinition; the LSP bridge consults them before asking the
// server, so the jump also works when no server is installed at all. A
// provider must claim narrowly — returning ok only when it positively
// resolved a target — because a claim short-circuits the server request.

// LocalDefinition inspects the cursor position (path, 0-based line/col, the
// cursor line's text) and either claims the jump target or passes.
type LocalDefinition func(path string, line, col int, lineText string) (DefinitionMsg, bool)

var (
	localDefMu sync.RWMutex
	localDefs  []LocalDefinition
)

// RegisterLocalDefinition adds a provider. Safe to call from a plugin init().
func RegisterLocalDefinition(f LocalDefinition) {
	localDefMu.Lock()
	defer localDefMu.Unlock()
	localDefs = append(localDefs, f)
}

// LocalDefinitionAt runs the registered providers in order; the first claim
// wins.
func LocalDefinitionAt(path string, line, col int, lineText string) (DefinitionMsg, bool) {
	localDefMu.RLock()
	fns := localDefs
	localDefMu.RUnlock()
	for _, f := range fns {
		if msg, ok := f(path, line, col, lineText); ok {
			return msg, true
		}
	}
	return DefinitionMsg{}, false
}
