package format

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ike/internal/config"
	iformat "ike/internal/format"
	"ike/internal/lang"
)

// fixture registers a throwaway language, resets the registry to just the
// config-override provider, and installs a config with the given [format.*]
// entry for it.
func fixture(t *testing.T, entry map[string]any) (langID, path string) {
	t.Helper()
	langID = "fmtlang"
	lang.Register(lang.Language{ID: langID, Extensions: []string{"fmtl"}})
	iformat.ResetForTest()
	iformat.Register(overrideProvider())
	t.Cleanup(iformat.ResetForTest)

	c, _ := config.Load(config.Options{})
	if c.Format == nil {
		c.Format = map[string]map[string]any{}
	}
	if entry != nil {
		c.Format[langID] = entry
	}
	config.Set(c)
	t.Cleanup(func() { fresh, _ := config.Load(config.Options{}); config.Set(fresh) })
	return langID, "x.fmtl"
}

// tool writes an executable script and returns its path.
func tool(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tool.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestOverrideBeatsPluginDefault: an explicit [format.<lang>] command wins
// over the plugin-registered external default (#1402 precedence).
func TestOverrideBeatsPluginDefault(t *testing.T) {
	override := tool(t, `printf 'override'`)
	fallback := tool(t, `printf 'default'`)
	langID, path := fixture(t, map[string]any{"command": override})
	iformat.RegisterExternalDefault(langID, iformat.External{Command: fallback})

	prov, ok := iformat.Resolve(langID, path)
	if !ok {
		t.Fatal("override must resolve")
	}
	res, err := prov.Format(context.Background(), iformat.Request{Path: path, Language: langID, Lines: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text == nil || *res.Text != "override" {
		t.Fatalf("config override must win, got %v", res.Text)
	}
	if got := prov.DisplayName(path); got != override {
		t.Fatalf("status name must be the override command, got %q", got)
	}
}

// TestOverrideEnabledFalseDisablesExternal: [format.<lang>] enabled = false
// switches off both the override and the plugin default.
func TestOverrideEnabledFalseDisablesExternal(t *testing.T) {
	langID, path := fixture(t, map[string]any{"command": tool(t, `printf 'x'`), "enabled": false})
	iformat.SetExternalEnabled(func(id string) bool {
		c := config.Get()
		if raw, ok := c.Format[id]; ok {
			if b, isBool := raw["enabled"].(bool); isBool {
				return b
			}
		}
		return true
	})
	t.Cleanup(func() { iformat.SetExternalEnabled(nil) })
	iformat.RegisterExternalDefault(langID, iformat.External{Command: tool(t, `printf 'y'`)})

	if _, ok := iformat.Resolve(langID, path); ok {
		t.Fatal("enabled=false must disable external formatting entirely")
	}
}

// TestOverrideRangeArgsEnableSelection: range_args opt into range support;
// without them the override has none and the registry reports whole-file
// only.
func TestOverrideRangeArgsEnableSelection(t *testing.T) {
	cmd := tool(t, `printf '%s-%s' "$1" "$2"`)
	langID, path := fixture(t, map[string]any{
		"command":    cmd,
		"range_args": []any{"${START_LINE}", "${END_LINE}"},
	})

	prov, ok, wholeOnly := iformat.ResolveRange(langID, path)
	if !ok || wholeOnly {
		t.Fatalf("range_args must enable ranges, ok=%v wholeOnly=%v", ok, wholeOnly)
	}
	res, err := prov.FormatRange(context.Background(), iformat.Request{Path: path, Language: langID, Lines: []string{"a", "b"}},
		iformat.Pos{Line: 0}, iformat.Pos{Line: 1, Col: 1})
	if err != nil {
		t.Fatal(err)
	}
	if *res.Text != "1-2" {
		t.Fatalf("got %q", *res.Text)
	}

	// Without range_args: available for whole-file, none for ranges.
	langID2, path2 := fixture(t, map[string]any{"command": cmd})
	_, ok, wholeOnly = iformat.ResolveRange(langID2, path2)
	if ok || !wholeOnly {
		t.Fatalf("no range_args must report whole-file only, ok=%v wholeOnly=%v", ok, wholeOnly)
	}
}

// TestOverrideMissingBinaryFallsThrough: a configured but uninstalled command
// never resolves (the registry falls to the next tier) and hints once.
func TestOverrideMissingBinaryFallsThrough(t *testing.T) {
	langID, path := fixture(t, map[string]any{"command": "not-a-real-formatter-binary"})
	var hints []string
	iformat.SetNotifier(func(text string) { hints = append(hints, text) })
	t.Cleanup(func() { iformat.SetNotifier(nil) })
	fallback := tool(t, `printf 'fb'`)
	iformat.RegisterExternalDefault(langID, iformat.External{Command: fallback})

	prov, ok := iformat.Resolve(langID, path)
	if !ok || prov.DisplayName(path) != fallback {
		t.Fatalf("missing override binary must fall through to the default, got %q ok=%v", prov.DisplayName(path), ok)
	}
	iformat.Resolve(langID, path)
	if len(hints) != 1 {
		t.Fatalf("want one hint, got %v", hints)
	}
}
