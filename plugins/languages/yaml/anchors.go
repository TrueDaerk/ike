package langyaml

import (
	"strings"

	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/yamlanchor"
)

// anchors.go wires YAML anchor/alias navigation (#1629) into the local
// provider seams (#922): goto-definition on an alias jumps to its anchor,
// find-usages on either lists every mark of the name, and hover on an alias
// previews the resolved value (merge-key aware) as a highlighted YAML fence.
// All three claim narrowly — only on a mark the scanner resolved — so every
// other position still reaches yaml-language-server.

// yamlFile gates the providers to buffers the registry resolves as YAML.
func yamlFile(path string) bool {
	l, ok := lang.ByPath(path)
	return ok && l.ID == "yaml"
}

// anchorDefinition claims the jump from an alias to its anchor.
func anchorDefinition(path string, line, col int, lines []string) (ilsp.DefinitionMsg, bool) {
	if !yamlFile(path) {
		return ilsp.DefinitionMsg{}, false
	}
	mk, ok := yamlanchor.DefinitionAt(lines, line, col)
	if !ok {
		return ilsp.DefinitionMsg{}, false
	}
	return ilsp.DefinitionMsg{Path: path, Line: mk.Line, Col: mk.Col}, true
}

// anchorHover claims hover on a resolvable alias: the anchor name as a title
// and the resolved value as a yaml code fence, so the popup renders it
// syntax-highlighted like server hovers do.
func anchorHover(path string, line, col int, lines []string) (string, bool) {
	if !yamlFile(path) {
		return "", false
	}
	name, value, ok := yamlanchor.ResolveAt(lines, line, col)
	if !ok {
		return "", false
	}
	return "&" + name + "\n```yaml\n" + strings.Join(value, "\n") + "\n```", true
}

// anchorReferences claims find-usages on an anchor or alias: every mark of
// the name in the document, each previewing its trimmed line.
func anchorReferences(path string, line, col int, lines []string) ([]ilsp.Reference, bool) {
	if !yamlFile(path) {
		return nil, false
	}
	_, marks, ok := yamlanchor.UsagesAt(lines, line, col)
	if !ok {
		return nil, false
	}
	refs := make([]ilsp.Reference, 0, len(marks))
	for _, mk := range marks {
		preview := ""
		if mk.Line < len(lines) {
			preview = strings.TrimSpace(lines[mk.Line])
		}
		refs = append(refs, ilsp.Reference{Path: path, Line: mk.Line, Col: mk.Col, Preview: preview})
	}
	return refs, true
}

func init() {
	ilsp.RegisterLocalDefinition(anchorDefinition)
	ilsp.RegisterLocalHover(anchorHover)
	ilsp.RegisterLocalReferences(anchorReferences)
}
