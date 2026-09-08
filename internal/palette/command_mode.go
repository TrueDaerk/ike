package palette

import (
	"sort"
	"strings"

	"ike/internal/fuzzy"
	"ike/internal/registry"
)

// CommandSource is the registry seam command mode reads. It never caches its own
// command list beyond the per-call snapshot it takes here, so the registry stays
// the single source of truth. *registry.Registry satisfies it.
type CommandSource interface {
	Commands() []registry.OwnedCommand
}

// BindingResolver reports the shortcut bound to a command, shown as the dim
// detail. *registry.Registry satisfies it.
type BindingResolver interface {
	Binding(id string) (string, bool)
}

// BindingTitler is an optional BindingResolver extension (#2548): the label
// the keymap table gives a command's default binding ("Last edit location",
// "Go to declaration"), which often names the action the way a user would
// where the command Title does not. The "did you mean" tier matches it.
// *keymap.LiveBindings satisfies it.
type BindingTitler interface {
	BindingTitle(id string) (string, bool)
}

// DidYouMeanBelow is the primary-tier size under which the command mode
// appends the "did you mean" tier (#2548): with this many or more title
// matches the user is narrowing a real list; with fewer, the fuzzy title match
// has probably failed to name what they want.
const DidYouMeanBelow = 5

// didYouMeanMax caps the second tier so a short query cannot flood the list
// with weak alias and menu-path matches.
const didYouMeanMax = 8

// DidYouMeanSeparator is the inert row heading the second tier (#2548).
const DidYouMeanSeparator = "did you mean"

// NoMatchHint is the inert row listed when neither tier matched (#2548), so
// an unknown query lands on a way forward instead of an empty box.
const NoMatchHint = "no command matches — press ? for the cheatsheet"

// relevance tiers rank a command against the focused context: an in-context
// (pane-scoped, matching) command outranks a global one, which outranks an
// off-context one.
const (
	tierContext = iota // pane scope equal to the focused context
	tierGlobal         // global scope, always applicable
	tierOff            // scoped to a different context
)

// CommandMode is the ":" mode: it snapshots the registry, fuzzy-filters by the
// query, and ranks context-first then by match score. It executes nothing — the
// chosen item carries a RunCommandMsg the root model dispatches.
type CommandMode struct {
	src     CommandSource
	res     BindingResolver
	usage   *Usage    // optional most-used ranking (#773); nil-safe
	frec    *Frecency // optional execution-history boost (#2153); nil-safe
	hideOff bool      // drop off-context commands instead of ranking them last
	prefix  rune
	// menuPaths maps a command id to its menu-bar path ("File › Switch
	// Project", #2548), one more surface the "did you mean" tier matches.
	menuPaths map[string]string
}

// NewCommandMode builds the ":" mode. When hideOff is true, commands scoped to a
// different context than the focused pane are omitted rather than ranked last.
func NewCommandMode(src CommandSource, res BindingResolver, hideOff bool) *CommandMode {
	return &CommandMode{src: src, res: res, hideOff: hideOff, prefix: ':'}
}

// SetUsage installs the most-used counter (#773): among equal tier and match
// score — notably the whole listing on an empty query — more-often-chosen
// commands rank first. Match quality still wins over usage.
func (c *CommandMode) SetUsage(u *Usage) { c.usage = u }

// SetFrecency installs the execution-history store (#2153): recently and often
// executed commands get a bonus added to their fuzzy score, strongest on an
// empty query and halved per typed rune, so a longer query's match quality
// dominates and history only breaks near-ties.
func (c *CommandMode) SetFrecency(f *Frecency) { c.frec = f }

// SetMenuPaths installs the menu-bar paths per command id (#2548), built by
// the root model from the menu definitions. The "did you mean" tier matches
// them, so ":switch" surfaces "File › Switch Project".
func (c *CommandMode) SetMenuPaths(paths map[string]string) { c.menuPaths = paths }

// Prefix implements Mode.
func (c *CommandMode) Prefix() rune { return c.prefix }

