package app

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/httpfile"
	"ike/internal/httphistory"
	"ike/internal/palette"
	"ike/internal/secret"
	"ike/internal/settings"
	"ike/internal/ui"
)

// http_varpalette.go is the HTTP Variables palette (#3122): every `{{name}}`
// the variable chain (httpfile.Vars) defines for the active .http file, one
// row per name, with the rung that wins it, its value — written and, when it
// holds placeholders, expanded — and for a captured value the request whose
// response produced it and how old that response is:
//
//	token    response · ••••  ← POST /login (### auth) · 2m ago
//	host     env dev · https://dev.example.com
//	apiBase  global project · https://{{domain}}/api → https://dev.example.com/api
//
// Completion (#2158) only lists names; this answers "what does it hold right
// now, and why". Enter goes to the value's origin, http.copyVariable (cmd+c /
// ctrl+c while the palette is up) copies the unmasked value. The list is
// built once when the palette opens; it does not follow edits while open.

const (
	httpVariablesCommand    = "http.variables"
	httpCopyVariableCommand = "http.copyVariable"
)

// httpVarsPrefix selects the HTTP Variables mode inside the palette. It is
// only opened locked, so the rune merely has to be unique among the modes.
const httpVarsPrefix = '¬'

// httpVarValueMax is how many runes of a value a row shows before cutting it.
const httpVarValueMax = 60

// HTTPVariablesMsg runs http.variables: open the HTTP Variables palette.
type HTTPVariablesMsg struct{}

// HTTPCopyVariableMsg runs http.copyVariable: copy the focused row's value.
type HTTPCopyVariableMsg struct{}

// HTTPVarJumpMsg is a row's activation: go to where the variable's value
// comes from. Which fields matter depends on Origin.
type HTTPVarJumpMsg struct {
	Source string // the .http file (or file-less buffer's lang path)
	Name   string
	Origin string // an httpfile.Origin* word; env rows carry "env <name>"
	// Line is the 1-based `@name =` line of a file variable.
	Line int
	// EnvPath / EnvLine locate an environment value's key (EnvLine 1-based).
	EnvPath string
	EnvLine int
	// Request / At name the stored response that captured the value.
	Request string
	At      time.Time
}

// httpVarRow is one listed variable.
type httpVarRow struct {
	name     string
	origin   string // display word: "response", "file", "env dev", …
	value    string // as written (the process value for a process-env row)
	expanded string // value with its placeholders resolved; "" when it has none
	resolved bool   // expanded is a full resolution (no unresolved names)
	masked   bool
	// producer is the "← POST /login (### auth) · 2m ago" tail of a capture.
	producer string
	jump     HTTPVarJumpMsg
}

// copyValue is what http.copyVariable puts on the clipboard: the value a
// dispatch would send — expanded when it resolves, as written otherwise.
func (r httpVarRow) copyValue() string {
	if r.expanded != "" && r.resolved {
		return r.expanded
	}
	return r.value
}

// display is the row's value column, masked under a secret-suspect name.
func (r httpVarRow) display() string {
	if r.masked {
		return secret.Mask
	}
	out := foldHintValue(r.value, httpVarValueMax)
	if r.expanded != "" {
		exp := "⚠ unresolved"
		if r.resolved {
			exp = foldHintValue(r.expanded, httpVarValueMax)
		}
		out += " → " + exp
	}
	return out
}

// detail is the dim text after the name.
func (r httpVarRow) detail() string {
	d := r.origin + " · " + r.display()
	if r.producer != "" {
		d += "  ← " + r.producer
	}
	return d
}

// httpVarsMode is the palette Mode listing httpVarRow rows. The model fills
// it before each locked open (the httpEnvMode pattern): building the rows
// needs the buffer, the history and the settings — model state.
type httpVarsMode struct {
	file string // base name of the listed .http file, for the placeholder
	rows []httpVarRow
}

func newHTTPVarsMode() *httpVarsMode { return &httpVarsMode{} }

// Prefix implements palette.Mode.
func (h *httpVarsMode) Prefix() rune { return httpVarsPrefix }

// Placeholder implements palette.Mode.
func (h *httpVarsMode) Placeholder() string {
	if h.file == "" {
		return "HTTP variables…"
	}
	return "HTTP variables of " + h.file + "…"
}

// Hint implements palette.HintMode: the two row actions.
func (h *httpVarsMode) Hint(string) string {
	return "enter go to origin · ctrl+c copy value"
}

