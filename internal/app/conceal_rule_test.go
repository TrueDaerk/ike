package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/concealexplain"
	"ike/internal/config"
	"ike/internal/editor"
	"ike/internal/numhint"
	"ike/internal/secret"
)

// conceal_rule_test.go covers the persistence half of the conceal explain
// popover (#1998): a rule made in the editor lands in the store the heuristics
// read, replaces the entry it corrects rather than queueing behind it, and is
// in force after the reload.

// TestApplyConcealRuleReplacesSamePattern (#1998): both stores resolve a key
// by first match, so a second rule for the same field has to overwrite the
// first — appending would leave the reclassification shadowed by the reading
// it was meant to fix.
func TestApplyConcealRuleReplacesSamePattern(t *testing.T) {
	list := []string{"*_bytes=bytes", "max_size=bytes"}
	next, changed := applyConcealRule(list, "max_size=timestamp-s", "max_size")
	if !changed {
		t.Fatal("rule reported no change")
	}
	if len(next) != 2 || next[1] != "max_size=timestamp-s" {
		t.Fatalf("list = %v", next)
	}
	if next[0] != "*_bytes=bytes" {
		t.Fatalf("unrelated entry lost: %v", next)
	}
	if _, changed := applyConcealRule(next, "max_size=timestamp-s", "max_size"); changed {
		t.Fatal("repeating a rule already in force must change nothing")
	}
	added, changed := applyConcealRule(next, "-PUBLIC_TOKEN", "public_token")
	if !changed || len(added) != 3 {
		t.Fatalf("new pattern not appended: %v", added)
	}
}

// TestConcealRuleWritesUnitsSetting (#1998): the popover's message persists
// into editor.number_hint_units, and the reload installs it — the field is
// classified by the rule from the next parse on.
func TestConcealRuleWritesUnitsSetting(t *testing.T) {
	m, userPath := concealRuleModel(t)
	numhint.SetFieldUnits(nil)
	if u, ok := numhint.FieldUnit("created_at"); ok {
		t.Fatalf("field pre-mapped to %v", u)
	}
	runConcealRule(t, m, editor.ConcealRuleMsg{
		Setting: concealexplain.UnitsSetting,
		Entry:   "created_at=timestamp-s",
		Pattern: "created_at",
		Note:    "number hints",
	})
	if got := readSetting(t, userPath); !strings.Contains(got, "created_at=timestamp-s") {
		t.Fatalf("settings file does not hold the rule:\n%s", got)
	}
	// The reload path installs the mapping; from there the field's reading is
	// the rule's, not the heuristics'.
	if !applyNumberHintUnits() {
		t.Fatal("reload did not install the new mapping")
	}
	u, ok := numhint.FieldUnit("created_at")
	if !ok || u.Kind != numhint.UnitTimestampSeconds {
		t.Fatalf("field unit = %v/%v after the rule", u, ok)
	}
	t.Cleanup(func() { numhint.SetFieldUnits(nil) })
}

// TestConcealRuleWritesSecretSetting (#1998): the masking rules land in
// editor.secret_masking_keys, and an exempting entry stops the built-in
// tables from masking the key.
func TestConcealRuleWritesSecretSetting(t *testing.T) {
	m, userPath := concealRuleModel(t)
	secret.SetKeyPatterns(nil)
	if !secret.Suspect("TOKEN") {
		t.Fatal("TOKEN is masked by the built-in tables; the test needs that")
	}
	runConcealRule(t, m, editor.ConcealRuleMsg{
		Setting: concealexplain.SecretSetting,
		Entry:   "-TOKEN",
		Pattern: "token",
		Note:    "secret masking",
	})
	if got := readSetting(t, userPath); !strings.Contains(got, "-TOKEN") {
		t.Fatalf("settings file does not hold the rule:\n%s", got)
	}
	if !applySecretMaskingKeys() {
		t.Fatal("reload did not install the new patterns")
	}
	if secret.Suspect("TOKEN") {
		t.Fatal("the exempting rule did not stop the built-in masking")
	}
	t.Cleanup(func() { secret.SetKeyPatterns(nil) })
}

// concealRuleModel builds a model whose config layer is a temp user file, with
// that config published so the rule writer reads and rewrites the live list.
func concealRuleModel(t *testing.T) (Model, string) {
	t.Helper()
	userPath := filepath.Join(t.TempDir(), "settings.toml")
	opts := config.Options{UserPath: userPath}
	cfg, _ := config.Load(opts)
	prev := config.Get()
	config.Set(cfg)
	t.Cleanup(func() { config.Set(prev) })
	m := newSized()
	m.cfgOpts = opts
	return m, userPath
}

// runConcealRule feeds the message through Update and runs the write command
// plus the reload it returns, the way the runtime would.
func runConcealRule(t *testing.T, m Model, msg editor.ConcealRuleMsg) {
	t.Helper()
	tm, cmd := m.Update(msg)
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("no write command for the rule")
	}
	reloaded, ok := reloadIn(cmd())
	if !ok {
		t.Fatalf("write produced %T, want a reload", cmd())
	}
	if reloaded.Config == nil {
		t.Fatal("reload carries no config")
	}
	config.Set(reloaded.Config)
}

// reloadIn digs the reload out of a message, which the app pass may have
// batched with its own housekeeping commands.
func reloadIn(msg tea.Msg) (config.ConfigReloadedMsg, bool) {
	switch v := msg.(type) {
	case config.ConfigReloadedMsg:
		return v, true
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			if r, ok := reloadIn(c()); ok {
				return r, true
			}
		}
	}
	return config.ConfigReloadedMsg{}, false
}

// readSetting returns the settings file's contents.
func readSetting(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
