package app

import (
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/config"
	"ike/internal/diff"
)

// diffwhitespace_test.go covers the persistence half of the diff viewer's
// ignore-whitespace toggle (#2170): the pane flips itself and reports the new
// state, the root model writes it into the user settings, and the reload the
// write triggers carries the value every open diff then follows.

func TestDiffIgnoreWhitespacePersists(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "settings.toml")
	opts := config.Options{UserPath: userPath}
	cfg, _ := config.Load(opts)
	prev := config.Get()
	config.Set(cfg)
	t.Cleanup(func() { config.Set(prev) })
	m := newSized()
	m.cfgOpts = opts

	tm, cmd := m.Update(diff.IgnoreWhitespaceMsg{Key: "diff", On: true})
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("the toggle produced no write command")
	}
	reloaded, ok := reloadIn(cmd())
	if !ok {
		t.Fatalf("write produced %T, want a reload", cmd())
	}
	if got := readSetting(t, userPath); !strings.Contains(got, "ignore_whitespace") {
		t.Fatalf("settings file does not hold the preference:\n%s", got)
	}
	if reloaded.Config == nil || !reloaded.Config.Diff.IgnoreWhitespace {
		t.Fatal("the reloaded config does not carry the preference")
	}
	if v, ok := reloaded.Config.Flat()["diff.ignore_whitespace"]; !ok || v != "true" {
		t.Fatalf("flat key = %q/%v, want true", v, ok)
	}
}

// TestDiffPaneChromeShowsIgnoreWhitespace: the toggle is state a reader has to
// see — it decides which changes are listed at all — so the title band marks it.
func TestDiffPaneChromeShowsIgnoreWhitespace(t *testing.T) {
	m := newSized()
	reg := m.activeWS().Panes
	key := reg.AddDiff("/tmp/left.txt", "/tmp/right.txt")
	inst := reg.Get(key)
	if title := contentPaneTitle(inst); strings.Contains(title, "-w") {
		t.Fatalf("a plain diff should not advertise the toggle: %q", title)
	}
	inst.Diff().SetIgnoreWhitespace(true)
	if title := contentPaneTitle(inst); !strings.Contains(title, "[-w]") {
		t.Fatalf("title band = %q, want the -w marker", title)
	}
}
