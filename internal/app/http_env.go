package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/httpfile"
	"ike/internal/palette"
)

// http_env.go carries the user-defined variables of the HTTP client (#1867)
// into a dispatch: the .http file's own `@name=value` definitions and, when a
// sibling http-client.env.json names several environments, the one the user
// picked. The selection is per directory — that is where the environment file
// lives — and persists in the project's .ike state store, so "dev" stays
// chosen across restarts.

// httpEnvPrefix selects the environment picker inside the palette; it is only
// opened locked, so the rune has no user-facing prefix story ('@' is the file
// finder, '|' the stored-requests picker).
const httpEnvPrefix = '%'

// SelectHTTPEnvMsg records the chosen environment for one directory — the
// picker's activation message. An empty Name clears the selection.
type SelectHTTPEnvMsg struct {
	Dir  string
	Name string
}

// httpEnvFile returns the per-project store of selected environments,
// following the layout store's IKE_CONFIG_DIR redirection seam.
func httpEnvFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "httpenv.json")
	}
	return filepath.Join(".ike", "httpenv.json")
}

// httpEnvStore maps a directory holding an http-client.env.json to the
// environment selected for it (#1867).
type httpEnvStore struct {
	path     string
	Selected map[string]string `json:"selected"`
}

// loadHTTPEnv reads the store, tolerating a missing or malformed file (no
// selection) — failing to read must never disrupt the session.
func loadHTTPEnv() *httpEnvStore {
	s := &httpEnvStore{path: httpEnvFile(), Selected: map[string]string{}}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return s
	}
	var onDisk httpEnvStore
	if json.Unmarshal(data, &onDisk) == nil && onDisk.Selected != nil {
		s.Selected = onDisk.Selected
	}
	return s
}

// Get returns the environment selected for dir, "" when none is.
func (s *httpEnvStore) Get(dir string) string {
	if s == nil {
		return ""
	}
	return s.Selected[canonicalPath(dir)]
}

// Set records (or, with an empty name, clears) the selection for dir.
func (s *httpEnvStore) Set(dir, name string) {
	if s == nil {
		return
	}
	if s.Selected == nil {
		s.Selected = map[string]string{}
	}
	key := canonicalPath(dir)
	if name == "" {
		delete(s.Selected, key)
	} else {
		s.Selected[key] = name
	}
	s.save()
}

// save persists the store; errors are swallowed (never disrupt the session).
func (s *httpEnvStore) save() {
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	if dir := filepath.Dir(s.path); dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	_ = os.WriteFile(s.path, data, 0o644)
}

// httpEnvName picks the environment to resolve against: the persisted choice
// while it still exists, else the only one a file with a single environment
// offers (no reason to make the user choose), else none.
func (m Model) httpEnvName(dir string, envs *httpfile.Environments) string {
	if sel := m.httpEnv.Get(dir); sel != "" && envs.Has(sel) {
		return sel
	}
	if envs.Len() == 1 {
		return envs.Names()[0]
	}
	return ""
}

// httpVars builds the variable chain of one dispatch (#1867): the parsed
// file's `@name=value` definitions over the selected environment's variables,
// with the process environment closing the chain inside httpclient. A
// malformed environment file is an error, not a silent miss — sending a
// request with unresolved variables because of a JSON typo would be worse
// than not sending it.
func (m Model) httpVars(source string, f *httpfile.File) (vars *httpfile.Vars, hint string, err error) {
	vars = &httpfile.Vars{File: f.VarMap()}
	dir := filepath.Dir(source)
	envs, err := httpfile.LoadEnvironments(dir)
	if err != nil {
		return vars, "", err
	}
	name := m.httpEnvName(dir, envs)
	vars.Env = envs.Vars(name)
	if name == "" && envs.Len() > 1 {
		// Nothing chosen yet while a choice exists: an unresolved variable is
		// most likely waiting in one of these, so say so where it shows.
		hint = "no environment selected (" + strings.Join(envs.Names(), ", ") +
			") — choose one with http.selectEnvironment"
	}
	return vars, hint, nil
}

// httpEnvMode is the palette Mode listing the environments of one directory.
// The model fills it before each locked open (the layoutsMode pattern, #1175):
// which file is meant is model state.
type httpEnvMode struct {
	dir      string
	names    []string
	selected string
}

func newHTTPEnvMode() *httpEnvMode { return &httpEnvMode{} }

// Prefix implements palette.Mode.
func (h *httpEnvMode) Prefix() rune { return httpEnvPrefix }

// Placeholder implements palette.Mode.
func (h *httpEnvMode) Placeholder() string { return "Use HTTP environment…" }

// Results implements palette.Mode: one row per environment, the active one
// badged, plus a row that clears the selection again.
func (h *httpEnvMode) Results(query string, _ palette.Context) []palette.Item {
	var items []palette.Item
	add := func(title, detail, name string) {
		res, ok := fuzzy.Match(query, title)
		if !ok {
			return
		}
		it := palette.Item{
			Title:  title,
			Spans:  res.Positions,
			Score:  res.Score,
			Detail: detail,
			Msg:    SelectHTTPEnvMsg{Dir: h.dir, Name: name},
		}
		if name == h.selected {
			it.Badge = "●"
		}
		items = append(items, it)
	}
	for _, name := range h.names {
		add(name, httpfile.EnvFileName, name)
	}
	if h.selected != "" {
		add("(no environment)", "resolve without environment variables", "")
	}
	return items
}

// openHTTPEnvPicker opens the palette locked to the environment mode
// (http.selectEnvironment). Nothing to list explains itself instead of showing
// an empty palette, like the stored-requests picker does (#1829).
func (m *Model) openHTTPEnvPicker() {
	source := m.httpPickerSource()
	if source == "" {
		m.host.Notify(host.Info, "http: focus an .http file first")
		return
	}
	dir := filepath.Dir(source)
	envs, err := httpfile.LoadEnvironments(dir)
	if err != nil {
		m.host.Notify(host.Error, "http: "+err.Error())
		return
	}
	if envs.Len() == 0 {
		m.host.Notify(host.Info, "http: no "+httpfile.EnvFileName+" next to "+
			filepath.Base(source)+" — define variables with @name=value in the file instead")
		return
	}
	m.httpEnvs.dir = dir
	m.httpEnvs.names = envs.Names()
	m.httpEnvs.selected = m.httpEnvName(dir, envs)
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, httpEnvPrefix)
}

// selectHTTPEnv persists a picked environment (#1867). It takes effect on the
// next dispatch; a response already shown was answered by the old one, and
// re-sending it (#1832) repeats it verbatim regardless.
func (m *Model) selectHTTPEnv(msg SelectHTTPEnvMsg) {
	m.httpEnv.Set(msg.Dir, msg.Name)
	if msg.Name == "" {
		m.host.Notify(host.Info, "http: environment cleared for "+displayPath(msg.Dir))
		return
	}
	m.host.Notify(host.Info, fmt.Sprintf("http: environment %q selected for %s", msg.Name, displayPath(msg.Dir)))
}
