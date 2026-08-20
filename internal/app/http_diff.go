package app

import (
	"fmt"
	"time"

	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/httpdiff"
	"ike/internal/httphistory"
	"ike/internal/palette"
)

// http_diff.go compares two stored responses of one request side by side
// (#1992): the scroll lock (#1493) only made *manual* comparison bearable,
// while the actual job — "what changed between these two runs?" — belongs to
// the diff viewer. "D" in the focused response pane (or the palette command
// http.diffResponses) lists the request's other stored responses; choosing
// one opens the shown entry against it in the reusable diff pane, both sides
// rendered by internal/httpdiff so JSON key order and formatting do not drown
// the real differences.

// httpEntriesPrefix selects the history-entry picker inside the palette. Like
// the stored-requests ('|') and environment ('?') pickers it is only ever
// opened locked, so the rune has no user-facing prefix story.
const httpEntriesPrefix = '{'

// DiffHTTPEntriesMsg opens the diff of two stored responses — the picker's
// activation message. Shown is the entry the pane displays, Other the chosen
// one; both index the store's newest-first list.
type DiffHTTPEntriesMsg struct {
	Source  string // .http file the request belongs to
	Request string // httpfile request key
	Shown   int
	Other   int
}

// storedEntry is one row of the picker: a stored response of the request,
// identified by its position in the newest-first history.
type storedEntry struct {
	Idx    int
	Label  string    // "2/5 · 200 OK"
	Detail string    // proto, size, capture count
	At     time.Time // when it was stored
}

// httpEntriesMode is the palette Mode listing storedEntry rows. Like the
// stored-requests mode it is filled before each locked open — which entries
// exist is model state, not something the mode can enumerate itself.
type httpEntriesMode struct {
	source  string
	request string
	shown   int
	entries []storedEntry
}

func newHTTPEntriesMode() *httpEntriesMode { return &httpEntriesMode{} }

// Prefix implements palette.Mode.
func (h *httpEntriesMode) Prefix() rune { return httpEntriesPrefix }

// Placeholder implements palette.Mode.
func (h *httpEntriesMode) Placeholder() string { return "Compare the shown response with…" }

// Results implements palette.Mode: the request's other stored responses,
// fuzzy-matched over their label, each row carrying status and age.
func (h *httpEntriesMode) Results(query string, _ palette.Context) []palette.Item {
	var items []palette.Item
	for _, e := range h.entries {
		res, ok := fuzzy.Match(query, e.Label)
		if !ok {
			continue
		}
		it := palette.Item{
			Title:  e.Label,
			Spans:  res.Positions,
			Score:  res.Score,
			Detail: e.Detail,
			Msg: DiffHTTPEntriesMsg{
				Source: h.source, Request: h.request, Shown: h.shown, Other: e.Idx,
			},
		}
		if !e.At.IsZero() {
			it.Time = e.At.Local().Format("2006-01-02 15:04:05")
		}
		items = append(items, it)
	}
	return items
}

// storedHTTPEntries describes the stored responses of one request for the
// picker, skipping the entry the pane already shows — comparing a response
// with itself is never the question.
func storedHTTPEntries(entries []httphistory.Entry, shown int) []storedEntry {
	out := make([]storedEntry, 0, len(entries))
	for i, e := range entries {
		if i == shown {
			continue
		}
		out = append(out, storedEntry{
			Idx:    i,
			Label:  fmt.Sprintf("%d/%d · %s", i+1, len(entries), e.Status),
			Detail: fmt.Sprintf("%s · %d bytes", e.Proto, len(e.Body)),
			At:     e.Time,
		})
	}
	return out
}

// openHTTPResponseDiff opens the entry picker for the response on show — "D"
// in the pane and the http.diffResponses command share it. Everything that
// makes the comparison impossible (no pane, no source file, a single stored
// response) explains itself instead of opening an empty picker.
func (m *Model) openHTTPResponseDiff() {
	p := m.httpPanel()
	if p == nil {
		m.host.Notify(host.Info, "http: no response pane open — run an .http request, or show a stored one with http.showResponse")
		return
	}
	source, key := p.Source(), p.Request()
	if source == "" || key == "" {
		m.host.Notify(host.Info, "http: no stored response to compare — run a request first")
		return
	}
	shown, _ := p.HistoryIndex()
	entries := httphistory.New(httpHistoryDir()).List(source, key)
	if len(entries) < 2 {
		m.host.Notify(host.Info, "http: only one stored response for "+key+" — nothing to compare it with")
		return
	}
	if shown < 0 || shown >= len(entries) {
		shown = 0
	}
	m.httpEntries.source = source
	m.httpEntries.request = key
	m.httpEntries.shown = shown
	m.httpEntries.entries = storedHTTPEntries(entries, shown)
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, httpEntriesPrefix)
}

// diffHTTPEntries opens the chosen pair in the reusable diff pane (#1992):
// the older response on the left, the newer on the right — the reading
// direction of every other diff in the IDE — with both sides normalized by
// internal/httpdiff. The pane is read-only: neither side is a file.
func (m *Model) diffHTTPEntries(msg DiffHTTPEntriesMsg) {
	entries := httphistory.New(httpHistoryDir()).List(msg.Source, msg.Request)
	a, b := msg.Shown, msg.Other
	if !validHTTPEntryPair(entries, a, b) {
		// The history was pruned or rewritten between opening the picker and
		// choosing a row — say so rather than diffing the wrong pair.
		m.host.Notify(host.Info, "http: those stored responses are gone — reopen the response and try again")
		return
	}
	// The list is newest first, so the higher index is the older response.
	older, newer := a, b
	if older < newer {
		older, newer = newer, older
	}
	left, right := entries[older], entries[newer]
	m.openDiffTexts("",
		httpEntryTitle(msg.Request, older, len(entries), left),
		httpEntryTitle(msg.Request, newer, len(entries), right),
		httpdiff.Text(left), httpdiff.Text(right), false)
	m.host.Notify(host.Info, fmt.Sprintf("http: comparing stored responses %d and %d of %s", older+1, newer+1, msg.Request))
}

// validHTTPEntryPair reports whether both indexes still address distinct
// stored responses.
func validHTTPEntryPair(entries []httphistory.Entry, a, b int) bool {
	if a == b {
		return false
	}
	inRange := func(i int) bool { return i >= 0 && i < len(entries) }
	return inRange(a) && inRange(b)
}

// httpEntryTitle labels one diff column, e.g. "list-users 3/5 @ 15:04:05".
func httpEntryTitle(key string, idx, n int, e httphistory.Entry) string {
	s := fmt.Sprintf("%s %d/%d", key, idx+1, n)
	if !e.Time.IsZero() {
		s += " @ " + e.Time.Local().Format("15:04:05")
	}
	return s
}
