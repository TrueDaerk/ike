package app

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

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

// Ranking tiers (#377). Servers like gopls return dependency and stdlib
// symbols alongside the project's own, and pure fuzzy score let
// `internal/abi` internals bury a project-local exact match. The tier
// offsets dominate any achievable fuzzy score (a few hundred at most):
// an exact name match beats every fuzzy-only match within its tier, and a
// non-project symbol can never outrank a project one. The adjusted score
// is stored on the palette item, so search everywhere (#236) sinks
// stdlib noise below commands and files too.
const (
	symbolExactBonus      = 1 << 12
	symbolNonProjectMalus = 1 << 16
)

// symbolItem is one cached workspace/symbol row plus whether its location is
// inside the project root — the ranking tier of Results.
type symbolItem struct {
	item    palette.Item
	project bool
}

// symbolMode caches the newest workspace/symbol hits as palette rows and
// re-queries them through the bridge continuation the LSP plugin installs.
type symbolMode struct {
	items []symbolItem
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
	s.items = make([]symbolItem, len(hits))
	for i, h := range hits {
		detail := displayPath(h.Ref.Path) + ":" + strconv.Itoa(h.Ref.Line+1)
		if p := h.Ref.Preview; p != "" {
			if runes := []rune(p); len(runes) > previewMax {
				p = string(runes[:previewMax-1]) + "…"
			}
			detail += "  " + p
		}
		s.items[i] = symbolItem{
			item: palette.Item{
				Title:  h.Name,
				Detail: detail,
				Msg:    ilsp.DefinitionMsg{Path: h.Ref.Path, Line: h.Ref.Line, Col: h.Ref.Col},
			},
			project: insideProject(h.Ref.Path),
		}
	}
}

// insideProject reports whether path lies under the project root (the working
// directory) — the same containment test displayPath applies for rendering.
func insideProject(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(cwd, abs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Prefix implements palette.Mode.
func (s *symbolMode) Prefix() rune { return symbolsPrefix }

// Placeholder implements palette.Mode.
func (s *symbolMode) Placeholder() string { return "Go to symbol — type to search the workspace…" }

// Results implements palette.Mode: the cached hits fuzzy-ranked on the symbol
// name (falling back to the location detail), like the references rows. The
// server already filtered for the settled query; the local match keeps the
// rows responsive between debounce ticks and supplies highlight spans. The
// fuzzy score is tier-adjusted (#377): an exact name match earns a bonus and
// any symbol outside the project root sinks below every project one.
func (s *symbolMode) Results(query string, cx palette.Context) []palette.Item {
	var out []palette.Item
	for _, si := range s.items {
		it := si.item
		if m, ok := fuzzy.Match(query, it.Title); ok {
			it.Spans = m.Positions
			it.Score = m.Score
			if strings.EqualFold(query, it.Title) {
				it.Score += symbolExactBonus
			}
		} else if m, ok := fuzzy.Match(query, it.Detail); ok {
			it.Spans = nil
			it.Score = m.Score
		} else {
			continue
		}
		if !si.project {
			it.Score -= symbolNonProjectMalus
		}
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
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
