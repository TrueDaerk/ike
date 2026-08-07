package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/format"
)

// format_page_test.go covers the Formatters settings page: the read-only row
// rendering (#1402) and the override editor it grew in #1662 — the per-row
// toggles, the form's write-what-differs save, the reset action and the write
// layer selector.

// formatPageFixture registers a plugin default plus a built-in formatter and
// binds the page to a throwaway user + project config.
func formatPageFixture(t *testing.T) (*FormatPage, config.Options) {
	t.Helper()
	restoreConfig(t)
	format.ResetForTest()
	t.Cleanup(format.ResetForTest)
	format.RegisterExternalDefault("fmtlang", format.External{
		Command: "definitely-missing-tool",
		Args:    []string{"format", "-"},
		Install: "pip install definitely-missing-tool",
	})
	format.RegisterBuiltin("fmtsql", format.ConfigKey{
		Key: "keywords", Values: []string{"upper", "lower", "preserve"}, Default: "upper", Help: "keyword casing",
	})

	opts := config.Options{
		UserPath:    filepath.Join(t.TempDir(), "settings.toml"),
		ProjectRoot: t.TempDir(),
	}
	c, _ := config.Load(opts)
	config.Set(c)

	p := NewFormatPage(opts)
	p.SetSubPanelHost(&stubHost{})
	selectFormatRow(t, p, "fmtlang")
	return p, opts
}

// selectFormatRow moves the selection onto a language row.
func selectFormatRow(t *testing.T, p *FormatPage, lang string) {
	t.Helper()
	for i, row := range p.rows() {
		if row.lang == lang {
			p.sel = i
			return
		}
	}
	t.Fatalf("no row for %q", lang)
}

// drainFormat executes a command tree, feeding config reloads into Set.
func drainFormat(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a write-reload command")
	}
	switch m := cmd().(type) {
	case config.ConfigReloadedMsg:
		config.Set(m.Config)
	case tea.BatchMsg:
		for _, c := range m {
			drainFormat(t, c)
		}
	default:
		t.Fatalf("unexpected message %#v", m)
	}
}

// formatForm opens the selected row's editor and returns it.
func openFormatForm(t *testing.T, p *FormatPage) *formatForm {
	t.Helper()
	p.Update(key("enter"))
	f, ok := p.host.(*stubHost).top().(*formatForm)
	if !ok {
		t.Fatal("enter must push the override form")
	}
	return f
}

// setField types a value into the form field named key.
func setField(t *testing.T, f *formatForm, key, value string) {
	t.Helper()
	for i, field := range f.fields {
		if field.key == key {
			f.values[i] = value
			return
		}
	}
	t.Fatalf("no %q field in the form", key)
}

// TestFormatPageListsEffectiveFormatters (#1402): the page shows each
// language's effective command, the supplying layer and the binary state.
func TestFormatPageListsEffectiveFormatters(t *testing.T) {
	p, _ := formatPageFixture(t)
	c := config.Get()
	if c.Format == nil {
		c.Format = map[string]map[string]any{}
	}
	c.Format["sql"] = map[string]any{"command": "sh", "args": []any{"-c", "cat"}}
	config.Set(c)

	out := p.View(120, 20)
	if !strings.Contains(out, "fmtlang") || !strings.Contains(out, "definitely-missing-tool format -") {
		t.Fatalf("default row missing:\n%s", out)
	}
	if !strings.Contains(out, "missing") {
		t.Fatalf("missing binary must be flagged:\n%s", out)
	}
	if !strings.Contains(out, "sql") || !strings.Contains(out, "found") {
		t.Fatalf("configured sh override must show as found:\n%s", out)
	}
	if !strings.Contains(out, "@plugin default") {
		t.Fatalf("layer attribution missing:\n%s", out)
	}
}

// TestFormatPageShowsBuiltinLanguages (#1662): a language whose only
// formatter is a built-in still gets a row, with its switch state.
func TestFormatPageShowsBuiltinLanguages(t *testing.T) {
	p, _ := formatPageFixture(t)
	out := p.View(120, 20)
	if !strings.Contains(out, "fmtsql") || !strings.Contains(out, "built-in on") {
		t.Fatalf("built-in language row missing:\n%s", out)
	}
}

