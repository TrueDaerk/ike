package settings

// marketplace_updates_test.go covers the update-notification half of the
// marketplace page (#2257): row badge, update-all, and the capability-change
// gate that keeps a grown capability list off the batch path.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/market"
)

// marketEntryOther is a second catalog plugin, for update-all.
const marketEntryOther = `{
	"name": "other",
	"version": "2.0.0",
	"description": "second plugin",
	"capabilities": ["commands"],
	"artifact": {
		"url": "https://example.com/other.wasm",
		"sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	}
}`

// TestMarketplaceUpdateBadgeAndCount: an outdated plugin shows the row badge
// and the page counts it in the header.
func TestMarketplaceUpdateBadgeAndCount(t *testing.T) {
	eng := &fakeMarketEngine{installed: map[string]market.Installed{
		"example": {Name: "example", Version: market.Version{Major: 1}, VersionOK: true,
			Capabilities: []string{"commands", "notify"}},
	}}
	p := loadedPage(t, eng)
	if got := p.Updates(); len(got) != 1 || got[0].Name() != "example" || got[0].NeedsConfirm() {
		t.Fatalf("Updates() = %+v", got)
	}
	v := p.View(120, 40)
	if !strings.Contains(v, "update 1.0.0 → 1.2.0") {
		t.Errorf("view lacks the row badge:\n%s", v)
	}
	if !strings.Contains(v, "1 plugin update available") {
		t.Errorf("view lacks the update count:\n%s", v)
	}
	if strings.Contains(v, "new capabilities") {
		t.Errorf("unchanged capability list flagged:\n%s", v)
	}
}

// TestMarketplaceUpdateAll: `u` installs every pending update in one go.
func TestMarketplaceUpdateAll(t *testing.T) {
	eng := &fakeMarketEngine{installed: map[string]market.Installed{
		"example": {Name: "example", Version: market.Version{Major: 1}, VersionOK: true,
			Capabilities: []string{"commands", "notify"}},
		"other": {Name: "other", Version: market.Version{Major: 1, Minor: 5}, VersionOK: true,
			Capabilities: []string{"commands"}},
	}}
	p := NewMarketplacePage(eng, nil)
	p.SetSubPanelHost(&stubHost{})
	p.Receive(MarketCatalogMsg{Index: marketIndex(t, marketEntry+","+marketEntryOther)})
	if got := len(p.Updates()); got != 2 {
		t.Fatalf("updates = %d", got)
	}
	cmd := p.Update(mktKey("u"))
	if cmd == nil {
		t.Fatal("update-all produced no command")
	}
	// The batch resolves to one message per install; feed them all back.
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("want a batch, got %T", cmd())
	}
	for _, c := range batch {
		p.Receive(c())
	}
	if len(eng.installs) != 2 {
		t.Fatalf("installs = %v", eng.installs)
	}
	if got := len(p.Updates()); got != 0 {
		t.Errorf("updates after update-all = %d", got)
	}
}

// TestMarketplaceUpdateAllHoldsCapabilityGrowth: an update asking for a
// capability the installed manifest does not pin is never applied by
// update-all; it stays on the per-plugin confirm path.
func TestMarketplaceUpdateAllHoldsCapabilityGrowth(t *testing.T) {
	eng := &fakeMarketEngine{installed: map[string]market.Installed{
		"example": {Name: "example", Version: market.Version{Major: 1}, VersionOK: true,
			Capabilities: []string{"commands"}}, // the catalog entry adds "notify"
	}}
	p := loadedPage(t, eng)
	if cmd := p.Update(mktKey("u")); cmd != nil {
		t.Fatal("update-all applied a capability change")
	}
	if len(eng.installs) != 0 {
		t.Fatalf("installs = %v", eng.installs)
	}
	v := p.View(120, 40)
	if !strings.Contains(v, "new capabilities") {
		t.Errorf("view lacks the capability-change flag:\n%s", v)
	}
	if !strings.Contains(v, "held back") {
		t.Errorf("view lacks the held-back note:\n%s", v)
	}
}

// TestMarketplaceCapabilityGrowthNeedsConfirm: `i` on such an update asks
// first, and the confirmation names the added capabilities.
func TestMarketplaceCapabilityGrowthNeedsConfirm(t *testing.T) {
	eng := &fakeMarketEngine{installed: map[string]market.Installed{
		"example": {Name: "example", Version: market.Version{Major: 1}, VersionOK: true,
			Capabilities: []string{"commands"}},
	}}
	p := loadedPage(t, eng)
	p.Update(mktKey("enter"))
	if v := p.View(120, 40); !strings.Contains(v, "new capabilities in 1.2.0: notify") {
		t.Errorf("detail lacks the added-capability list:\n%s", v)
	}
	// i pushes the confirmation instead of installing.
	if cmd := p.Update(mktKey("i")); cmd != nil {
		t.Fatal("capability change installed without confirmation")
	}
	if len(eng.installs) != 0 {
		t.Fatalf("installs = %v", eng.installs)
	}
	host := p.host.(*stubHost)
	if c, ok := host.top().(*confirmPanel); !ok || !strings.Contains(c.what, "notify") {
		t.Fatalf("confirmation does not name the new capability: %#v", host.top())
	}
	runCmd(t, p, confirmVia(t, host))
	if len(eng.installs) != 1 {
		t.Fatalf("installs after confirmation = %v", eng.installs)
	}
}
