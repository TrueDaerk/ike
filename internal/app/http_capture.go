package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/httpclient"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// http_capture.go reports a failed `# @capture name = <jq-expr>` directive
// (#1993) where the author is looking: on the directive's own line in the
// .http buffer. A capture cannot fail the exchange — the response arrived and
// is worth reading — so a silent miss would be the easy outcome and the wrong
// one: the *next* request would then go out with a stale or unresolved
// {{name}} and the reason would be nowhere.
//
// The report rides the ordinary diagnostic path (applyDiagnostics): the marker
// shows inline, the message reads in the diagnostic popup, and the entry lands
// in the Problems tool window like any other. `.http` files have no language
// server, so nothing else publishes for these paths and the set is ours alone.
// Each dispatch republishes the file's whole set, and a dispatch whose
// captures all succeeded publishes an empty one — which clears the previous
// markers, so a fixed directive stops complaining the moment it works.

// httpCaptureSource labels the diagnostics in the Problems window and in the
// popup, the way a server name would.
const httpCaptureSource = "http capture"

// captureDiagnostics turns the failed captures of one response into
// diagnostics anchored at their directive lines.
func captureDiagnostics(results []httpclient.CaptureResult) []ilsp.Diagnostic {
	var out []ilsp.Diagnostic
	for _, c := range results {
		if c.OK() || c.Line <= 0 {
			continue
		}
		line := c.Line - 1 // diagnostics are 0-based
		out = append(out, ilsp.Diagnostic{
			Range: buffer.Range{
				// The whole directive line: the failure is about all of it,
				// name and expression alike.
				Start: buffer.Position{Line: line},
				End:   buffer.Position{Line: line, Col: c.EndCol},
			},
			Severity: protocol.SeverityWarning,
			Message:  "capture " + c.Name + ": " + c.Err,
			Source:   httpCaptureSource,
			Code:     "capture",
		})
	}
	return out
}

// reportHTTPCaptures publishes the capture diagnostics of one dispatch for the
// .http file it came from. A response without any capture directive publishes
// nothing at all (rather than an empty set): a request that never captured
// must not clear the markers of a sibling request that did.
func (m *Model) reportHTTPCaptures(source string, resp *httpclient.Response) tea.Cmd {
	if source == "" || resp == nil || len(resp.Captures) == 0 {
		return nil
	}
	cmd := m.applyDiagnostics(source, captureDiagnostics(resp.Captures))
	m.refreshProblemsPanel()
	return cmd
}
