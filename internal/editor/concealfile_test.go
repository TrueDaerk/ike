package editor

import (
	"testing"

	"ike/internal/epochtime"
	"ike/internal/host"
	"ike/internal/secret"
)

// concealfile_test.go covers the per-file conceal gate (#1704) at the editor
// seam: that the filter composes with the family toggles, that a per-view
// toggle still wins, and that an edited pattern list reaches an already-open
// buffer.

// TestConcealExcludeGatesStandIns: a global exclude switches every stand-in
// family off in the matching file and leaves the others alone.
func TestConcealExcludeGatesStandIns(t *testing.T) {
	m, _ := loadedAs(t, "app.env", "TOKEN=abc\n")
	m.Configure(host.MapConfig{
		"editor.secret_masking":     "true",
		"editor.timestamp_decoding": "true",
		"editor.conceal_exclude":    "*.env",
	})
	if m.decodeOn(secret.Capture) {
		t.Error("an excluded file must not mask secrets")
	}
	if m.decodeOn(epochtime.Capture) {
		t.Error("the global exclude covers every family")
	}
	// The toggle itself is untouched — the file is what was excluded.
	if !m.secretMask {
		t.Error("the filter must not clobber the family toggle")
	}

	other, _ := loadedAs(t, "app.yaml", "created: 1722945600\n")
	other.Configure(host.MapConfig{
		"editor.secret_masking":     "true",
		"editor.timestamp_decoding": "true",
		"editor.conceal_exclude":    "*.env",
	})
	if !other.decodeOn(epochtime.Capture) {
		t.Error("a file the exclude misses keeps concealing")
	}
}

// TestConcealIncludeRestricts: with an include list, only matching files
// conceal.
func TestConcealIncludeRestricts(t *testing.T) {
	cfg := host.MapConfig{
		"editor.timestamp_decoding": "true",
		"editor.conceal_include":    "*.json",
	}
	in, _ := loadedAs(t, "a.json", "{\"t\": 1722945600}\n")
	in.Configure(cfg)
	if !in.decodeOn(epochtime.Capture) {
		t.Error("an included file must conceal")
	}
	out, _ := loadedAs(t, "a.yaml", "t: 1722945600\n")
	out.Configure(cfg)
	if out.decodeOn(epochtime.Capture) {
		t.Error("a file outside the include list must not conceal")
	}
}

// TestConcealFamilyRuleOverridesGlobal: a per-family rule decides its family
// on its own, and only its family (#1704's second acceptance criterion).
func TestConcealFamilyRuleOverridesGlobal(t *testing.T) {
	m, _ := loadedAs(t, "fixture.env", "TOKEN=abc\nAT=1722945600\n")
	m.Configure(host.MapConfig{
		"editor.secret_masking":     "true",
		"editor.timestamp_decoding": "true",
		"editor.conceal_file_rules": "secret_masking=-*.env",
	})
	if m.decodeOn(secret.Capture) {
		t.Error("the family exclude must switch masking off")
	}
	if !m.decodeOn(epochtime.Capture) {
		t.Error("a family rule must not leak into another family")
	}
}

// TestConcealViewToggleBeatsFilter: an explicit per-view toggle bypasses the
// filter — a pattern states a default, not a prohibition.
func TestConcealViewToggleBeatsFilter(t *testing.T) {
	m, _ := loadedAs(t, "app.env", "TOKEN=abc\n")
	m.Configure(host.MapConfig{
		"editor.secret_masking":  "true",
		"editor.conceal_exclude": "*.env",
	})
	if m.decodeOn(secret.Capture) {
		t.Fatal("precondition: the exclude must gate masking off")
	}
	// Toggle twice: back to on, but now marked as an explicit view override.
	m.toggleSecretMasking()
	m.toggleSecretMasking()
	if !m.decodeOn(secret.Capture) {
		t.Error("an explicit view toggle must win over the file filter")
	}
	// And it keeps winning across a config refresh.
	m.Configure(host.MapConfig{
		"editor.secret_masking":  "true",
		"editor.conceal_exclude": "*.env",
	})
	if !m.decodeOn(secret.Capture) {
		t.Error("the view override must survive applyConfig")
	}
}