// TestFormatPageTogglesEnabledAndBuiltin (#1662): e and b write the switches
// of the selected language into the project layer.
func TestFormatPageTogglesEnabledAndBuiltin(t *testing.T) {
	p, opts := formatPageFixture(t)
	drainFormat(t, p.Update(key("e")))
	if got := config.Get().Format["fmtlang"]["enabled"]; got != false {
		t.Fatalf("enabled = %v, want false", got)
	}
	if got := config.Origin(opts, "format.fmtlang.enabled"); got != "project" {
		t.Fatalf("write layer = %q, want project", got)
	}
	if out := p.View(120, 20); !strings.Contains(out, "disabled") {
		t.Fatalf("a disabled language must render as disabled:\n%s", out)
	}
	drainFormat(t, p.Update(key("e")))
	if got := config.Get().Format["fmtlang"]["enabled"]; got != true {
		t.Fatalf("enabled = %v, want true after the second toggle", got)
	}

	// b needs a built-in formatter; on a language without one it says so.
	if cmd := p.Update(key("b")); cmd != nil || p.notice == "" {
		t.Fatalf("b without a built-in must explain itself, notice = %q", p.notice)
	}
	selectFormatRow(t, p, "fmtsql")
	drainFormat(t, p.Update(key("b")))
	if got := config.Get().Format["fmtsql"]["builtin"]; got != false {
		t.Fatalf("builtin = %v, want false", got)
	}
	if out := p.View(120, 20); !strings.Contains(out, "built-in off") {
		t.Fatalf("the built-in switch must render:\n%s", out)
	}
}

// TestFormatFormWritesOnlyChangedKeys (#1662): the form starts on the
// effective values and persists exactly what differs from the plugin default.
func TestFormatFormWritesOnlyChangedKeys(t *testing.T) {
	p, opts := formatPageFixture(t)
	f := openFormatForm(t, p)
	if got := f.values[0]; got != "definitely-missing-tool" {
		t.Fatalf("command field = %q, want the plugin default", got)
	}
	setField(t, f, "command", "sh")
	setField(t, f, "args", "-c cat")
	setField(t, f, "range_args", "-c cat")
	setField(t, f, "temp_file", "true")
	drainFormat(t, f.save())

	raw := config.Get().Format["fmtlang"]
	if raw["command"] != "sh" {
		t.Fatalf("command = %v, want sh", raw["command"])
	}
	if got := format.ExternalFromConfig(raw); strings.Join(got.Args, " ") != "-c cat" || !got.TempFile {
		t.Fatalf("override = %+v, want args [-c cat] and temp_file", got)
	}
	if _, written := raw["install"]; written {
		t.Fatalf("a field left at the plugin default must not be written: %v", raw)
	}
	if got := config.Origin(opts, "format.fmtlang.command"); got != "project" {
		t.Fatalf("write layer = %q, want project", got)
	}
	if out := p.View(120, 20); !strings.Contains(out, "sh -c cat") || !strings.Contains(out, "@project") {
		t.Fatalf("view must show the override and its layer:\n%s", out)
	}

	// Clearing a field removes its key again — back to the plugin default.
	f = openFormatForm(t, p)
	setField(t, f, "command", "")
	setField(t, f, "args", "")
	setField(t, f, "range_args", "")
	setField(t, f, "temp_file", "")
	drainFormat(t, f.save())
	if raw := config.Get().Format["fmtlang"]; len(raw) != 0 {
		t.Fatalf("cleared fields must drop their keys, got %v", raw)
	}
}

// TestFormatFormValidates (#1662): typed values are checked before anything
// reaches disk, and the form stays open.
func TestFormatFormValidates(t *testing.T) {
	p, _ := formatPageFixture(t)
	f := openFormatForm(t, p)
	setField(t, f, "temp_file", "yes")
	if cmd := f.save(); cmd != nil {
		t.Fatal("an invalid bool must not be written")
	}
	if f.note == "" || p.host.(*stubHost).top() != f {
		t.Fatalf("the form must stay open with a note, note = %q", f.note)
	}

	selectFormatRow(t, p, "fmtsql")
	f = openFormatForm(t, p)
	setField(t, f, "keywords", "UPPER")
	if cmd := f.save(); cmd != nil {
		t.Fatal("a value outside the declared set must not be written")
	}
	if !strings.Contains(f.note, "upper, lower, preserve") {
		t.Fatalf("note must name the accepted values, got %q", f.note)
	}
	setField(t, f, "keywords", "lower")
	drainFormat(t, f.save())
	if got := config.Get().Format["fmtsql"]["keywords"]; got != "lower" {
		t.Fatalf("keywords = %v, want lower", got)
	}
}

