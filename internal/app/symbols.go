package app

import (
	"sort"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/fuzzy"
	ilsp "ike/internal/lsp"
	"ike/internal/palette"
)

// symbols.go is the live workspace-symbol palette mode (0250 phase 2, #295):
// project.goToClass (cmd+o / leader S) opens the palette locked to this mode,
// every settled keystroke re-queries workspace/symbol through the palette's
// debounced live plumbing, and the same mode holds the search-everywhere seat
// (#236). It replaces the phase-1 floating prompt as the cmd+o front end.

// symbolsPrefix selects the symbol mode inside the palette. Only ever opened
// locked (cmd+o) or composed (search everywhere), so the rune just has to be
// unique among modes.
const symbolsPrefix = '$'

// symbolMode caches the newest workspace/symbol hits as palette rows and
// re-queries them through the bridge continuation the LSP plugin installs.
type symbolMode struct {
	items []palette.Item
	// request is the bridge's workspace-symbol continuation (#294); nil until
	// project.goToClass ran once (the app primes it eagerly for search
	// everywhere).
	request  func(query string) tea.Cmd
	lastSent string
}

// SetRequest installs (or replaces) the bridge continuation.
func (s *symbolMode) SetRequest(f func(string) tea.Cmd) { s.request = f }

// SetHits caches fresh workspace/symbol results as palette rows. Replies for
// a query that is no longer the latest sent are dropped — out-of-order LSP
// responses must not overwrite newer rows.
func (s *symbolMode) SetHits(query string, hits []ilsp.SymbolHit) {
	if query != s.lastSent {
		return
	}
	s.items = make([]palette.Item, len(hits))
	for i, h := range hits {
		detail := displayPath(h.Ref.Path) + ":" + strconv.Itoa(h.Ref.Line+1)
		if p := h.Ref.Preview; p != "" {
			if runes := []rune(p); len(runes) > previewMax {
				p = string(runes[:previewMax-1]) + "…"
			}
			detail += "  " + p
		}
		s.items[i] = palette.Item{
			Title:  h.Name,
			Detail: detail,
			Msg:    ilsp.DefinitionMsg{Path: h.Ref.Path, Line: h.Ref.Line, Col: h.Ref.Col},
		}
	}
}

// Prefix implements palette.Mode.
func (s *symbolMode) Prefix() rune { return symbolsPrefix }

// Placeholder implements palette.Mode.
func (s *symbolMode) Placeholder() string { return "Go to symbol — type to search the workspace…" }

// Results implements palette.Mode: the cached hits fuzzy-ranked on the symbol
// name (falling back to the location detail), like the references rows. The
// server already filtered for the settled query; the local match keeps the
// rows responsive between debounce ticks and supplies highlight spans.
func (s *symbolMode) Results(query string, cx palette.Context) []palette.Item {
	type scored struct {
		item  palette.Item
		score int
	}
	var out []scored
	for _, it := range s.items {
		if m, ok := fuzzy.Match(query, it.Title); ok {
			it.Spans = m.Positions
			out = append(out, scored{item: it, score: m.Score})
			continue
		}
		if m, ok := fuzzy.Match(query, it.Detail); ok {
			it.Spans = nil
			out = append(out, scored{item: it, score: m.Score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	items := make([]palette.Item, len(out))
	for i, sc := range out {
		items[i] = sc.item
	}
	return items
}

// QueryChanged implements palette.LiveMode: a settled query re-queries the
// workspace through the bridge continuation. Without one (no LSP plugin, or
// goToClass never primed) the mode stays a static cache.
func (s *symbolMode) QueryChanged(query string, cx palette.Context) tea.Cmd {
	if s.request == nil || query == "" {
		return nil
	}
	s.lastSent = query
	return s.request(query)
}