// TestConcealFilterAppliesLive: editing the patterns reaches an already-open
// buffer, with no reload (#1704's fourth acceptance criterion).
func TestConcealFilterAppliesLive(t *testing.T) {
	m, _ := loadedAs(t, "app.env", "TOKEN=abc\n")
	m.Configure(host.MapConfig{"editor.secret_masking": "true"})
	if !m.decodeOn(secret.Capture) {
		t.Fatal("precondition: masking is on by default")
	}
	m.Configure(host.MapConfig{
		"editor.secret_masking":  "true",
		"editor.conceal_exclude": "*.env",
	})
	if m.decodeOn(secret.Capture) {
		t.Error("a new exclude must take effect without a reload")
	}
	m.Configure(host.MapConfig{
		"editor.secret_masking":  "true",
		"editor.conceal_exclude": "*.log",
	})
	if !m.decodeOn(secret.Capture) {
		t.Error("dropping the exclude must restore masking without a reload")
	}
}

// TestConcealRenderingLayersGated: the four layers read directly by their
// renderers (Markdown, CSV, log, PEM) go through the same gate.
func TestConcealRenderingLayersGated(t *testing.T) {
	m, _ := loadedAs(t, "notes.md", "**bold**\n")
	m.Configure(host.MapConfig{
		"editor.markdown_rendering": "true",
		"editor.csv_rendering":      "true",
		"editor.log_rendering":      "true",
		"editor.pem_summary":        "true",
		"editor.conceal_exclude":    "*.md",
	})
	if m.mdRenderOn() || m.svRenderOn() || m.logRenderOn() || m.pemSummaryOn() {
		t.Error("a global exclude gates every rendering layer, not just Markdown")
	}
}

// TestConcealRenderingLayersPerFamily: with per-family rules the layers gate
// independently of one another.
func TestConcealRenderingLayersPerFamily(t *testing.T) {
	m, _ := loadedAs(t, "notes.md", "**bold**\n")
	m.Configure(host.MapConfig{
		"editor.markdown_rendering": "true",
		"editor.csv_rendering":      "true",
		"editor.log_rendering":      "true",
		"editor.pem_summary":        "true",
		"editor.conceal_file_rules": "markdown_rendering=-*.md",
	})
	if m.mdRenderOn() {
		t.Error("the family exclude must switch Markdown rendering off")
	}
	if !m.svRenderOn() || !m.logRenderOn() || !m.pemSummaryOn() {
		t.Error("a per-family rule must not gate the other layers")
	}
	// The Markdown toggle bypasses it like every other view override.
	m.toggleMarkdownRendering()
	m.toggleMarkdownRendering() // back on, but now marked as a view override
	if !m.mdRenderOn() {
		t.Error("an explicit view toggle must win over the file filter")
	}
}

// TestConcealUntitledBufferAllowed: a buffer with no path has no name to
// match, so an include list never silences it.
func TestConcealUntitledBufferAllowed(t *testing.T) {
	m := New()
	m.SetSize(80, 20)
	m.Configure(host.MapConfig{
		"editor.secret_masking":  "true",
		"editor.conceal_include": "*.json",
	})
	if !m.decodeOn(secret.Capture) {
		t.Error("an untitled buffer must keep concealing")
	}
}

// TestConcealRulesMemo: the compiled filter is rebuilt only when the settings
// change, since applyConfig runs on every routed message.
func TestConcealRulesMemo(t *testing.T) {
	m, _ := loadedAs(t, "app.env", "TOKEN=abc\n")
	m.Configure(host.MapConfig{"editor.conceal_exclude": "*.env"})
	if m.concealRules.Empty() {
		t.Fatal("the exclude must compile into a non-empty filter")
	}
	before := m.concealRaw
	m.applyConfig()
	if m.concealRaw != before {
		t.Error("an unchanged config must not recompile the filter")
	}
	m.Configure(host.MapConfig{})
	if !m.concealRules.Empty() {
		t.Error("clearing the settings must clear the filter")
	}
}