// TestFormatPageResetRemovesOverride (#1662): r drops every key of the
// language, in both layers.
func TestFormatPageResetRemovesOverride(t *testing.T) {
	p, opts := formatPageFixture(t)
	drainFormat(t, p.Update(key("e"))) // project layer: enabled = false
	p.scope = config.UserScope
	f := openFormatForm(t, p)
	setField(t, f, "command", "sh")
	drainFormat(t, f.save())
	if got := config.Origin(opts, "format.fmtlang.command"); got != "user" {
		t.Fatalf("write layer = %q, want user", got)
	}

	row, _ := p.current()
	drainFormat(t, p.reset(row))
	if raw := config.Get().Format["fmtlang"]; len(raw) != 0 {
		t.Fatalf("reset must clear the override, got %v", raw)
	}
	for _, key := range []string{"format.fmtlang.command", "format.fmtlang.enabled"} {
		if got := config.Origin(opts, key); got != "default" {
			t.Fatalf("%s still set in the %s layer", key, got)
		}
	}
	if out := p.View(120, 20); !strings.Contains(out, "@plugin default") {
		t.Fatalf("the row must fall back to the plugin default:\n%s", out)
	}
}

// TestFormatPageWriteScopeSelector (#1662): s cycles the layer writes land
// in, and says so in the header.
func TestFormatPageWriteScopeSelector(t *testing.T) {
	p, opts := formatPageFixture(t)
	if !strings.Contains(p.View(120, 20), "write layer: project") {
		t.Fatal("format.* writes default to the project layer")
	}
	p.Update(key("s"))
	if !strings.Contains(p.View(120, 20), "write layer: user") {
		t.Fatal("s must cycle to the user layer")
	}
	drainFormat(t, p.Update(key("e")))
	if got := config.Origin(opts, "format.fmtlang.enabled"); got != "user" {
		t.Fatalf("write layer = %q, want user", got)
	}

	// Without a project there is nothing to cycle to.
	solo := NewFormatPage(config.Options{UserPath: opts.UserPath})
	if solo.scope != config.UserScope {
		t.Fatal("a project-less page must write to the user layer")
	}
	solo.Update(key("s"))
	if solo.scope != config.UserScope || solo.notice == "" {
		t.Fatalf("cycling without a project layer must explain itself, notice = %q", solo.notice)
	}
}

// TestFormatFormCompletesCommandPath (#1662): tab completes the command
// field through the shared path engine.
func TestFormatFormCompletesCommandPath(t *testing.T) {
	p, _ := formatPageFixture(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "unique-formatter"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := openFormatForm(t, p)
	setField(t, f, "command", filepath.Join(dir, "uni"))
	f.cur = len([]rune(f.values[0]))
	f.Update(key("tab"))
	if want := filepath.Join(dir, "unique-formatter"); f.values[0] != want {
		t.Fatalf("tab completed to %q, want %q", f.values[0], want)
	}
}

// TestFormatPageClickOpensForm (#1662): a press on the selected row opens the
// editor, a press elsewhere selects.
func TestFormatPageClickOpensForm(t *testing.T) {
	p, _ := formatPageFixture(t)
	p.View(120, 20) // record the list height
	rows := p.rows()
	other := 0
	if p.sel == 0 && len(rows) > 1 {
		other = 1
	}
	p.Click(0, other+formatHeadLines)
	if p.sel != other {
		t.Fatalf("a press must select row %d, sel = %d", other, p.sel)
	}
	p.Click(0, other+formatHeadLines)
	if _, ok := p.host.(*stubHost).top().(*formatForm); !ok {
		t.Fatal("a press on the selection must open the form")
	}
}
