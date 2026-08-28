package app

// marketupdates_test.go covers the gating of the startup plugin-update check
// (#2257): the setting, the configured catalog, the daily rate limit, and the
// silence on a failed fetch.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/market"
)

// checkModel wires a Model with a scripted catalog fetch over an empty
// plugins directory, and points the state file at a temp dir.
func checkModel(t *testing.T, autoCheck bool, fetch func() (market.Index, []string, error)) (Model, *int) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	prev := config.Get()
	t.Cleanup(func() { config.Set(prev) })
	cfg, err := config.Load(config.Options{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.Marketplace.CatalogURL = "https://catalog.example/index.json"
	cfg.Marketplace.AutoCheck = autoCheck
	config.Set(cfg)

	calls := 0
	m := Model{
		marketEngine: market.NewEngine(nil, filepath.Join(dir, "plugins")),
		marketFetch: func(context.Context, string) (market.Index, []string, error) {
			calls++
			return fetch()
		},
	}
	return m, &calls
}

// emptyCatalog is a valid, plugin-less index.
func emptyCatalog() (market.Index, []string, error) {
	return market.Index{Version: market.IndexVersion}, nil, nil
}

// TestMarketUpdateCheckRespectsSetting: auto_check off means no command at all.
func TestMarketUpdateCheckRespectsSetting(t *testing.T) {
	m, calls := checkModel(t, false, emptyCatalog)
	if cmd := m.marketUpdateCheckCmd(); cmd != nil {
		t.Fatal("check ran with marketplace.auto_check off")
	}
	if *calls != 0 {
		t.Fatalf("calls = %d", *calls)
	}
}

// TestMarketUpdateCheckRateLimited: a recent successful check suppresses the
// next one, and the successful check itself stamps the state file.
func TestMarketUpdateCheckRateLimited(t *testing.T) {
	m, calls := checkModel(t, true, emptyCatalog)
	cmd := m.marketUpdateCheckCmd()
	if cmd == nil {
		t.Fatal("first check produced no command")
	}
	if _, ok := cmd().(MarketUpdateCheckMsg); !ok {
		t.Fatal("check did not report a result")
	}
	if *calls != 1 {
		t.Fatalf("calls = %d", *calls)
	}
	if st := market.LoadCheckState(market.StatePath()); st.LastCheck.IsZero() {
		t.Fatal("successful check did not stamp the state file")
	}
	if cmd := m.marketUpdateCheckCmd(); cmd != nil {
		t.Fatal("second check ran inside the rate-limit window")
	}
	// A day later it is due again.
	if err := market.SaveCheckState(market.StatePath(),
		market.CheckState{LastCheck: time.Now().Add(-25 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if cmd := m.marketUpdateCheckCmd(); cmd == nil {
		t.Fatal("check did not run again after the interval elapsed")
	}
}

// TestMarketUpdateCheckSilentOnFailure: an unreachable catalog produces no
// message and leaves the rate limit untouched, so the next start retries.
func TestMarketUpdateCheckSilentOnFailure(t *testing.T) {
	m, _ := checkModel(t, true, func() (market.Index, []string, error) {
		return market.Index{}, nil, errors.New("dial tcp: no route to host")
	})
	cmd := m.marketUpdateCheckCmd()
	if cmd == nil {
		t.Fatal("check produced no command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("failed fetch reported %T", msg)
	}
	if _, err := os.Stat(market.StatePath()); !os.IsNotExist(err) {
		t.Fatalf("failed fetch stamped the state file (%v)", err)
	}
}

// TestMarketUpdateCheckFindsUpdates: an installed plugin older than the
// catalog comes back in the message the app turns into the notification.
func TestMarketUpdateCheckFindsUpdates(t *testing.T) {
	m, _ := checkModel(t, true, func() (market.Index, []string, error) {
		return market.ParseIndex([]byte(`{"version": 1, "plugins": [
			{"name": "alpha", "version": "2.0.0", "capabilities": ["commands"],
			 "artifact": {"url": "https://example.com/alpha.wasm",
			  "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
		]}`))
	})
	dir := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "plugins")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "alpha.wasm"), []byte("\x00asm"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name": "alpha", "version": "1.0.0", "capabilities": ["commands"]}`
	if err := os.WriteFile(filepath.Join(dir, "alpha.manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	var msg tea.Msg
	if cmd := m.marketUpdateCheckCmd(); cmd != nil {
		msg = cmd()
	}
	got, ok := msg.(MarketUpdateCheckMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	if len(got.Updates) != 1 || got.Updates[0].Name() != "alpha" {
		t.Fatalf("updates = %+v", got.Updates)
	}
}
