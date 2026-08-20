package app

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"ike/internal/concealexplain"
	"ike/internal/config"
	"ike/internal/editor"
	"ike/internal/host"
)

// conceal_rule.go persists the rules made in the editor's conceal explain
// popover (#1998). The popover resolves *why* a value draws the way it does;
// this is the other half — writing the correction into the store the
// heuristics already read, so the same field classifies correctly from the
// next parse on and the entry shows up in the Settings UI (#1685's
// editor.number_hint_units, #1712's editor.secret_masking_keys) exactly like a
// hand-written one. No parallel store, no popover-only state.

// writeConcealRule adds (or replaces) the entry the popover produced. Both
// stores resolve a key by first match, so an entry already covering the same
// pattern is replaced in place rather than appended behind — otherwise a
// reclassification would be shadowed by the rule it was meant to correct. The
// reload that follows re-installs the mapping and re-parses the open editors
// through the ordinary ConfigReloadedMsg path.
func (m Model) writeConcealRule(msg editor.ConcealRuleMsg) tea.Cmd {
	if msg.Setting != concealexplain.UnitsSetting && msg.Setting != concealexplain.SecretSetting {
		return nil
	}
	next, changed := applyConcealRule(concealRuleList(msg.Setting), msg.Entry, msg.Pattern)
	if !changed {
		m.host.Notify(host.Info, "rule already set: "+msg.Entry)
		return nil
	}
	m.host.Notify(host.Info, msg.Note)
	return config.WriteAndReload(m.cfgOpts, config.DefaultScope(msg.Setting), msg.Setting, next)
}

// concealRuleList reads the live value of one of the two stores.
func concealRuleList(setting string) []string {
	c := config.Get()
	if c == nil {
		return nil
	}
	switch setting {
	case concealexplain.UnitsSetting:
		return c.Editor.NumberHintUnits
	case concealexplain.SecretSetting:
		return c.Editor.SecretMaskingKeys
	}
	return nil
}

// applyConcealRule puts entry into list: replacing the first entry whose
// pattern is the same one (so the new reading wins where the old one did),
// appending otherwise. It reports whether the list changed at all — repeating
// a rule that is already in force writes nothing.
func applyConcealRule(list []string, entry, pattern string) ([]string, bool) {
	out := slices.Clone(list)
	for i, e := range out {
		if concealexplain.EntryPattern(e) != pattern {
			continue
		}
		if e == entry {
			return out, false
		}
		out[i] = entry
		return out, true
	}
	return append(out, entry), true
}
