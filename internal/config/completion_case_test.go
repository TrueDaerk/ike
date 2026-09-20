package config

import "testing"

// completion_case_test.go covers completion.case_sensitivity (#2650): the
// hump filter's case rule is a defaulted, validated enum key.

func TestCompletionCaseSensitivityDefaultsAndValidates(t *testing.T) {
	c, _ := Load(Options{})
	if c.Completion.CaseSensitivity != "first_letter" {
		t.Errorf("case_sensitivity default = %q, want first_letter", c.Completion.CaseSensitivity)
	}
	if v, ok := c.Flat()["completion.case_sensitivity"]; !ok || v != "first_letter" {
		t.Errorf("Flat must expose completion.case_sensitivity, got %q,%v", v, ok)
	}
	for _, valid := range []string{"none", "first_letter", "all"} {
		proj := writeProject(t, "[completion]\ncase_sensitivity = \""+valid+"\"\n")
		c, diags := Load(Options{ProjectRoot: proj})
		if c.Completion.CaseSensitivity != valid || len(diags) != 0 {
			t.Errorf("%q must load unchanged, got %q %v", valid, c.Completion.CaseSensitivity, diags)
		}
	}
	proj := writeProject(t, "[completion]\ncase_sensitivity = \"sometimes\"\n")
	c, diags := Load(Options{ProjectRoot: proj})
	if c.Completion.CaseSensitivity != "first_letter" {
		t.Errorf("an unknown value should fall back to first_letter, got %q", c.Completion.CaseSensitivity)
	}
	if len(diags) != 1 || diags[0].Field != "completion.case_sensitivity" {
		t.Errorf("expected one diagnostic on the key, got %v", diags)
	}
}
