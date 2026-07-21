package settings

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/market"
)

// fakeMarketEngine records calls and serves a scripted installed map.
type fakeMarketEngine struct {
	installed  map[string]market.Installed
	installErr error
	removeErr  error
	installs   []string
	removes    []string
}

func (f *fakeMarketEngine) Installed() (map[string]market.Installed, error) {
	out := map[string]market.Installed{}
	for k, v := range f.installed {
		out[k] = v
	}
	return out, nil
}

func (f *fakeMarketEngine) Install(_ context.Context, e market.Entry) error {
	f.installs = append(f.installs, e.Name)
	if f.installErr != nil {
		return f.installErr
	}
	f.installed[e.Name] = market.Installed{Name: e.Name, Version: e.ParsedVersion(), VersionOK: true}
	return nil
}

func (f *fakeMarketEngine) Remove(name string) error {
	f.removes = append(f.removes, name)
	if f.removeErr != nil {
		return f.removeErr
	}
	delete(f.installed, name)
	return nil
}

// marketIndex parses a one-entry catalog for the page to consume.
func marketIndex(t *testing.T, entries string) market.Index {
	t.Helper()
	idx, diags, err := market.ParseIndex([]byte(`{"version": 1, "plugins": [` + entries + `]}`))
	if err != nil || len(diags) != 0 {
		t.Fatalf("ParseIndex: %v %v", err, diags)
	}
	return idx
}

const marketEntry = `{
	"name": "example",
	"version": "1.2.0",
	"description": "demo plugin",
	"capabilities": ["commands", "notify"],
	"artifact": {
		"url": "https://example.com/example.wasm",
		"sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	}
}`

// loadedPage returns a page with the catalog already delivered.
func loadedPage(t *testing.T, eng *fakeMarketEngine) *MarketplacePage {
	t.Helper()
	p := NewMarketplacePage(eng, nil)
	p.SetSubPanelHost(&stubHost{})
	p.Receive(MarketCatalogMsg{Index: marketIndex(t, marketEntry)})
	return p
}

func mktKey(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	default:
		r := []rune(s)[0]
		return tea.KeyPressMsg{Code: r, Text: s}
	}
}

func runCmd(t *testing.T, p *MarketplacePage, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("want a command")
	}
	p.Receive(cmd())
}

func TestMarketplaceInstallRequiresDetail(t *testing.T) {
	eng := &fakeMarketEngine{installed: map[string]market.Installed{}}
	p := loadedPage(t, eng)

	// i without the detail expanded: no install — the review step is mandatory.
	if cmd := p.Update(mktKey("i")); cmd != nil {
		t.Fatal("install fired without capability review")
	}
	p.Update(mktKey("enter")) // expand detail
	runCmd(t, p, p.Update(mktKey("i")))
	if len(eng.installs) != 1 || eng.installs[0] != "example" {
		t.Fatalf("installs = %v", eng.installs)
	}
	if !strings.Contains(p.View(120, 40), "installed") {
		t.Error("view does not show installed status")
	}
	if !strings.Contains(p.View(120, 40), "restart") {
		t.Error("view does not show restart notice after install")
	}
}

func TestMarketplaceDetailShowsCapabilities(t *testing.T) {
	p := loadedPage(t, &fakeMarketEngine{installed: map[string]market.Installed{}})
	p.Update(mktKey("enter"))
	v := p.View(120, 40)
	if !strings.Contains(v, "capabilities: commands, notify") {
		t.Errorf("detail lacks capability list:\n%s", v)
	}
	if !strings.Contains(v, "press i to install") {
		t.Errorf("detail lacks install hint:\n%s", v)
	}
}

func TestMarketplaceUpdateStatus(t *testing.T) {
	eng := &fakeMarketEngine{installed: map[string]market.Installed{
		"example": {Name: "example", Version: market.Version{Major: 1, Minor: 0, Patch: 0}, VersionOK: true},
	}}
	p := loadedPage(t, eng)
	if v := p.View(120, 40); !strings.Contains(v, "update 1.0.0 → 1.2.0") {
		t.Errorf("view lacks update status:\n%s", v)
	}
	p.Update(mktKey("enter"))
	runCmd(t, p, p.Update(mktKey("i")))
	if v := p.View(120, 40); !strings.Contains(v, "installed") {
		t.Errorf("view after update:\n%s", v)
	}
}

func TestMarketplaceRemove(t *testing.T) {
	eng := &fakeMarketEngine{installed: map[string]market.Installed{
		"example": {Name: "example", Version: market.Version{Major: 1, Minor: 2, Patch: 0}, VersionOK: true},
	}}
	p := loadedPage(t, eng)
	p.Update(mktKey("x"))
	runCmd(t, p, confirmVia(t, p.host.(*stubHost)))
	if len(eng.removes) != 1 {
		t.Fatalf("removes = %v", eng.removes)
	}
	if v := p.View(120, 40); !strings.Contains(v, "available") {
		t.Errorf("view after remove:\n%s", v)
	}
}

func TestMarketplaceRemoveNotInstalledNoop(t *testing.T) {
	eng := &fakeMarketEngine{installed: map[string]market.Installed{}}
	p := loadedPage(t, eng)
	if cmd := p.Update(mktKey("x")); cmd != nil {
		t.Fatal("remove fired for a plugin that is not installed")
	}
}

func TestMarketplaceActionErrorShownInline(t *testing.T) {
	eng := &fakeMarketEngine{
		installed:  map[string]market.Installed{},
		installErr: errors.New("checksum mismatch"),
	}
	p := loadedPage(t, eng)
	p.Update(mktKey("enter"))
	runCmd(t, p, p.Update(mktKey("i")))
	if v := p.View(120, 40); !strings.Contains(v, "checksum mismatch") {
		t.Errorf("view lacks inline error:\n%s", v)
	}
	if v := p.View(120, 40); strings.Contains(v, "restart") {
		t.Error("restart notice shown after failed install")
	}
}

func TestMarketplaceFetchErrorShown(t *testing.T) {
	p := NewMarketplacePage(&fakeMarketEngine{installed: map[string]market.Installed{}}, nil)
	p.Receive(MarketCatalogMsg{Err: errors.New("GET https://cat.example: 500")})
	if v := p.View(120, 40); !strings.Contains(v, "catalog: GET") {
		t.Errorf("view lacks fetch error:\n%s", v)
	}
}

func TestMarketplaceRefreshCmdOnce(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	calls := 0
	fetch := func(context.Context, string) (market.Index, []string, error) {
		calls++
		return market.Index{Version: 1}, nil, nil
	}
	p := NewMarketplacePage(&fakeMarketEngine{installed: map[string]market.Installed{}}, fetch)

	// Without a configured catalog URL RefreshCmd is a no-op.
	if cmd := p.RefreshCmd(); cmd != nil {
		t.Fatal("RefreshCmd fetched without a configured catalog")
	}
	if calls != 0 {
		t.Fatalf("calls = %d", calls)
	}
}
