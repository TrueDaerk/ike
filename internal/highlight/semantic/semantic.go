// Package semantic decodes LSP semantic tokens into the editor's highlight
// spans (Roadmap 0100, #9). It is pure data mapping: the packed relative
// 5-tuples are expanded against the server's legend, token types (refined by
// modifiers) map onto the Tree-sitter capture names the theme system already
// resolves (Roadmap 0110) — no colours are defined here — and positions cross
// from the negotiated encoding into rune columns via protocol/convert.go.
// The overlay is optional by construction: no server support simply means no
// spans, and the editor keeps rendering the Tree-sitter base layer.
package semantic

import (
	"ike/internal/highlight"
	"ike/internal/lsp/protocol"
)

// Legend is the server's token vocabulary, from its advertised capabilities.
type Legend struct {
	TokenTypes     []string
	TokenModifiers []string
}

// Decode expands packed token data (relative-encoded 5-tuples: deltaLine,
// deltaStart, length, tokenType, tokenModifiers) into per-line highlight
// spans. Tokens whose type maps to no capture are dropped; lines beyond the
// document are ignored (a stale result racing an edit).
func Decode(data []uint32, legend Legend, lines []string, enc string) []highlight.Span {
	spans := make([]highlight.Span, 0, len(data)/5)
	line, start := 0, 0
	for i := 0; i+4 < len(data); i += 5 {
		dLine, dStart := int(data[i]), int(data[i+1])
		length, typeIdx, modBits := int(data[i+2]), int(data[i+3]), data[i+4]
		if dLine > 0 {
			line += dLine
			start = dStart
		} else {
			start += dStart
		}
		if line >= len(lines) || typeIdx >= len(legend.TokenTypes) {
			continue
		}
		capture := captureFor(legend.TokenTypes[typeIdx], modifiers(modBits, legend.TokenModifiers))
		if capture == "" {
			continue
		}
		// start/length are in the negotiated encoding's units; convert both
		// boundaries through the shared position mapping.
		sp := protocol.FromLSPPosition(lines, protocol.Position{Line: line, Character: start}, enc)
		ep := protocol.FromLSPPosition(lines, protocol.Position{Line: line, Character: start + length}, enc)
		if ep.Col <= sp.Col {
			continue
		}
		spans = append(spans, highlight.Span{Line: line, StartCol: sp.Col, EndCol: ep.Col, Capture: capture})
	}
	return spans
}

// ApplyDelta applies semanticTokens/full/delta edits to a previous data
// array, returning the new array. Edits are index-based on the flat uint32
// stream and, per the spec, ordered; out-of-range edits are clamped.
func ApplyDelta(prev []uint32, edits []protocol.SemanticTokensEdit) []uint32 {
	out := make([]uint32, len(prev))
	copy(out, prev)
	// Apply back-to-front so earlier edit offsets stay valid.
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		start := e.Start
		if start < 0 {
			start = 0
		}
		if start > len(out) {
			start = len(out)
		}
		end := start + e.DeleteCount
		if end > len(out) {
			end = len(out)
		}
		next := make([]uint32, 0, len(out)-(end-start)+len(e.Data))
		next = append(next, out[:start]...)
		next = append(next, e.Data...)
		next = append(next, out[end:]...)
		out = next
	}
	return out
}

// captureFor maps an LSP token type (refined by its modifiers) onto the
// capture vocabulary of internal/highlight; "" drops the token.
func captureFor(tokenType string, mods map[string]bool) string {
	switch tokenType {
	case "keyword", "modifier":
		return "keyword"
	case "comment":
		return "comment"
	case "string", "regexp":
		return "string"
	case "number":
		return "number"
	case "operator":
		return "operator"
	case "function", "method", "macro":
		return "function"
	case "namespace", "type", "class", "enum", "interface", "struct", "typeParameter":
		return "type"
	case "parameter", "variable", "event":
		if mods["readonly"] {
			return "constant"
		}
		if mods["defaultLibrary"] {
			return "variable.builtin"
		}
		return "variable"
	case "property":
		return "property"
	case "enumMember":
		return "constant"
	case "decorator":
		return "attribute"
	}
	return ""
}

// modifiers expands a modifier bitset against the legend.
func modifiers(bits uint32, legend []string) map[string]bool {
	if bits == 0 {
		return nil
	}
	out := map[string]bool{}
	for i, name := range legend {
		if bits&(1<<uint(i)) != 0 {
			out[name] = true
		}
	}
	return out
}
