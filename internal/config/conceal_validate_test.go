package config

import (
	"strings"
	"testing"
)

// editor.conceal_file_rules validation (#1704): an entry that is not a
// `family=pattern` rule over a known conceal family gates nothing, so it is
// reported. The list itself is left alone — the valid rules still apply.
func TestValidateConcealFileRules(t *testing.T) {
	c := defaults()
	c.Editor.ConcealFileRules = []string{
		"secret_masking=-*.log",
		"editor.cron_hints=*.yaml",
		"nope=-*.log",
		"secret_masking=",
		"*.log",
	}
	diags := validate(c)
	var bad []string
	for _, d := range diags {
		if d.Field == "editor.conceal_file_rules" {
			bad = append(bad, d.Message)
		}
	}
	if len(bad) != 3 {
		t.Fatalf("want 3 diagnostics, got %d (%v)", len(bad), bad)
	}
	for _, want := range []string{"nope=-*.log", "secret_masking=", "*.log"} {
		found := false
		for _, m := range bad {
			if strings.Contains(m, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("no diagnostic mentions %q: %v", want, bad)
		}
	}
	if len(c.Editor.ConcealFileRules) != 5 {
		t.Errorf("the rule list must survive validation, got %v", c.Editor.ConcealFileRules)
	}
}

// The conceal file settings default to empty, so an unconfigured IKE conceals
// everywhere it did before #1704.
func TestConcealFileDefaultsEmpty(t *testing.T) {
	c := defaults()
	if len(c.Editor.ConcealInclude) != 0 || len(c.Editor.ConcealExclude) != 0 || len(c.Editor.ConcealFileRules) != 0 {
		t.Errorf("conceal file filter must default to empty, got %v / %v / %v",
			c.Editor.ConcealInclude, c.Editor.ConcealExclude, c.Editor.ConcealFileRules)
	}
	if diags := validate(c); len(diags) != 0 {
		t.Errorf("the defaults must validate cleanly, got %v", diags)
	}
}
