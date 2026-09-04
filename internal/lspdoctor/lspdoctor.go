// Package lspdoctor is the LSP Doctor tool window (#2164): a singleton pane
// that diagnoses language-server failures instead of leaving the user with a
// raw error and a possibly-wrong install hint. For every configured server it
// runs a check chain — binary resolution (PATH vs the GUI-launch PATH gap,
// #1614), executable/architecture sanity, a --version probe, and a real spawn
// + initialize round-trip — then maps the collected evidence to one diagnosis
// class with a concrete fix command. The app owns the Report so results (and
// the previous run's failure classes) survive the panel being closed; a re-run
// compares against them and marks each server resolved / still failing instead
// of repeating the same hint.
package lspdoctor

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
	"ike/internal/ui"
)

// Server is one configured language server as the doctor probes it: the
// effective launch spec (after config overlays) plus the install recipe and
// the workspace root for the spawn probe.
type Server struct {
	Lang    string
	Command string
	Args    []string
	Env     []string
	Install []string // install recipe argv; empty = no shipped recipe
	Root    string   // workspace root the spawn probe initializes against
}

// Status is one check's outcome.
type Status int

const (
	// StatusOK: the check passed.
	StatusOK Status = iota
	// StatusWarn: the check found something noteworthy that does not break
	// the server by itself (e.g. binary off PATH but in a resolvable dir).
	StatusWarn
	// StatusFail: the check found the breakage.
	StatusFail
	// StatusSkip: a prerequisite failed, the check did not run.
	StatusSkip
)

// Check is one named probe result with its human-readable evidence.
type Check struct {
	Name   string
	Status Status
	Detail string
}

// Class is the diagnosis classification a server's evidence maps to. The
// classes are exactly the failure signatures that proved reliably
// distinguishable from captured evidence (#2164).
type Class string

const (
	// ClassOK: every check passed; the server spawns and answers initialize.
	ClassOK Class = "ok"
	// ClassMissing: the binary is nowhere — not on PATH, not in any
	// well-known install directory.
	ClassMissing Class = "binary missing"
	// ClassPathMismatch: the binary exists in a well-known directory
	// (Homebrew, ~/.local/bin, …) that is missing from IKE's PATH — the
	// GUI-launch PATH gap (#1614). IKE's own fallback resolution does not
	// cover the directory, so the server genuinely cannot start.
	ClassPathMismatch Class = "PATH mismatch"
	// ClassNotExecutable: the resolved file exists but lacks the execute bit.
	ClassNotExecutable Class = "not executable"
	// ClassArchMismatch: the binary is for the wrong architecture (exec
	// format error / bad CPU type — the Rosetta trap, #1614).
	ClassArchMismatch Class = "wrong architecture"
	// ClassRuntimeMismatch: the server script's runtime (node) is too old or
	// incompatible — engine complaints, syntax errors from an old node,
	// NODE_MODULE_VERSION / ERR_REQUIRE_ESM signatures.
	ClassRuntimeMismatch Class = "runtime mismatch"
	// ClassCrashInit: the process spawns but dies or errors during the
	// initialize handshake; the decisive stderr line is the evidence.
	ClassCrashInit Class = "crash on initialize"
	// ClassBadRoot: the workspace root the server would initialize against
	// does not exist or is not a directory.
	ClassBadRoot Class = "bad workspace root"
)

// Result is one server's full doctor verdict: the check chain, the diagnosis
// class with its wording and concrete fix, plus the verification verdict a
// re-run computes against the previous run.
type Result struct {
	Lang      string
	Command   string
	Path      string // resolved absolute path, "" when missing
	Checks    []Check
	Class     Class
	Diagnosis string
	Fix       string
	// Verdict is set by Report.Finish on a re-run: "resolved" (was failing,
	// now ok), "unresolved" (same class again), "changed" (different failure
	// now). Empty on a first run or for servers that were healthy before.
	Verdict string
}

// Failed reports whether the result's class is a failure.
func (r Result) Failed() bool { return r.Class != ClassOK }

// RerunMsg asks the root model to re-run the checks ('r' in the panel).
type RerunMsg struct{}

// ResultsMsg delivers a finished check run to the root model.
type ResultsMsg struct{ Results []Result }

// Report is the app-owned doctor state: the probed server set, the latest
// results, and the previous run's failure classes for fix verification. It
// survives the panel being closed.
type Report struct {
	servers []Server
	results []Result
	prev    map[string]Class // lang → class of the previous finished run
	running bool
	ran     bool
}

