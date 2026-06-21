package palette

import (
	"sort"

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
	hideOff bool // drop off-context commands instead of ranking them last
	prefix  rune
}

// NewCommandMode builds the ":" mode. When hideOff is true, commands scoped to a
// different context than the focused pane are omitted rather than ranked last.
func NewCommandMode(src CommandSource, res BindingResolver, hideOff bool) *CommandMode {
	return &CommandMode{src: src, res: res, hideOff: hideOff, prefix: ':'}
}

// Prefix implements Mode.
func (c *CommandMode) Prefix() rune { return c.prefix }

// Placeholder implements Mode.
func (c *CommandMode) Placeholder() string { return "Run a command…" }

// rankedCommand is a command with its computed tier and match for sorting.
type rankedCommand struct {
	item Item
	tier int
}

// Results implements Mode. It snapshots all registered commands, keeps those
// whose Title (or id) fuzzy-matches the query, and orders them by (tier, score,
// title). An empty query lists every command in tier/title order.
func (c *CommandMode) Results(query string, cx Context) []Item {
	cmds := c.src.Commands()
	ranked := make([]rankedCommand, 0, len(cmds))
	for _, cmd := range cmds {
		tier := c.tier(cmd, cx.ContextID)
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
		ranked = append(ranked, rankedCommand{
			tier: tier,
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
		if ranked[i].item.Score != ranked[j].item.Score {
			return ranked[i].item.Score > ranked[j].item.Score
		}
		return ranked[i].item.Title < ranked[j].item.Title
	})
	out := make([]Item, len(ranked))
	for i, r := range ranked {
		out[i] = r.item
	}
	return out
}

// tier classifies a command's scope against the focused context id.
func (c *CommandMode) tier(cmd registry.OwnedCommand, ctxID string) int {
	switch {
	case cmd.Scope.ContextID != "" && cmd.Scope.ContextID == ctxID:
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