// Placeholder implements Mode.
func (c *CommandMode) Placeholder() string { return "Run a command…" }

// rankedCommand is a command with its computed tier and match for sorting.
// key is the sort score: the fuzzy match score plus the query-length-damped
// frecency boost (#2153), so history orders the empty-query listing and fades
// to a tiebreaker as the query grows.
type rankedCommand struct {
	item  Item
	tier  int
	key   float64
	usage int
}

// Results implements Mode. It snapshots all registered commands, keeps those
// whose Title (or id) fuzzy-matches the query, and orders them by (tier,
// score+frecency boost, usage, title). An empty query lists every command in
// tier order, most-recently/often-executed first.
//
// When that primary tier comes up short (#2548 — fewer than DidYouMeanBelow
// rows on a non-empty query) a second tier follows under an inert "did you
// mean" separator: the commands whose aliases, binding label, chord text or
// menu path match the query, best match first. The primary tier's ranking is
// never touched by it. With no match in either tier the single row is the
// NoMatchHint, so the list never goes blank.
func (c *CommandMode) Results(query string, cx Context) []Item {
	cmds := c.src.Commands()
	items, listed := c.primary(query, cx, cmds)
	if query == "" || len(items) >= DidYouMeanBelow {
		return items
	}
	if alt := c.didYouMean(query, cx, cmds, listed); len(alt) > 0 {
		items = append(items, Item{Title: DidYouMeanSeparator, Inert: true})
		items = append(items, alt...)
	}
	if len(items) == 0 {
		items = append(items, Item{Title: NoMatchHint, Inert: true})
	}
	return items
}

// PrimaryResults is Results without the second tier and the hint row (#2548):
// the plain title/id ranking a composing mode wants — search everywhere
// interleaves command rows with files and symbols and has no place for a
// command-only separator.
func (c *CommandMode) PrimaryResults(query string, cx Context) []Item {
	items, _ := c.primary(query, cx, c.src.Commands())
	return items
}

// primary ranks the title/id matches and reports the ids it listed.
func (c *CommandMode) primary(query string, cx Context, cmds []registry.OwnedCommand) ([]Item, map[string]bool) {
	listed := make(map[string]bool, len(cmds))
	qlen := len([]rune(query))
	ranked := make([]rankedCommand, 0, len(cmds))
	for _, cmd := range cmds {
		tier := c.tier(cmd, cx)
		if tier == tierOff && c.hideOff {
			continue
		}
		m, ok := fuzzy.Match(query, cmd.Title)
		if !ok {
			// Fall back to matching the id so ":hello" finds "example.hello"; its
			// spans index the id, not the Title, so they are dropped (no highlight).
			im, idOK := fuzzy.Match(query, cmd.ID)
			if !idOK {
				continue
			}
			m = fuzzy.Result{Score: im.Score} // id match: score only, no Title spans
		}
		listed[cmd.ID] = true
		ranked = append(ranked, rankedCommand{
			tier:  tier,
			key:   float64(m.Score) + frecencyBoost(c.frec.Score(cmd.ID), qlen),
			usage: c.usage.Count(cmd.ID),
			item: Item{
				Title:  cmd.Title,
				Detail: c.detail(cmd),
				Spans:  m.Positions,
				Score:  m.Score,
				Msg:    RunCommandMsg{ID: cmd.ID},
			},
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].tier != ranked[j].tier {
			return ranked[i].tier < ranked[j].tier
		}
		if ranked[i].key != ranked[j].key {
			return ranked[i].key > ranked[j].key
		}
		if ranked[i].usage != ranked[j].usage {
			return ranked[i].usage > ranked[j].usage
		}
		return ranked[i].item.Title < ranked[j].item.Title
	})
	out := make([]Item, len(ranked))
	for i, r := range ranked {
		out[i] = r.item
	}
	return out, listed
}

