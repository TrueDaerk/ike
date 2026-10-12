package app

// complexity_problems.go is the complexity index's Problems feed (0550,
// #2920): every function at or above a warning threshold becomes one warning
// under the "complexity" task source, anchored at the function's header
// line. The feed rides the store's task-source channel, which only the
// Problems pane reads — the editor's gutter and underlines come from the LSP
// and lint flows, so the inline complexity hint stays the editor's only
// marker. Each publish replaces the source wholesale (never appends across
// generations), so a raised threshold or a simplified function drops its row.

import (
	"strconv"

	"ike/internal/complexity"
	"ike/internal/config"
	"ike/internal/editor/buffer"
	ilsp "ike/internal/lsp"
)

// complexityProblemSource is the Problems source the feed publishes under —
// the `source:complexity` filter's handle.
const complexityProblemSource = "complexity"

// complexityDiagnostics builds the feed from a snapshot: one warning per
// function whose CCN reaches ccnWarn or whose cognitive complexity reaches
// cogWarn, listing every metric over its threshold in one message. Code names
// the first exceeded metric ("ccn" before "cognitive").
func complexityDiagnostics(v complexity.View, ccnWarn, cogWarn int) map[string][]ilsp.Diagnostic {
	byPath := map[string][]ilsp.Diagnostic{}
	for path, fns := range v.Files {
		for _, fn := range fns {
			overCCN := fn.CCN >= ccnWarn
			overCog := fn.Cognitive >= cogWarn
			if !overCCN && !overCog {
				continue
			}
			msg, code := "function "+fn.Name+": ", ""
			if overCCN {
				msg += "CCN " + strconv.Itoa(fn.CCN) + " ≥ " + strconv.Itoa(ccnWarn)
				code = "ccn"
			}
			if overCog {
				if overCCN {
					msg += ", "
				} else {
					code = "cognitive"
				}
				msg += "cognitive " + strconv.Itoa(fn.Cognitive) + " ≥ " + strconv.Itoa(cogWarn)
			}
			pos := buffer.Position{Line: fn.Line}
			byPath[path] = append(byPath[path], ilsp.Diagnostic{
				Range:    buffer.Range{Start: pos, End: pos},
				Severity: 2,
				Message:  msg,
				Source:   complexityProblemSource,
				Code:     code,
			})
		}
	}
	return byPath
}

// feedComplexityProblems republishes the feed from the index's current
// snapshot under c's thresholds, or clears it when the index or the feed is
// switched off. Called on every ComplexityChangedMsg and config reload.
func (m *Model) feedComplexityProblems(c config.Complexity) {
	if m.probStore == nil {
		return
	}
	if !c.Enabled || !c.Problems || m.complexityIndex == nil {
		m.probStore.ClearTaskSource(complexityProblemSource)
	} else {
		byPath := complexityDiagnostics(m.complexityIndex.Snapshot(), c.CCNWarn, c.CognitiveWarn)
		m.probStore.SetTaskSource(complexityProblemSource, byPath)
	}
	m.refreshProblemsPanel()
}
