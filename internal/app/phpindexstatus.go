package app

// phpindexstatus.go is the operations surface of the PHP declaration index
// (Epic 0520, #2673). The index (#2667) works in the background and the trait
// features (#2668-#2672) only ever show its answers, so nothing so far said
// whether it is warm, how much it holds, or what to do when the workspace
// changed underneath ike. Three things say it now:
//
//   - php.traitIndex.status — a small info popup in the floating shell with
//     the Stats() numbers, including the two states where the index can never
//     answer (disabled, or a build without the PHP grammar).
//   - php.traitIndex.rebuild — drops the walk and rescans, for the change no
//     watcher event described: a branch switch with thousands of files,
//     generator output. A no-op with a message when there is nothing to scan.
//   - the status line's LSP slot, which shows "php-index …" while a scan runs
//     and nothing afterwards — the index's warm-up, told the way the server's
//     is.
//
// The telemetry op per scan (php.trait.index_scan) is wired in app.go beside
// the other 0520 recorders; this file only formats.

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/phpindex"
	"ike/internal/ui"
)

// PHPIndexStatusMsg opens the index status popup (php.traitIndex.status).
type PHPIndexStatusMsg struct{}

// PHPIndexRebuildMsg drops and rescans the index (php.traitIndex.rebuild).
type PHPIndexRebuildMsg struct{}

// phpIndexScanning reports whether the project's PHP index is mid-scan — the
// initial walk or a rebuild. Cheap enough for the status line: it asks the
// walk whether it is done, it does not derive a snapshot the way Stats does.
func (m Model) phpIndexScanning() bool {
	return m.phpIndex != nil && !m.phpIndex.ScanDone()
}

// phpIndexSegment is the status line's share of the index (#2673): while a
// scan runs and the focused buffer is PHP — the only buffer the index can say
// anything about — the LSP slot carries "php-index …" beside the server's own
// state, and nothing once the walk finished. The scan's completion already
// wakes the Update loop (PHPIndexChangedMsg), so the slot clears on its own.
func (m Model) phpIndexSegment(ed *editor.Model) string {
	return phpIndexSegmentFor(m.phpIndexScanning(), ed)
}

// phpIndexSegmentFor is the segment's rule with the scan state passed in, so
// both halves of it — the label during a walk, the silence after one — are
// testable without racing a real scan.
func phpIndexSegmentFor(scanning bool, ed *editor.Model) string {
	if !scanning || ed == nil || !ed.HasFile() {
		return ""
	}
	if l, ok := lang.ByPath(ed.Path()); !ok || l.ServerLang() != "php" {
		return ""
	}
	return "php-index …"
}

// lspStatusSegment composes the LSP slot: the focused language's server state
// (#380) and, while the PHP index walks, its warm-up beside it (#2673).
func lspStatusSegment(m Model, ed *editor.Model) string {
	return joinLSPStatus(m.focusedLangStatus(ed), m.phpIndexSegment(ed))
}

// joinLSPStatus puts the two warm-ups in one slot, separated the way the
// status line separates what belongs together; either half alone stands on
// its own, and two silent halves keep the slot hidden.
func joinLSPStatus(server, idx string) string {
	switch {
	case idx == "":
		return server
	case server == "":
		return idx
	}
	return server + " · " + idx
}

// openPHPIndexStatus shows the index status popup. It reads Stats() once —
// the body is a fixed string, not a live closure — because the popup answers
// "where is the index right now", and a box whose numbers move under the
// reader is harder to report than one that names a moment.
func (m *Model) openPHPIndexStatus() {
	body := phpIndexStatusBody(m.phpIndex)
	m.shell.SetContent(ui.ModelContent{
		Heading: "PHP Declaration Index",
		Body:    func() string { return body },
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// phpIndexStatusBody renders the popup body for an index (nil included: a
// model without one is a model whose project never built it).
func phpIndexStatusBody(x *phpindex.Index) string {
	if x == nil {
		return "unavailable — no index for this project\n"
	}
	s := x.Stats()
	switch {
	case s.Unavailable:
		return "unavailable — this build cannot parse PHP (no cgo / no grammar)\n" +
			"The trait features fall back to the language server alone.\n"
	case !s.Enabled:
		return "disabled — php.trait_index is off\n" +
			"Turn it on in the settings to build the index.\n"
	}
	state := "ready"
	if s.Scanning {
		state = "scanning…"
	}
	body := "  state          " + state + "\n" +
		"  files          " + strconv.Itoa(s.Files) + "\n" +
		"  declarations   " + strconv.Itoa(s.Declarations) + "\n" +
		"  edges          " + strconv.Itoa(s.Edges) + "\n" +
		"  last scan      " + phpScanDuration(s.LastScan) + "\n"
	if s.Truncated {
		body += "  truncated      yes — the walk stopped at php.index.max_files\n"
	}
	if root := x.Root(); root != "" {
		body += "\n  " + elideHead(root, phpStatusPathWidth) + "\n"
	}
	return body
}

// phpStatusPathWidth caps the scanned root's line so a deeply nested project
// cannot widen the popup past the terminal — the box is sized from its
// widest line, and a 200-column path would push the frame off screen.
const phpStatusPathWidth = 60

// elideHead keeps the tail of s — for a path the last segments are what
// identifies the project — and marks the cut with a leading ellipsis.
func elideHead(s string, max int) string {
	r := []rune(s)
	if max <= 1 || len(r) <= max {
		return s
	}
	return "…" + string(r[len(r)-(max-1):])
}

// phpScanDuration renders a scan duration in milliseconds; a walk that never
// ran (or was too fast to time) says so rather than printing "0ms".
func phpScanDuration(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
}

// rebuildPHPIndex runs php.traitIndex.rebuild: a fresh walk plus a toast, or
// the reason there is nothing to walk. The rescan is asynchronous — the
// status line's "php-index …" is the progress, the popup the result.
func (m Model) rebuildPHPIndex() (tea.Model, tea.Cmd) {
	if m.phpIndex == nil || !m.phpIndex.Rebuild() {
		m.host.Notify(host.Info, "PHP index: nothing to rebuild — "+phpIndexInertReason(m.phpIndex))
		return m, nil
	}
	m.host.Notify(host.Info, "PHP index: rescanning the project…")
	return m, nil
}

// phpIndexInertReason names why a rebuild did nothing, in the vocabulary of
// the setting the user would change.
func phpIndexInertReason(x *phpindex.Index) string {
	switch {
	case x == nil:
		return "no index for this project"
	case !x.Available():
		return "this build cannot parse PHP"
	default:
		return "php.trait_index is off"
	}
}
