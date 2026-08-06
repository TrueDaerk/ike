package lsp

import "sync"

// localdef.go is the IKE-side local navigation seam (#922, widened by #1629):
// a language plugin whose targets no server resolves registers providers the
// LSP bridge consults before asking the server, so the features work with no
// server installed at all. Ansible inventory hosts/groups claim definitions;
// YAML anchors claim definitions, hover previews and references. A provider
// must claim narrowly — returning ok only when it positively resolved —
// because a claim short-circuits the server request. Every provider receives
// the full document lines (from the bridge's synced copy, or disk when no
// server tracks the file): same-file targets like a YAML anchor cannot be
// found from the cursor line alone.

// LocalDefinition inspects the cursor position (path, 0-based line/col, the
// document's lines) and either claims the jump target or passes.
type LocalDefinition func(path string, line, col int, lines []string) (DefinitionMsg, bool)

// LocalHover claims hover content (markdown, code fences allowed) for the
// cursor position, or passes.
type LocalHover func(path string, line, col int, lines []string) (string, bool)

// LocalReferences claims the find-usages result list for the cursor position,
// or passes.
type LocalReferences func(path string, line, col int, lines []string) ([]Reference, bool)

var (
	localMu   sync.RWMutex
	localDefs []LocalDefinition
	localHovs []LocalHover
	localRefs []LocalReferences
)

// RegisterLocalDefinition adds a provider. Safe to call from a plugin init().
func RegisterLocalDefinition(f LocalDefinition) {
	localMu.Lock()
	defer localMu.Unlock()
	localDefs = append(localDefs, f)
}

// RegisterLocalHover adds a hover provider. Safe to call from a plugin init().
func RegisterLocalHover(f LocalHover) {
	localMu.Lock()
	defer localMu.Unlock()
	localHovs = append(localHovs, f)
}

// RegisterLocalReferences adds a references provider. Safe to call from a
// plugin init().
func RegisterLocalReferences(f LocalReferences) {
	localMu.Lock()
	defer localMu.Unlock()
	localRefs = append(localRefs, f)
}

// LocalDefinitionAt runs the registered providers in order; the first claim
// wins.
func LocalDefinitionAt(path string, line, col int, lines []string) (DefinitionMsg, bool) {
	localMu.RLock()
	fns := localDefs
	localMu.RUnlock()
	for _, f := range fns {
		if msg, ok := f(path, line, col, lines); ok {
			return msg, true
		}
	}
	return DefinitionMsg{}, false
}

// LocalHoverAt runs the registered hover providers; the first claim wins.
func LocalHoverAt(path string, line, col int, lines []string) (string, bool) {
	localMu.RLock()
	fns := localHovs
	localMu.RUnlock()
	for _, f := range fns {
		if text, ok := f(path, line, col, lines); ok {
			return text, true
		}
	}
	return "", false
}

// LocalReferencesAt runs the registered references providers; the first claim
// wins.
func LocalReferencesAt(path string, line, col int, lines []string) ([]Reference, bool) {
	localMu.RLock()
	fns := localRefs
	localMu.RUnlock()
	for _, f := range fns {
		if refs, ok := f(path, line, col, lines); ok {
			return refs, true
		}
	}
	return nil, false
}
