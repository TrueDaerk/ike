package config

// forge_validate_test.go covers forge.poll_interval_seconds (#2085). The key
// is not a plain interval: 0 is a meaningful value (background polling off),
// so a too-small number is snapped up to the floor rather than clamped down,
// and a negative one reads as "off" rather than as "poll constantly".

import "testing"

func TestForgePollIntervalDefaultAndBounds(t *testing.T) {
	c, _ := Load(Options{})
	if c.Forge.PollIntervalSeconds != 20 {
		t.Errorf("default poll_interval_seconds should be 20, got %d", c.Forge.PollIntervalSeconds)
	}

	proj := writeProject(t, "[forge]\npoll_interval_seconds = 0\n")
	c, diags := Load(Options{ProjectRoot: proj})
	if c.Forge.PollIntervalSeconds != 0 {
		t.Errorf("0 must stay 0 (polling off), got %d", c.Forge.PollIntervalSeconds)
	}
	if len(diags) != 0 {
		t.Errorf("0 is a valid value and must not warn, got %v", diags)
	}

	proj = writeProject(t, "[forge]\npoll_interval_seconds = 3\n")
	c, diags = Load(Options{ProjectRoot: proj})
	if c.Forge.PollIntervalSeconds != ForgePollMinSeconds {
		t.Errorf("3 should be raised to %d, got %d", ForgePollMinSeconds, c.Forge.PollIntervalSeconds)
	}
	if len(diags) != 1 {
		t.Errorf("expected one floor diagnostic, got %v", diags)
	}

	proj = writeProject(t, "[forge]\npoll_interval_seconds = -5\n")
	c, diags = Load(Options{ProjectRoot: proj})
	if c.Forge.PollIntervalSeconds != 0 {
		t.Errorf("a negative interval should read as off, got %d", c.Forge.PollIntervalSeconds)
	}
	if len(diags) != 1 {
		t.Errorf("expected one diagnostic for the negative interval, got %v", diags)
	}

	proj = writeProject(t, "[forge]\npoll_interval_seconds = 99999\n")
	c, diags = Load(Options{ProjectRoot: proj})
	if c.Forge.PollIntervalSeconds != ForgePollMaxSeconds {
		t.Errorf("99999 should clamp to %d, got %d", ForgePollMaxSeconds, c.Forge.PollIntervalSeconds)
	}
	if len(diags) != 1 {
		t.Errorf("expected one ceiling diagnostic, got %v", diags)
	}

	proj = writeProject(t, "[forge]\npoll_interval_seconds = 45\n")
	c, diags = Load(Options{ProjectRoot: proj})
	if c.Forge.PollIntervalSeconds != 45 {
		t.Errorf("an in-range interval must stick, got %d", c.Forge.PollIntervalSeconds)
	}
	if len(diags) != 0 {
		t.Errorf("an in-range interval must not warn, got %v", diags)
	}
	if got := c.Flat()["forge.poll_interval_seconds"]; got != "45" {
		t.Errorf("flat view = %q, want \"45\"", got)
	}
}
