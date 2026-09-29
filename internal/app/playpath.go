package app

// playpath.go is the playground's "drill in" command (#2783): the inverse of
// …PlaygroundAtPath. Where that one seeds the query from the *editor* caret's
// path, this one takes the path of the *result* cursor and appends it to the
// running program as a pipeline stage — standing on "name" inside the fourth
// item of `.items` turns the program into `.items | .[3].name`, and the
// generalised flavour into `.items | .[].name`. The path comes from the same
// structural scan the status-line breadcrumb uses (internal/docpath), run over
// the read-only result buffer, whose display extension already names the
// language the result is written in.

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/docpath"
	"ike/internal/jqplay"
)

// AppendPlayPathMsg appends the result cursor's path to the playground's
// program (#2783). Dispatched by json.jqAppendPath (Any false) and
// json.jqAppendPathAny (Any true, every sequence index generalised to `[]`).
type AppendPlayPathMsg struct{ Any bool }

// appendPlayPath is the command itself: compute the result cursor's path in
// the dialect's spelling, append it as a stage, hand the keyboard back to the
// query line with the caret at the end and run. Every case with nothing to
// append — no playground, a result without a path scanner, the cursor at the
// root — says why on the info row and leaves the program alone.
func (m *Model) appendPlayPath(msg AppendPlayPathMsg) tea.Cmd {
	s := m.play
	if s == nil || s.resultEd == nil {
		return nil
	}
	path, reason := playResultPath(s.dialect, s.result, s.resultEd.DocPathAvailable(), s.resultEd.DocPathSteps(), msg.Any)
	if reason != "" {
		s.status, s.statusWarn = reason, true
		return nil
	}
	s.program.Set(playAppendStage(s.program.Text, path))
	s.histIdx, s.comp = -1, nil
	s.setBufFocus(false)
	s.status, s.statusWarn = "appended "+path, false
	m.sizePlayResult() // a longer program may change the expanded header's height (#2032)
	return m.schedulePlayEval()
}

// playResultPath renders the result cursor's path for dialect d, or the reason
// there is none to append. scannable is whether the result buffer has a path
// scanner at all (JSON or YAML, not a large result); steps is its caret path.
//
// jq and yq write their own spelling of the path. xmq has no path stage: its
// command line is CLI arguments, not a jq pipeline, so even a `to-json` result
// — the only xmq output the scan reads — gets the path named on the info row
// rather than appended to a program it would break.
func playResultPath(d jqplay.Dialect, res jqplay.Result, scannable bool, steps []docpath.Step, generalise bool) (path, reason string) {
	if d == jqplay.DialectXMQ && res.Ext() != "json" {
		return "", "append path needs to-json output — xmq's own notation has no JSON path"
	}
	if !scannable {
		return "", "no json/yaml path in this result"
	}
	if len(steps) == 0 {
		return "", "the result cursor is at the document root — nothing to append"
	}
	if generalise {
		steps = docpath.Generalize(steps)
	}
	switch d {
	case jqplay.DialectYQ:
		return docpath.YQ(steps), ""
	case jqplay.DialectXMQ:
		return "", "xmq has no path stage to append — the value is at " + docpath.JQ(steps)
	}
	return docpath.JQ(steps), ""
}

// playAppendStage joins path onto program as a new pipeline stage. The
// identity program (and the blank one, which runs as the identity, #2807) is
// replaced rather than piped into: `. | .a` says nothing `.a` does not. A
// program whose last line carries a `#` comment gets the stage on a line of
// its own, since the comment would otherwise swallow it.
func playAppendStage(program, path string) string {
	p := strings.TrimSpace(program)
	if p == "" || p == "." {
		return path
	}
	last := p[strings.LastIndexByte(p, '\n')+1:]
	if strings.Contains(last, "#") {
		return p + "\n| " + path
	}
	return p + " | " + path
}
