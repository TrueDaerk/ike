package app

// marketupdates.go is the automatic plugin-update check (#2257): on start,
// compare the installed plugin sidecars against the marketplace catalog and
// announce how many updates are waiting. Everything expensive (one HTTPS
// fetch plus a directory scan) happens inside the returned tea.Cmd, so the
// UI never blocks on it. The check is rate-limited through a persisted
// timestamp (market.CheckState) and silent on failure — a missing network is
// the normal state of a laptop, not something worth a toast.

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/market"
	"ike/internal/settings"
)

// MarketUpdateCheckMsg carries the finished startup update check. A failed
// fetch is dropped in the command, so this message only ever describes a
// successful one.
type MarketUpdateCheckMsg struct {
	Index   market.Index
	Diags   []string
	Updates []market.Update
}

// marketUpdateCheckCmd returns the startup check, or nil when it must not run:
// the setting is off, no catalog is configured, or the rate limit has not
// elapsed since the last successful check.
func (m Model) marketUpdateCheckCmd() tea.Cmd {
	cfg := config.Get()
	if cfg == nil || !cfg.Marketplace.AutoCheck {
		return nil
	}
	url := cfg.Marketplace.CatalogURL
	if url == "" {
		url = market.DefaultCatalogURL
	}
	if url == "" || m.marketFetch == nil || m.marketEngine == nil {
		return nil
	}
	statePath := market.StatePath()
	if !market.LoadCheckState(statePath).Due(time.Now(), market.DefaultCheckInterval) {
		return nil
	}
	fetch, engine := m.marketFetch, m.marketEngine
	return func() tea.Msg {
		idx, diags, err := fetch(context.Background(), url)
		if err != nil {
			// Silent: an unreachable catalog leaves the timestamp untouched,
			// so the next start tries again instead of waiting a day.
			return nil
		}
		installed, err := engine.Installed()
		if err != nil {
			return nil
		}
		// Only a check that actually completed spends the rate-limit budget.
		_ = market.SaveCheckState(statePath, market.CheckState{LastCheck: time.Now()})
		return MarketUpdateCheckMsg{
			Index:   idx,
			Diags:   diags,
			Updates: market.FindUpdates(idx, installed),
		}
	}
}

// handleMarketUpdateCheck feeds the checked catalog into the marketplace page
// (so opening it shows the badges without re-fetching) and raises the single
// summary notification. Nothing to update means nothing to say.
func (m Model) handleMarketUpdateCheck(msg MarketUpdateCheckMsg) (tea.Model, tea.Cmd) {
	cmd := m.settings.Deliver(settings.MarketCatalogMsg{Index: msg.Index, Diags: msg.Diags})
	if n := len(msg.Updates); n > 0 {
		m.host.Notify(host.Info, "marketplace: "+plural(n, "plugin update is", "plugin updates are")+
			" available — Settings ▸ Marketplace")
	}
	return m, cmd
}
