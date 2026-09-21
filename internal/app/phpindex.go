package app

// phpindex.go wires the workspace-wide PHP declaration index (Epic 0520,
// #2667) into the app: one index per project root, built beside the
// completion sources in buildModel, reconfigured live from the [php] section
// on every config reload, and exposed to the trait features (completion
// #2668, diagnostics #2669, navigation #2670, references #2671, rename
// #2672, status #2673) through PHPIndex.

import (
	"strconv"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/phpindex"
)

// PHPIndexChangedMsg says the PHP declaration index reached a new content
// generation (#2669): the initial scan finished, or a re-extract changed a
// trait or one of its consumers. The root model answers it by re-running the
// diagnostic filter over every cached raw set, so a trait marker disappears
// once the index is warm and comes back when a consumer loses the member.
// The index debounces the notification, so one keystroke cannot refilter the
// world.
type PHPIndexChangedMsg struct{}

// PHPIndex returns the project's PHP declaration index (never nil; a
// disabled index answers nothing and reports Enabled false).
func (m Model) PHPIndex() *phpindex.Index { return m.phpIndex }

// phpOptionsFrom reads the [php] keys off the flat host config the model is
// built from — the typed global may not be published yet at construction —
// falling back to the schema defaults for a missing or malformed value.
func phpOptionsFrom(cfg host.Config) phpindex.Options {
	def := phpindex.FromConfig(config.Get().PHP)
	if cfg == nil {
		return def
	}
	if v, ok := cfg.Get("php.trait_index"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			def.Enabled = b
		}
	}
	if v, ok := cfg.Get("php.index.include_vendor"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			def.IncludeVendor = b
		}
	}
	if v, ok := cfg.Get("php.index.parent_depth"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			def.ParentDepth = n
		}
	}
	if v, ok := cfg.Get("php.index.max_files"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			def.MaxFiles = n
		}
	}
	return def
}

// reconfigurePHPIndex applies a reloaded [php] section live: the master
// switch drops or rebuilds the index, a changed walk bound rescans, a
// changed parent depth refreshes the derived scope.
func (m *Model) reconfigurePHPIndex(cfg *config.Config) {
	if m.phpIndex == nil || cfg == nil {
		return
	}
	m.phpIndex.Reconfigure(phpindex.FromConfig(cfg.PHP))
}
