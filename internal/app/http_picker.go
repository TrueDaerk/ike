package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/httpfile"
	"ike/internal/httphistory"
	"ike/internal/palette"
)

// http_picker.go makes switching the response pane to another request's stored
// history discoverable (#1829): "r" in the focused viewer lists the requests of
// the associated .http file that have responses under .ike/http/ (#1251), and
// the chosen row loads that history exactly like http.showResponse does
// (#1492) — no detour through the editor, no palette command to know about.

// httpRequestsPrefix selects the stored-requests mode inside the palette; the
// root model only opens it locked, so the rune has no user-facing prefix story.
const httpRequestsPrefix = '|'

// ShowStoredHTTPResponseMsg loads the stored responses of one request into the
// viewer — the picker's activation message.
type ShowStoredHTTPResponseMsg struct {
	Source  string // .http file the request belongs to
	Request string // httpfile request key
}

// storedRequest is one row of the picker: a request of the source file that
// has stored responses.
type storedRequest struct {
	Key   string    // httpfile request key (### name or index)
	Label string    // "GET /users", the request's own line
	N     int       // stored responses
	Last  time.Time // newest stored response
}

// httpRequestsMode is the palette Mode listing storedRequest rows. The model
// fills source/entries before each locked open (the layoutsMode pattern,
// #1175): enumerating them needs the .http file's text, which is model state.
type httpRequestsMode struct {
	source  string
	entries []storedRequest
}

func newHTTPRequestsMode() *httpRequestsMode { return &httpRequestsMode{} }

// Prefix implements palette.Mode.
func (h *httpRequestsMode) Prefix() rune { return httpRequestsPrefix }

// Placeholder implements palette.Mode.
func (h *httpRequestsMode) Placeholder() string { return "Show stored responses of…" }

// Results implements palette.Mode: the enumerated requests, fuzzy-matched over
// their key, each row carrying how many responses are stored and how old the
// newest one is.
func (h *httpRequestsMode) Results(query string, _ palette.Context) []palette.Item {
	var items []palette.Item
	for _, e := range h.entries {
		res, ok := fuzzy.Match(query, e.Key)
		if !ok {
			continue
		}
		it := palette.Item{
			Title:  e.Key,
			Spans:  res.Positions,
			Score:  res.Score,
			Detail: fmt.Sprintf("%s · %d stored", e.Label, e.N),
			Msg:    ShowStoredHTTPResponseMsg{Source: h.source, Request: e.Key},
		}
		if !e.Last.IsZero() {
			it.Time = e.Last.Local().Format("2006-01-02 15:04:05")
		}
		items = append(items, it)
	}
	return items
}

// httpPickerSource resolves which .http file the picker lists: the one the
// shown response came from, falling back to the focused editor's file — right
// after a restart the pane is empty but the user is sitting in the requests.
func (m Model) httpPickerSource() string {
	if p := m.httpPanel(); p != nil && p.Source() != "" {
		return p.Source()
	}
	if ed := m.activeEditor(); ed != nil && ed.HasFile() && isHTTPPath(ed.Path()) {
		return ed.Path()
	}
	return ""
}

// httpSourceText returns the current text of a .http file: the open buffer
// when it has one (unsaved request edits count), the file on disk otherwise.
func (m Model) httpSourceText(path string) string {
	if ed := m.editorForPath(path); ed != nil {
		return ed.Text()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// storedHTTPRequests enumerates the requests of source that have stored
// responses, in file order. Requests never dispatched are left out — the
// picker lists what can actually be shown.
func (m Model) storedHTTPRequests(source string) []storedRequest {
	text := m.httpSourceText(source)
	if text == "" {
		return nil
	}
	store := httphistory.New(httpHistoryDir())
	var out []storedRequest
	seen := map[string]bool{}
	for _, req := range httpfile.Parse(text).Requests {
		key := req.Key()
		if seen[key] {
			continue
		}
		seen[key] = true
		entries := store.List(source, key)
		if len(entries) == 0 {
			continue
		}
		out = append(out, storedRequest{
			Key:   key,
			Label: requestLabel(req),
			N:     len(entries),
			Last:  entries[0].Time,
		})
	}
	return out
}

// openHTTPRequestPicker opens the palette locked to the stored-requests mode
// ("r" in the response pane, #1829). Nothing to list explains itself instead
// of showing an empty palette, like the layouts picker does.
func (m *Model) openHTTPRequestPicker() {
	source := m.httpPickerSource()
	if source == "" {
		m.host.Notify(host.Info, "http: no .http file for this pane — run a request first")
		return
	}
	entries := m.storedHTTPRequests(source)
	if len(entries) == 0 {
		m.host.Notify(host.Info, "http: no stored responses in "+filepath.Base(source)+" — dispatch a request with http.run")
		return
	}
	m.httpRequests.source = source
	m.httpRequests.entries = entries
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(m.paletteContext(), httpRequestsPrefix)
}