// NewReport returns an empty report.
func NewReport() *Report { return &Report{} }

// SetServers replaces the server set the next run probes.
func (r *Report) SetServers(s []Server) { r.servers = s }

// Servers returns the probed server set.
func (r *Report) Servers() []Server { return r.servers }

// Begin marks a run in flight.
func (r *Report) Begin() { r.running = true }

// Running reports whether a run is in flight.
func (r *Report) Running() bool { return r.running }

// Ran reports whether at least one run has finished.
func (r *Report) Ran() bool { return r.ran }

// Results returns the latest finished run.
func (r *Report) Results() []Result { return r.results }

// Finish stores a finished run and computes per-server verification against
// the previous run (#2164): a server that was failing and now passes is
// "resolved", one failing with the same class again is "unresolved", one
// failing differently is "changed" — so the panel reports whether the
// suggested fix actually worked instead of repeating the same hint.
func (r *Report) Finish(results []Result) {
	for i := range results {
		prev, had := r.prev[results[i].Lang]
		if !had || prev == ClassOK {
			continue
		}
		switch {
		case results[i].Class == ClassOK:
			results[i].Verdict = "resolved"
		case results[i].Class == prev:
			results[i].Verdict = "unresolved"
		default:
			results[i].Verdict = "changed"
		}
	}
	next := make(map[string]Class, len(results))
	for _, res := range results {
		next[res.Lang] = res.Class
	}
	r.prev = next
	r.results = results
	r.running = false
	r.ran = true
}

// row is one rendered line of the flattened report.
type row struct {
	text   string
	kind   rowKind
	status Status
}

type rowKind int

const (
	rowServer rowKind = iota
	rowCheck
	rowDiagnosis
	rowFix
	rowVerdict
)

// Model is the tool window state. Value type with pointer-receiver mutators,
// embedded in a pane.Instance like the Xdebug Doctor.
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette

	report *Report

	cursor int
	top    int
}

// New returns an empty panel; the report arrives via SetReport.
func New(pal *theme.Palette) Model {
	return Model{pal: pal}
}

// SetReport shares the app-owned report; the panel renders it live.
func (m *Model) SetReport(r *Report) { m.report = r }

// SetSize records the interior content size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetFocused marks the panel focused (header + selection highlight).
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the active theme.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// Rows reports the rendered row count (tests).
func (m *Model) Rows() int { return len(m.rows()) }

// Cursor reports the selected row index (tests).
func (m *Model) Cursor() int { return m.cursor }

// rows flattens the report into rendered lines: per server the summary line,
// its checks, and — when failing — the diagnosis, the fix and the re-run
// verdict.
func (m *Model) rows() []row {
	if m.report == nil {
		return nil
	}
	var out []row
	for _, res := range m.report.Results() {
		head := res.Lang + " — " + res.Command
		if res.Path != "" && res.Path != res.Command {
			head += "  (" + res.Path + ")"
		}
		st := StatusOK
		if res.Failed() {
			st = StatusFail
		}
		out = append(out, row{text: head, kind: rowServer, status: st})
		for _, c := range res.Checks {
			out = append(out, row{text: "  " + c.Name + ": " + c.Detail, kind: rowCheck, status: c.Status})
		}
		if res.Failed() {
			out = append(out, row{text: "  diagnosis [" + string(res.Class) + "]: " + res.Diagnosis, kind: rowDiagnosis, status: StatusFail})
			if res.Fix != "" {
				out = append(out, row{text: "  fix: " + res.Fix, kind: rowFix, status: StatusWarn})
			}
		}
		switch res.Verdict {
		case "resolved":
			out = append(out, row{text: "  ✔ resolved — the previous failure is gone", kind: rowVerdict, status: StatusOK})
		case "unresolved":
			out = append(out, row{text: "  ✗ still failing after re-run (" + string(res.Class) + ") — the suggested fix did not resolve it", kind: rowVerdict, status: StatusFail})
		case "changed":
			out = append(out, row{text: "  ⚠ failure changed after re-run — see the new diagnosis above", kind: rowVerdict, status: StatusWarn})
		}
	}
	return out
}

// Update handles one message while the panel exists; only key presses reach
// it, focus-filtered by the pane layer.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if ui.ListNav(k.String(), &m.cursor, len(m.rows()), m.bodyHeight(), ui.NavFull) {
		m.clampScroll()
		return nil
	}
	switch k.String() {
	case "r":
		if m.report != nil && !m.report.Running() && len(m.report.Servers()) > 0 {
			return func() tea.Msg { return RerunMsg{} }
		}
	case "c":
		// The pane-local twin of lsp.doctor.copy (#2487); nil while
		// there is no finished run to copy.
		return m.CopyKeyCmd()
	}
	m.clampScroll()
	return nil
}