// didYouMean builds the second tier (#2548): every command the primary tier
// skipped whose alternate surfaces — Aliases, the keymap's binding label, the
// resolved chord text, the menu-bar path — fuzzy-match the query. The best
// surface's score ranks the row; the surface itself is shown as the row's
// badge, so the user sees why a title that says nothing of the sort was
// suggested. Context tiers still break score ties, and hideOff still drops
// off-context commands, exactly as in the primary tier.
func (c *CommandMode) didYouMean(query string, cx Context, cmds []registry.OwnedCommand, listed map[string]bool) []Item {
	var ranked []rankedCommand
	for _, cmd := range cmds {
		if listed[cmd.ID] {
			continue
		}
		tier := c.tier(cmd, cx)
		if tier == tierOff && c.hideOff {
			continue
		}
		score, via, ok := c.bestSurface(query, cmd)
		if !ok {
			continue
		}
		ranked = append(ranked, rankedCommand{
			tier: tier,
			key:  float64(score),
			item: Item{
				Title:  cmd.Title,
				Detail: c.detail(cmd),
				Badge:  via,
				Score:  score,
				Msg:    RunCommandMsg{ID: cmd.ID},
			},
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].key != ranked[j].key {
			return ranked[i].key > ranked[j].key
		}
		if ranked[i].tier != ranked[j].tier {
			return ranked[i].tier < ranked[j].tier
		}
		return ranked[i].item.Title < ranked[j].item.Title
	})
	if len(ranked) > didYouMeanMax {
		ranked = ranked[:didYouMeanMax]
	}
	out := make([]Item, len(ranked))
	for i, r := range ranked {
		out[i] = r.item
	}
	return out
}

// bestSurface fuzzy-matches the query against a command's alternate surfaces
// and returns the strongest score with the surface text that produced it.
func (c *CommandMode) bestSurface(query string, cmd registry.OwnedCommand) (score int, via string, ok bool) {
	for _, s := range c.surfaces(cmd) {
		if s == "" {
			continue
		}
		m, matched := fuzzy.Match(query, s)
		if !matched {
			continue
		}
		if !ok || m.Score > score {
			score, via, ok = m.Score, s, true
		}
	}
	return score, via, ok
}

// surfaces lists a command's alternate match surfaces in display priority:
// its aliases, the keymap's label for its binding, the chord text, its
// documentation-only shortcut and its menu-bar path.
func (c *CommandMode) surfaces(cmd registry.OwnedCommand) []string {
	out := make([]string, 0, len(cmd.Aliases)+4)
	for _, a := range cmd.Aliases {
		out = append(out, strings.TrimSpace(a))
	}
	if t, isTitler := c.res.(BindingTitler); isTitler {
		if title, found := t.BindingTitle(cmd.ID); found {
			out = append(out, title)
		}
	}
	if c.res != nil {
		if key, found := c.res.Binding(cmd.ID); found {
			out = append(out, key)
		}
	}
	if cmd.Shortcut != "" {
		out = append(out, cmd.Shortcut)
	}
	if path, found := c.menuPaths[cmd.ID]; found {
		out = append(out, path)
	}
	return out
}

// tier classifies a command's scope against the focused context id and buffer
// language. A file-type-gated command (#2483) whose gate does not list the
// focused buffer's language ranks off-context — the same applicability the
// help overlay shows — so a jq command sinks (or, with hideOff, drops) while a
// Go file is focused, and surfaces over JSON.
func (c *CommandMode) tier(cmd registry.OwnedCommand, cx Context) int {
	if !cmd.AppliesToLang(cx.Lang) {
		return tierOff
	}
	switch {
	case cmd.Scope.ContextID != "" && cmd.Scope.ContextID == cx.ContextID:
		return tierContext
	case cmd.Scope.Global:
		return tierGlobal
	default:
		return tierOff
	}
}

// detail returns the dim suffix for a command: its resolved key binding, else
// its documentation-only shortcut hint, else its owner.
func (c *CommandMode) detail(cmd registry.OwnedCommand) string {
	if c.res != nil {
		if key, ok := c.res.Binding(cmd.ID); ok && key != "" {
			return key
		}
	}
	if cmd.Shortcut != "" {
		return cmd.Shortcut
	}
	return cmd.Owner
}