// Results implements palette.Mode: the rows in name order, narrowed by the
// query — a fuzzy match on the name (highlighted), or a plain substring of
// the origin or the shown value. The shown value, not the real one: a masked
// secret must not be findable by typing it.
func (h *httpVarsMode) Results(query string, _ palette.Context) []palette.Item {
	var items []palette.Item
	q := strings.ToLower(strings.TrimSpace(query))
	for _, r := range h.rows {
		it := palette.Item{Title: r.name, Detail: r.detail(), Key: r.name, Msg: r.jump}
		if res, ok := fuzzy.Match(query, r.name); ok {
			it.Spans, it.Score = res.Positions, res.Score
		} else if q == "" || !strings.Contains(strings.ToLower(r.origin+" "+r.display()), q) {
			continue
		}
		items = append(items, it)
	}
	return items
}

// row returns the listed row named name.
func (h *httpVarsMode) row(name string) (httpVarRow, bool) {
	for _, r := range h.rows {
		if r.name == name {
			return r, true
		}
	}
	return httpVarRow{}, false
}

// foldHintValue folds a value onto one row — line breaks as ⏎, tabs as
// spaces — and cuts it to max runes with an ellipsis.
func foldHintValue(value string, max int) string {
	value = strings.ReplaceAll(value, "\r\n", "⏎")
	value = strings.NewReplacer("\n", "⏎", "\r", "⏎", "\t", " ").Replace(value)
	if r := []rune(value); len(r) > max {
		value = string(r[:max]) + "…"
	}
	return value
}

// httpVarMasked reports whether a variable's value is drawn masked: the
// editor's secret masking is on and the name is secret-suspect — the rule the
// editor and the HTTP Variables settings page draw by.
func httpVarMasked(name string) bool {
	c := config.Get()
	return c != nil && c.Editor.SecretMasking && secret.Suspect(name)
}

// httpVarRows builds the palette rows of one .http file: the chain a
// dispatch would resolve against (httpVars, closed by the process
// environment), one row per name its Definitions list.
func (m Model) httpVarRows(source, text string, now time.Time) ([]httpVarRow, error) {
	f := httpfile.Parse(text)
	vars, _, err := m.httpVars(source, f)
	vars.Lookup = os.LookupEnv
	dir := filepath.Dir(source)
	envName := ""
	if err == nil {
		envs, _ := httpfile.LoadEnvironments(dir)
		envName = m.httpEnvName(dir, envs)
	}
	captured := m.httpCapturedFrom(source, f)
	fileLines := map[string]int{}
	for _, v := range f.Vars {
		fileLines[v.Name] = v.Line // the last definition wins, as in VarMap
	}
	var rows []httpVarRow
	for _, d := range vars.Definitions() {
		r := httpVarRow{
			name:   d.Name,
			origin: d.Origin,
			value:  d.Value,
			masked: httpVarMasked(d.Name),
			jump:   HTTPVarJumpMsg{Source: source, Name: d.Name, Origin: d.Origin},
		}
		if d.Origin != httpfile.OriginProcess && (strings.Contains(d.Value, "{{") || strings.Contains(d.Value, "${")) {
			r.expanded, r.resolved = vars.Value(d.Name)
			if !r.resolved {
				r.expanded = d.Value // marks the row as holding placeholders
			}
		}
		switch d.Origin {
		case httpfile.OriginFile:
			r.jump.Line = fileLines[d.Name]
		case httpfile.OriginEnv:
			if envName != "" {
				r.origin = httpfile.OriginEnv + " " + envName
				r.jump.EnvPath, r.jump.EnvLine, _ = httpfile.EnvKeySource(dir, envName, d.Name)
			}
		case httpfile.OriginResponse:
			if c, ok := captured[d.Name]; ok {
				r.jump.Request, r.jump.At = c.Key, c.Entry.Time
				r.producer = httpVarProducer(f, c, now)
			}
		}
		r.jump.Origin = r.origin
		rows = append(rows, r)
	}
	return rows, err
}

// httpVarProducer names the request whose response captured a value — its
// request line plus the `###` name when it has one — and the response's age.
func httpVarProducer(f *httpfile.File, c httphistory.CapturedValue, now time.Time) string {
	label := "### " + c.Key
	for _, req := range f.Requests {
		if req.Key() == c.Key {
			label = requestLabel(req)
			if req.Name != "" {
				label += " (### " + req.Name + ")"
			}
			break
		}
	}
	if age := ui.RelTime(c.Entry.Time, now); age != "" {
		label += " · " + age
	}
	return label
}