// View renders the run-status header, the scrolled report, and the key-hint
// footer.
func (m *Model) View() string {
	pal := m.theme()
	return ui.ListPaneView(m.headerLine(pal), m.statusLine(pal), m.renderRows(pal, m.bodyHeight()), m.footer(pal))
}

// headerLine is the panel title with the server / failure counts.
func (m *Model) headerLine(pal *theme.Palette) string {
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" LSP Doctor")
	counts := ""
	if m.report != nil && m.report.Ran() {
		counts = m.summary()
	}
	return title + lipgloss.NewStyle().Faint(true).Render("   "+counts)
}

// statusLine renders the run state: probing, or the last run's summary with
// the re-run hint that closes the verify loop.
func (m *Model) statusLine(pal *theme.Palette) string {
	if m.report == nil || (!m.report.Ran() && !m.report.Running()) {
		return " " + lipgloss.NewStyle().Faint(true).Render("no check run yet — press r")
	}
	if m.report.Running() {
		mark := lipgloss.NewStyle().Foreground(pal.Warning).Render(" ● ")
		return mark + lipgloss.NewStyle().Foreground(pal.Foreground).Render("probing language servers…")
	}
	failing := 0
	for _, r := range m.report.Results() {
		if r.Failed() {
			failing++
		}
	}
	if failing == 0 {
		mark := lipgloss.NewStyle().Foreground(pal.Success).Render(" ● ")
		return mark + lipgloss.NewStyle().Foreground(pal.Foreground).Render(m.clip("every configured server is healthy"))
	}
	mark := lipgloss.NewStyle().Foreground(pal.Error).Render(" ● ")
	text := strconv.Itoa(failing) + " server"
	if failing != 1 {
		text += "s"
	}
	text += " failing — apply the fix, then press r to verify"
	return mark + lipgloss.NewStyle().Foreground(pal.Foreground).Render(m.clip(text))
}

// renderRows draws the flattened report, scrolled around the cursor.
func (m *Model) renderRows(pal *theme.Palette, height int) string {
	rows := m.rows()
	if len(rows) == 0 {
		text := " (no language servers configured for this workspace)"
		if m.report != nil && m.report.Running() {
			text = " (checks running…)"
		}
		return lipgloss.NewStyle().Faint(true).Render(text) + strings.Repeat("\n", height)
	}
	m.clampScroll()
	var b strings.Builder
	for k := 0; k < height; k++ {
		i := m.top + k
		if i < len(rows) {
			b.WriteString(m.renderRow(pal, rows[i], i))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderRow draws one report line with its status glyph and colour.
func (m *Model) renderRow(pal *theme.Palette, r row, i int) string {
	style := lipgloss.NewStyle().Foreground(pal.Foreground)
	glyph := "✓"
	switch r.status {
	case StatusOK:
		style = style.Foreground(pal.Success)
	case StatusWarn:
		glyph = "⚠"
		style = style.Foreground(pal.Warning)
	case StatusFail:
		glyph = "✗"
		style = style.Foreground(pal.Error)
	case StatusSkip:
		glyph = "·"
		style = style.Faint(true)
	}
	line := " " + glyph + " " + r.text
	if r.kind == rowServer {
		style = style.Bold(true)
	}
	if i == m.cursor {
		if m.focused {
			style = style.Background(pal.Selection).Bold(true)
		} else {
			style = style.Background(pal.SelectionMuted)
		}
	}
	return style.Render(m.clip(line))
}

// footer shows the key hints.
func (m *Model) footer(pal *theme.Palette) string {
	return lipgloss.NewStyle().Faint(true).Render(m.clip(" r re-run checks & verify fixes · c copy report"))
}

// bodyHeight is the room between the two header lines and the footer.
func (m *Model) bodyHeight() int {
	h := m.height - 3
	if h < 1 {
		h = 1
	}
	return h
}

// clampScroll keeps the cursor valid and inside the visible window.
func (m *Model) clampScroll() {
	ui.ClampWindow(&m.cursor, &m.top, len(m.rows()), m.bodyHeight())
}

// clip bounds one rendered line to the panel width.
func (m *Model) clip(s string) string {
	if m.width > 0 && len([]rune(s)) > m.width {
		return string([]rune(s)[:m.width-1]) + "…"
	}
	return s
}

// theme resolves the palette with the shared default fallback.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}
