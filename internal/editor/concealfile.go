package editor

// concealfile.go holds the per-file gate on the conceal families (#1704): the
// file-pattern dimension that sits beside their on/off toggles.
//
// The compiled filter lives on the view (concealRules), refreshed by
// applyConfig from editor.conceal_include / editor.conceal_exclude /
// editor.conceal_file_rules. The gate is then applied where a family is
// *read*, not where its toggle is resolved — concealGate(family, on, set) —
// which is what keeps the two dimensions independent: the toggle fields go on
// meaning "this family is on", the filter goes on meaning "here", and neither
// can strand the other in a state it cannot come back from.
//
// Two rules fall out of that. A stand-in draws only if its family is on *and*
// the path passes the filter. But an explicit per-view toggle bypasses the
// filter (the `set` flag): a pattern list states a default, not a prohibition,
// and a user who asks for masking in *this* buffer should get it.
//
// Because applyConfig runs on every routed message, an edited pattern list —
// or a buffer that changed its path — takes effect on the next frame, with no
// reload.

import (
	"strings"

	"ike/internal/concealfilter"
	"ike/internal/host"
)

// refreshConcealRules recompiles the file filter when its three settings
// changed. The joined raw values are the cache key: applyConfig runs on every
// Update, and re-parsing three lists per keystroke to almost always get the
// same filter back is the kind of waste that shows up in a profile.
func (m *Model) refreshConcealRules() {
	raw := [3]string{
		stringOr(m.cfg, "editor.conceal_include"),
		stringOr(m.cfg, "editor.conceal_exclude"),
		stringOr(m.cfg, "editor.conceal_file_rules"),
	}
	if raw == m.concealRaw {
		return // includes the initial all-empty case: the zero Rules allow everything
	}
	m.concealRaw = raw
	m.concealRules = concealfilter.Compile(splitList(raw[0]), splitList(raw[1]), splitList(raw[2]))
}

// concealGate composes a conceal family's two dimensions: on is its toggle
// (config default or per-view override) and set marks that override. An
// overridden family answers its toggle alone; otherwise the file filter has
// to agree.
func (m Model) concealGate(family string, on, set bool) bool {
	if !on || set {
		return on
	}
	return m.concealAllows(family)
}

// concealAllows reports whether the named conceal family may draw in this
// buffer's file. A view with no filter configured (the default) answers yes
// without touching the path.
func (m Model) concealAllows(family string) bool {
	if m.concealRules.Empty() {
		return true
	}
	return m.concealRules.Allows(family, m.langPath())
}

// The rendering-layer gates. The stand-in families all funnel through
// decodeOn (escapes.go); these four are read directly by their renderers, so
// each gets a named accessor that carries the filter.

// mdRenderOn reports whether the Markdown rendering layer draws here (#1599).
func (m Model) mdRenderOn() bool {
	return m.concealGate(concealfilter.MarkdownRendering, m.mdRender, m.mdRenderSet)
}

// svRenderOn reports whether the separator-table layer draws here (#1589).
// editor.csv_rendering has no per-view toggle, so the filter always applies.
func (m Model) svRenderOn() bool {
	return m.concealGate(concealfilter.CSVRendering, m.svRender, false)
}

// logRenderOn reports whether the log rendering layer draws here (#1621).
func (m Model) logRenderOn() bool {
	return m.concealGate(concealfilter.LogRendering, m.logRender, m.logRenderSet)
}

// pemSummaryOn reports whether PEM blocks collapse here (#1652).
func (m Model) pemSummaryOn() bool {
	return m.concealGate(concealfilter.PemSummary, m.pemSummary, m.pemSummarySet)
}

// stringOr reads a config key, defaulting to "" when it is unset.
func stringOr(cfg host.Config, key string) string {
	v, _ := cfg.Get(key)
	return v
}

// splitList splits a config list value, which the config layer hands over
// comma-joined. Empty entries are dropped by the filter itself.
func splitList(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}