// httpCapturedFrom is httpCaptured keeping each value's producing entry.
func (m Model) httpCapturedFrom(source string, f *httpfile.File) map[string]httphistory.CapturedValue {
	var keys []string
	for _, r := range f.Requests {
		if len(r.Captures) > 0 {
			keys = append(keys, r.Key())
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return httphistory.CapturedFrom(keys, func(key string) []httphistory.Entry { return m.httpHistoryEntries(source, key) })
}

// openHTTPVarsPalette runs http.variables: build the rows of the focused
// .http buffer and open the palette locked to them. A file without variables
// opens the palette empty, with the usual no-results hint.
func (m *Model) openHTTPVarsPalette() {
	ed := m.activeEditor()
	if ed == nil || !isHTTPBuffer(ed) {
		m.host.Notify(host.Info, "http: focus an .http file first")
		return
	}
	source := httpSource(ed)
	rows, err := m.httpVarRows(source, ed.Text(), time.Now())
	if err != nil {
		// A broken environment file: list the rest, but say why its values
		// are missing.
		m.host.Notify(host.Warn, "http: "+err.Error())
	}
	m.httpVarsPick.file = filepath.Base(source)
	m.httpVarsPick.rows = rows
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(m.paletteContext(), httpVarsPrefix)
}

// httpVarsPaletteOpen reports whether the palette shows the HTTP Variables
// mode — the gate of the copy chord.
func (m Model) httpVarsPaletteOpen() bool {
	prefix, ok := m.palette.LockedPrefix()
	return m.palette.IsOpen() && ok && prefix == httpVarsPrefix
}

// copyFocusedHTTPVar runs http.copyVariable: the focused row's value goes to
// the clipboard unmasked — masking hides it on screen, copying is asking for
// it. The palette stays open, so several values can be copied in a row.
func (m *Model) copyFocusedHTTPVar() {
	if !m.httpVarsPaletteOpen() {
		m.host.Notify(host.Info, "http: open the HTTP Variables palette ("+httpVariablesCommand+") first")
		return
	}
	it, ok := m.palette.FocusedItem()
	if !ok {
		m.host.Notify(host.Info, "http: no variable to copy")
		return
	}
	r, ok := m.httpVarsPick.row(it.Key)
	if !ok {
		return
	}
	m.copyToClipboard(r.copyValue())
	m.host.Notify(host.Info, "copied the value of {{"+r.name+"}}")
}

// jumpToHTTPVar goes to a row's origin: the `@name =` line of a file
// variable, the stored response that captured a value, the environment
// file's key, the HTTP Variables settings page for a global. A process
// environment value has nowhere to go and says so.
func (m Model) jumpToHTTPVar(msg HTTPVarJumpMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Origin == httpfile.OriginFile:
		if msg.Line <= 0 {
			break
		}
		if ed := m.activeEditor(); ed != nil && httpSource(ed) == msg.Source {
			ed.JumpTo(msg.Line-1, 0)
			return m, nil
		}
		return m.openPathFocusedAt(msg.Source, msg.Line-1, 0)
	case msg.Origin == httpfile.OriginResponse:
		if msg.Request == "" {
			break
		}
		return m, m.loadStoredHTTPResponseAt(msg.Source, msg.Request, msg.At)
	case msg.Origin == httpfile.OriginEnv || strings.HasPrefix(msg.Origin, httpfile.OriginEnv+" "):
		if msg.EnvPath == "" {
			m.host.Notify(host.Info, "http: {{"+msg.Name+"}} is not in "+httpfile.EnvFileName)
			return m, nil
		}
		return m.openPathFocusedAt(msg.EnvPath, msg.EnvLine-1, 0)
	case msg.Origin == httpfile.OriginProject || msg.Origin == httpfile.OriginUser:
		w, h := m.settingsSize()
		m.settings.SetSize(w, h)
		if !m.settings.OpenPage(settings.HTTPVarsPageTitle) {
			m.host.Notify(host.Warn, "http: no HTTP Variables settings page")
		}
		return m, nil
	case msg.Origin == httpfile.OriginProcess:
		m.host.Notify(host.Info, "http: {{"+msg.Name+"}} is defined by the process environment")
		return m, nil
	}
	m.host.Notify(host.Info, "http: no origin to go to for {{"+msg.Name+"}}")
	return m, nil
}

// loadStoredHTTPResponseAt shows the stored responses of one request like
// http.showResponse, then browses to the entry stamped at — the response a
// captured value came from, which a later run that captured nothing may
// have pushed off the top.
func (m *Model) loadStoredHTTPResponseAt(source, key string, at time.Time) tea.Cmd {
	cmd := m.loadStoredHTTPResponse(source, key)
	p := m.httpPanel()
	if p == nil || at.IsZero() {
		return cmd
	}
	for i, e := range m.httpHistoryEntries(source, key) {
		if e.Time.Equal(at) {
			if i > 0 {
				p.ShowHistoryEntry(i)
				return p.HighlightCmd()
			}
			break
		}
	}
	return cmd
}
